// Package botui implements the Telegram management bot: the conversational
// interface (menus, login, variable/field editors, import/export) that lets a
// whitelisted user drive the whole self-bot. It speaks the Bot API and runs in
// webhook mode (Railway) or long-polling mode (local).
package botui

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"

	"github.com/selfbot/selfbot/internal/config"
	"github.com/selfbot/selfbot/internal/i18n"
	"github.com/selfbot/selfbot/internal/scheduler"
	"github.com/selfbot/selfbot/internal/storage"
	"github.com/selfbot/selfbot/internal/userbot"
)

// Bot is the management bot.
type Bot struct {
	cfg *config.Config
	st  *storage.Store
	mgr *userbot.Manager
	sch *scheduler.Scheduler
	log *zap.Logger
	api *bot.Bot

	loginMu sync.Mutex
	logins  map[int64]*userbot.LoginSession
}

// New constructs the management bot and registers the single dispatch handler.
func New(cfg *config.Config, st *storage.Store, mgr *userbot.Manager, sch *scheduler.Scheduler, log *zap.Logger) (*Bot, error) {
	bt := &Bot{cfg: cfg, st: st, mgr: mgr, sch: sch, log: log, logins: map[int64]*userbot.LoginSession{}}

	opts := []bot.Option{
		bot.WithDefaultHandler(bt.dispatch),
	}
	if cfg.UseWebhook() && cfg.WebhookSecret != "" {
		opts = append(opts, bot.WithWebhookSecretToken(cfg.WebhookSecret))
	}
	api, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}
	bt.api = api
	return bt, nil
}

// Run starts the bot. In webhook mode it registers the webhook and serves HTTP
// (with a health endpoint); in polling mode it long-polls. It blocks until ctx
// is canceled.
func (bt *Bot) Run(ctx context.Context) error {
	bt.setCommands(ctx)

	if !bt.cfg.UseWebhook() {
		bt.log.Info("management bot started (long polling)")
		bt.api.Start(ctx)
		return nil
	}

	if _, err := bt.api.SetWebhook(ctx, &bot.SetWebhookParams{
		URL:            bt.cfg.WebhookURL(),
		SecretToken:    bt.cfg.WebhookSecret,
		AllowedUpdates: []string{"message", "callback_query"},
		MaxConnections: 40,
	}); err != nil {
		return fmt.Errorf("set webhook: %w", err)
	}
	bt.log.Info("webhook registered", zap.String("url", bt.cfg.WebhookURL()))

	go bt.api.StartWebhook(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc(bt.cfg.WebhookPath, bt.api.WebhookHandler())
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: ":" + bt.cfg.Port, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	bt.log.Info("http server listening", zap.String("port", bt.cfg.Port))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (bt *Bot) setCommands(ctx context.Context) {
	_, _ = bt.api.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "start", Description: "منوی اصلی / Main menu"},
			{Command: "menu", Description: "منوی اصلی / Main menu"},
			{Command: "cancel", Description: "لغو / Cancel"},
		},
	})
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// dispatch is the single entry point for every update. It enforces the
// whitelist, loads the user, then routes to message or callback handling.
func (bt *Bot) dispatch(ctx context.Context, _ *bot.Bot, update *models.Update) {
	uid, chatID, ok := extractIDs(update)
	if !ok {
		return
	}
	if !bt.cfg.IsWhitelisted(uid) {
		bt.send(ctx, chatID, i18n.T(i18n.FA, "common.notauth", uid), nil)
		return
	}
	user, err := bt.st.GetOrCreateUser(uid, bt.cfg.DefaultLang)
	if err != nil {
		bt.log.Error("get user", zap.Error(err))
		return
	}

	defer func() {
		if r := recover(); r != nil {
			bt.log.Error("handler panic", zap.Any("recover", r))
		}
	}()

	if update.CallbackQuery != nil {
		bt.handleCallback(ctx, user, update.CallbackQuery)
		return
	}
	if update.Message != nil {
		bt.handleMessage(ctx, user, update.Message)
	}
}

func extractIDs(u *models.Update) (uid, chatID int64, ok bool) {
	switch {
	case u.CallbackQuery != nil:
		uid = u.CallbackQuery.From.ID
		if u.CallbackQuery.Message.Message != nil {
			chatID = u.CallbackQuery.Message.Message.Chat.ID
		} else {
			chatID = uid
		}
		return uid, chatID, true
	case u.Message != nil:
		return u.Message.From.ID, u.Message.Chat.ID, true
	}
	return 0, 0, false
}

// ---------------------------------------------------------------------------
// Messaging helpers
// ---------------------------------------------------------------------------

func (bt *Bot) lang(u *storage.User) i18n.Lang { return i18n.Normalize(u.Lang) }

func (bt *Bot) send(ctx context.Context, chatID int64, text string, markup models.ReplyMarkup) *models.Message {
	m, err := bt.api.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: markup,
		LinkPreviewOptions: &models.LinkPreviewOptions{
			IsDisabled: boolPtr(true),
		},
	})
	if err != nil {
		bt.log.Warn("send message", zap.Error(err))
	}
	return m
}

// edit edits an existing message; falls back to sending a new one on failure.
func (bt *Bot) edit(ctx context.Context, chatID int64, msgID int, text string, markup *models.InlineKeyboardMarkup) {
	_, err := bt.api.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: msgID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: func() models.ReplyMarkup {
			if markup == nil {
				return nil
			}
			return markup
		}(),
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: boolPtr(true)},
	})
	if err != nil {
		bt.send(ctx, chatID, text, markup)
	}
}

func (bt *Bot) answer(ctx context.Context, cq *models.CallbackQuery, text string, alert bool) {
	_, _ = bt.api.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: cq.ID,
		Text:            text,
		ShowAlert:       alert,
	})
}

// ---------------------------------------------------------------------------
// Tiny utilities
// ---------------------------------------------------------------------------

func boolPtr(b bool) *bool { return &b }

func itoa(i int) string { return strconv.Itoa(i) }

func esc(s string) string { return html.EscapeString(s) }

func onOff(l i18n.Lang, b bool) string {
	if b {
		return i18n.T(l, "common.on")
	}
	return i18n.T(l, "common.off")
}
