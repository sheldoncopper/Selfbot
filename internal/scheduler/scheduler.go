// Package scheduler periodically evaluates variables and pushes the rendered
// profile fields to Telegram. Each variable refreshes on its own interval and
// each field has its own minimum push gap, so two time-based variables in the
// same field can legitimately tick at different rates.
package scheduler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/selfbot/selfbot/internal/storage"
	"github.com/selfbot/selfbot/internal/userbot"
	"github.com/selfbot/selfbot/internal/variables"
)

// Settings keys.
const (
	keyLiveEnabled = "live_enabled"
	keyTickSec     = "tick_interval"
)

// Scheduler drives the live-update loop.
type Scheduler struct {
	st  *storage.Store
	mgr *userbot.Manager
	log *zap.Logger
}

// New constructs a Scheduler.
func New(st *storage.Store, mgr *userbot.Manager, log *zap.Logger) *Scheduler {
	return &Scheduler{st: st, mgr: mgr, log: log}
}

// LiveEnabled reports whether automatic updates are turned on.
func (s *Scheduler) LiveEnabled() bool {
	return s.st.GetSetting(keyLiveEnabled, "0") == "1"
}

// SetLive turns automatic updates on/off.
func (s *Scheduler) SetLive(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return s.st.SetSetting(keyLiveEnabled, v)
}

// TickSeconds returns the global evaluation interval (min 5s).
func (s *Scheduler) TickSeconds() int {
	n, _ := strconv.Atoi(s.st.GetSetting(keyTickSec, "15"))
	if n < 5 {
		n = 5
	}
	return n
}

// SetTickSeconds sets the global evaluation interval.
func (s *Scheduler) SetTickSeconds(n int) error {
	if n < 5 {
		n = 5
	}
	return s.st.SetSetting(keyTickSec, strconv.Itoa(n))
}

// Run blocks, evaluating the schedule until ctx is canceled. The ticker period
// adapts to the configured tick interval.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		d := time.Duration(s.TickSeconds()) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
		if !s.LiveEnabled() || !s.mgr.IsOnline() {
			continue
		}
		if _, err := s.runOnce(ctx, false); err != nil {
			s.log.Warn("scheduled update failed", zap.Error(err))
		}
	}
}

// UpdateNow forces an immediate evaluation and push regardless of per-field
// gaps (used by the "update now" button). It still respects the global gap in
// the manager. Returns whether anything was pushed.
func (s *Scheduler) UpdateNow(ctx context.Context) (bool, error) {
	return s.runOnce(ctx, true)
}

// runOnce performs one evaluation cycle. When force is true, per-variable and
// per-field timing checks are bypassed.
func (s *Scheduler) runOnce(ctx context.Context, force bool) (bool, error) {
	now := time.Now()
	nu := now.Unix()

	vars, err := s.st.ListVariables()
	if err != nil {
		return false, err
	}
	byName := make(map[string]*storage.Variable, len(vars))
	for _, v := range vars {
		byName[v.Name] = v
		if force || nu-v.LastComputedAt >= int64(v.IntervalSec) {
			val, cur := variables.ResolveValue(v, now)
			v.LastValue = val
			v.Cursor = cur
			v.LastComputedAt = nu
			if err := s.st.SetVariableComputed(v.Name, val, cur, nu); err != nil {
				s.log.Warn("persist variable failed", zap.String("name", v.Name), zap.Error(err))
			}
		}
	}

	fields, err := s.st.ListFields()
	if err != nil {
		return false, err
	}

	// Collect fields ready to push into a single updateProfile call.
	pending := map[string]string{}
	for _, f := range fields {
		if !f.Enabled {
			continue
		}
		rendered := variables.RenderField(f, byName)
		// Telegram rejects an empty first name; never push a blank one.
		if f.ID == storage.FieldFirstName && strings.TrimSpace(rendered) == "" {
			continue
		}
		if !force {
			if rendered == f.LastValue {
				continue
			}
			if nu-f.LastPushedAt < int64(f.MinIntervalSec) {
				continue
			}
		}
		pending[f.ID] = rendered
	}
	if len(pending) == 0 {
		return false, nil
	}

	var first, last, about *string
	if v, ok := pending[storage.FieldFirstName]; ok {
		fv := v
		first = &fv
	}
	if v, ok := pending[storage.FieldLastName]; ok {
		lv := v
		last = &lv
	}
	if v, ok := pending[storage.FieldAbout]; ok {
		av := v
		about = &av
	}

	if err := s.mgr.UpdateProfile(ctx, first, last, about); err != nil {
		return false, err
	}
	for id, val := range pending {
		if err := s.st.SetFieldPushed(id, val, nu); err != nil {
			s.log.Warn("persist field push failed", zap.String("id", id), zap.Error(err))
		}
	}
	s.log.Info("profile updated", zap.Int("fields", len(pending)))
	return true, nil
}
