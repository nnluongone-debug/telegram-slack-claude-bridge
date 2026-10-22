package slackbridge

import (
	"context"
	"errors"
	"strings"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
)

const maxStatusRecentBytes = 1500

// Status keeps one "Claude is working" message at the bottom of the thread,
// edited in place while busy and deleted when Claude is done (implements session.Sink).
func (t *threadSink) Status(_ *session.Session, activity, recent string, busy bool) bool {
	if !busy {
		t.clearStatus()
		return true
	}
	t.statusBody = statusText(activity, recent, t.lastTool)
	body := slack.MsgOptionText(t.statusBody, false)
	if t.statusTS != "" {
		err := t.poster.update(t.channel, t.statusTS, body)
		if err == nil || errors.Is(err, errThrottled) {
			return err == nil // throttled: the session retries next tick
		}
		t.statusTS = "" // e.g. deleted by someone: post a fresh one
	}
	t.statusTS = t.poster.post(t.channel, t.threadTS, body)
	return t.statusTS != ""
}

// moveStatusToBottom re-posts the status after new replies so it stays last.
func (t *threadSink) moveStatusToBottom() {
	if t.statusTS == "" {
		return
	}
	t.poster.delete(t.channel, t.statusTS)
	t.statusTS = t.poster.post(t.channel, t.threadTS, slack.MsgOptionText(t.statusBody, false))
}

func (t *threadSink) clearStatus() {
	if t.statusTS != "" {
		t.poster.delete(t.channel, t.statusTS)
		t.statusTS, t.statusBody = "", ""
	}
}

// statusText renders e.g.
//
//	⏳ Flummoxing… (running hook · 10s · ↓ 88 tokens)
//	🔧 Bash `git log`
//	```<latest screen rows>```
func statusText(activity, recent, lastTool string) string {
	var sb strings.Builder
	sb.WriteString(":hourglass_flowing_sand: _" + escapeSlack(activity) + "_")
	if lastTool != "" {
		sb.WriteString("\n" + lastTool)
	}
	if recent = strings.TrimSpace(recent); recent != "" {
		sb.WriteString("\n" + codeBlock(tailText(recent, maxStatusRecentBytes)))
	}
	return sb.String()
}

func (p *messagePoster) delete(channel, ts string) {
	ctx, cancel := context.WithTimeout(context.Background(), postTimeout)
	defer cancel()
	if _, _, err := p.api.DeleteMessageContext(ctx, channel, ts); err != nil {
		p.log.Warn("delete status message failed", "channel", channel, "err", err)
	}
}
