package telegrambridge

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
)

const (
	callbackApprove = "ap:"
	callbackReject  = "rj:"
	callbackChoice  = "ch:"
)

func (b *Bridge) handleMessage(ctx context.Context, m *models.Message) {
	if m.From == nil || m.From.IsBot {
		return
	}
	raw := m.Text
	if strings.TrimSpace(raw) == "" {
		raw = m.Caption // text under a photo or document
	}
	hasFile := len(attachments(m)) > 0
	if strings.TrimSpace(raw) == "" && !hasFile {
		return
	}
	user := strconv.FormatInt(m.From.ID, 10)
	if m.Chat.Type == models.ChatTypePrivate {
		// Direct messages only help with setup: tell people their user ID.
		b.poster.send(m.Chat.ID, 0, fmt.Sprintf("Your Telegram user ID is <code>%s</code>.\nAdd it to <code>TELEGRAM_BRIDGE_ALLOWED_USER_IDS</code> in the bridge's .env, then talk to me in a project group.", user), nil)
		return
	}
	if !b.cfg.TelegramAllowedUserIDs[user] {
		b.log.Warn("ignoring telegram message from unauthorised user", "user", user, "chat", m.Chat.ID)
		return
	}
	if !b.seen.FirstSeen(fmt.Sprintf("%d:%d", m.Chat.ID, m.ID)) {
		return
	}

	conv := &topicConversation{b: b, chatID: m.Chat.ID}
	if m.IsTopicMessage {
		conv.threadID = m.MessageThreadID
	}
	project, ok := b.cfg.Projects.Lookup(conv.ChatID())
	if !ok {
		b.poster.send(m.Chat.ID, conv.threadID, unmappedChatHelp(m.Chat), nil)
		return
	}
	if !m.Chat.IsForum {
		conv.Reply("⚠️ Turn on **Topics** for this group (group settings → Edit → Topics): every session runs in its own topic.")
		return
	}
	text := normalizeCommand(raw, b.username)
	if hasFile && !strings.HasPrefix(text, "!") { // commands ignore attachments
		text = chatcore.WithAttachments(text, b.saveAttachments(ctx, conv, project, m))
	}
	if text == "" {
		return
	}
	if project.IsMulti() {
		b.core.HandleMultiMessage(ctx, conv, project, b.cfg.Projects.Members(project), text, b.askProject(m.Chat.ID))
		return
	}
	b.core.HandleMessage(ctx, conv, project, text)
}

// normalizeCommand maps Telegram commands ("/model@my_bot sonnet", "/start")
// to the core's "!" commands; other text passes through unchanged.
func normalizeCommand(text, botUsername string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return text
	}
	cmd, rest, _ := strings.Cut(text[1:], " ")
	cmd, _, _ = strings.Cut(cmd, "@"+botUsername)
	if cmd == "start" {
		cmd = "help"
	}
	return strings.TrimSpace("!" + cmd + " " + rest)
}

func unmappedChatHelp(chat models.Chat) string {
	return fmt.Sprintf("👋 This group isn't linked yet. Add one line to <b>projects.json</b> (macOS app: menu bar → Edit projects…).\n\nAll projects in this group:\n<pre>\"%d\": { \"name\": \"%s\", \"projects\": [\"*\"] }</pre>\nOnly some: <code>\"projects\": [\"web-app\", \"api\"]</code>. One project: <code>\"workdir\": \"$HOME/path\"</code> instead.",
		chat.ID, html.EscapeString(chat.Title))
}

func (b *Bridge) handleCallback(ctx context.Context, cq *models.CallbackQuery) {
	answer := func(text string) {
		if _, err := b.bot.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID, Text: text}); err != nil {
			b.log.Warn("answer callback failed", "err", err)
		}
	}
	user := strconv.FormatInt(cq.From.ID, 10)
	if !b.cfg.TelegramAllowedUserIDs[user] {
		b.log.Warn("unauthorised telegram approval click", "user", user)
		answer("⛔ You are not authorised to approve agent actions.")
		return
	}

	who := cq.From.FirstName
	if cq.From.Username != "" {
		who = "@" + cq.From.Username
	}
	var approve bool
	var value string
	switch {
	case strings.HasPrefix(cq.Data, callbackChoice):
		b.finishButton(cq, answer, b.core.ResolveChoice(strings.TrimPrefix(cq.Data, callbackChoice), who))
		return
	case strings.HasPrefix(cq.Data, callbackPick):
		b.pickProject(ctx, cq, strings.TrimPrefix(cq.Data, callbackPick), answer)
		return
	case strings.HasPrefix(cq.Data, callbackApprove):
		approve, value = true, strings.TrimPrefix(cq.Data, callbackApprove)
	case strings.HasPrefix(cq.Data, callbackReject):
		value = strings.TrimPrefix(cq.Data, callbackReject)
	default:
		answer("")
		return
	}

	b.finishButton(cq, answer, b.core.ResolveApproval(value, approve, who))
}

// finishButton reports a click's outcome and replaces the buttons with it, so
// the prompt can't be answered twice.
func (b *Bridge) finishButton(cq *models.CallbackQuery, answer func(string), result string) {
	b.log.Info("telegram button click", "user", cq.From.ID, "result", result)
	answer(result)
	if msg := cq.Message.Message; msg != nil {
		text := html.EscapeString(msg.Text) + "\n\n<b>" + html.EscapeString(result) + "</b>"
		if err := b.poster.replace(msg.Chat.ID, msg.ID, text); err != nil {
			b.log.Warn("could not update approval message", "err", err)
		}
	}
}
