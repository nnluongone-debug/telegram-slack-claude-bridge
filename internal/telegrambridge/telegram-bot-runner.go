package telegrambridge

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/uploads"
)

// errorGrace: after a polling error the bridge counts as disconnected for this long.
const errorGrace = 30 * time.Second

// Bridge serves Telegram updates by long polling (no public URL needed).
type Bridge struct {
	bot     *bot.Bot
	cfg     *config.Config
	core    *chatcore.Core
	poster  *poster
	log     *slog.Logger
	seen    *chatcore.RecentKeys
	uploads *uploads.Store
	http    *http.Client // downloads attachments from api.telegram.org

	username  string // the bot's @username, to strip from /command@bot
	running   atomic.Bool
	lastErrAt atomic.Int64
	lastErr   atomic.Pointer[string]
}

// New creates the Telegram bridge; Run connects it. store receives files sent in chats.
func New(cfg *config.Config, core *chatcore.Core, store *uploads.Store, log *slog.Logger) (*Bridge, error) {
	b := &Bridge{
		cfg: cfg, core: core, log: log, seen: chatcore.NewRecentKeys(chatcore.EventDedupeTTL),
		uploads: store, http: &http.Client{}, // downloads are bounded by a context deadline
	}
	client, err := bot.New(cfg.TelegramBotToken,
		bot.WithSkipGetMe(), // Run validates the token with a cancellable GetMe
		bot.WithDefaultHandler(b.onUpdate),
		bot.WithAllowedUpdates(bot.AllowedUpdates{models.AllowedUpdateMessage, models.AllowedUpdateCallbackQuery}),
		bot.WithErrorsHandler(func(err error) {
			b.lastErrAt.Store(time.Now().UnixNano())
			log.Warn("telegram polling error", "err", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("telegram: %w", err)
	}
	b.bot, b.poster = client, newPoster(client, log)
	return b, nil
}

// Run polls until ctx is cancelled.
func (b *Bridge) Run(ctx context.Context) error {
	me, err := b.bot.GetMe(ctx)
	if err != nil {
		msg := "telegram: " + err.Error()
		b.lastErr.Store(&msg)
		return fmt.Errorf("telegram: check TELEGRAM_BRIDGE_BOT_TOKEN: %w", err)
	}
	b.username = me.Username
	b.log.Info("connected to Telegram", "bot", "@"+me.Username)
	b.registerCommands(ctx)
	b.running.Store(true)
	defer b.running.Store(false)
	b.bot.Start(ctx) // returns when ctx is cancelled
	return ctx.Err()
}

// Connected reports whether polling is running without recent errors.
func (b *Bridge) Connected() bool {
	return b.running.Load() && time.Since(time.Unix(0, b.lastErrAt.Load())) > errorGrace
}

// Name identifies the platform in status displays.
func (b *Bridge) Name() string { return "Telegram" }

// Err returns the startup error, if any.
func (b *Bridge) Err() string {
	if p := b.lastErr.Load(); p != nil {
		return *p
	}
	return ""
}

// onUpdate runs on the library's single worker, so messages (which may
// download attachments) are handled in their own goroutine to keep button
// clicks responsive.
func (b *Bridge) onUpdate(ctx context.Context, _ *bot.Bot, update *models.Update) {
	switch {
	case update.Message != nil:
		go b.guarded(func() { b.handleMessage(ctx, update.Message) })
	case update.CallbackQuery != nil:
		b.guarded(func() { b.handleCallback(ctx, update.CallbackQuery) })
	}
}

func (b *Bridge) guarded(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("recovered from panic in telegram handler", "panic", r)
		}
	}()
	fn()
}
