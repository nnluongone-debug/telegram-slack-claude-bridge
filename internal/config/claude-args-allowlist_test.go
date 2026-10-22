package config

import (
	"path/filepath"
	"testing"
)

func TestValidateClaudeArgs(t *testing.T) {
	allowed := [][]string{
		nil,
		{"--model", "sonnet"},
		{"--model=opus", "--verbose"},
		{"--permission-mode", "acceptEdits", "--effort", "high"},
		{"--disallowedTools", "Bash", "WebFetch", "--model", "sonnet"},
		{"--append-system-prompt", "Answer in English."},
	}
	for _, args := range allowed {
		if err := ValidateClaudeArgs(args); err != nil {
			t.Errorf("%q: unexpected error %v", args, err)
		}
	}

	rejected := [][]string{
		{"--dangerously-skip-permissions"},
		{"--allow-dangerously-skip-permissions"},
		{"--permission-mode", "bypassPermissions"},
		{"--permission-mode=BYPASSPERMISSIONS"},
		{"--allowedTools", "Bash"},
		{"--add-dir", "/"},
		{"--mcp-config", "evil.json"},
		{"--settings", `{"permissions":{"allow":["Bash"]}}`},
		{"--resume=abc"},
		{"-p"},
		{"--agent", "evil"},
		{"--chrome"},
		{"--ide"},
		{"--model"},                    // missing value
		{"--model", "--verbose"},       // a flag is not a value
		{"--verbose=true"},             // switch with a value
		{"rm -rf ~"},                   // positional: would become the prompt
		{"--model", "sonnet", "extra"}, // stray positional after a value
	}
	for _, args := range rejected {
		if err := ValidateClaudeArgs(args); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}
}

func TestWorkdirExpansionAndScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("BRIDGE_SECRET", "s3cret")
	if got := expandHome("$HOME/code"); got != home+"/code" {
		t.Fatalf("$HOME: got %q", got)
	}
	if got := expandHome("~/code"); got != home+"/code" {
		t.Fatalf("~: got %q", got)
	}
	if got := expandHome("/x/$BRIDGE_SECRET"); got != "/x/$BRIDGE_SECRET" {
		t.Fatalf("other variables must stay unexpanded, got %q", got)
	}
	for _, dir := range []string{home, filepath.Dir(home), string(filepath.Separator)} {
		if !tooBroad(dir) {
			t.Errorf("%s must be refused", dir)
		}
	}
	if tooBroad(filepath.Join(home, "code")) {
		t.Error("a project folder under home must be allowed")
	}
}
