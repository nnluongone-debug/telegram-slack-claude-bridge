//go:build !windows

package chatcore

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

// fakeConversation records replies; threads are numbered "t1", "t2"…
type fakeConversation struct {
	chat, thread string
	log          *replyLog
}

type replyLog struct {
	mu      sync.Mutex
	replies []string
	threads int
	screen  strings.Builder
}

func (r *replyLog) all() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.replies, "\n") + "\n" + r.screen.String()
}

func (c *fakeConversation) ChatID() string   { return c.chat }
func (c *fakeConversation) ThreadID() string { return c.thread }
func (c *fakeConversation) Reply(md string) {
	c.log.mu.Lock()
	defer c.log.mu.Unlock()
	c.log.replies = append(c.log.replies, "["+c.thread+"] "+md)
}
func (c *fakeConversation) OpenThread(context.Context, string) (Conversation, error) {
	c.log.mu.Lock()
	defer c.log.mu.Unlock()
	c.log.threads++
	return &fakeConversation{chat: c.chat, thread: "t" + string(rune('0'+c.log.threads)), log: c.log}, nil
}
func (c *fakeConversation) NewSink() session.Sink                         { return fakeSink{c.log} }
func (c *fakeConversation) LastSessionID(context.Context) (string, error) { return "", nil }
func (c *fakeConversation) ThreadLink(*session.Session) string            { return "" }

type fakeSink struct{ log *replyLog }

func (s fakeSink) Screen(_ *session.Session, text string, _ bool) bool {
	s.log.mu.Lock()
	defer s.log.mu.Unlock()
	s.log.screen.WriteString(text + "\n")
	return true
}
func (fakeSink) ApprovalRequested(*session.Session, uint64, terminal.ApprovalPrompt) bool {
	return true
}
func (fakeSink) Transcript(*session.Session, []transcript.Event)    {}
func (fakeSink) Status(*session.Session, string, string, bool) bool { return true }
func (fakeSink) Exited(*session.Session, error)                     {}

func waitContains(t *testing.T, log *replyLog, want string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if strings.Contains(log.all(), want) {
			return
		}
	}
	t.Fatalf("%q never appeared in:\n%s", want, log.all())
}

func TestTopLevelMessageStartsSessionInNewThreadAndResumes(t *testing.T) {
	// A shell stands in for claude: it echoes its args and each typed line.
	sessions := session.NewManager(session.Options{Bin: "/bin/sh", Args: []string{"-c", `echo "args: $*"; while read l; do echo "got: $l"; done`, "sh"},
		Cols: 80, Rows: 24, FlushInterval: 50 * time.Millisecond, MirrorScreen: true, PlainCLI: true}, 3)
	defer sessions.StopAll()
	core := New(sessions, OpenSessionDirectory(""), slog.New(slog.NewTextHandler(io.Discard, nil)))
	project := config.Project{Name: "demo", WorkDir: t.TempDir()}
	log := &replyLog{}
	top := &fakeConversation{chat: "-100", log: log}
	ctx := context.Background()

	core.HandleMessage(ctx, top, project, "fix the tests")
	waitContains(t, log, "[t1] 🚀 Session `")
	waitContains(t, log, "args: --session-id")

	thread := &fakeConversation{chat: "-100", thread: "t1", log: log}
	core.HandleMessage(ctx, thread, project, "hello agent")
	waitContains(t, log, "got: hello agent")

	s, ok := sessions.ByThread("-100", "t1")
	if !ok {
		t.Fatal("session not registered under the new thread")
	}
	core.HandleMessage(ctx, thread, project, "!stop")
	<-s.Done()
	core.HandleMessage(ctx, thread, project, "!resume") // found via the session directory
	waitContains(t, log, "args: --resume "+s.ID)

	core.HandleMessage(ctx, top, project, "!status")
	waitContains(t, log, "[] 💤 No live session here")
}
