package slackbridge

import (
	"context"
	"regexp"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
)

// announcedSessionRe matches the core's "Session `<id>` started|resumed" posts.
var announcedSessionRe = regexp.MustCompile("Session `([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})` (?:started|resumed)")

// slackConversation is a channel (threadTS "") or a thread; implements chatcore.Conversation.
type slackConversation struct {
	b         *Bridge
	channel   string
	threadTS  string
	messageTS string // the incoming message; a top-level message's thread hangs under it
}

func (c *slackConversation) ChatID() string   { return c.channel }
func (c *slackConversation) ThreadID() string { return c.threadTS }

// Reply posts Markdown in the thread (top-level: in a thread under the message).
// The text fallback keeps the Markdown so thread history stays searchable.
func (c *slackConversation) Reply(markdown string) {
	ts := c.threadTS
	if ts == "" {
		ts = c.messageTS
	}
	c.b.poster.post(c.channel, ts, slack.MsgOptionText(markdown, false),
		slack.MsgOptionBlocks(slack.NewMarkdownBlock("", markdown)))
}

// OpenThread uses the Slack thread under the incoming top-level message.
func (c *slackConversation) OpenThread(context.Context, string) (chatcore.Conversation, error) {
	return &slackConversation{b: c.b, channel: c.channel, threadTS: c.messageTS, messageTS: c.messageTS}, nil
}

func (c *slackConversation) NewSink() session.Sink {
	return &threadSink{poster: c.b.poster, channel: c.channel, threadTS: c.threadTS}
}

func (c *slackConversation) ThreadLink(s *session.Session) string {
	link, err := c.b.api.GetPermalink(&slack.PermalinkParameters{Channel: s.ChannelID, Ts: s.ThreadTS})
	if err != nil {
		return ""
	}
	return link
}

// LastSessionID scans the thread for the newest session ID the bot announced
// (covers threads from before the session directory existed).
func (c *slackConversation) LastSessionID(ctx context.Context) (string, error) {
	params := &slack.GetConversationRepliesParameters{ChannelID: c.channel, Timestamp: c.threadTS, Limit: 200}
	var last string
	for {
		msgs, hasMore, cursor, err := c.b.api.GetConversationRepliesContext(ctx, params)
		if err != nil {
			return "", err
		}
		for _, msg := range msgs {
			if msg.User != c.b.botUserID {
				continue // only trust this bot's own announcements
			}
			if match := announcedSessionRe.FindStringSubmatch(msg.Text); match != nil {
				last = match[1]
			}
		}
		if !hasMore || cursor == "" {
			return last, nil
		}
		params.Cursor = cursor
	}
}
