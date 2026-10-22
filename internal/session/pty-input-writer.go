package session

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
)

const (
	submitDelay     = 150 * time.Millisecond // gap between typed text and Enter so the TUI doesn't treat it as one paste
	approvalCooloff = 3 * time.Second        // ignore the just-answered prompt while the TUI redraws
	replyQueueSize  = 32
	submitAttempts  = 10 // ~15s: Enter is ignored while startup hooks run after --resume
	submitCheckWait = 1500 * time.Millisecond
	inputBoxWait    = 15 * time.Second
	keyStepDelay    = 400 * time.Millisecond // lets the TUI redraw between approval keystrokes
)

// ErrStaleApproval is returned when a button click no longer matches the pending prompt.
var ErrStaleApproval = errors.New("approval request is no longer pending")

// SendText types a prompt into the agent and presses Enter. While a choice
// dialog with a "Type something." option is open, it picks that option first
// so the text becomes the user's own answer.
func (s *Session) SendText(text string) error {
	s.mu.Lock()
	var ownAnswer []byte
	if s.approval != nil {
		if o, ok := s.approval.OwnAnswerOption(); ok {
			ownAnswer = []byte(o.Number)
		}
	}
	s.consumeApprovalLocked() // typing answers/abandons any prompt on screen
	s.fresh = true            // the reply to a new prompt gets its own Slack message
	if s.inputSince.IsZero() {
		s.inputSince = time.Now()
	}
	s.mu.Unlock()

	if strings.Contains(text, "\n") {
		text = "\x1b[200~" + text + "\x1b[201~" // bracketed paste keeps newlines from submitting early
	}

	if ownAnswer != nil {
		if err := s.writeRaw(ownAnswer); err != nil {
			return err
		}
		time.Sleep(keyStepDelay)
		// The dialog's own text field: no prompt box to wait for or verify.
		if err := s.writeRaw([]byte(text)); err != nil {
			return err
		}
		time.Sleep(submitDelay)
		return s.writeRaw([]byte{'\r'})
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.waitInputBox() // keystrokes sent before the prompt box exists are dropped
	if _, err := s.proc.Write([]byte(text)); err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}
	time.Sleep(submitDelay)

	// The TUI can drop Enter while it is busy (e.g. right after --resume loads
	// history), leaving the prompt in the input box. Re-press Enter only while our
	// own text is still there; a paste shows a placeholder, so it is not checked.
	marker := submitMarker(text)
	for attempt := 1; ; attempt++ {
		if _, err := s.proc.Write([]byte{'\r'}); err != nil {
			return fmt.Errorf("write enter: %w", err)
		}
		if marker == "" || s.waitInputCleared(marker) {
			return nil
		}
		if attempt == submitAttempts {
			return errors.New("Claude has not submitted the prompt yet; it is still in the input box (try `!enter`)")
		}
	}
}

// submitMarker is the start of a single-line prompt, used to recognise it in the input box.
func submitMarker(text string) string {
	if strings.Contains(text, "\n") {
		return ""
	}
	r := []rune(strings.TrimSpace(text))
	return string(r[:min(len(r), 30)])
}

func (s *Session) waitInputCleared(marker string) bool {
	deadline := time.Now().Add(submitCheckWait)
	for time.Now().Before(deadline) {
		if s.exited.Load() || s.plainCLI {
			return true // process gone, or not Claude's TUI: nothing to verify
		}
		lines := s.screen.Lines()
		if line, ok := terminal.InputLine(lines); terminal.Busy(lines) || ok && !strings.HasPrefix(line, marker) {
			return true // accepted: Claude is working, or the box no longer holds our text
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// waitInputBox waits (bounded) for the prompt box; if it never shows, e.g.
// because a dialog is open, the text is typed anyway as a user would.
func (s *Session) waitInputBox() {
	deadline := time.Now().Add(inputBoxWait)
	for time.Now().Before(deadline) {
		if s.plainCLI {
			return // no prompt box to wait for
		}
		if _, ok := terminal.InputLine(s.screen.Lines()); ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// SendKeys writes raw bytes (Esc, Ctrl-C, Enter...) into the PTY.
func (s *Session) SendKeys(keys []byte) error {
	s.mu.Lock()
	s.consumeApprovalLocked()
	s.mu.Unlock()
	return s.writeRaw(keys)
}

// RespondApproval answers the pending prompt identified by seq.
func (s *Session) RespondApproval(seq uint64, approve bool) error {
	s.mu.Lock()
	if s.approval == nil || seq != s.approvalSeq {
		s.mu.Unlock()
		return ErrStaleApproval
	}
	steps := s.approval.Keys(approve)
	s.consumeApprovalLocked()
	s.fresh = true
	s.mu.Unlock()
	for i, step := range steps {
		if i > 0 {
			time.Sleep(keyStepDelay)
		}
		if err := s.writeRaw(step); err != nil {
			return err
		}
	}
	return nil
}

// RespondChoice picks option number (a digit) of the pending choice prompt seq.
func (s *Session) RespondChoice(seq uint64, number string) (label string, err error) {
	s.mu.Lock()
	if s.approval == nil || seq != s.approvalSeq || s.approval.Kind != terminal.PromptChoice {
		s.mu.Unlock()
		return "", ErrStaleApproval
	}
	for _, o := range s.approval.Options {
		if o.Number == number {
			label = o.Label
		}
	}
	if label == "" {
		s.mu.Unlock()
		return "", fmt.Errorf("no option %q", number)
	}
	s.consumeApprovalLocked()
	s.fresh = true
	s.mu.Unlock()
	return label, s.writeRaw([]byte(number))
}

// consumeApprovalLocked retires the pending prompt so its buttons go stale and
// the same prompt is not re-raised while the TUI catches up. Caller holds s.mu.
func (s *Session) consumeApprovalLocked() {
	if s.approval == nil {
		return
	}
	s.answeredKey, s.answeredAt = s.approval.Key, time.Now()
	s.approval = nil
}

func (s *Session) writeRaw(b []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.proc.Write(b)
	return err
}

// ptyReplyWriter queues the emulator's answers to terminal queries (cursor
// position, status). The emulator writes them while holding its own lock from
// readLoop, so blocking here on writeMu could deadlock against a large paste.
type ptyReplyWriter struct{ s *Session }

func (w ptyReplyWriter) Write(p []byte) (int, error) {
	select {
	case w.s.replies <- append([]byte(nil), p...):
	default: // queue full: drop rather than stall output processing
	}
	return len(p), nil
}

func (s *Session) replyLoop() {
	for {
		select {
		case <-s.readerDone:
			return
		case b := <-s.replies:
			_ = s.writeRaw(b)
		}
	}
}
