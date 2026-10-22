package config

import (
	"fmt"
	"strings"
)

// Claude flags that projects.json "args" and CLAUDE_ARGS may set. It is an
// allowlist, not a blocklist: projects.json is reloaded while the bridge runs
// and the agent itself can write to it, so a flag that widens what the agent
// may do (--dangerously-skip-permissions, --allowedTools, --add-dir,
// --mcp-config, --settings, --agent, --chrome…) would let a prompt-injected agent grant itself
// more power for every later session. Configure those in Claude Code's own
// settings instead.
var (
	// valueFlags take one value, either as the next item or after "=".
	valueFlags = map[string]bool{
		"--model": true, "--fallback-model": true, "--permission-mode": true, "--effort": true,
		"--append-system-prompt": true, "--name": true, "-n": true,
	}
	// listFlags take one or more values, up to the next flag.
	listFlags = map[string]bool{"--disallowedTools": true, "--disallowed-tools": true}
	// switchFlags take no value.
	switchFlags = map[string]bool{"--verbose": true}
)

// ValidateClaudeArgs rejects any flag outside the allowlist, positional
// arguments (Claude would read them as a prompt) and the bypassPermissions mode.
func ValidateClaudeArgs(args []string) error {
	for i := 0; i < len(args); i++ {
		flag, value, hasValue := strings.Cut(args[i], "=")
		switch {
		case !strings.HasPrefix(flag, "-"):
			return fmt.Errorf("unexpected argument %q (only flags are allowed)", args[i])
		case switchFlags[flag]:
			if hasValue {
				return fmt.Errorf("flag %s takes no value", flag)
			}
		case valueFlags[flag]:
			if !hasValue {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return fmt.Errorf("flag %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			if flag == "--permission-mode" && strings.EqualFold(value, "bypassPermissions") {
				return fmt.Errorf("--permission-mode bypassPermissions is not allowed")
			}
		case listFlags[flag]:
			for !hasValue && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		default:
			return fmt.Errorf("flag %s is not allowed (allowed: --model, --fallback-model, --permission-mode, --effort, --append-system-prompt, --name, --disallowedTools, --verbose)", flag)
		}
	}
	return nil
}
