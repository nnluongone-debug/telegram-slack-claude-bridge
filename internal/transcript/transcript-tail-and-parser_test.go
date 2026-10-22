package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	textLine  = `{"type":"assistant","isSidechain":false,"message":{"content":[{"type":"text","text":"## Done\n| a | b |\n|---|---|\n| 1 | 2 |"}]}}`
	toolLine  = `{"type":"assistant","isSidechain":false,"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"git status\ngit diff","description":"check"}}]}}`
	noiseLine = `{"type":"assistant","isSidechain":false,"message":{"content":[{"type":"thinking","thinking":"hmm"}]}}`
	userLine  = `{"type":"user","message":{"content":"hello"}}`
	sideLine  = `{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"subagent"}]}}`
)

func TestParseLine(t *testing.T) {
	if ev := ParseLine([]byte(textLine)); len(ev) != 1 || ev[0].Kind != EventText || ev[0].Text[:7] != "## Done" {
		t.Fatalf("text: %+v", ev)
	}
	if ev := ParseLine([]byte(toolLine)); len(ev) != 1 || ev[0].Text != "Bash" || ev[0].Detail != "git status" {
		t.Fatalf("tool: %+v", ev)
	}
	for _, l := range []string{noiseLine, userLine, sideLine, `not json`} {
		if ev := ParseLine([]byte(l)); len(ev) != 0 {
			t.Fatalf("expected no events for %s, got %+v", l, ev)
		}
	}
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestTailFollowsNewAndResumedTranscripts(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "projects", "-tmp-project")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess-1.jsonl")

	// New session: file appears after the tail starts → read from the beginning.
	tail := NewTail("sess-1")
	if ev, _ := tail.Poll(); len(ev) != 0 || tail.Found() {
		t.Fatal("nothing should be found yet")
	}
	appendTo(t, path, textLine+"\n"+toolLine[:40]) // second line only partially written
	if ev, _ := tail.Poll(); len(ev) != 1 || ev[0].Kind != EventText {
		t.Fatalf("first poll: %+v", ev)
	}
	appendTo(t, path, toolLine[40:]+"\n")
	if ev, _ := tail.Poll(); len(ev) != 1 || ev[0].Kind != EventTool {
		t.Fatalf("partial line must be completed: %+v", ev)
	}

	// Resumed session: existing history is not replayed.
	resumed := NewTail("sess-1")
	appendTo(t, path, textLine+"\n")
	if ev, _ := resumed.Poll(); len(ev) != 1 {
		t.Fatalf("resumed tail should only see new entries: %+v", ev)
	}
}

func TestSessionBelongsTo(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	workDir := t.TempDir()
	dir := filepath.Join(cfg, "projects", projectDirName(workDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(dir, "sess-9.jsonl"), "{}\n")
	if !SessionBelongsTo("sess-9", workDir) || SessionBelongsTo("sess-9", t.TempDir()) || SessionBelongsTo("nope", workDir) {
		t.Fatal("wrong project match")
	}
	if got := projectDirName("/Users/a/Code/my_app/file-management"); got != "-Users-a-Code-my-app-file-management" {
		t.Fatalf("encoding: %q", got)
	}
}
