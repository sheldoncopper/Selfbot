package botui

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/selfbot/selfbot/internal/i18n"
	"github.com/selfbot/selfbot/internal/storage"
	"github.com/selfbot/selfbot/internal/userbot"
	"github.com/selfbot/selfbot/internal/variables"
)

// Conversation states.
const (
	stateLoginPhone    = "login_phone"
	stateLoginCode     = "login_code"
	stateLoginPassword = "login_password"
	stateLiveTick      = "live_tick"
	stateImportWait    = "import_wait"
	stateVarName       = "var_name"

	// Prefix states carry an identifier after the prefix.
	stateFieldTemplate = "field_tpl:"
	stateFieldInterval = "field_int:"
	stateVarInterval   = "var_int:"

	// Variable-creation step states.
	stateCreateText  = "create:text"
	stateCreateItems = "create:items"
	stateCreateStart = "create:start"
	stateCreateStep  = "create:step"
	stateCreateCB    = "create:cb"
)

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,32}$`)

// varDraft is the in-progress variable being created or edited. It is persisted
// as JSON in the user's state_data between steps.
type varDraft struct {
	Name     string         `json:"name"`
	OrigName string         `json:"orig"`
	Type     string         `json:"type"`
	Config   map[string]any `json:"config"`
	Interval int            `json:"interval"`
	Font     string         `json:"font"`
	Step     int            `json:"step"`
	Editing  bool           `json:"editing"`
}

func kindOf(typ string) variables.ConfigKind {
	if typ == variables.CustomType {
		return variables.KindText
	}
	if s, ok := variables.SpecByKey(typ); ok {
		return s.Kind
	}
	return variables.KindNone
}

var kindSteps = map[variables.ConfigKind][]string{
	variables.KindNone:    {},
	variables.KindText:    {"text"},
	variables.KindTZ:      {"tz"},
	variables.KindTime:    {"tz", "layout"},
	variables.KindDate:    {"cal", "tz"},
	variables.KindList:    {"items"},
	variables.KindCounter: {"start", "step"},
}

// ---------------------------------------------------------------------------
// Message handling
// ---------------------------------------------------------------------------

func (bt *Bot) handleMessage(ctx context.Context, u *storage.User, msg *models.Message) {
	l := bt.lang(u)
	text := strings.TrimSpace(msg.Text)

	switch text {
	case "/start", "/menu":
		_ = bt.st.SetUserState(u.ID, "", "")
		bt.showMain(ctx, l, msg.Chat.ID, 0)
		return
	case "/cancel":
		_ = bt.st.SetUserState(u.ID, "", "")
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "common.canceled"), removeKB())
		bt.showMain(ctx, l, msg.Chat.ID, 0)
		return
	}

	state := u.State
	switch {
	case state == stateLoginPhone:
		bt.onLoginPhone(ctx, u, msg)
	case state == stateLoginCode:
		bt.onLoginCode(ctx, u, msg, text)
	case state == stateLoginPassword:
		bt.onLoginPassword(ctx, u, msg, msg.Text)
	case state == stateLiveTick:
		bt.onIntInput(ctx, u, text, func(n int) {
			_ = bt.sch.SetTickSeconds(n)
			bt.send(ctx, msg.Chat.ID, i18n.T(l, "common.saved"), nil)
			bt.showMain(ctx, l, msg.Chat.ID, 0)
		})
	case state == stateImportWait:
		bt.onImportDocument(ctx, u, msg)
	case state == stateVarName:
		bt.onVarName(ctx, u, msg, text)
	case strings.HasPrefix(state, stateFieldTemplate):
		bt.onFieldTemplate(ctx, u, msg, strings.TrimPrefix(state, stateFieldTemplate))
	case strings.HasPrefix(state, stateFieldInterval):
		id := strings.TrimPrefix(state, stateFieldInterval)
		bt.onIntInput(ctx, u, text, func(n int) {
			f, err := bt.st.GetField(id)
			if err == nil {
				f.MinIntervalSec = n
				_ = bt.st.UpdateField(f)
			}
			_ = bt.st.SetUserState(u.ID, "", "")
			bt.send(ctx, msg.Chat.ID, i18n.T(l, "common.saved"), nil)
			bt.showFieldItem(ctx, l, msg.Chat.ID, 0, id)
		})
	case strings.HasPrefix(state, stateVarInterval):
		name := strings.TrimPrefix(state, stateVarInterval)
		bt.onIntInput(ctx, u, text, func(n int) {
			if v, err := bt.st.GetVariable(name); err == nil {
				v.IntervalSec = n
				_ = bt.st.UpdateVariable(v)
			}
			_ = bt.st.SetUserState(u.ID, "", "")
			bt.send(ctx, msg.Chat.ID, i18n.T(l, "common.saved"), nil)
			bt.showVarItem(ctx, l, msg.Chat.ID, 0, name)
		})
	case state == stateCreateText, state == stateCreateItems, state == stateCreateStart, state == stateCreateStep:
		bt.onCreateTextStep(ctx, u, msg, state, text)
	case state == stateCreateCB:
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "common.unknown"), nil)
	default:
		bt.showMain(ctx, l, msg.Chat.ID, 0)
	}
}

// ---------------------------------------------------------------------------
// Login flow
// ---------------------------------------------------------------------------

var phoneRe = regexp.MustCompile(`[0-9]+`)

func (bt *Bot) onLoginPhone(ctx context.Context, u *storage.User, msg *models.Message) {
	l := bt.lang(u)
	var phone string
	if msg.Contact != nil {
		phone = msg.Contact.PhoneNumber
	} else {
		phone = strings.Join(phoneRe.FindAllString(msg.Text, -1), "")
	}
	if len(phone) < 6 {
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "acc.bad_phone"), contactKB(l))
		return
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}

	ls, err := bt.mgr.StartLogin(phone)
	if err != nil {
		_ = bt.st.SetUserState(u.ID, "", "")
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "acc.login_fail", err.Error()), removeKB())
		return
	}

	chatID := msg.Chat.ID
	uid := u.ID
	bgLang := l
	ls.NotifyCode = func() {
		_ = bt.st.SetUserState(uid, stateLoginCode, "")
		bt.send(context.Background(), chatID, i18n.T(bgLang, "acc.ask_code"), removeKB())
	}
	ls.NotifyPassword = func() {
		_ = bt.st.SetUserState(uid, stateLoginPassword, "")
		bt.send(context.Background(), chatID, i18n.T(bgLang, "acc.ask_password"), removeKB())
	}
	ls.NotifyDone = func(self *userbot.Self) {
		_ = bt.st.SetUserState(uid, "", "")
		bt.clearLogin(uid)
		bt.send(context.Background(), chatID, i18n.T(bgLang, "acc.login_ok", self.Display()), removeKB())
		bt.showAccount(context.Background(), bgLang, chatID, 0)
	}
	ls.NotifyError = func(err error) {
		_ = bt.st.SetUserState(uid, "", "")
		bt.clearLogin(uid)
		bt.send(context.Background(), chatID, i18n.T(bgLang, "acc.login_fail", err.Error()), removeKB())
	}
	bt.storeLogin(uid, ls)
	bt.send(ctx, msg.Chat.ID, i18n.T(l, "acc.sending_code"), removeKB())
}

func (bt *Bot) onLoginCode(ctx context.Context, u *storage.User, msg *models.Message, text string) {
	l := bt.lang(u)
	code := strings.Join(phoneRe.FindAllString(text, -1), "")
	if code == "" {
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "acc.bad_code"), nil)
		return
	}
	if ls := bt.getLogin(u.ID); ls != nil {
		ls.SubmitCode(code)
	}
}

func (bt *Bot) onLoginPassword(ctx context.Context, u *storage.User, msg *models.Message, pw string) {
	if ls := bt.getLogin(u.ID); ls != nil {
		ls.SubmitPassword(pw)
	}
}

// login session registry helpers
func (bt *Bot) storeLogin(uid int64, ls *userbot.LoginSession) {
	bt.loginMu.Lock()
	bt.logins[uid] = ls
	bt.loginMu.Unlock()
}

func (bt *Bot) getLogin(uid int64) *userbot.LoginSession {
	bt.loginMu.Lock()
	defer bt.loginMu.Unlock()
	return bt.logins[uid]
}

func (bt *Bot) clearLogin(uid int64) {
	bt.loginMu.Lock()
	delete(bt.logins, uid)
	bt.loginMu.Unlock()
}

// ---------------------------------------------------------------------------
// Field template input
// ---------------------------------------------------------------------------

func (bt *Bot) onFieldTemplate(ctx context.Context, u *storage.User, msg *models.Message, id string) {
	l := bt.lang(u)
	if !validField(id) {
		_ = bt.st.SetUserState(u.ID, "", "")
		return
	}
	f, err := bt.st.GetField(id)
	if err != nil {
		return
	}
	f.Template = msg.Text
	_ = bt.st.UpdateField(f)
	_ = bt.st.SetUserState(u.ID, "", "")
	bt.send(ctx, msg.Chat.ID, i18n.T(l, "common.saved"), nil)
	bt.showFieldItem(ctx, l, msg.Chat.ID, 0, id)
}

// ---------------------------------------------------------------------------
// Integer helper
// ---------------------------------------------------------------------------

func (bt *Bot) onIntInput(ctx context.Context, u *storage.User, text string, ok func(n int)) {
	l := bt.lang(u)
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < 5 {
		bt.send(ctx, u.ID, i18n.T(l, "interval.bad"), nil)
		return
	}
	ok(n)
}

// ---------------------------------------------------------------------------
// Variable creation / editing
// ---------------------------------------------------------------------------

func (bt *Bot) startVarCreation(ctx context.Context, u *storage.User, parts []string, chatID int64) {
	l := bt.lang(u)
	if len(parts) < 2 {
		return
	}
	typ := parts[1]
	draft := varDraft{Type: typ, Config: map[string]any{}, Interval: 60}
	if typ != variables.CustomType {
		spec, ok := variables.SpecByKey(typ)
		if !ok {
			return
		}
		draft.Interval = spec.DefaultInterval
		draft.Config = cloneConfig(spec.DefaultConfig)
	}
	bt.saveDraft(u.ID, stateVarName, draft)
	bt.send(ctx, chatID, i18n.T(l, "vars.ask_name"), nil)
}

func (bt *Bot) onVarName(ctx context.Context, u *storage.User, msg *models.Message, text string) {
	l := bt.lang(u)
	draft, ok := bt.loadDraft(u)
	if !ok {
		bt.showMain(ctx, l, msg.Chat.ID, 0)
		return
	}
	name := strings.TrimSpace(text)
	if !nameRe.MatchString(name) {
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "vars.bad_name"), nil)
		return
	}
	if exists, _ := bt.st.VariableExists(name); exists {
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "vars.name_taken"), nil)
		return
	}
	draft.Name = name
	draft.OrigName = name
	bt.advance(ctx, u, draft, msg.Chat.ID, 0)
}

func (bt *Bot) startVarConfigEdit(ctx context.Context, u *storage.User, name string, chatID int64) {
	v, err := bt.st.GetVariable(name)
	if err != nil {
		return
	}
	draft := varDraft{
		Name:     v.Name,
		OrigName: v.Name,
		Type:     v.Type,
		Config:   variables.DecodeConfig(v.Config),
		Interval: v.IntervalSec,
		Font:     v.Font,
		Editing:  true,
	}
	bt.advance(ctx, u, draft, chatID, 0)
}

// advance runs the next configuration step for the draft, or finalizes it.
func (bt *Bot) advance(ctx context.Context, u *storage.User, draft varDraft, chatID int64, msgID int) {
	l := bt.lang(u)
	steps := kindSteps[kindOf(draft.Type)]
	if draft.Step >= len(steps) {
		bt.finalizeVar(ctx, u, draft, chatID, msgID)
		return
	}
	step := steps[draft.Step]
	switch step {
	case "text":
		bt.saveDraft(u.ID, stateCreateText, draft)
		bt.send(ctx, chatID, i18n.T(l, "vars.ask_custom_text"), nil)
	case "items":
		bt.saveDraft(u.ID, stateCreateItems, draft)
		bt.send(ctx, chatID, i18n.T(l, "vars.ask_emojis"), nil)
	case "start":
		bt.saveDraft(u.ID, stateCreateStart, draft)
		bt.send(ctx, chatID, i18n.T(l, "vars.ask_start"), nil)
	case "step":
		bt.saveDraft(u.ID, stateCreateStep, draft)
		bt.send(ctx, chatID, i18n.T(l, "vars.ask_step"), nil)
	case "tz":
		bt.saveDraft(u.ID, stateCreateCB, draft)
		bt.showPicker(ctx, chatID, msgID, i18n.T(l, "vars.ask_tz"), tzKB(l, "cfgtz:"))
	case "layout":
		bt.saveDraft(u.ID, stateCreateCB, draft)
		bt.showPicker(ctx, chatID, msgID, i18n.T(l, "vars.time_fmt"), layoutKB(l, "cfglay:"))
	case "cal":
		bt.saveDraft(u.ID, stateCreateCB, draft)
		bt.showPicker(ctx, chatID, msgID, i18n.T(l, "vars.date_cal"), calKB(l, "cfgcal:"))
	}
}

func (bt *Bot) showPicker(ctx context.Context, chatID int64, msgID int, text string, markup *models.InlineKeyboardMarkup) {
	if msgID == 0 {
		bt.send(ctx, chatID, text, markup)
		return
	}
	bt.edit(ctx, chatID, msgID, text, markup)
}

// onCreateTextStep handles the text-input creation steps (text/items/start/step).
func (bt *Bot) onCreateTextStep(ctx context.Context, u *storage.User, msg *models.Message, state, text string) {
	l := bt.lang(u)
	draft, ok := bt.loadDraft(u)
	if !ok {
		bt.showMain(ctx, l, msg.Chat.ID, 0)
		return
	}
	switch state {
	case stateCreateText:
		draft.Config["text"] = msg.Text
	case stateCreateItems:
		items := splitItems(msg.Text)
		if len(items) > 0 {
			draft.Config["items"] = items
		}
	case stateCreateStart:
		n, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			bt.send(ctx, msg.Chat.ID, i18n.T(l, "int.bad"), nil)
			return
		}
		draft.Config["start"] = n
	case stateCreateStep:
		n, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			bt.send(ctx, msg.Chat.ID, i18n.T(l, "int.bad"), nil)
			return
		}
		draft.Config["step"] = n
	}
	draft.Step++
	bt.advance(ctx, u, draft, msg.Chat.ID, 0)
}

// handleConfigSelect handles tz/layout/cal button selections during creation.
func (bt *Bot) handleConfigSelect(ctx context.Context, u *storage.User, head string, parts []string, chatID int64, msgID int) {
	l := bt.lang(u)
	draft, ok := bt.loadDraft(u)
	if !ok {
		bt.showMain(ctx, l, chatID, msgID)
		return
	}
	switch head {
	case "cfgtz":
		draft.Config["tz"] = strings.Join(parts[1:], ":") // IANA names contain no ':' but be safe
	case "cfglay":
		idx, _ := strconv.Atoi(parts[1])
		if idx >= 0 && idx < len(variables.TimeLayouts) {
			draft.Config["layout"] = variables.TimeLayouts[idx].Layout
		}
	case "cfgcal":
		draft.Config["cal"] = parts[1]
	}
	draft.Step++
	bt.advance(ctx, u, draft, chatID, msgID)
}

// finalizeVar persists the draft (create or update) and shows the result.
func (bt *Bot) finalizeVar(ctx context.Context, u *storage.User, draft varDraft, chatID int64, msgID int) {
	l := bt.lang(u)
	_ = bt.st.SetUserState(u.ID, "", "")

	if draft.Editing {
		v, err := bt.st.GetVariable(draft.OrigName)
		if err == nil {
			v.Config = variables.EncodeConfig(draft.Config)
			_ = bt.st.UpdateVariable(v)
			// Reset cached state so the new config takes effect immediately.
			_ = bt.st.SetVariableComputed(v.Name, "", 0, 0)
		}
		bt.showVarItem(ctx, l, chatID, msgID, draft.OrigName)
		return
	}

	v := &storage.Variable{
		Name:        draft.Name,
		Type:        draft.Type,
		Config:      variables.EncodeConfig(draft.Config),
		IntervalSec: draft.Interval,
		Font:        draft.Font,
	}
	if err := bt.st.CreateVariable(v); err != nil {
		bt.send(ctx, chatID, i18n.T(l, "err.generic", err.Error()), nil)
		return
	}
	bt.send(ctx, chatID, i18n.T(l, "vars.created", draft.Name), nil)
	bt.showVarItem(ctx, l, chatID, 0, draft.Name)
}

// ---------------------------------------------------------------------------
// Draft persistence helpers
// ---------------------------------------------------------------------------

func (bt *Bot) saveDraft(uid int64, state string, draft varDraft) {
	b, _ := json.Marshal(draft)
	_ = bt.st.SetUserState(uid, state, string(b))
}

func (bt *Bot) loadDraft(u *storage.User) (varDraft, bool) {
	var d varDraft
	if strings.TrimSpace(u.StateData) == "" {
		return d, false
	}
	if err := json.Unmarshal([]byte(u.StateData), &d); err != nil {
		return d, false
	}
	if d.Config == nil {
		d.Config = map[string]any{}
	}
	return d, true
}

func cloneConfig(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// splitItems splits a user-provided value set on whitespace/newlines, ignoring
// a lone "-" which means "keep defaults".
func splitItems(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
	var out []string
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
