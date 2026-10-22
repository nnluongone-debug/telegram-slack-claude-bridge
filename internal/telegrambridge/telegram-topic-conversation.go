package telegrambridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
)

const maxTopicNameRunes = 60 // Telegram allows 128; short names read better in the topic list

// topicConversation is a forum group (threadID 0 = General) or one of its
// topics; implements chatcore.Conversation.
type topicConversation struct {
	b        *Bridge
	chatID   int64
	threadID int
}

func (c *topicConversation) ChatID() string { return strconv.FormatInt(c.chatID, 10) }

func (c *topicConversation) ThreadID() string {
	if c.threadID == 0 {
		return ""
	}
	return strconv.Itoa(c.threadID)
}

func (c *topicConversation) Reply(markdown string) {
	for _, msg := range markdownToTelegramHTML(markdown) {
		c.b.poster.send(c.chatID, c.threadID, msg, nil)
	}
}

// OpenThread creates a forum topic named after the prompt.
func (c *topicConversation) OpenThread(ctx context.Context, title string) (chatcore.Conversation, error) {
	topic, err := c.b.bot.CreateForumTopic(ctx, &bot.CreateForumTopicParams{ChatID: c.chatID, Name: topicName(title)})
	if err != nil {
		return nil, fmt.Errorf("%w (enable Topics in the group and make the bot an admin with Manage Topics)", err)
	}
	return &topicConversation{b: c.b, chatID: c.chatID, threadID: topic.MessageThreadID}, nil
}

func (c *topicConversation) NewSink() session.Sink {
	return &topicSink{poster: c.b.poster, chatID: c.chatID, threadID: c.threadID}
}

// LastSessionID: bots cannot read chat history; the session directory covers Telegram.
func (c *topicConversation) LastSessionID(context.Context) (string, error) { return "", nil }

// ThreadLink links to a topic of a supergroup (chat IDs look like -100<internal id>).
func (c *topicConversation) ThreadLink(s *session.Session) string {
	internal, ok := strings.CutPrefix(s.ChannelID, "-100")
	if !ok || s.ThreadTS == "" {
		return ""
	}
	return fmt.Sprintf("https://t.me/c/%s/%s", internal, s.ThreadTS)
}

// topicName is "🤖 " plus the first line of the title ("[project] prompt").
func topicName(prompt string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(prompt), "\n")
	if r := []rune(line); len(r) > maxTopicNameRunes {
		line = string(r[:maxTopicNameRunes]) + "…"
	}
	if line == "" {
		line = "session"
	}
	return "🤖 " + line
}
