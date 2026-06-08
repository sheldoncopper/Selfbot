package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStorageRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	// Seeded fields exist.
	fields, err := st.ListFields()
	if err != nil || len(fields) != 3 {
		t.Fatalf("seed fields: %v len=%d", err, len(fields))
	}

	// Field update round-trip.
	f, _ := st.GetField(FieldFirstName)
	f.Enabled = true
	f.Template = "{clock} hi"
	f.Font = "bold"
	if err := st.UpdateField(f); err != nil {
		t.Fatal(err)
	}
	f2, _ := st.GetField(FieldFirstName)
	if !f2.Enabled || f2.Template != "{clock} hi" || f2.Font != "bold" {
		t.Fatalf("field not persisted: %+v", f2)
	}

	// Variable CRUD.
	v := &Variable{Name: "clock", Type: "time", Config: `{"tz":"UTC"}`, IntervalSec: 60}
	if err := st.CreateVariable(v); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.VariableExists("clock"); !ok {
		t.Fatal("variable should exist")
	}
	if err := st.SetVariableComputed("clock", "12:00", 3, 1700000000); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetVariable("clock")
	if got.LastValue != "12:00" || got.Cursor != 3 {
		t.Fatalf("computed not persisted: %+v", got)
	}
	if err := st.DeleteVariable("clock"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.VariableExists("clock"); ok {
		t.Fatal("variable should be deleted")
	}

	// Settings.
	if err := st.SetSetting("k", "v"); err != nil {
		t.Fatal(err)
	}
	if st.GetSetting("k", "def") != "v" {
		t.Fatal("setting not persisted")
	}
	if st.GetSetting("missing", "def") != "def" {
		t.Fatal("default not returned")
	}

	// Session storage round-trip + sentinel error.
	ss := st.Session()
	if _, err := ss.LoadSession(context.Background()); err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
	if err := ss.StoreSession(context.Background(), []byte("blob")); err != nil {
		t.Fatal(err)
	}
	if !st.HasSession() {
		t.Fatal("HasSession should be true")
	}
	data, err := ss.LoadSession(context.Background())
	if err != nil || string(data) != "blob" {
		t.Fatalf("load session = %q %v", data, err)
	}
	if err := st.ClearSession(); err != nil {
		t.Fatal(err)
	}
	if st.HasSession() {
		t.Fatal("HasSession should be false after clear")
	}

	// User state.
	u, err := st.GetOrCreateUser(42, "fa")
	if err != nil || u.Lang != "fa" {
		t.Fatalf("create user: %v %+v", err, u)
	}
	if err := st.SetUserState(42, "s1", "data1"); err != nil {
		t.Fatal(err)
	}
	u2, _ := st.GetOrCreateUser(42, "en")
	if u2.State != "s1" || u2.StateData != "data1" || u2.Lang != "fa" {
		t.Fatalf("user state not persisted: %+v", u2)
	}
}
