package chatcore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
)

// ApprovalValue encodes a button payload; seq lets stale buttons be rejected.
// It stays well under Telegram's 64-byte callback-data limit.
func ApprovalValue(sessionID string, seq uint64) string {
	return fmt.Sprintf("%s|%d", sessionID, seq)
}

// ParseApprovalValue decodes ApprovalValue.
func ParseApprovalValue(value string) (sessionID string, seq uint64, err error) {
	sessionID, rawSeq, ok := strings.Cut(value, "|")
	if !ok || sessionID == "" {
		return "", 0, fmt.Errorf("malformed approval value %q", value)
	}
	seq, err = strconv.ParseUint(rawSeq, 10, 64)
	return sessionID, seq, err
}

// ResolveApproval forwards an Approve/Reject click to the session and returns a
// one-line result for the message, e.g. "✅ Approved by @alice". who is
// already formatted for the platform.
func (c *Core) ResolveApproval(value string, approve bool, who string) string {
	sessionID, seq, err := ParseApprovalValue(value)
	if err != nil {
		return "⚠️ Invalid approval payload."
	}
	s, ok := c.sessions.ByID(sessionID)
	if !ok {
		return "⚠️ Session has already ended."
	}
	switch err := s.RespondApproval(seq, approve); {
	case errors.Is(err, session.ErrStaleApproval):
		return "⌛ This prompt is no longer pending."
	case err != nil:
		return "❌ Failed to send answer: " + err.Error()
	case approve:
		return "✅ Approved by " + who
	default:
		return "🚫 Rejected by " + who
	}
}

// ChoiceValue encodes a choice button: "<sessionID>|<seq>|<digit>".
func ChoiceValue(sessionID string, seq uint64, number string) string {
	return ApprovalValue(sessionID, seq) + "|" + number
}

// ResolveChoice forwards an option click to the session and returns a
// one-line result, e.g. "✅ @alice chose “Green”".
func (c *Core) ResolveChoice(value, who string) string {
	parts := strings.Split(value, "|")
	if len(parts) != 3 {
		return "⚠️ Invalid choice payload."
	}
	sessionID, seq, err := ParseApprovalValue(parts[0] + "|" + parts[1])
	number := parts[2]
	if err != nil {
		return "⚠️ Invalid choice payload."
	}
	s, ok := c.sessions.ByID(sessionID)
	if !ok {
		return "⚠️ Session has already ended."
	}
	label, err := s.RespondChoice(seq, number)
	switch {
	case errors.Is(err, session.ErrStaleApproval):
		return "⌛ This question is no longer pending."
	case err != nil:
		return "❌ Failed to send answer: " + err.Error()
	default:
		return fmt.Sprintf("✅ %s chose “%s”", who, label)
	}
}
