package session

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
)

// ProtectedPaths lists the bridge's own files. Paths may end in "/**" for a
// whole folder.
type ProtectedPaths struct {
	NoEdit []string // config and records: projects.json, the support folder
	NoRead []string // secrets: .env files with the bot tokens (also never edited)
}

// protectSettingsArgs returns `--settings <json>` with permission rules that
// stop the agent from rewriting the bridge's config (a prompt-injected agent
// could otherwise grant itself more power for later sessions) and from
// reading its tokens. Shell commands are not covered by these rules; the
// args allowlist in config is the second line of defence.
func protectSettingsArgs(p ProtectedPaths) []string {
	deny := slices.Clone(claudeConfigRules)
	for _, path := range p.NoEdit {
		deny = append(deny, "Edit("+absRule(path)+")")
	}
	for _, path := range p.NoRead {
		deny = append(deny, "Edit("+absRule(path)+")", "Read("+absRule(path)+")")
	}
	raw, _ := json.Marshal(map[string]any{"permissions": map[string]any{"deny": deny}})
	return []string{"--settings", string(raw)}
}

// claudeConfigRules keep the agent out of Claude Code's own configuration,
// where it could allow itself shell commands, add hooks or MCP servers, or
// rewrite agents for later sessions. Relative patterns match under the
// session's folder, worktrees included.
var claudeConfigRules = []string{
	"Edit(**/.claude/settings*.json)", "Edit(**/.claude/agents/**)", "Edit(**/.claude/commands/**)",
	"Edit(**/.claude/hooks/**)", "Edit(**/.mcp.json)",
	"Edit(~/.claude/settings*.json)", "Edit(~/.claude/agents/**)", "Edit(~/.claude/commands/**)",
	"Edit(~/.claude/hooks/**)", "Edit(~/.claude.json)",
}

// absRule writes an absolute path the way Claude permission rules expect:
// "//" followed by the path from the filesystem root, with forward slashes.
// Windows paths take Claude's POSIX form: C:\Users\a → //c/Users/a.
func absRule(path string) string {
	glob := strings.HasSuffix(path, "/**")
	path = strings.TrimSuffix(path, "/**")
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved // /tmp → /private/tmp: rules match real paths
	}
	slashed := filepath.ToSlash(path)
	if vol := filepath.VolumeName(path); len(vol) == 2 && vol[1] == ':' {
		slashed = "/" + strings.ToLower(vol[:1]) + slashed[2:]
	}
	rule := "//" + strings.TrimPrefix(slashed, "/")
	if glob {
		rule += "/**"
	}
	return rule
}
