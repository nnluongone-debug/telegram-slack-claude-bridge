package terminal

import (
	"bytes"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

// claude-permission-prompt.pty is real PTY output captured from Claude Code v2.1
// (alternate screen, absolute cursor positioning) ending on a Bash permission menu.
func TestVirtualScreenRendersRealClaudePermissionMenu(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-permission-prompt.pty")
	if err != nil {
		t.Fatal(err)
	}
	screen := NewVirtualScreen(120, 40, io.Discard)
	// Feed in small chunks to exercise sequences and runes split across reads.
	for i := 0; i < len(raw); i += 7 {
		_, _ = screen.Write(raw[i:min(i+7, len(raw))])
	}
	lines := screen.Lines()
	text := strings.Join(lines, "\n")

	for _, want := range []string{"Run the bash command: echo hi > hello.txt", "Do you want to proceed?", "❯ 1. Yes", "Esc to cancel"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered screen missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "╌╌╌") || strings.Contains(text, "\x1b") {
		t.Fatalf("separator rows / escapes should be removed:\n%s", text)
	}

	p, ok := DetectApprovalPrompt(lines)
	if !ok || p.Kind != PromptMenu || !strings.Contains(p.Context, "echo hi > hello.txt") {
		t.Fatalf("menu not detected: ok=%v kind=%v ctx=%q", ok, p.Kind, p.Context)
	}
}

func TestVirtualScreenCarriesSplitRune(t *testing.T) {
	screen := NewVirtualScreen(20, 3, io.Discard)
	check := []byte("✓ ok") // ✓ is 3 bytes
	_, _ = screen.Write(check[:2])
	_, _ = screen.Write(check[2:])
	if got := screen.Lines(); !slices.Equal(got, []string{"✓ ok"}) {
		t.Fatalf("got %q", got)
	}
}

func TestDetectApprovalPrompt(t *testing.T) {
	cases := []struct {
		name    string
		lines   []string
		kind    PromptKind
		found   bool
		approve string
		reject  string
	}{
		{"yes/no pending", []string{"build ok", "Overwrite file? (y/n)"}, PromptYesNo, true, "y\r", "n\r"},
		{"yes/no answered", []string{"Overwrite file? (y/n) y", "done"}, 0, false, "", ""},
		{"menu", []string{"Bash command", "rm -rf build", "Do you want to proceed?", "❯ 1. Yes", "  2. No", "Esc to cancel"}, PromptMenu, true, "1", "\x1b"},
		{"prose is not a menu", []string{"Do you want to proceed with the refactor?", "I can also add tests."}, 0, false, "", ""},
		{"none", []string{"All tests passed"}, 0, false, "", ""},
		{"trust dialog", []string{" Accessing workspace:", " /tmp/repo", " Quick safety check: Is this a project you created or one you trust?",
			" ❯ No, exit", "   Yes, I trust this folder", " Enter to confirm · Esc to cancel"}, PromptTrust, true, "\x1b[B\r", "\x1b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := DetectApprovalPrompt(tc.lines)
			if ok != tc.found || p.Kind != tc.kind {
				t.Fatalf("got kind=%v ok=%v", p.Kind, ok)
			}
			if ok && (string(bytes.Join(p.Keys(true), nil)) != tc.approve || string(bytes.Join(p.Keys(false), nil)) != tc.reject) {
				t.Fatalf("keys approve=%q reject=%q", p.Keys(true), p.Keys(false))
			}
		})
	}
}

func TestPermissionModeFromStatusLine(t *testing.T) {
	cases := map[string]string{
		"  ⏸ plan mode on (shift+tab to cycle) · ← for agents":     "plan mode",
		"  ⏵⏵ accept edits on (shift+tab to cycle) · ← for agents": "accept edits",
		"  ⏸ manual mode on · ? for shortcuts · ← for agents":      "manual mode",
		"  ⏵⏵ auto mode on (shift+tab to cycle)":                   "auto mode",
		"❯ Try \"fix lint errors\"":                                "",
	}
	for line, want := range cases {
		if got := PermissionMode([]string{"⏺ some reply", line}); got != want {
			t.Errorf("%q → %q, want %q", line, got, want)
		}
	}
}

func TestInputLineAndBusy(t *testing.T) {
	loading := []string{"❯ Reply only: ONE", "⏺ ONE", "◐ medium · /effort"}
	if _, ok := InputLine(loading); ok {
		t.Fatal("history rows are not the prompt box")
	}
	hookRunning := []string{"❯ old prompt", "✻ Flummoxing… (running UserPromptSubmit hook · 10s)", "❯ What was the code word?", "  ⏵⏵ auto mode on · esc to interrupt"}
	if line, ok := InputLine(hookRunning); !ok || line != "What was the code word?" || !Busy(hookRunning) {
		t.Fatalf("got %q ok=%v busy=%v", line, ok, Busy(hookRunning))
	}
	if Busy([]string{"❯", "  ⏸ manual mode on · ? for shortcuts"}) {
		t.Fatal("idle prompt is not busy")
	}
}

func TestActivityLine(t *testing.T) {
	busy := []string{"❯ check vouchers", "⏺ Bash(git log)", "✻ Flummoxing… (running UserPromptSubmit hook · 10s)", "", "❯", "  ⏵⏵ auto mode on · esc to interrupt"}
	if got := ActivityLine(busy); got != "Flummoxing… (running UserPromptSubmit hook · 10s)" {
		t.Fatalf("got %q", got)
	}
	done := []string{"⏺ DONE", "✻ Cooked for 15s · done 9:27 AM", "❯", "  ⏸ manual mode on · ? for shortcuts"}
	if got := ActivityLine(done); got != "" {
		t.Fatalf("finished spinner is not activity: %q", got)
	}
}

func TestRecentOutput(t *testing.T) {
	screen := []string{"❯ check vouchers", "⏺ Checking the backend assets", "⏺ Bash(git log --oneline)", "  ⎿  Running…",
		"✻ Flummoxing… (12s)", "                ◐ medium · /effort", "❯", "  ⏵⏵ auto mode on · esc to interrupt"}
	want := "⏺ Checking the backend assets\n⏺ Bash(git log --oneline)\n  ⎿  Running…"
	if got := RecentOutput(screen, 3); got != want {
		t.Fatalf("got %q", got)
	}
}

// Screens captured from Claude Code's AskUserQuestion dialog (probe, v2.1.306).
func TestDetectAskUserQuestion(t *testing.T) {
	question := []string{
		"❯ Use the AskUserQuestion tool to ask me two questions",
		"←  ☐ Color  ☐ Size  ✔ Submit  →",
		"Which color do you prefer?",
		"❯ 1. Red", "     Red", "  2. Green", "     Green", "  3. Blue", "     Blue",
		"  4. Type something.", "  5. Chat about this",
		"Enter to select · Tab/Arrow keys to navigate · Esc to cancel",
	}
	p, ok := DetectApprovalPrompt(question)
	if !ok || p.Kind != PromptChoice || p.Question != "Which color do you prefer?" || len(p.Options) != 5 ||
		p.Options[1] != (ChoiceOption{"2", "Green"}) {
		t.Fatalf("question: ok=%v %+v", ok, p)
	}
	if own, ok := p.OwnAnswerOption(); !ok || own.Number != "4" {
		t.Fatalf("own answer option: %+v", own)
	}

	review := []string{
		"←  ☒ Color  ☒ Size  ✔ Submit  →",
		"Review your answers",
		" ● Which color do you prefer?", "   → Green",
		"Ready to submit your answers?",
		"❯ 1. Submit answers", "  2. Cancel",
	}
	r, ok := DetectApprovalPrompt(review)
	if !ok || r.Kind != PromptChoice || r.Question != "Ready to submit your answers?" || len(r.Options) != 2 || r.Key == p.Key {
		t.Fatalf("review: ok=%v %+v", ok, r)
	}

	// A permission menu (no "Enter to select") is still an approve/reject menu.
	perm := []string{"Do you want to proceed?", "❯ 1. Yes", "  2. No", "Esc to cancel · Tab to amend"}
	if m, _ := DetectApprovalPrompt(perm); m.Kind != PromptMenu {
		t.Fatalf("permission menu misdetected as %v", m.Kind)
	}
}
