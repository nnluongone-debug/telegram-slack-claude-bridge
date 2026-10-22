package session

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const gitCheckTimeout = 3 * time.Second

// isGitRepo reports whether dir is inside a git work tree (claude --worktree needs one).
func isGitRepo(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), gitCheckTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}
