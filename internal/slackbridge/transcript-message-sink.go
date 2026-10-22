package slackbridge

import (
	"fmt"
	"strings"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

const (
	maxToolLines    = 15
	maxFallbackText = 300 // notification preview text for block messages
)

// Transcript posts Claude's replies as rendered markdown and groups
// consecutive tool calls into one compact grey line (implements session.Sink).
func (t *threadSink) Transcript(_ *session.Session, events []transcript.Event) {
	var tools []transcript.Event
	flushTools := func() {
		if len(tools) > 0 {
			t.poster.post(t.channel, t.threadTS,
				slack.MsgOptionText(fmt.Sprintf("%d tool call(s)", len(tools)), false),
				slack.MsgOptionBlocks(toolSummaryBlock(tools)))
			tools = nil
		}
	}

	for _, e := range events {
		switch e.Kind {
		case transcript.EventTool:
			tools = append(tools, e)
			t.lastTool = toolLine(e)
		case transcript.EventText:
			flushTools()
			t.postMarkdown(e.Text)
		case transcript.EventNotice:
			flushTools()
			t.poster.text(t.channel, t.threadTS, ":information_source: "+e.Text)
		}
	}
	flushTools()
	t.moveStatusToBottom()
}

// postMarkdown posts a reply as markdown/table blocks; if Slack rejects the
// blocks, the raw text is posted instead so the reply is never lost.
func (t *threadSink) postMarkdown(md string) {
	for _, blocks := range markdownMessages(md) {
		preview := slack.MsgOptionText(escapeSlack(truncateRunes(md, maxFallbackText)), false)
		if t.poster.post(t.channel, t.threadTS, preview, slack.MsgOptionBlocks(blocks...)) == "" {
			t.poster.post(t.channel, t.threadTS, slack.MsgOptionText(escapeSlack(md), false))
			return
		}
	}
}

// toolSummaryBlock renders tool calls as small context text, e.g. "🔧 Bash `git status`".
func toolSummaryBlock(tools []transcript.Event) slack.Block {
	var lines []string
	for i, e := range tools {
		if i == maxToolLines {
			lines = append(lines, fmt.Sprintf("… +%d more", len(tools)-maxToolLines))
			break
		}
		lines = append(lines, toolLine(e))
	}
	return slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, strings.Join(lines, "\n"), false, false))
}

// toolLine renders one tool call, e.g. ":wrench: *Bash* `git status`".
func toolLine(e transcript.Event) string {
	line := ":wrench: *" + escapeSlack(e.Text) + "*"
	if e.Detail != "" {
		line += " `" + escapeSlack(strings.ReplaceAll(e.Detail, "`", "ʼ")) + "`"
	}
	return line
}

func truncateRunes(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}
