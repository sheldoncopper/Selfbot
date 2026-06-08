package botui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/selfbot/selfbot/internal/i18n"
	"github.com/selfbot/selfbot/internal/storage"
)

// exportDoc is the portable representation of the whole configuration.
type exportDoc struct {
	Version   int               `json:"version"`
	Fields    []exportField     `json:"fields"`
	Variables []exportVar       `json:"variables"`
	Settings  map[string]string `json:"settings"`
}

type exportField struct {
	ID             string `json:"id"`
	Enabled        bool   `json:"enabled"`
	Template       string `json:"template"`
	Font           string `json:"font"`
	MinIntervalSec int    `json:"min_interval_sec"`
}

type exportVar struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Config      string `json:"config"`
	IntervalSec int    `json:"interval_sec"`
	Font        string `json:"font"`
}

// ---------------------------------------------------------------------------
// Menu
// ---------------------------------------------------------------------------

func (bt *Bot) showPort(ctx context.Context, l i18n.Lang, chatID int64, msgID int) {
	rows := [][]models.InlineKeyboardButton{
		row(btn(i18n.T(l, "port.export_json"), "port:json"), btn(i18n.T(l, "port.export_txt"), "port:txt")),
		row(btn(i18n.T(l, "port.import"), "port:import")),
		backHomeRow(l, ""),
	}
	text := "<b>" + esc(i18n.T(l, "menu.portability")) + "</b>\n\n" + i18n.T(l, "port.title")
	if msgID == 0 {
		bt.send(ctx, chatID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
		return
	}
	bt.edit(ctx, chatID, msgID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (bt *Bot) handlePortCB(ctx context.Context, u *storage.User, parts []string, chatID int64, msgID int) {
	l := bt.lang(u)
	if len(parts) == 1 {
		bt.showPort(ctx, l, chatID, msgID)
		return
	}
	switch parts[1] {
	case "json":
		bt.exportConfig(ctx, l, chatID, false)
	case "txt":
		bt.exportConfig(ctx, l, chatID, true)
	case "import":
		_ = bt.st.SetUserState(u.ID, stateImportWait, "")
		bt.send(ctx, chatID, i18n.T(l, "port.ask_import"), nil)
	}
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

func (bt *Bot) buildExport() (*exportDoc, error) {
	fields, err := bt.st.ListFields()
	if err != nil {
		return nil, err
	}
	vars, err := bt.st.ListVariables()
	if err != nil {
		return nil, err
	}
	doc := &exportDoc{
		Version:  1,
		Settings: map[string]string{"live_enabled": bt.st.GetSetting("live_enabled", "0"), "tick_interval": bt.st.GetSetting("tick_interval", "15")},
	}
	for _, f := range fields {
		doc.Fields = append(doc.Fields, exportField{f.ID, f.Enabled, f.Template, f.Font, f.MinIntervalSec})
	}
	for _, v := range vars {
		doc.Variables = append(doc.Variables, exportVar{v.Name, v.Type, v.Config, v.IntervalSec, v.Font})
	}
	return doc, nil
}

func (bt *Bot) exportConfig(ctx context.Context, l i18n.Lang, chatID int64, asText bool) {
	doc, err := bt.buildExport()
	if err != nil {
		bt.send(ctx, chatID, i18n.T(l, "err.generic", err.Error()), nil)
		return
	}
	var data []byte
	var filename string
	if asText {
		data = []byte(renderTextExport(doc))
		filename = "selfbot-config.txt"
	} else {
		data, _ = json.MarshalIndent(doc, "", "  ")
		filename = "selfbot-config.json"
	}
	_, err = bt.api.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: filename, Data: bytes.NewReader(data)},
		Caption:  i18n.T(l, "port.exported"),
	})
	if err != nil {
		bt.send(ctx, chatID, i18n.T(l, "err.generic", err.Error()), nil)
	}
}

func renderTextExport(doc *exportDoc) string {
	var b strings.Builder
	b.WriteString("# Self-Bot configuration export (v")
	b.WriteString(itoa(doc.Version))
	b.WriteString(")\n\n[settings]\n")
	for k, v := range doc.Settings {
		fmt.Fprintf(&b, "%s = %s\n", k, v)
	}
	b.WriteString("\n[fields]\n")
	for _, f := range doc.Fields {
		fmt.Fprintf(&b, "- %s | enabled=%v | font=%s | min=%ds\n  template: %s\n",
			f.ID, f.Enabled, f.Font, f.MinIntervalSec, f.Template)
	}
	b.WriteString("\n[variables]\n")
	for _, v := range doc.Variables {
		fmt.Fprintf(&b, "- {%s} | type=%s | interval=%ds | font=%s\n  config: %s\n",
			v.Name, v.Type, v.IntervalSec, v.Font, v.Config)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

func (bt *Bot) onImportDocument(ctx context.Context, u *storage.User, msg *models.Message) {
	l := bt.lang(u)
	if msg.Document == nil {
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "port.ask_import"), nil)
		return
	}
	data, err := bt.downloadFile(ctx, msg.Document.FileID)
	if err != nil {
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "port.import_fail", err.Error()), nil)
		return
	}
	var doc exportDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "port.import_fail", "invalid JSON: "+err.Error()), nil)
		return
	}

	nf, nv, err := bt.applyImport(&doc)
	_ = bt.st.SetUserState(u.ID, "", "")
	if err != nil {
		bt.send(ctx, msg.Chat.ID, i18n.T(l, "port.import_fail", err.Error()), nil)
		return
	}
	bt.send(ctx, msg.Chat.ID, i18n.T(l, "port.import_ok", nf, nv), nil)
	bt.showMain(ctx, l, msg.Chat.ID, 0)
}

// applyImport replaces fields and variables with those in doc.
func (bt *Bot) applyImport(doc *exportDoc) (fields, vars int, err error) {
	for _, f := range doc.Fields {
		if !validField(f.ID) {
			continue
		}
		cur, gerr := bt.st.GetField(f.ID)
		if gerr != nil {
			continue
		}
		cur.Enabled = f.Enabled
		cur.Template = f.Template
		cur.Font = f.Font
		if f.MinIntervalSec >= 5 {
			cur.MinIntervalSec = f.MinIntervalSec
		}
		if err = bt.st.UpdateField(cur); err != nil {
			return fields, vars, err
		}
		fields++
	}

	// Replace the variable set.
	existing, _ := bt.st.ListVariables()
	for _, v := range existing {
		_ = bt.st.DeleteVariable(v.Name)
	}
	for _, v := range doc.Variables {
		if !nameRe.MatchString(v.Name) {
			continue
		}
		interval := v.IntervalSec
		if interval < 5 {
			interval = 60
		}
		cfg := v.Config
		if strings.TrimSpace(cfg) == "" {
			cfg = "{}"
		}
		nv := &storage.Variable{
			Name:        v.Name,
			Type:        v.Type,
			Config:      cfg,
			IntervalSec: interval,
			Font:        v.Font,
		}
		if err = bt.st.CreateVariable(nv); err != nil {
			return fields, vars, err
		}
		vars++
	}

	if doc.Settings != nil {
		if t, ok := doc.Settings["tick_interval"]; ok {
			_ = bt.st.SetSetting("tick_interval", t)
		}
	}
	return fields, vars, nil
}

// downloadFile fetches a Telegram file's bytes via the Bot API file endpoint.
func (bt *Bot) downloadFile(ctx context.Context, fileID string) ([]byte, error) {
	f, err := bt.api.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, err
	}
	if f.FilePath == "" {
		return nil, fmt.Errorf("empty file path")
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", bt.cfg.BotToken, f.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5 MiB cap
}
