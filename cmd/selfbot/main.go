// Command selfbot is the entrypoint. It wires configuration, storage, the
// Telegram user-account manager, the live-update scheduler and the management
// bot, then runs until interrupted.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/selfbot/selfbot/internal/botui"
	"github.com/selfbot/selfbot/internal/config"
	"github.com/selfbot/selfbot/internal/scheduler"
	"github.com/selfbot/selfbot/internal/storage"
	"github.com/selfbot/selfbot/internal/userbot"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	logger, err := newLogger()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()

	st, err := storage.Open(cfg.DBPath)
	if err != nil {
		logger.Fatal("open storage", zap.Error(err))
	}
	defer func() { _ = st.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr := userbot.New(cfg, st, logger)
	mgr.Start(ctx) // auto-connects if a session already exists

	sch := scheduler.New(st, mgr, logger)
	go sch.Run(ctx)

	bt, err := botui.New(cfg, st, mgr, sch, logger)
	if err != nil {
		logger.Fatal("create bot", zap.Error(err))
	}

	logger.Info("self-bot starting",
		zap.Bool("webhook", cfg.UseWebhook()),
		zap.Int("whitelist", len(cfg.Whitelist)),
	)
	if err := bt.Run(ctx); err != nil {
		logger.Fatal("bot run", zap.Error(err))
	}
	logger.Info("shutting down")
}

func newLogger() (*zap.Logger, error) {
	if os.Getenv("LOG_LEVEL") == "debug" {
		return zap.NewDevelopment()
	}
	cfg := zap.NewProductionConfig()
	cfg.DisableStacktrace = true
	return cfg.Build()
}
