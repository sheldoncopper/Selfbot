package botui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/selfbot/selfbot/internal/i18n"
	"github.com/selfbot/selfbot/internal/storage"
	"github.com/selfbot/selfbot/internal/variables"
)

// handleCallback routes an inline-button press.
func (bt *Bot) handleCallback(ctx context.Context, u *storage.User, cq *models.CallbackQuery) {
	bt.answer(ctx, cq, "", false)

	var chatID int64
	var msgID int
	if cq.Message.Message != nil {
		chatID = cq.Message.Message.Chat.ID
		msgID = cq.Message.Message.ID
	} else {
		chatID = u.ID
	}
	l := bt.lang(u)
	data := cq.Data
	parts := strings.Split(data, ":")
	head := parts[0]

	switch head {
	case "home":
		_ = bt.st.SetUserState(u.ID, "", "")
		bt.showMain(ctx, l, chatID, msgID)
	case "acc":
		bt.handleAccountCB(ctx, u, cq, parts, chatID, msgID)
	case "fields":
		bt.showFields(ctx, l, chatID, msgID)
	case "fld":
		bt.handleFieldCB(ctx, u, cq, parts, chatID, msgID)
	case "fldfont":
		bt.handleFieldFont(ctx, u, parts, chatID, msgID)
	case "fldprev":
		bt.handleFieldPreview(ctx, u, cq, parts)
	case "vars":
		if len(parts) > 1 && parts[1] == "add" {
			bt.edit(ctx, chatID, msgID, i18n.T(l, "vars.choose_type"), typeChooserKB(l))
		} else {
			bt.showVars(ctx, l, chatID, msgID)
		}
	case "vtype":
		bt.startVarCreation(ctx, u, parts, chatID)
	case "cfgtz", "cfglay", "cfgcal":
		bt.handleConfigSelect(ctx, u, head, parts, chatID, msgID)
	case "var":
		bt.handleVarCB(ctx, u, cq, parts, chatID, msgID)
	case "vfont":
		bt.handleVarFont(ctx, u, parts, chatID, msgID)
	case "live":
		bt.handleLiveCB(ctx, u, cq, parts, chatID, msgID)
	case "port":
		bt.handlePortCB(ctx, u, parts, chatID, msgID)
	case "lang":
		if len(parts) > 1 {
			_ = bt.st.SetUserLang(u.ID, parts[1])
			u.Lang = parts[1]
			bt.showMain(ctx, bt.lang(u), chatID, msgID)
		} else {
			bt.edit(ctx, chatID, msgID, i18n.T(l, "lang.title"), langKB(l))
		}
	case "status":
		bt.showStatus(ctx, l, chatID, msgID)
	case "noop":
	}
}

// ---------------------------------------------------------------------------
// Main menu / simple screens
// ---------------------------------------------------------------------------

func (bt *Bot) showMain(ctx context.Context, l i18n.Lang, chatID int64, msgID int) {
	text := "<b>" + esc(i18n.T(l, "app.title")) + "</b>\n\n" + i18n.T(l, "menu.welcome")
	if msgID == 0 {
		bt.send(ctx, chatID, text, mainMenuKB(l))
		return
	}
	bt.edit(ctx, chatID, msgID, text, mainMenuKB(l))
}

func (bt *Bot) showStatus(ctx context.Context, l i18n.Lang, chatID int64, msgID int) {
	vars, _ := bt.st.ListVariables()
	enabled, _ := bt.st.CountEnabledFields()
	acc := i18n.T(l, "common.disabled")
	if bt.mgr.IsOnline() {
		acc = i18n.T(l, "common.enabled")
		if s := bt.mgr.Self(); s != nil {
			acc = "✅ " + esc(s.FirstName)
		}
	}
	last := i18n.T(l, "common.none")
	if f, err := bt.st.GetField(storage.FieldFirstName); err == nil && f.LastPushedAt > 0 {
		last = time.Unix(f.LastPushedAt, 0).Format("2006-01-02 15:04:05")
	}
	body := i18n.T(l, "status.body",
		acc, onOff(l, bt.sch.LiveEnabled()), bt.sch.TickSeconds(), len(vars), enabled, last)
	text := "<b>" + esc(i18n.T(l, "status.title")) + "</b>\n\n" + body
	bt.edit(ctx, chatID, msgID, text, kb(backHomeRow(l, "")))
}

// ---------------------------------------------------------------------------
// Account
// ---------------------------------------------------------------------------

func (bt *Bot) showAccount(ctx context.Context, l i18n.Lang, chatID int64, msgID int) {
	var text string
	var rows [][]models.InlineKeyboardButton
	if bt.mgr.IsOnline() {
		s := bt.mgr.Self()
		name, phone, id := "?", "—", int64(0)
		if s != nil {
			name = strings.TrimSpace(s.FirstName + " " + s.LastName)
			if s.Username != "" {
				name += " (@" + s.Username + ")"
			}
			if s.Phone != "" {
				phone = "+" + s.Phone
			}
			id = s.ID
		}
		text = "<b>" + esc(i18n.T(l, "acc.title")) + "</b>\n\n" + esc(i18n.T(l, "acc.connected", name, id, phone))
		rows = append(rows, row(btn(i18n.T(l, "acc.logout"), "acc:logout")))
	} else {
		text = "<b>" + esc(i18n.T(l, "acc.title")) + "</b>\n\n" + i18n.T(l, "acc.disconnected")
		rows = append(rows, row(btn(i18n.T(l, "acc.login"), "acc:login")))
	}
	rows = append(rows, backHomeRow(l, ""))
	if msgID == 0 {
		bt.send(ctx, chatID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
		return
	}
	bt.edit(ctx, chatID, msgID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (bt *Bot) handleAccountCB(ctx context.Context, u *storage.User, cq *models.CallbackQuery, parts []string, chatID int64, msgID int) {
	l := bt.lang(u)
	if len(parts) == 1 {
		bt.showAccount(ctx, l, chatID, msgID)
		return
	}
	switch parts[1] {
	case "login":
		_ = bt.st.SetUserState(u.ID, stateLoginPhone, "")
		bt.send(ctx, chatID, i18n.T(l, "acc.ask_phone"), contactKB(l))
	case "logout":
		if err := bt.mgr.Logout(ctx); err != nil {
			bt.answer(ctx, cq, i18n.T(l, "acc.logout_fail", err.Error()), true)
		} else {
			bt.answer(ctx, cq, i18n.T(l, "acc.logout_ok"), true)
		}
		bt.showAccount(ctx, l, chatID, msgID)
	}
}

// ---------------------------------------------------------------------------
// Fields
// ---------------------------------------------------------------------------

func fieldName(l i18n.Lang, id string) string {
	switch id {
	case storage.FieldFirstName:
		return i18n.T(l, "fields.first")
	case storage.FieldLastName:
		return i18n.T(l, "fields.last")
	default:
		return i18n.T(l, "fields.about")
	}
}

func (bt *Bot) showFields(ctx context.Context, l i18n.Lang, chatID int64, msgID int) {
	fields, _ := bt.st.ListFields()
	var rows [][]models.InlineKeyboardButton
	for _, f := range fields {
		mark := "⛔️"
		if f.Enabled {
			mark = "✅"
		}
		rows = append(rows, row(btn(mark+" "+fieldName(l, f.ID), "fld:"+f.ID)))
	}
	rows = append(rows, backHomeRow(l, ""))
	bt.edit(ctx, chatID, msgID, "<b>"+esc(i18n.T(l, "fields.title"))+"</b>", &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (bt *Bot) showFieldItem(ctx context.Context, l i18n.Lang, chatID int64, msgID int, id string) {
	f, err := bt.st.GetField(id)
	if err != nil {
		return
	}
	tpl := f.Template
	if strings.TrimSpace(tpl) == "" {
		tpl = i18n.T(l, "fields.empty_tpl")
	}
	text := fmt.Sprintf("<b>%s</b>\n\n%s\n%s: %s\n%s: %s\n%s: %ds",
		esc(i18n.T(l, "fields.edit_title", fieldName(l, id))),
		fmt.Sprintf(i18n.T(l, "fields.cur_template"), esc(tpl)),
		esc(i18n.T(l, "fields.toggle")), onOff(l, f.Enabled),
		esc(i18n.T(l, "fields.font")), esc(variables.FontName(f.Font, string(l))),
		esc(i18n.T(l, "fields.interval")), f.MinIntervalSec,
	)
	rows := [][]models.InlineKeyboardButton{
		row(btn(i18n.T(l, "fields.set_template"), "fld:"+id+":tpl"), btn(i18n.T(l, "common.preview"), "fldprev:"+id)),
		row(btn(i18n.T(l, "fields.font"), "fld:"+id+":font"), btn(i18n.T(l, "fields.interval"), "fld:"+id+":int")),
		row(btn(onOff(l, f.Enabled), "fld:"+id+":tog")),
		backHomeRow(l, "fields"),
	}
	bt.edit(ctx, chatID, msgID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (bt *Bot) handleFieldCB(ctx context.Context, u *storage.User, cq *models.CallbackQuery, parts []string, chatID int64, msgID int) {
	l := bt.lang(u)
	if len(parts) < 2 {
		bt.showFields(ctx, l, chatID, msgID)
		return
	}
	id := parts[1]
	if !validField(id) {
		return
	}
	action := ""
	if len(parts) > 2 {
		action = parts[2]
	}
	switch action {
	case "":
		bt.showFieldItem(ctx, l, chatID, msgID, id)
	case "tog":
		f, _ := bt.st.GetField(id)
		f.Enabled = !f.Enabled
		_ = bt.st.UpdateField(f)
		bt.showFieldItem(ctx, l, chatID, msgID, id)
	case "tpl":
		names := bt.varNames()
		_ = bt.st.SetUserState(u.ID, stateFieldTemplate+id, "")
		bt.send(ctx, chatID, i18n.T(l, "fields.ask_template", names), removeKB())
	case "int":
		_ = bt.st.SetUserState(u.ID, stateFieldInterval+id, "")
		bt.send(ctx, chatID, i18n.T(l, "fields.ask_interval"), removeKB())
	case "font":
		bt.edit(ctx, chatID, msgID, i18n.T(l, "font.pick"), fontPickerKB(l, "fldfont:"+id+":", "fld:"+id))
	}
}

func (bt *Bot) handleFieldFont(ctx context.Context, u *storage.User, parts []string, chatID int64, msgID int) {
	if len(parts) < 3 {
		return
	}
	id, font := parts[1], parts[2]
	if !validField(id) {
		return
	}
	f, _ := bt.st.GetField(id)
	f.Font = font
	_ = bt.st.UpdateField(f)
	bt.showFieldItem(ctx, bt.lang(u), chatID, msgID, id)
}

func (bt *Bot) handleFieldPreview(ctx context.Context, u *storage.User, cq *models.CallbackQuery, parts []string) {
	l := bt.lang(u)
	if len(parts) < 2 {
		return
	}
	f, err := bt.st.GetField(parts[1])
	if err != nil {
		return
	}
	preview := variables.RenderField(f, bt.previewVars())
	if strings.TrimSpace(preview) == "" {
		preview = i18n.T(l, "fields.empty_tpl")
	}
	bt.answer(ctx, cq, "👁 "+preview, true)
}

// ---------------------------------------------------------------------------
// Variables menus
// ---------------------------------------------------------------------------

func (bt *Bot) showVars(ctx context.Context, l i18n.Lang, chatID int64, msgID int) {
	vars, _ := bt.st.ListVariables()
	var rows [][]models.InlineKeyboardButton
	for _, v := range vars {
		rows = append(rows, row(btn("🧩 {"+v.Name+"} · "+variables.TypeName(v.Type, string(l)), "var:"+v.Name)))
	}
	rows = append(rows, row(btn(i18n.T(l, "vars.add"), "vars:add")))
	rows = append(rows, backHomeRow(l, ""))
	text := "<b>" + esc(i18n.T(l, "vars.title")) + "</b>"
	if len(vars) == 0 {
		text += "\n\n" + i18n.T(l, "vars.none")
	}
	bt.edit(ctx, chatID, msgID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (bt *Bot) showVarItem(ctx context.Context, l i18n.Lang, chatID int64, msgID int, name string) {
	v, err := bt.st.GetVariable(name)
	if err != nil {
		bt.showVars(ctx, l, chatID, msgID)
		return
	}
	val, _ := variables.ResolveValue(v, time.Now())
	if strings.TrimSpace(val) == "" {
		val = i18n.T(l, "common.none")
	}
	text := i18n.T(l, "vars.item_title",
		esc(v.Name), esc(variables.TypeName(v.Type, string(l))), v.IntervalSec,
		esc(variables.FontName(v.Font, string(l))), esc(val))
	rows := [][]models.InlineKeyboardButton{
		row(btn(i18n.T(l, "vars.set_interval"), "var:"+name+":int"), btn(i18n.T(l, "vars.set_font"), "var:"+name+":font")),
	}
	if needsConfig(v.Type) {
		rows = append(rows, row(btn(i18n.T(l, "vars.set_config"), "var:"+name+":cfg")))
	}
	rows = append(rows,
		row(btn(i18n.T(l, "common.delete"), "var:"+name+":del")),
		backHomeRow(l, "vars"),
	)
	bt.edit(ctx, chatID, msgID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (bt *Bot) handleVarCB(ctx context.Context, u *storage.User, cq *models.CallbackQuery, parts []string, chatID int64, msgID int) {
	l := bt.lang(u)
	if len(parts) < 2 {
		bt.showVars(ctx, l, chatID, msgID)
		return
	}
	name := parts[1]
	action := ""
	if len(parts) > 2 {
		action = parts[2]
	}
	switch action {
	case "":
		bt.showVarItem(ctx, l, chatID, msgID, name)
	case "int":
		_ = bt.st.SetUserState(u.ID, stateVarInterval+name, "")
		bt.send(ctx, chatID, i18n.T(l, "vars.ask_interval"), nil)
	case "font":
		bt.edit(ctx, chatID, msgID, i18n.T(l, "font.pick"), fontPickerKB(l, "vfont:"+name+":", "var:"+name))
	case "cfg":
		bt.startVarConfigEdit(ctx, u, name, chatID)
	case "del":
		bt.edit(ctx, chatID, msgID, i18n.T(l, "common.confirm.del"), kb(
			row(btn(i18n.T(l, "common.delete"), "var:"+name+":delok"), btn(i18n.T(l, "common.cancel"), "var:"+name)),
		))
	case "delok":
		_ = bt.st.DeleteVariable(name)
		bt.answer(ctx, cq, i18n.T(l, "vars.deleted"), false)
		bt.showVars(ctx, l, chatID, msgID)
	}
}

func (bt *Bot) handleVarFont(ctx context.Context, u *storage.User, parts []string, chatID int64, msgID int) {
	if len(parts) < 3 {
		return
	}
	name, font := parts[1], parts[2]
	v, err := bt.st.GetVariable(name)
	if err != nil {
		return
	}
	v.Font = font
	_ = bt.st.UpdateVariable(v)
	bt.showVarItem(ctx, bt.lang(u), chatID, msgID, name)
}

// ---------------------------------------------------------------------------
// Live update menu
// ---------------------------------------------------------------------------

func (bt *Bot) showLive(ctx context.Context, l i18n.Lang, chatID int64, msgID int) {
	on := bt.sch.LiveEnabled()
	text := "<b>" + esc(i18n.T(l, "menu.live")) + "</b>\n\n" +
		i18n.T(l, "live.title", onOff(l, on)) + "\n\n" + i18n.T(l, "live.safety")
	toggle := btn(i18n.T(l, "live.toggle_on"), "live:on")
	if on {
		toggle = btn(i18n.T(l, "live.toggle_off"), "live:off")
	}
	rows := [][]models.InlineKeyboardButton{
		row(toggle, btn(i18n.T(l, "live.now"), "live:now")),
		row(btn(fmt.Sprintf("%s (%ds)", i18n.T(l, "live.tick"), bt.sch.TickSeconds()), "live:tick")),
		backHomeRow(l, ""),
	}
	bt.edit(ctx, chatID, msgID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (bt *Bot) handleLiveCB(ctx context.Context, u *storage.User, cq *models.CallbackQuery, parts []string, chatID int64, msgID int) {
	l := bt.lang(u)
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch action {
	case "on":
		if !bt.mgr.IsOnline() {
			bt.answer(ctx, cq, i18n.T(l, "acc.need_login"), true)
			return
		}
		_ = bt.sch.SetLive(true)
		bt.showLive(ctx, l, chatID, msgID)
	case "off":
		_ = bt.sch.SetLive(false)
		bt.showLive(ctx, l, chatID, msgID)
	case "tick":
		_ = bt.st.SetUserState(u.ID, stateLiveTick, "")
		bt.send(ctx, chatID, i18n.T(l, "live.ask_tick"), nil)
	case "now":
		if !bt.mgr.IsOnline() {
			bt.answer(ctx, cq, i18n.T(l, "acc.need_login"), true)
			return
		}
		pushed, err := bt.sch.UpdateNow(ctx)
		switch {
		case err != nil:
			bt.answer(ctx, cq, i18n.T(l, "err.generic", err.Error()), true)
		case pushed:
			bt.answer(ctx, cq, i18n.T(l, "live.pushed"), true)
		default:
			bt.answer(ctx, cq, i18n.T(l, "live.nochange"), true)
		}
		bt.showLive(ctx, l, chatID, msgID)
	default:
		bt.showLive(ctx, l, chatID, msgID)
	}
}

// ---------------------------------------------------------------------------
// Helpers shared across handlers
// ---------------------------------------------------------------------------

func validField(id string) bool {
	for _, f := range storage.AllFields {
		if f == id {
			return true
		}
	}
	return false
}

func needsConfig(typ string) bool {
	if typ == variables.CustomType {
		return true
	}
	s, ok := variables.SpecByKey(typ)
	return ok && s.Kind != variables.KindNone
}

// varNames returns a human list of defined variable tokens for prompts.
func (bt *Bot) varNames() string {
	vars, _ := bt.st.ListVariables()
	if len(vars) == 0 {
		return "—"
	}
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		out = append(out, "{"+v.Name+"}")
	}
	return strings.Join(out, " ")
}

// previewVars returns variables with freshly-computed display values (not
// persisted) for rendering previews.
func (bt *Bot) previewVars() map[string]*storage.Variable {
	vars, _ := bt.st.ListVariables()
	now := time.Now()
	out := make(map[string]*storage.Variable, len(vars))
	for _, v := range vars {
		val, _ := variables.ResolveValue(v, now)
		v.LastValue = val
		out[v.Name] = v
	}
	return out
}
