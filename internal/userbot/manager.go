// Package userbot owns the MTProto user-account side of the application: it
// logs into Telegram with gotd/td using a stored session, keeps the connection
// alive, and pushes profile (first name / last name / bio) updates safely.
package userbot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/selfbot/selfbot/internal/config"
	"github.com/selfbot/selfbot/internal/storage"
)

// Telegram profile length limits (non-premium). We clamp to stay safe.
const (
	maxNameLen  = 64
	maxAboutLen = 70
)

// ErrNotConnected is returned when an operation needs a live account session.
var ErrNotConnected = errors.New("telegram account is not connected")

// Manager supervises a single Telegram user-account connection.
type Manager struct {
	cfg *config.Config
	st  *storage.Store
	log *zap.Logger

	baseCtx context.Context

	mu       sync.Mutex
	client   *telegram.Client
	api      *tg.Client
	self     *tg.User
	online   bool
	cancel   context.CancelFunc
	runDone  chan struct{}
	lastPush time.Time
}

// New constructs a Manager.
func New(cfg *config.Config, st *storage.Store, log *zap.Logger) *Manager {
	return &Manager{cfg: cfg, st: st, log: log}
}

// Start records the base context and, if a session exists on disk, begins
// auto-connecting in the background. It returns immediately.
func (m *Manager) Start(ctx context.Context) {
	m.baseCtx = ctx
	if m.st.HasSession() {
		m.log.Info("existing session found, auto-connecting")
		go m.runClient(nil)
	}
}

// IsOnline reports whether the account is currently connected and authorized.
func (m *Manager) IsOnline() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online
}

// Self returns a snapshot of the connected account (or nil).
func (m *Manager) Self() *tg.User {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.self
}

// newClient builds a gotd client with a realistic device fingerprint and the
// flood-wait + rate-limit middlewares that keep us well-behaved (anti-ban).
func (m *Manager) newClient() *telegram.Client {
	waiter := floodwait.NewSimpleWaiter().WithMaxRetries(5).WithMaxWait(60 * time.Second)
	limiter := ratelimit.New(rate.Every(100*time.Millisecond), 5)

	return telegram.NewClient(m.cfg.APIID, m.cfg.APIHash, telegram.Options{
		Logger:         m.log.Named("gotd"),
		SessionStorage: m.st.Session(),
		Device: telegram.DeviceConfig{
			DeviceModel:    m.cfg.DeviceModel,
			SystemVersion:  m.cfg.SystemVersion,
			AppVersion:     m.cfg.AppVersion,
			SystemLangCode: m.cfg.LangCode,
			LangCode:       m.cfg.LangCode,
		},
		Middlewares:   []telegram.Middleware{waiter, limiter},
		RetryInterval: 5 * time.Second,
		MaxRetries:    10,
	})
}

// runClient owns one connection lifecycle. If login is non-nil it performs the
// interactive auth flow; otherwise it expects a valid stored session. It blocks
// until the connection ends (context cancel or fatal error).
func (m *Manager) runClient(login *LoginSession) {
	client := m.newClient()
	runCtx, cancel := context.WithCancel(m.baseCtx)
	done := make(chan struct{})

	m.mu.Lock()
	// Tear down any previous run before taking over.
	if m.cancel != nil {
		m.cancel()
	}
	m.client = client
	m.cancel = cancel
	m.runDone = done
	m.mu.Unlock()

	err := client.Run(runCtx, func(ctx context.Context) error {
		if login != nil {
			flow := auth.NewFlow(interactiveAuth{s: login}, auth.SendCodeOptions{})
			if err := client.Auth().IfNecessary(ctx, flow); err != nil {
				return err
			}
		} else {
			status, err := client.Auth().Status(ctx)
			if err != nil {
				return err
			}
			if !status.Authorized {
				return errors.New("stored session is not authorized")
			}
		}

		self, err := client.Self(ctx)
		if err != nil {
			return err
		}

		m.setOnline(client.API(), self)
		m.log.Info("telegram account connected", zap.String("user", self.Username), zap.Int64("id", self.ID))
		if login != nil && login.NotifyDone != nil {
			login.NotifyDone(&Self{User: self})
		}

		<-ctx.Done() // keep the connection alive for profile updates
		return ctx.Err()
	})

	m.setOffline(done)

	if err != nil && !errors.Is(err, context.Canceled) {
		m.log.Warn("telegram connection ended", zap.Error(err))
		if login != nil && login.NotifyError != nil {
			login.NotifyError(err)
		}
	}
}

func (m *Manager) setOnline(api *tg.Client, self *tg.User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.api = api
	m.self = self
	m.online = true
}

func (m *Manager) setOffline(done chan struct{}) {
	m.mu.Lock()
	if m.runDone == done {
		m.online = false
		m.api = nil
		m.self = nil
		m.cancel = nil
		m.runDone = nil
	}
	m.mu.Unlock()
	close(done)
}

// StartLogin begins a fresh interactive login for the given phone number. Any
// existing connection/session is discarded first. The returned LoginSession is
// driven by the bot layer (SubmitCode / SubmitPassword + Notify* callbacks).
func (m *Manager) StartLogin(phone string) (*LoginSession, error) {
	if m.baseCtx == nil {
		return nil, errors.New("manager not started")
	}
	m.disconnect()
	if err := m.st.ClearSession(); err != nil {
		return nil, err
	}
	ls := newLoginSession(phone)
	go m.runClient(ls)
	return ls, nil
}

// disconnect cancels the current run and waits for it to finish.
func (m *Manager) disconnect() {
	m.mu.Lock()
	cancel := m.cancel
	done := m.runDone
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}
}

// Logout signs out from Telegram (invalidating the session server-side) and
// clears local state.
func (m *Manager) Logout(ctx context.Context) error {
	m.mu.Lock()
	api := m.api
	online := m.online
	m.mu.Unlock()

	if online && api != nil {
		if _, err := api.AuthLogOut(ctx); err != nil {
			m.log.Warn("auth.logOut failed (continuing)", zap.Error(err))
		}
	}
	m.disconnect()
	return m.st.ClearSession()
}

// UpdateProfile pushes the given non-nil fields to Telegram, enforcing a global
// minimum gap between calls and clamping to Telegram's length limits.
func (m *Manager) UpdateProfile(ctx context.Context, first, last, about *string) error {
	m.mu.Lock()
	api := m.api
	online := m.online
	gap := time.Duration(m.cfg.MinUpdateGapSec) * time.Second
	since := time.Since(m.lastPush)
	m.mu.Unlock()

	if !online || api == nil {
		return ErrNotConnected
	}
	if since < gap {
		time.Sleep(gap - since)
	}

	req := &tg.AccountUpdateProfileRequest{}
	if first != nil {
		req.SetFirstName(clamp(*first, maxNameLen))
	}
	if last != nil {
		req.SetLastName(clamp(*last, maxNameLen))
	}
	if about != nil {
		req.SetAbout(clamp(*about, maxAboutLen))
	}
	if first == nil && last == nil && about == nil {
		return nil
	}

	if _, err := api.AccountUpdateProfile(ctx, req); err != nil {
		return fmt.Errorf("account.updateProfile: %w", err)
	}

	m.mu.Lock()
	m.lastPush = time.Now()
	m.mu.Unlock()
	return nil
}

// CurrentProfile returns the live first name, last name and bio from Telegram.
func (m *Manager) CurrentProfile(ctx context.Context) (first, last, about string, err error) {
	m.mu.Lock()
	api := m.api
	self := m.self
	online := m.online
	m.mu.Unlock()
	if !online || api == nil || self == nil {
		return "", "", "", ErrNotConnected
	}
	first = self.FirstName
	last = self.LastName

	full, err := api.UsersGetFullUser(ctx, &tg.InputUserSelf{})
	if err == nil {
		about = full.FullUser.About
	}
	return first, last, about, nil
}

// clamp truncates s to at most n runes (rune-safe).
func clamp(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
