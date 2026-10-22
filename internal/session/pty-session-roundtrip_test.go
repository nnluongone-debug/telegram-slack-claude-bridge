//go:build !windows

package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

// recordingSink collects what a real PTY session emits.
type recordingSink struct {
	mu       sync.Mutex
	output   strings.Builder
	approval chan uint64
	prompts  chan terminal.ApprovalPrompt
	exited   chan error
}

func newRecordingSink() *recordingSink {
	return &recordingSink{approval: make(chan uint64, 4), prompts: make(chan terminal.ApprovalPrompt, 4), exited: make(chan error, 1)}
}

func (r *recordingSink) Screen(_ *Session, text string, _ bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output.WriteString(text + "\n")
	return true
}

func (r *recordingSink) ApprovalRequested(_ *Session, seq uint64, p terminal.ApprovalPrompt) bool {
	r.approval <- seq
	select {
	case r.prompts <- p:
	default:
	}
	return true
}

func (r *recordingSink) Transcript(_ *Session, events []transcript.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range events {
		r.output.WriteString(fmt.Sprintf("[%d] %s %s\n", e.Kind, e.Text, e.Detail))
	}
}

func (r *recordingSink) Status(_ *Session, activity, _ string, busy bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output.WriteString(fmt.Sprintf("[status busy=%v] %s\n", busy, activity))
	return true
}

func (r *recordingSink) Exited(_ *Session, err error) { r.exited <- err }

func (r *recordingSink) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.output.String()
}

func newShellManager(script string) *Manager {
	return NewManager(Options{
		Bin: "/bin/sh", Args: []string{"-c", script},
		Cols: 80, Rows: 24, FlushInterval: 50 * time.Millisecond, MirrorScreen: true, PlainCLI: true,
	}, 2)
}

func TestApprovalRoundTripThroughRealPTY(t *testing.T) {
	m := newShellManager(`printf 'Continue? (y/n) '; read a; echo "answer=$a"; read b; echo "typed=$b"`)
	sink := newRecordingSink()
	s, err := m.Start("C1", "1.0", config.Project{Name: "tmp", WorkDir: t.TempDir()}, "", sink)
	if err != nil {
		t.Fatal(err)
	}

	var seq uint64
	select {
	case seq = <-sink.approval:
	case <-time.After(5 * time.Second):
		t.Fatal("approval prompt was not detected")
	}
	if err := s.RespondApproval(seq+1, true); !errors.Is(err, ErrStaleApproval) {
		t.Fatalf("wrong seq should be stale, got %v", err)
	}
	if err := s.RespondApproval(seq, true); err != nil {
		t.Fatal(err)
	}
	if err := s.RespondApproval(seq, true); !errors.Is(err, ErrStaleApproval) {
		t.Fatalf("double answer should be stale, got %v", err)
	}
	if err := s.SendText("hello from slack"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-sink.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
	out := sink.text()
	if !strings.Contains(out, "answer=y") || !strings.Contains(out, "typed=hello from slack") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if _, ok := m.ByID(s.ID); ok {
		t.Fatal("exited session should be removed from manager")
	}
}

func TestManagerLimitsAndStop(t *testing.T) {
	m := newShellManager("sleep 30")
	project := config.Project{Name: "tmp", WorkDir: t.TempDir()}

	a, err := m.Start("C1", "1.0", project, "", newRecordingSink())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start("C1", "1.0", project, "", newRecordingSink()); !errors.Is(err, ErrSessionExists) {
		t.Fatalf("expected ErrSessionExists, got %v", err)
	}
	if _, err := m.Start("C1", "2.0", project, "", newRecordingSink()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start("C1", "3.0", project, "", newRecordingSink()); !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("expected ErrTooManySessions, got %v", err)
	}

	if got, ok := m.ByThread("C1", "1.0"); !ok || got != a {
		t.Fatal("ByThread lookup failed")
	}

	done := make(chan struct{})
	go func() { m.StopAll(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("StopAll did not terminate sessions")
	}
	if n := len(m.List("")); n != 0 {
		t.Fatalf("expected no sessions after StopAll, got %d", n)
	}
}

// Two Claude-style menus back to back: the second must get its own buttons
// immediately (no cool-off delay), and answering by typing must make every
// earlier button stale.
func TestConsecutiveMenusEachGetFreshButtons(t *testing.T) {
	menu := func(q string) string {
		return `printf '\033[2J\033[H` + q + `\n❯ 1. Yes\n  2. No\nEsc to cancel\n'; `
	}
	script := menu("Do you want to proceed?") + `read -rsn1 a; ` +
		menu("Do you want to make this edit?") + `read -rsn1 b; ` +
		`printf '\033[2J\033[Ha=%s b=%s\n' "$a" "$b"; sleep 1`
	m := NewManager(Options{Bin: "/bin/bash", Args: []string{"-c", script}, Cols: 80, Rows: 24, FlushInterval: 50 * time.Millisecond, MirrorScreen: true, PlainCLI: true}, 1)
	sink := newRecordingSink()
	s, err := m.Start("C1", "1.0", config.Project{Name: "tmp", WorkDir: t.TempDir()}, "", sink)
	if err != nil {
		t.Fatal(err)
	}
	waitSeq := func() uint64 {
		select {
		case seq := <-sink.approval:
			return seq
		case <-time.After(approvalCooloff - time.Second): // must beat the cool-off
			t.Fatalf("prompt not raised in time; output:\n%s", sink.text())
			return 0
		}
	}

	first := waitSeq()
	if err := s.RespondApproval(first, true); err != nil {
		t.Fatal(err)
	}
	second := waitSeq()
	if second == first {
		t.Fatalf("second menu reused seq %d", first)
	}
	if err := s.SendKeys([]byte("1")); err != nil { // answered in the terminal, not via button
		t.Fatal(err)
	}
	if err := s.RespondApproval(second, true); !errors.Is(err, ErrStaleApproval) {
		t.Fatalf("button for an answered prompt must be stale, got %v", err)
	}

	select {
	case <-sink.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
	if out := sink.text(); !strings.Contains(out, "a=1 b=1") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

// The session ID doubles as Claude's --session-id; Resume passes --resume <id>
// and refuses invalid IDs or one already open in another thread.
func TestResumePassesSessionIDsToClaude(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // no real transcripts
	m := NewManager(Options{Bin: "/bin/sh", Args: []string{"-c", `echo "args: $*"; sleep 2`, "sh"},
		Cols: 80, Rows: 24, FlushInterval: 50 * time.Millisecond, MirrorScreen: true, PlainCLI: true}, 3)
	project := config.Project{Name: "tmp", WorkDir: t.TempDir()}

	newSink := newRecordingSink()
	fresh, err := m.Start("C1", "1.0", project, "", newSink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Resume("C1", "2.0", project, "not-a-uuid", nil, newRecordingSink()); err == nil {
		t.Fatal("invalid session ID must be rejected")
	}
	if _, err := m.Resume("C1", "2.0", project, fresh.ID, nil, newRecordingSink()); !errors.Is(err, ErrSessionOpenElsewhere) {
		t.Fatalf("expected ErrSessionOpenElsewhere, got %v", err)
	}
	resumeSink := newRecordingSink()
	const oldID = "0B1C2D3E-4F50-4617-8293-A4B5C6D7E8F9" // canonicalised to lower case
	if _, err := m.Resume("C1", "3.0", project, oldID, []string{"--model", "sonnet"}, resumeSink); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		sink *recordingSink
		want string
	}{{newSink, "args: --session-id " + fresh.ID}, {resumeSink, "args: --model sonnet --resume " + strings.ToLower(oldID)}} {
		select {
		case <-c.sink.exited:
		case <-time.After(5 * time.Second):
			t.Fatal("session did not exit")
		}
		if !strings.Contains(c.sink.text(), c.want) {
			t.Fatalf("want %q in output:\n%s", c.want, c.sink.text())
		}
	}
}

// A second session in a folder that already has one gets its own worktree
// (git repos only); "never" and non-git folders share the folder.
func TestSecondSessionInFolderGetsWorktree(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v %s", err, out)
	}
	m := NewManager(Options{Bin: "/bin/sh", Args: []string{"-c", `echo "args: $*"; sleep 5`, "sh"},
		Cols: 80, Rows: 24, FlushInterval: 50 * time.Millisecond, MirrorScreen: true, PlainCLI: true}, 5)
	defer m.StopAll()
	project := config.Project{Name: "repo", WorkDir: repo}

	first, err := m.Start("C1", "1", project, "", newRecordingSink())
	if err != nil || first.Worktree != "" {
		t.Fatalf("first session must use the folder itself: %v %q", err, first.Worktree)
	}
	sink := newRecordingSink()
	second, err := m.Start("C1", "2", project, "", sink)
	if err != nil || second.Worktree != "bridge-"+second.ID[:8] {
		t.Fatalf("second session must get a worktree: %v %q", err, second.Worktree)
	}
	if len(m.SharingFolder(second)) != 0 || len(m.SharingFolder(first)) != 0 {
		t.Fatal("sessions in separate worktrees don't share a folder")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(sink.text(), "--worktree "+second.Worktree) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(sink.text(), "args: --worktree "+second.Worktree+" --session-id") {
		t.Fatalf("claude args: %s", sink.text())
	}

	never := config.Project{Name: "shared", WorkDir: repo, Worktree: "never"}
	third, err := m.Start("C1", "3", never, "", newRecordingSink())
	if err != nil || third.Worktree != "" || len(m.SharingFolder(third)) != 1 {
		t.Fatalf(`"never" must share the folder: %v %q %d`, err, third.Worktree, len(m.SharingFolder(third)))
	}

	plain := config.Project{Name: "plain", WorkDir: t.TempDir()}
	m.Start("C2", "1", plain, "", newRecordingSink())
	fourth, _ := m.Start("C2", "2", plain, "", newRecordingSink())
	if fourth.Worktree != "" {
		t.Fatal("a non-git folder cannot have worktrees")
	}
}

// A session whose transcript lives under another project's folder is refused,
// so `!resume <id>` can't open one project's conversation in another channel.
func TestResumeRefusesSessionOfAnotherProject(t *testing.T) {
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	const id = "11111111-2222-3333-4444-555555555555"
	other := filepath.Join(claudeDir, "projects", "-somewhere-else")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, id+".jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(Options{Bin: "/bin/sh", Args: []string{"-c", "true", "sh"}, Cols: 80, Rows: 24,
		FlushInterval: 50 * time.Millisecond, MirrorScreen: true, PlainCLI: true}, 3)
	project := config.Project{Name: "tmp", WorkDir: t.TempDir()}
	if _, err := m.Resume("C1", "1.0", project, id, nil, newRecordingSink()); !errors.Is(err, ErrSessionOtherProject) {
		t.Fatalf("expected ErrSessionOtherProject, got %v", err)
	}
}
