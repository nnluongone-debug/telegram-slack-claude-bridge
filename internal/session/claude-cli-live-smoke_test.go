//go:build smoke

// Live check against the installed `claude` binary (uses your Claude account):
//
//	go test -tags smoke -run TestClaudeCLILiveApproval -v ./internal/session/
//
// Run it after upgrading Claude Code to confirm screen rendering and
// permission-menu detection still match the TUI.
package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
)

func TestClaudeCLILiveApproval(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(Options{
		Bin: "claude", Args: []string{"--permission-mode", "default"},
		Cols: 100, Rows: 40, FlushInterval: time.Second,
	}, 1)
	sink := newRecordingSink()
	s, err := m.Start("C", "1", config.Project{Name: "smoke", WorkDir: dir},
		"Use the Bash tool to run exactly: echo hi > hello.txt  then reply DONE.", sink)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Stop(); <-s.Done() }()

	select {
	case seq := <-sink.approval:
		if err := s.RespondApproval(seq, true); err != nil {
			t.Fatal(err)
		}
	case <-time.After(90 * time.Second):
		t.Fatalf("no permission prompt detected; last output:\n%s", sink.text())
	}

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(filepath.Join(dir, "hello.txt")); string(b) == "hi\n" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("approved command did not run; last output:\n%s", sink.text())
}

// A session started by the bridge can be stopped and resumed with its context intact,
// and replies arrive through the transcript relay (markdown mode).
func TestClaudeCLILiveResume(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(Options{Bin: "claude", Cols: 100, Rows: 40, FlushInterval: time.Second}, 1)
	project := config.Project{Name: "smoke", WorkDir: dir}

	first := newRecordingSink()
	s, err := m.Start("C", "1", project, "Remember the code word PINEAPPLE-42. Reply only: NOTED", first)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, first, "[1] NOTED", 90*time.Second) // [1] = transcript text event, not the echoed prompt
	// While working, a live status was reported; once idle it was cleared.
	cleared := func() bool {
		out := first.text()
		busy := strings.LastIndex(out, "[status busy=true]")
		return busy >= 0 && strings.LastIndex(out, "[status busy=false]") > busy
	}
	for deadline := time.Now().Add(30 * time.Second); !cleared() && time.Now().Before(deadline); {
		time.Sleep(time.Second)
	}
	if !cleared() {
		t.Fatalf("live status not reported/cleared:\n%s", first.text())
	}
	s.Stop()
	<-s.Done()

	second := newRecordingSink()
	r, err := m.Resume("C", "1", project, s.ID, nil, second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { r.Stop(); <-r.Done() }()
	if err := r.SendText("What was the code word? Reply with it only, prefixed by ANSWER:"); err != nil {
		t.Fatalf("%v; screen:\n%s", err, r.ScreenText())
	}
	waitFor(t, second, "[1] ANSWER: PINEAPPLE-42", 90*time.Second)
	if strings.Contains(second.text(), "[1] NOTED") {
		t.Fatal("resumed session must not replay old transcript entries")
	}
}

func waitFor(t *testing.T, sink *recordingSink, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(sink.text(), want) {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%q never appeared; output:\n%s", want, sink.text())
}

// !mode presses Shift+Tab until the status line matches; !model resumes with
// --model. Neither may touch the user's saved Claude settings.
func TestClaudeCLILiveModeAndModel(t *testing.T) {
	settingsPath := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	before, _ := os.ReadFile(settingsPath)

	m := NewManager(Options{Bin: "claude", Cols: 100, Rows: 40, FlushInterval: time.Second}, 1)
	project := config.Project{Name: "smoke", WorkDir: t.TempDir()}
	first := newRecordingSink()
	s, err := m.Start("C", "1", project, "Reply only: READY", first)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, first, "[1] READY", 90*time.Second)
	if got, err := s.SwitchPermissionMode("plan"); err != nil || got != "plan mode" {
		t.Fatalf("switch to plan: %q %v", got, err)
	}
	s.Stop()
	<-s.Done()

	second := newRecordingSink()
	r, err := m.Resume("C", "1", project, s.ID, []string{"--model", "sonnet"}, second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { r.Stop(); <-r.Done() }()
	if err := r.SendText("Reply only: SWITCHED"); err != nil {
		t.Fatalf("%v; screen:\n%s", err, r.ScreenText())
	}
	waitFor(t, second, "[1] SWITCHED", 90*time.Second)

	matches, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), ".claude", "projects", "*", s.ID+".jsonl"))
	if len(matches) != 1 {
		t.Fatalf("transcript for %s not found", s.ID)
	}
	raw, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(raw), `"requestedModel":"claude-sonnet`) {
		t.Fatal("resumed session did not use the requested model")
	}
	if after, _ := os.ReadFile(settingsPath); string(after) != string(before) {
		t.Fatal("~/.claude/settings.json was modified")
	}
}

// Claude's AskUserQuestion dialog is surfaced as a choice; picking options
// (question, then the "Submit answers" review page) reaches Claude.
func TestClaudeCLILiveAskUserQuestion(t *testing.T) {
	m := NewManager(Options{Bin: "claude", Cols: 100, Rows: 40, FlushInterval: time.Second}, 1)
	sink := newRecordingSink()
	s, err := m.Start("C", "1", config.Project{Name: "smoke", WorkDir: t.TempDir()},
		"Use the AskUserQuestion tool to ask me which color I prefer, options Red, Green, Blue. Then reply with exactly: CHOSEN <color>", sink)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Stop(); <-s.Done() }()

	for answered := 0; answered < 3; answered++ {
		var seq uint64
		select {
		case seq = <-sink.approval:
		case <-time.After(90 * time.Second):
			t.Fatalf("no choice prompt; output:\n%s", sink.text())
		}
		p := <-sink.prompts
		if p.Kind != terminal.PromptChoice {
			t.Fatalf("want a choice prompt, got %+v", p)
		}
		pick := "2" // Green
		if strings.Contains(p.Question, "submit") {
			pick = "1" // Submit answers
		}
		if _, err := s.RespondChoice(seq, pick); err != nil {
			t.Fatal(err)
		}
		if pick == "1" || !strings.Contains(p.Question, "color") {
			break
		}
		// A single question may submit directly; otherwise a review page follows.
		select {
		case <-time.After(5 * time.Second):
			waitFor(t, sink, "[1] CHOSEN Green", 60*time.Second)
			return
		case seq = <-sink.approval:
			sink.approval <- seq
		}
	}
	waitFor(t, sink, "[1] CHOSEN Green", 60*time.Second)
}
