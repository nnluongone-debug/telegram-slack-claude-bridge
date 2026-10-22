package session

import (
	"fmt"
	"strings"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
)

const (
	shiftTab        = "\x1b[Z"
	modeRedrawWait  = 900 * time.Millisecond
	maxModePresses  = 6 // the TUI cycles through at most five modes
	modeNamesForErr = "manual, plan, auto, accept, bypass"
)

// modeAliases maps what users type to the status-line wording.
var modeAliases = map[string]string{
	"manual": "manual mode", "default": "manual mode",
	"plan":   "plan mode",
	"auto":   "auto mode",
	"accept": "accept edits", "acceptedits": "accept edits", "edits": "accept edits",
	"bypass": "bypass permissions",
}

// PermissionMode reports the mode on the agent's status line ("" if not visible).
func (s *Session) PermissionMode() string {
	return terminal.PermissionMode(s.screen.Lines())
}

// SwitchPermissionMode presses Shift+Tab until the status line shows the
// wanted mode, like a user would. It returns the mode finally shown.
func (s *Session) SwitchPermissionMode(want string) (string, error) {
	target, ok := modeAliases[strings.ToLower(strings.ReplaceAll(want, " ", ""))]
	if !ok {
		return "", fmt.Errorf("unknown mode %q (use: %s)", want, modeNamesForErr)
	}
	for i := 0; i <= maxModePresses; i++ {
		if current := s.PermissionMode(); current == target {
			return current, nil
		}
		if i == maxModePresses {
			break
		}
		if err := s.writeRaw([]byte(shiftTab)); err != nil {
			return "", err
		}
		time.Sleep(modeRedrawWait)
	}
	return s.PermissionMode(), fmt.Errorf("%q is not in this session's mode cycle (it may need a launch flag, e.g. args in projects.json)", target)
}
