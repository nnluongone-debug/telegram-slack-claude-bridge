package slackbridge

import (
	"context"
	"html"
	"regexp"
	"strings"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
)

type incomingMessage struct {
	channel, user, text, ts, threadTS string
	mentioned                         bool
	files                             []slack.File
}

// isTopLevel is true for channel messages (Slack sets thread_ts == ts on thread parents).
func (m incomingMessage) isTopLevel() bool { return m.threadTS == "" || m.threadTS == m.ts }

// replyTS is where answers go: the existing thread, or a new thread under the message.
func (m incomingMessage) replyTS() string {
	if m.isTopLevel() {
		return m.ts
	}
	return m.threadTS
}

func (b *Bridge) handleUserMessage(ctx context.Context, m incomingMessage) {
	if !b.cfg.AllowedUserIDs[m.user] {
		b.log.Warn("ignoring message from unauthorised user", "user", m.user, "channel", m.channel)
		if m.mentioned {
			b.poster.ephemeral(ctx, m.channel, m.user, ":no_entry: You are not authorised to control agents.")
		}
		return
	}
	// The same message can arrive twice: as message + app_mention, or redelivered after a reconnect.
	if !b.seen.FirstSeen(m.channel + ":" + m.ts) {
		return
	}

	conv := &slackConversation{b: b, channel: m.channel, messageTS: m.ts}
	if !m.isTopLevel() {
		conv.threadTS = m.threadTS
	}
	project, ok := b.cfg.Projects.Lookup(m.channel)
	if !ok {
		if m.mentioned {
			conv.Reply("⚠️ This channel is not mapped to a project in projects.json.")
		}
		return
	}
	text := normalizeSlackText(strings.ReplaceAll(m.text, b.mentionTag(), ""))
	if len(m.files) > 0 && !strings.HasPrefix(text, "!") { // commands ignore attachments
		text = chatcore.WithAttachments(text, b.saveAttachments(ctx, conv, project, m.files))
	}
	if text == "" {
		return
	}
	if project.IsMulti() {
		b.core.HandleMultiMessage(ctx, conv, project, b.cfg.Projects.Members(project), text, b.askProject(conv))
		return
	}
	b.core.HandleMessage(ctx, conv, project, text)
}

// slackLinkRe matches Slack link markup: <https://x.y|label>, <mailto:a@b.c>.
var slackLinkRe = regexp.MustCompile(`<((?:https?|mailto):[^|>]+)(?:\|[^>]*)?>`)

// normalizeSlackText turns Slack markup into what the user typed: links become
// plain URLs and &amp;/&lt;/&gt; escaping is undone.
func normalizeSlackText(text string) string {
	text = slackLinkRe.ReplaceAllString(text, "$1")
	return strings.TrimSpace(html.UnescapeString(text))
}

func shortID(id string) string { return id[:min(8, len(id))] }
