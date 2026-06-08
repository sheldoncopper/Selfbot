// Package config loads and validates runtime configuration from the
// environment (and an optional .env file). It is the single source of truth
// for every tunable the application needs at boot.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration.
type Config struct {
	// Management bot (Bot API).
	BotToken string

	// Telegram MTProto application credentials (https://my.telegram.org).
	APIID   int
	APIHash string

	// Authorized Telegram user IDs allowed to control the bot.
	Whitelist []int64

	// Webhook / HTTP server.
	PublicURL     string // e.g. https://myapp.up.railway.app (empty => long polling)
	WebhookPath   string // e.g. /tg/webhook
	WebhookSecret string // optional secret token validated on each webhook call
	Port          string // HTTP port (Railway injects PORT)

	// Storage.
	DBPath string

	// Defaults.
	DefaultLang string

	// Device fingerprint reported to Telegram (looks like a real client).
	DeviceModel   string
	SystemVersion string
	AppVersion    string
	LangCode      string

	// Safety limits.
	MinUpdateGapSec int // global minimum seconds between two profile pushes
}

// Load reads configuration from the environment, loading a .env file first if
// present. It returns a descriptive error when a required value is missing.
func Load() (*Config, error) {
	// Best-effort: a missing .env is fine (Railway uses real env vars).
	_ = godotenv.Load()

	c := &Config{
		BotToken:        strings.TrimSpace(os.Getenv("BOT_TOKEN")),
		APIHash:         strings.TrimSpace(os.Getenv("API_HASH")),
		PublicURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_URL")), "/"),
		WebhookPath:     getDefault("WEBHOOK_PATH", "/tg/webhook"),
		WebhookSecret:   strings.TrimSpace(os.Getenv("WEBHOOK_SECRET")),
		Port:            getDefault("PORT", "8080"),
		DBPath:          getDefault("DB_PATH", "data/selfbot.sqlite"),
		DefaultLang:     getDefault("DEFAULT_LANG", "fa"),
		DeviceModel:     getDefault("DEVICE_MODEL", "Samsung Galaxy S23 Ultra"),
		SystemVersion:   getDefault("SYSTEM_VERSION", "Android 14 (SDK 34)"),
		AppVersion:      getDefault("APP_VERSION", "Telegram Android 11.2.0"),
		LangCode:        getDefault("LANG_CODE", "en"),
		MinUpdateGapSec: getIntDefault("MIN_UPDATE_GAP_SEC", 20),
	}

	apiIDStr := strings.TrimSpace(os.Getenv("API_ID"))
	if apiIDStr != "" {
		v, err := strconv.Atoi(apiIDStr)
		if err != nil {
			return nil, fmt.Errorf("API_ID must be an integer: %w", err)
		}
		c.APIID = v
	}

	for _, part := range strings.Split(os.Getenv("WHITELIST"), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("WHITELIST contains a non-numeric id %q: %w", part, err)
		}
		c.Whitelist = append(c.Whitelist, id)
	}

	if !strings.HasPrefix(c.WebhookPath, "/") {
		c.WebhookPath = "/" + c.WebhookPath
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	var missing []string
	if c.BotToken == "" {
		missing = append(missing, "BOT_TOKEN")
	}
	if c.APIID == 0 {
		missing = append(missing, "API_ID")
	}
	if c.APIHash == "" {
		missing = append(missing, "API_HASH")
	}
	if len(c.Whitelist) == 0 {
		missing = append(missing, "WHITELIST")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if c.DefaultLang != "fa" && c.DefaultLang != "en" {
		c.DefaultLang = "fa"
	}
	return nil
}

// UseWebhook reports whether the bot should run in webhook mode.
func (c *Config) UseWebhook() bool { return c.PublicURL != "" }

// WebhookURL returns the full public URL Telegram should call.
func (c *Config) WebhookURL() string { return c.PublicURL + c.WebhookPath }

// IsWhitelisted reports whether the given Telegram user id may use the bot.
func (c *Config) IsWhitelisted(id int64) bool {
	for _, w := range c.Whitelist {
		if w == id {
			return true
		}
	}
	return false
}

func getDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getIntDefault(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
