package telegrambridge

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	requestTimeout  = 20 * time.Second
	maxSendAttempts = 3
	minSendGap      = time.Second     // Telegram allows ~20 messages/minute per group
	minEditGap      = 3 * time.Second // live status edits share that budget
)

// errThrottled means an edit was skipped to respect rate limits; callers retry later.
var errThrottled = errors.New("telegram edit throttled")

// poster sends Telegram-HTML messages with per-chat pacing, 429 retries and a
// plain-text fallback when Telegram rejects the HTML.
type poster struct {
	bot *bot.Bot
	log *slog.Logger

	mu       sync.Mutex
	nextSend map[int64]time.Time
	nextEdit map[int64]time.Time
}

func newPoster(b *bot.Bot, log *slog.Logger) *poster {
	return &poster{bot: b, log: log, nextSend: map[int64]time.Time{}, nextEdit: map[int64]time.Time{}}
}

// send posts html into a chat topic (threadID 0 = General) and returns the message ID (0 on failure).
func (p *poster) send(chatID int64, threadID int, text string, markup models.ReplyMarkup) int {
	p.pace(chatID)
	params := &bot.SendMessageParams{
		ChatID: chatID, MessageThreadID: threadID, Text: text, ParseMode: models.ParseModeHTML,
		ReplyMarkup: markup, LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
	}
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		msg, err := p.bot.SendMessage(ctx, params)
		cancel()
		if err == nil {
			return msg.ID
		}
		var tooMany *bot.TooManyRequestsError
		switch {
		case errors.As(err, &tooMany):
			time.Sleep(time.Duration(tooMany.RetryAfter) * time.Second)
		case isParseError(err) && params.ParseMode != "":
			p.log.Warn("telegram rejected HTML, sending plain text", "err", err)
			params.Text, params.ParseMode = htmlToPlain(params.Text), ""
		default:
			p.log.Error("telegram send failed", "chat", chatID, "err", err)
			return 0
		}
	}
	return 0
}

// edit replaces a message's text; it never waits and returns errThrottled instead.
func (p *poster) edit(chatID int64, msgID int, text string, markup models.ReplyMarkup) error {
	p.mu.Lock()
	if time.Now().Before(p.nextEdit[chatID]) {
		p.mu.Unlock()
		return errThrottled
	}
	p.nextEdit[chatID] = time.Now().Add(minEditGap)
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	_, err := p.bot.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: msgID, Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
	})
	var tooMany *bot.TooManyRequestsError
	switch {
	case err == nil, strings.Contains(err.Error(), "message is not modified"):
		return nil
	case errors.As(err, &tooMany):
		p.mu.Lock()
		p.nextEdit[chatID] = time.Now().Add(time.Duration(tooMany.RetryAfter) * time.Second)
		p.mu.Unlock()
		return errThrottled
	default:
		p.log.Warn("telegram edit failed", "chat", chatID, "err", err)
		return err
	}
}

// replace edits a message that must change (e.g. removing answered buttons):
// unlike edit it waits for the chat's budget instead of skipping.
func (p *poster) replace(chatID int64, msgID int, text string) error {
	var err error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		p.pace(chatID)
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		_, err = p.bot.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: chatID, MessageID: msgID, Text: text, ParseMode: models.ParseModeHTML})
		cancel()
		var tooMany *bot.TooManyRequestsError
		if !errors.As(err, &tooMany) {
			return err
		}
		time.Sleep(time.Duration(tooMany.RetryAfter) * time.Second)
	}
	return err
}

func (p *poster) delete(chatID int64, msgID int) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if _, err := p.bot.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID}); err != nil {
		p.log.Warn("telegram delete failed (is the bot an admin with Delete Messages?)", "chat", chatID, "err", err)
	}
}

// pace blocks until the chat's send budget allows another message.
func (p *poster) pace(chatID int64) {
	p.mu.Lock()
	at := time.Now()
	if next := p.nextSend[chatID]; next.After(at) {
		at = next
	}
	p.nextSend[chatID] = at.Add(minSendGap)
	p.mu.Unlock()
	time.Sleep(time.Until(at))
}

func isParseError(err error) bool {
	return strings.Contains(err.Error(), "can't parse entities") || strings.Contains(err.Error(), "unsupported start tag")
}
