package session

import (
	"strings"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
)

// flush renders the screen, pushes it to the sink when it changed, and keeps
// the approval state in sync with what is visible (prompts are compared by Key):
//   - no prompt visible                        → pending approval cleared (old buttons go stale)
//   - same prompt as the pending one           → nothing to do
//   - prompt just answered, TUI not redrawn yet → wait (re-check next tick)
//   - otherwise (new or different prompt)       → seq++, raise buttons
func (s *Session) flush() {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	s.dirty = false

	lines := s.screen.Lines()
	text := strings.Join(lines, "\n")
	changed := text != "" && text != s.lastRender
	if alt := s.screen.AltScreen(); alt != s.lastAlt {
		s.lastAlt, s.fresh = alt, true // a different screen must not overwrite the live view
	}
	fresh := s.fresh
	if changed {
		s.lastRender = text
		s.fresh = false
	}

	var raise *terminal.ApprovalPrompt
	prompt, visible := terminal.DetectApprovalPrompt(lines)
	switch {
	case !visible:
		s.approval = nil
	case s.approval != nil && s.approval.Key == prompt.Key:
	case prompt.Key == s.answeredKey && time.Since(s.answeredAt) < approvalCooloff:
		s.dirty = true // the screen may stay static, so re-check after the cool-off
	default:
		s.approvalSeq++
		s.approval, raise = &prompt, &prompt
	}
	seq := s.approvalSeq
	s.mu.Unlock()

	if !s.mirror.Load() {
		s.reportStatus(lines)
	}
	if changed && s.mirror.Load() && !s.sink.Screen(s, text, fresh) {
		s.mu.Lock()
		s.lastRender, s.dirty = "", true // not delivered (e.g. rate limited): resend next tick
		s.fresh = s.fresh || fresh
		s.mu.Unlock()
	}
	if raise == nil {
		return
	}
	posted := s.sink.ApprovalRequested(s, seq, *raise)
	s.mu.Lock()
	if posted {
		s.fresh = true // keep the live view below the buttons
	} else if s.approvalSeq == seq {
		s.approval, s.dirty = nil, true // buttons never reached Slack: raise again next tick
	}
	s.mu.Unlock()
}

const statusRecentRows = 8

// reportStatus forwards Claude's live activity (markdown mode only: in screen
// mode the mirrored terminal already shows it). Caller holds flushMu.
func (s *Session) reportStatus(lines []string) {
	busy := terminal.Busy(lines)
	activity, recent := "", ""
	if busy {
		activity = terminal.ActivityLine(lines)
		if activity == "" {
			activity = "Working…"
		}
		recent = terminal.RecentOutput(lines, statusRecentRows)
	}
	key := activity + "\x00" + recent
	if key == s.lastStatus {
		return
	}
	if s.sink.Status(s, activity, recent, busy) {
		s.lastStatus = key
		return
	}
	s.mu.Lock()
	s.dirty = true // throttled: try again next tick even if the screen is static
	s.mu.Unlock()
}
