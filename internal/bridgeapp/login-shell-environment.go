package bridgeapp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

const envMarker = "__CLAUDE_BRIDGE_ENV__"

// volatileEnv describe the probing shell itself, not the user's environment.
var volatileEnv = []string{"PWD", "OLDPWD", "SHLVL", "_", "TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "TERM_SESSION_ID"}

// ImportLoginShellEnv copies the environment of the user's interactive login
// shell into this process. Apps opened from Finder start with a bare PATH
// (no ~/.local/bin, Homebrew, pyenv…), so claude and the tools it runs would
// not be found.
func ImportLoginShellEnv(timeout time.Duration) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// -i loads .zshrc (where most people set PATH); the marker skips anything rc files print.
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", "printf '"+envMarker+"'; env -0")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read login shell environment: %w", err)
	}
	_, envBlock, found := bytes.Cut(out, []byte(envMarker))
	if !found {
		return fmt.Errorf("read login shell environment: marker not found")
	}
	for _, kv := range strings.Split(string(envBlock), "\x00") {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" || slices.Contains(volatileEnv, key) {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return nil
}
