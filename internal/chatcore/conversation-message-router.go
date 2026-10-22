// Package chatcore is the platform-neutral heart of the bridge: it turns chat
// messages and `!commands` into agent-session actions. Chat adapters (Slack,
// Telegram) supply a Conversation; every reply is standard Markdown that the
// adapter renders for its platform.
package chatcore

import (
	"context"
	"log/slog"
	"strings"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
)

// Conversation is the place a message arrived: a Slack channel/thread or a
// Telegram group/topic.
type Conversation interface {
	ChatID() string   // project key in projects.json (Slack channel ID, Telegram chat ID)
	ThreadID() string // "" for a top-level message (channel, Telegram "General" topic)
	Reply(markdown string)
	// OpenThread returns the thread a new session lives in: Slack uses the
	// thread under the incoming message, Telegram creates a forum topic.
	OpenThread(ctx context.Context, title string) (Conversation, error)
	NewSink() session.Sink
	// LastSessionID finds a session announced in this thread by other means
	// than the session directory (Slack: thread history). "" if unknown.
	LastSessionID(ctx context.Context) (string, error)
	ThreadLink(s *session.Session) string // link for listings; "" if none
}

// Core routes messages to agent sessions. Safe for concurrent use.
type Core struct {
	sessions  *session.Manager
	directory *SessionDirectory
	log       *slog.Logger
	threads   KeyedMutex    // one message per thread at a time keeps input ordered
	pending   pendingStarts // prompts waiting for a project choice
}

// New creates the shared message router.
func New(sessions *session.Manager, directory *SessionDirectory, log *slog.Logger) *Core {
	return &Core{sessions: sessions, directory: directory, log: log}
}

// Sessions exposes the session manager to adapters (button callbacks).
func (c *Core) Sessions() *session.Manager { return c.sessions }

// Directory exposes the thread→session/project records to adapters.
func (c *Core) Directory() *SessionDirectory { return c.directory }

// StartSession opens a new thread under a top-level conversation and starts a
// session there, e.g. after the user picked a project from buttons.
func (c *Core) StartSession(ctx context.Context, conv Conversation, project config.Project, prompt string) {
	unlock := c.threads.Lock(threadKey(conv))
	defer unlock()
	c.startInNewThread(ctx, conv, project, prompt)
}

// HandleMessage processes one authorised message in a mapped project chat.
// Text starting with "!" is a command; a top-level message starts a session;
// anything else is typed into the thread's session.
func (c *Core) HandleMessage(ctx context.Context, conv Conversation, project config.Project, text string) {
	unlock := c.threads.Lock(threadKey(conv))
	defer unlock()

	text = strings.TrimSpace(text)
	switch {
	case text == "":
	case strings.HasPrefix(text, "!"):
		c.runCommand(ctx, conv, project, text)
	case conv.ThreadID() == "":
		c.startInNewThread(ctx, conv, project, text)
	default:
		s, ok := c.sessions.ByThread(conv.ChatID(), conv.ThreadID())
		if !ok {
			conv.Reply("💤 No live session here. `!resume` continues the last one, `!restart [prompt]` starts a new one.")
			return
		}
		if err := s.SendText(text); err != nil {
			conv.Reply("❌ Failed to send input: " + err.Error())
		}
	}
}

func (c *Core) startInNewThread(ctx context.Context, conv Conversation, project config.Project, prompt string) {
	thread, err := conv.OpenThread(ctx, threadTitle(project, prompt))
	if err != nil {
		conv.Reply("❌ Could not open a thread for the session: " + err.Error())
		return
	}
	unlock := c.threads.Lock(threadKey(thread)) // replies in the new thread wait until the session exists
	defer unlock()
	c.launch(thread, project, "started", func(sink session.Sink) (*session.Session, error) {
		return c.sessions.Start(thread.ChatID(), thread.ThreadID(), project, prompt, sink)
	})
}

func threadKey(conv Conversation) string { return conv.ChatID() + ":" + conv.ThreadID() }

// threadTitle names a new thread/topic, e.g. "[web-app] fix the tests".
func threadTitle(project config.Project, text string) string {
	return "[" + project.Name + "] " + strings.TrimSpace(text)
}
