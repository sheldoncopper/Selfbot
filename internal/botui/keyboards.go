package botui

import (
	"github.com/go-telegram/bot/models"

	"github.com/selfbot/selfbot/internal/i18n"
	"github.com/selfbot/selfbot/internal/variables"
)

// ---------------------------------------------------------------------------
// Small inline-keyboard construction helpers
// ---------------------------------------------------------------------------

func btn(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

func row(b ...models.InlineKeyboardButton) []models.InlineKeyboardButton { return b }

func kb(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func backHomeRow(l i18n.Lang, back string) []models.InlineKeyboardButton {
	r := []models.InlineKeyboardButton{}
	if back != "" {
		r = append(r, btn(i18n.T(l, "menu.back"), back))
	}
	r = append(r, btn(i18n.T(l, "menu.home"), "home"))
	return r
}

// ---------------------------------------------------------------------------
// Static menus
// ---------------------------------------------------------------------------

func mainMenuKB(l i18n.Lang) *models.InlineKeyboardMarkup {
	return kb(
		row(btn(i18n.T(l, "menu.account"), "acc"), btn(i18n.T(l, "menu.live"), "live")),
		row(btn(i18n.T(l, "menu.fields"), "fields"), btn(i18n.T(l, "menu.variables"), "vars")),
		row(btn(i18n.T(l, "menu.portability"), "port"), btn(i18n.T(l, "menu.status"), "status")),
		row(btn(i18n.T(l, "menu.language"), "lang")),
	)
}

func langKB(l i18n.Lang) *models.InlineKeyboardMarkup {
	return kb(
		row(btn("🇮🇷 فارسی", "lang:fa"), btn("🇬🇧 English", "lang:en")),
		backHomeRow(l, ""),
	)
}

// fontPickerKB builds a grid of font choices. The callback prefix receives the
// font key appended (e.g. "vfont:myvar:" + key).
func fontPickerKB(l i18n.Lang, prefix, back string) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	var cur []models.InlineKeyboardButton
	for _, f := range variables.Fonts() {
		name := f.FA
		if l == i18n.EN {
			name = f.EN
		}
		cur = append(cur, btn(name, prefix+f.Key))
		if len(cur) == 2 {
			rows = append(rows, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	rows = append(rows, backHomeRow(l, back))
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// typeChooserKB lists predefined variable types plus custom.
func typeChooserKB(l i18n.Lang) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	rows = append(rows, row(btn(i18n.T(l, "vars.type_custom"), "vtype:"+variables.CustomType)))
	var cur []models.InlineKeyboardButton
	for _, s := range variables.Specs() {
		name := s.FA
		if l == i18n.EN {
			name = s.EN
		}
		cur = append(cur, btn(name, "vtype:"+s.Key))
		if len(cur) == 2 {
			rows = append(rows, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	rows = append(rows, backHomeRow(l, "vars"))
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// tzKB builds a timezone picker. cbPrefix receives the IANA name appended.
func tzKB(l i18n.Lang, cbPrefix string) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	var cur []models.InlineKeyboardButton
	for _, z := range variables.Timezones {
		cur = append(cur, btn(z.Label, cbPrefix+z.TZ))
		if len(cur) == 2 {
			rows = append(rows, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	rows = append(rows, row(btn(i18n.T(l, "common.cancel"), "vars")))
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// layoutKB builds a time-format picker. cbPrefix receives the layout index.
func layoutKB(l i18n.Lang, cbPrefix string) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	for i, lay := range variables.TimeLayouts {
		rows = append(rows, row(btn(lay.Label, cbPrefix+itoa(i))))
	}
	rows = append(rows, row(btn(i18n.T(l, "common.cancel"), "vars")))
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// calKB builds a calendar-type picker. cbPrefix receives "greg"/"jalali".
func calKB(l i18n.Lang, cbPrefix string) *models.InlineKeyboardMarkup {
	return kb(
		row(btn(i18n.T(l, "vars.cal_jalali"), cbPrefix+"jalali"), btn(i18n.T(l, "vars.cal_greg"), cbPrefix+"greg")),
		row(btn(i18n.T(l, "common.cancel"), "vars")),
	)
}

// contactKB is the reply keyboard that requests the user's phone number.
func contactKB(l i18n.Lang) *models.ReplyKeyboardMarkup {
	return &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{{Text: i18n.T(l, "acc.share_contact"), RequestContact: true}},
		},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
}

func removeKB() *models.ReplyKeyboardRemove {
	return &models.ReplyKeyboardRemove{RemoveKeyboard: true}
}
