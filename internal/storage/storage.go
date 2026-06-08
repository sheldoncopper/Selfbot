// Package storage is the persistence layer. It owns a single SQLite file
// (pure-Go driver, no CGO) and exposes typed CRUD helpers plus the
// gotd-compatible session storage. The database is intentionally small: a
// handful of users control one Telegram account.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gotd/td/session"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Field identifiers (rows in the fields table).
const (
	FieldFirstName = "first_name"
	FieldLastName  = "last_name"
	FieldAbout     = "about"
)

// AllFields lists the managed profile fields in display order.
var AllFields = []string{FieldFirstName, FieldLastName, FieldAbout}

// Field is one managed profile field and its rendering rules.
type Field struct {
	ID             string
	Enabled        bool
	Template       string
	Font           string
	MinIntervalSec int
	LastValue      string
	LastPushedAt   int64
}

// Variable is a named value injected into field templates.
type Variable struct {
	Name           string
	Type           string // "custom" or a predefined key (see variables package)
	Config         string // JSON, type-specific
	IntervalSec    int
	Font           string
	LastValue      string
	LastComputedAt int64
	Cursor         int
	CreatedAt      int64
}

// User is a Telegram user permitted to drive the bot, with UI state.
type User struct {
	ID        int64
	Lang      string
	State     string
	StateData string
}

// Store wraps the database handle.
type Store struct {
	db *sql.DB
	mu sync.Mutex // serializes writes (SQLite single-writer)
}

// Open creates (if needed) and opens the SQLite database, runs migrations and
// seeds default rows.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	// _pragma options keep SQLite robust under a long-running daemon.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single connection avoids "database is locked"
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.seed(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS fields (
    id               TEXT PRIMARY KEY,
    enabled          INTEGER NOT NULL DEFAULT 0,
    template         TEXT NOT NULL DEFAULT '',
    font             TEXT NOT NULL DEFAULT '',
    min_interval_sec INTEGER NOT NULL DEFAULT 30,
    last_value       TEXT NOT NULL DEFAULT '',
    last_pushed_at   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS variables (
    name             TEXT PRIMARY KEY,
    type             TEXT NOT NULL,
    config           TEXT NOT NULL DEFAULT '{}',
    interval_sec     INTEGER NOT NULL DEFAULT 60,
    font             TEXT NOT NULL DEFAULT '',
    last_value       TEXT NOT NULL DEFAULT '',
    last_computed_at INTEGER NOT NULL DEFAULT 0,
    cursor           INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY,
    lang       TEXT NOT NULL DEFAULT 'fa',
    state      TEXT NOT NULL DEFAULT '',
    state_data TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS session (
    id   INTEGER PRIMARY KEY CHECK (id = 1),
    data BLOB
);
`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) seed() error {
	for _, id := range AllFields {
		def := 30
		if id == FieldAbout {
			def = 60
		}
		if _, err := s.db.Exec(
			`INSERT OR IGNORE INTO fields (id, enabled, template, min_interval_sec) VALUES (?, 0, '', ?)`,
			id, def,
		); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Settings (key/value)
// ---------------------------------------------------------------------------

// GetSetting returns the value for key, or def if unset.
func (s *Store) GetSetting(key, def string) string {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
         ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// ---------------------------------------------------------------------------
// Fields
// ---------------------------------------------------------------------------

// GetField returns one field by id.
func (s *Store) GetField(id string) (*Field, error) {
	f := &Field{}
	err := s.db.QueryRow(
		`SELECT id, enabled, template, font, min_interval_sec, last_value, last_pushed_at
         FROM fields WHERE id = ?`, id,
	).Scan(&f.ID, &f.Enabled, &f.Template, &f.Font, &f.MinIntervalSec, &f.LastValue, &f.LastPushedAt)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ListFields returns every field in display order.
func (s *Store) ListFields() ([]*Field, error) {
	out := make([]*Field, 0, len(AllFields))
	for _, id := range AllFields {
		f, err := s.GetField(id)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// UpdateField persists template/font/enabled/interval for a field.
func (s *Store) UpdateField(f *Field) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`UPDATE fields SET enabled=?, template=?, font=?, min_interval_sec=? WHERE id=?`,
		f.Enabled, f.Template, f.Font, f.MinIntervalSec, f.ID,
	)
	return err
}

// SetFieldPushed records the last rendered value and push time.
func (s *Store) SetFieldPushed(id, value string, at int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE fields SET last_value=?, last_pushed_at=? WHERE id=?`, value, at, id)
	return err
}

// CountEnabledFields returns how many fields are enabled.
func (s *Store) CountEnabledFields() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM fields WHERE enabled = 1`).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Variables
// ---------------------------------------------------------------------------

// GetVariable returns a variable by name.
func (s *Store) GetVariable(name string) (*Variable, error) {
	v := &Variable{}
	err := s.db.QueryRow(
		`SELECT name, type, config, interval_sec, font, last_value, last_computed_at, cursor, created_at
         FROM variables WHERE name = ?`, name,
	).Scan(&v.Name, &v.Type, &v.Config, &v.IntervalSec, &v.Font, &v.LastValue, &v.LastComputedAt, &v.Cursor, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// ListVariables returns all variables ordered by creation time.
func (s *Store) ListVariables() ([]*Variable, error) {
	rows, err := s.db.Query(
		`SELECT name, type, config, interval_sec, font, last_value, last_computed_at, cursor, created_at
         FROM variables ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Variable
	for rows.Next() {
		v := &Variable{}
		if err := rows.Scan(&v.Name, &v.Type, &v.Config, &v.IntervalSec, &v.Font,
			&v.LastValue, &v.LastComputedAt, &v.Cursor, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreateVariable inserts a new variable.
func (s *Store) CreateVariable(v *Variable) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.CreatedAt == 0 {
		v.CreatedAt = time.Now().Unix()
	}
	_, err := s.db.Exec(
		`INSERT INTO variables (name, type, config, interval_sec, font, last_value, last_computed_at, cursor, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Name, v.Type, v.Config, v.IntervalSec, v.Font, v.LastValue, v.LastComputedAt, v.Cursor, v.CreatedAt,
	)
	return err
}

// UpdateVariable persists config/interval/font of a variable.
func (s *Store) UpdateVariable(v *Variable) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`UPDATE variables SET type=?, config=?, interval_sec=?, font=? WHERE name=?`,
		v.Type, v.Config, v.IntervalSec, v.Font, v.Name,
	)
	return err
}

// SetVariableComputed records the last computed value, cursor and time.
func (s *Store) SetVariableComputed(name, value string, cursor int, at int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`UPDATE variables SET last_value=?, cursor=?, last_computed_at=? WHERE name=?`,
		value, cursor, at, name,
	)
	return err
}

// DeleteVariable removes a variable by name.
func (s *Store) DeleteVariable(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM variables WHERE name = ?`, name)
	return err
}

// VariableExists reports whether a name is taken.
func (s *Store) VariableExists(name string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM variables WHERE name = ?`, name).Scan(&n)
	return n > 0, err
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

// GetOrCreateUser fetches a user, creating it with the default language if new.
func (s *Store) GetOrCreateUser(id int64, defLang string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(`SELECT id, lang, state, state_data FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Lang, &u.State, &u.StateData)
	if errors.Is(err, sql.ErrNoRows) {
		s.mu.Lock()
		_, ierr := s.db.Exec(`INSERT INTO users (id, lang) VALUES (?, ?)`, id, defLang)
		s.mu.Unlock()
		if ierr != nil {
			return nil, ierr
		}
		return &User{ID: id, Lang: defLang}, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// SetUserLang updates a user's language.
func (s *Store) SetUserLang(id int64, lang string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE users SET lang=? WHERE id=?`, lang, id)
	return err
}

// SetUserState updates a user's conversation state and payload.
func (s *Store) SetUserState(id int64, state, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE users SET state=?, state_data=? WHERE id=?`, state, data, id)
	return err
}

// ---------------------------------------------------------------------------
// Session storage (implements gotd session.Storage)
// ---------------------------------------------------------------------------

// ErrSessionNotFound mirrors gotd's session.ErrNotFound semantics.
var ErrSessionNotFound = session.ErrNotFound

// SessionStorage is a SQLite-backed gotd session store (single account, id=1).
type SessionStorage struct{ s *Store }

// Session returns a gotd-compatible session storage bound to this store.
func (s *Store) Session() *SessionStorage { return &SessionStorage{s: s} }

// LoadSession returns the stored session bytes or ErrSessionNotFound.
func (ss *SessionStorage) LoadSession(_ context.Context) ([]byte, error) {
	var data []byte
	err := ss.s.db.QueryRow(`SELECT data FROM session WHERE id = 1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) || len(data) == 0 {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// StoreSession persists session bytes.
func (ss *SessionStorage) StoreSession(_ context.Context, data []byte) error {
	ss.s.mu.Lock()
	defer ss.s.mu.Unlock()
	_, err := ss.s.db.Exec(
		`INSERT INTO session (id, data) VALUES (1, ?)
         ON CONFLICT(id) DO UPDATE SET data = excluded.data`, data)
	return err
}

// HasSession reports whether a session is stored.
func (s *Store) HasSession() bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM session WHERE id = 1 AND data IS NOT NULL`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// ClearSession deletes the stored session (used on logout).
func (s *Store) ClearSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM session WHERE id = 1`)
	return err
}
