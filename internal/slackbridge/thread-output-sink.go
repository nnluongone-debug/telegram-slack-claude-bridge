package slackbridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
)

const (
	maxScreenBytes  = 3500 // keeps a rendered screen well under Slack's ~4000-char message guidance
	postTimeout     = 15 * time.Second
	maxPostAttempts = 3
	// chat.update allows ~50/min per workspace; one shared gap keeps all live views under it.
	minUpdateGap = 1300 * time.Millisecond
)

// errThrottled means a live-view edit was skipped to respect rate limits; the
// session re-sends the latest screen on its next tick.
var errThrottled = errors.New("slack update throttled")

// messagePoster wraps Slack writes with timeouts, rate-limit retries and a
// workspace-wide throttle for message edits.
type messagePoster struct {
	api *slack.Client
	log *slog.Logger

	mu         sync.Mutex
	nextUpdate time.Time
}

// post sends a thread reply and returns its timestamp ("" on failure).
// Link previews are off: Slack fetching a URL the agent printed would let a
// prompt-injected agent leak data in the URL.
func (p *messagePoster) post(channel, threadTS string, opts ...slack.MsgOption) string {
	var ts string
	opts = append(opts, slack.MsgOptionTS(threadTS), slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl())
	p.withRetry("post", func(ctx context.Context) (err error) {
		_, ts, err = p.api.PostMessageContext(ctx, channel, opts...)
		return err
	})
	return ts
}

// update edits an existing message. It never waits: when throttled it returns
// errThrottled so the caller can drop this frame.
func (p *messagePoster) update(channel, ts string, opts ...slack.MsgOption) error {
	p.mu.Lock()
	if time.Now().Before(p.nextUpdate) {
		p.mu.Unlock()
		return errThrottled
	}
	p.nextUpdate = time.Now().Add(minUpdateGap)
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), postTimeout)
	defer cancel()
	_, _, _, err := p.api.UpdateMessageContext(ctx, channel, ts, opts...)
	var rl *slack.RateLimitedError
	if errors.As(err, &rl) {
		p.mu.Lock()
		p.nextUpdate = time.Now().Add(rl.RetryAfter)
		p.mu.Unlock()
		return errThrottled
	}
	if err != nil {
		p.log.Error("slack update failed", "channel", channel, "err", err)
	}
	return err
}

func (p *messagePoster) withRetry(op string, call func(ctx context.Context) error) bool {
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), postTimeout)
		err := call(ctx)
		cancel()
		if err == nil {
			return true
		}
		var rl *slack.RateLimitedError
		if errors.As(err, &rl) && attempt < maxPostAttempts {
			time.Sleep(rl.RetryAfter)
			continue
		}
		p.log.Error("slack "+op+" failed", "attempt", attempt, "err", err)
		return false
	}
}

func (p *messagePoster) text(channel, threadTS, text string) {
	p.post(channel, threadTS, slack.MsgOptionText(text, false))
}

func (p *messagePoster) ephemeral(ctx context.Context, channel, user, text string) {
	if _, err := p.api.PostEphemeralContext(ctx, channel, user, slack.MsgOptionText(text, false)); err != nil {
		p.log.Error("post ephemeral failed", "channel", channel, "err", err)
	}
}

// threadSink mirrors one session's screen into its Slack thread as a "live"
// message that is edited in place, starting a new one each turn.
// Implements session.Sink; calls are serialised by the session.
type threadSink struct {
	poster   *messagePoster
	channel  string
	threadTS string
	liveTS   string

	statusTS   string // live "Claude is working" message (markdown mode)
	statusBody string
	lastTool   string // latest tool call, shown in the status
}

func (t *threadSink) Screen(_ *session.Session, text string, fresh bool) bool {
	body := slack.MsgOptionText(codeBlock(tailText(text, maxScreenBytes)), false)
	if !fresh && t.liveTS != "" {
		err := t.poster.update(t.channel, t.liveTS, body)
		if err == nil || errors.Is(err, errThrottled) {
			return err == nil
		}
		// Any other failure (e.g. message deleted): fall through and start a new live message.
	}
	ts := t.poster.post(t.channel, t.threadTS, body)
	if ts == "" {
		return false
	}
	t.liveTS = ts
	return true
}

func (t *threadSink) ApprovalRequested(s *session.Session, seq uint64, prompt terminal.ApprovalPrompt) bool {
	return t.poster.post(t.channel, t.threadTS,
		slack.MsgOptionText("Claude is asking for confirmation", false),
		slack.MsgOptionBlocks(approvalBlocks(s.ID, seq, prompt)...)) != ""
}

func (t *threadSink) Exited(s *session.Session, err error) {
	t.clearStatus()
	msg := fmt.Sprintf(":checkered_flag: Session `%s` ended.", s.ID)
	if err != nil {
		msg = fmt.Sprintf(":checkered_flag: Session `%s` ended: `%v`.", s.ID, err)
	}
	t.poster.text(t.channel, t.threadTS, msg+" `!resume` to continue it, or `!restart [prompt]` for a new one.")
}

// tailText keeps the last limit bytes on a line boundary: the bottom of the
// screen holds the newest output and any prompt.
func tailText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := text[len(text)-limit:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 {
		cut = cut[i+1:]
	}
	return "…\n" + strings.ToValidUTF8(cut, "")
}

// codeBlock wraps text in a Slack code block, escaping Slack control characters.
func codeBlock(text string) string {
	return "```\n" + escapeSlack(strings.ReplaceAll(text, "```", "ʼʼʼ")) + "\n```"
}

func escapeSlack(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}
