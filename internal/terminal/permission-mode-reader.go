package terminal

import (
	"regexp"
	"strings"
)

// modeStatusRe matches Claude Code's status line, e.g. "⏸ plan mode on (shift+tab to cycle)".
var modeStatusRe = regexp.MustCompile(`(?i)\b(manual mode|plan mode|auto mode|accept edits|bypass permissions) on\b`)

// PermissionMode returns the mode shown in the bottom rows of the screen
// ("plan mode", "accept edits", ...), or "" if no status line is visible.
func PermissionMode(lines []string) string {
	for i := len(lines) - 1; i >= max(0, len(lines)-6); i-- {
		if m := modeStatusRe.FindStringSubmatch(lines[i]); m != nil {
			return strings.ToLower(m[1])
		}
	}
	return ""
}

// InputLine returns the text in Claude Code's prompt box. The box is a "❯" row
// in the last two rows (only the status line sits below it); "❯" rows higher up
// are submitted history. ok is false while the box is not drawn, e.g. while a
// resumed session is still loading or a dialog is open.
func InputLine(lines []string) (text string, ok bool) {
	for i := len(lines) - 1; i >= max(0, len(lines)-2); i-- {
		if rest, found := strings.CutPrefix(lines[i], "❯"); found {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// Busy reports whether Claude is working on a submitted prompt: its status line
// then offers "esc to interrupt" (also while UserPromptSubmit hooks run).
func Busy(lines []string) bool {
	for i := len(lines) - 1; i >= max(0, len(lines)-3); i-- {
		if strings.Contains(strings.ToLower(lines[i]), "esc to interrupt") {
			return true
		}
	}
	return false
}

// activityRe matches Claude's spinner row, e.g.
// "✻ Flummoxing… (running UserPromptSubmit hook · 10s · ↓ 88 tokens)".
var activityRe = regexp.MustCompile(`^\s*[✻✽✶✳✢·*]\s+(\S.*….*)$`)

// ActivityLine returns what Claude is doing right now (the spinner row near
// the bottom of the screen), or "" if no spinner is shown.
func ActivityLine(lines []string) string {
	for i := len(lines) - 1; i >= max(0, len(lines)-8); i-- {
		if m := activityRe.FindStringSubmatch(lines[i]); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// RecentOutput returns up to n non-blank rows just above the spinner (or the
// prompt box): the text Claude is streaming or the tool currently running.
func RecentOutput(lines []string, n int) string {
	end := len(lines)
	for i := len(lines) - 1; i >= max(0, len(lines)-8); i-- {
		if activityRe.MatchString(lines[i]) {
			end = i
			break
		}
		if _, ok := InputLine(lines[:i+1]); ok && strings.HasPrefix(lines[i], "❯") {
			end = i
		}
	}
	var out []string
	for i := end - 1; i >= 0 && len(out) < n; i-- {
		if row := strings.TrimRight(lines[i], " "); strings.TrimSpace(row) != "" && !strings.Contains(row, "/effort") {
			out = append([]string{row}, out...)
		}
	}
	return strings.Join(out, "\n")
}
