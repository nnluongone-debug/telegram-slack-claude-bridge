package telegrambridge

import (
	"errors"
	"fmt"
	"html"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

const (
	maxScreenChars = 3500
	maxRecentChars = 1500
	maxToolLines   = 15
	maxButtonRunes = 60
)

// topicSink streams one session into its forum topic (implements session.Sink).
// Calls are serialised by the session.
type topicSink struct {
	poster   *poster
	chatID   int64
	threadID int

	liveID     int // OUTPUT_MODE=screen mirror message
	statusID   int // live "Claude is working" message
	statusText string
	lastTool   string
}

func (t *topicSink) Screen(_ *session.Session, text string, fresh bool) bool {
	body := pre(tail(text, maxScreenChars))
	if !fresh && t.liveID != 0 {
		err := t.poster.edit(t.chatID, t.liveID, body, nil)
		if err == nil || errors.Is(err, errThrottled) {
			return err == nil
		}
	}
	t.liveID = t.poster.send(t.chatID, t.threadID, body, nil)
	return t.liveID != 0
}

func (t *topicSink) ApprovalRequested(s *session.Session, seq uint64, prompt terminal.ApprovalPrompt) bool {
	if prompt.Kind == terminal.PromptChoice {
		return t.askChoice(s, seq, prompt)
	}
	approve, reject := prompt.Labels()
	approve, reject = "✅ "+approve, "🚫 "+reject
	value := chatcore.ApprovalValue(s.ID, seq)
	keyboard := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: approve, CallbackData: callbackApprove + value},
		{Text: reject, CallbackData: callbackReject + value},
	}}}
	text := "✋ <b>Claude is asking for confirmation</b>\n" + pre(tail(prompt.Context, maxScreenChars))
	return t.poster.send(t.chatID, t.threadID, text, keyboard) != 0
}

// askChoice shows a question (e.g. Claude's AskUserQuestion) with one button per option.
func (t *topicSink) askChoice(s *session.Session, seq uint64, prompt terminal.ApprovalPrompt) bool {
	var rows [][]models.InlineKeyboardButton
	for _, o := range prompt.Options {
		rows = append(rows, []models.InlineKeyboardButton{{
			Text: truncateRunes(o.Number+". "+o.Label, maxButtonRunes), CallbackData: callbackChoice + chatcore.ChoiceValue(s.ID, seq, o.Number),
		}})
	}
	text := "❓ <b>" + html.EscapeString(prompt.Question) + "</b>"
	if prompt.Context != "" && prompt.Context != prompt.Question {
		text += "\n" + pre(tail(prompt.Context, maxScreenChars))
	}
	if _, ok := prompt.OwnAnswerOption(); ok {
		text += "\n✍️ <i>Or reply in this topic with your own answer.</i>"
	}
	return t.poster.send(t.chatID, t.threadID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows}) != 0
}

func (t *topicSink) Transcript(_ *session.Session, events []transcript.Event) {
	var tools []string
	flushTools := func() {
		if len(tools) > maxToolLines {
			tools = append(tools[:maxToolLines], fmt.Sprintf("… +%d more", len(tools)-maxToolLines))
		}
		if len(tools) > 0 {
			t.poster.send(t.chatID, t.threadID, strings.Join(tools, "\n"), nil)
			tools = nil
		}
	}
	for _, e := range events {
		switch e.Kind {
		case transcript.EventTool:
			t.lastTool = toolLine(e)
			tools = append(tools, t.lastTool)
		case transcript.EventText:
			flushTools()
			for _, msg := range markdownToTelegramHTML(e.Text) {
				t.poster.send(t.chatID, t.threadID, msg, nil)
			}
		case transcript.EventNotice:
			flushTools()
			t.poster.send(t.chatID, t.threadID, "ℹ️ "+html.EscapeString(e.Text), nil)
		}
	}
	flushTools()
	t.moveStatusToBottom()
}

// Status keeps one "working" message at the bottom of the topic while Claude is busy.
func (t *topicSink) Status(_ *session.Session, activity, recent string, busy bool) bool {
	if !busy {
		t.clearStatus()
		return true
	}
	t.statusText = "⏳ <i>" + html.EscapeString(activity) + "</i>"
	if t.lastTool != "" {
		t.statusText += "\n" + t.lastTool
	}
	if recent = strings.TrimSpace(recent); recent != "" {
		t.statusText += "\n" + pre(tail(recent, maxRecentChars))
	}
	if t.statusID != 0 {
		err := t.poster.edit(t.chatID, t.statusID, t.statusText, nil)
		if err == nil || errors.Is(err, errThrottled) {
			return err == nil
		}
	}
	t.statusID = t.poster.send(t.chatID, t.threadID, t.statusText, nil)
	return t.statusID != 0
}

func (t *topicSink) moveStatusToBottom() {
	if t.statusID == 0 {
		return
	}
	t.poster.delete(t.chatID, t.statusID)
	t.statusID = t.poster.send(t.chatID, t.threadID, t.statusText, nil)
}

func (t *topicSink) clearStatus() {
	if t.statusID != 0 {
		t.poster.delete(t.chatID, t.statusID)
		t.statusID, t.statusText = 0, ""
	}
}

func (t *topicSink) Exited(s *session.Session, err error) {
	t.clearStatus()
	msg := fmt.Sprintf("🏁 Session <code>%s</code> ended", s.ID)
	if err != nil {
		msg += ": <code>" + html.EscapeString(err.Error()) + "</code>"
	}
	t.poster.send(t.chatID, t.threadID, msg+". <code>!resume</code> to continue it, or <code>!restart [prompt]</code> for a new one.", nil)
}

// toolLine renders one tool call, e.g. "🔧 <b>Bash</b> <code>git status</code>".
func toolLine(e transcript.Event) string {
	line := "🔧 <b>" + html.EscapeString(e.Text) + "</b>"
	if e.Detail != "" {
		line += " <code>" + html.EscapeString(e.Detail) + "</code>"
	}
	return line
}

func pre(text string) string { return "<pre>" + html.EscapeString(text) + "</pre>" }

// tail keeps the last limit bytes of text on a line boundary (the newest output).
func tail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := text[len(text)-limit:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 {
		cut = cut[i+1:]
	}
	return "…\n" + strings.ToValidUTF8(cut, "")
}
