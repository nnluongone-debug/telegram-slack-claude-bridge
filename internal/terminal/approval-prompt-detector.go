package terminal

import (
	"regexp"
	"strings"
)

// PromptKind identifies how a confirmation prompt expects to be answered.
type PromptKind int

const (
	// PromptYesNo is a classic "(y/n)" line prompt answered with y/n + Enter.
	PromptYesNo PromptKind = iota + 1
	// PromptMenu is a Claude Code selection menu:
	//   Do you want to proceed?
	//   ❯ 1. Yes
	//     2. No
	//   Esc to cancel
	PromptMenu
	// PromptTrust is the first-run folder trust dialog ("❯ No, exit / Yes, I
	// trust this folder"): unnumbered, with "No" preselected.
	PromptTrust
)

var (
	// A line prompt is pending only while nothing has been typed after it.
	yesNoRe      = regexp.MustCompile(`(?i)(\(y/n\)|\[y/n\]|\(yes/no\))\s*[:?]?\s*$`)
	menuYesRe    = regexp.MustCompile(`(?i)^\s*(❯\s*)?1\.\s+yes\b`)
	menuAnchorRe = regexp.MustCompile(`(?i)esc to cancel|do you want to|do you trust`)
	trustRe      = regexp.MustCompile(`(?i)yes,\s*i trust this folder`)
)

const (
	menuScanLines    = 25 // menus render at the bottom of the screen
	menuContextAbove = 8  // rows above the question that describe the action (command, diff header)
)

// ApprovalPrompt is a detected confirmation request plus the surrounding text.
type ApprovalPrompt struct {
	Kind    PromptKind
	Context string // rows describing the action, for display
	Key     string // the prompt rows only; identical while the same prompt stays on screen

	Question string         // PromptChoice: the question being asked
	Options  []ChoiceOption // PromptChoice: options selectable by digit
}

// Keys returns the keystrokes that approve or reject, as steps to be written
// with a short pause in between (a TUI needs to redraw after a cursor move).
func (p ApprovalPrompt) Keys(approve bool) [][]byte {
	switch {
	case p.Kind == PromptMenu && approve:
		return [][]byte{[]byte("1")} // option 1 is always "Yes"
	case p.Kind == PromptMenu, p.Kind == PromptTrust && !approve:
		return [][]byte{{0x1b}} // "Esc to cancel"
	case p.Kind == PromptTrust:
		return [][]byte{[]byte("\x1b[B"), []byte("\r")} // move from "No, exit" to "Yes", confirm
	case approve:
		return [][]byte{[]byte("y\r")}
	default:
		return [][]byte{[]byte("n\r")}
	}
}

// Labels returns the approve/reject button texts, e.g. "Approve (1. Yes)".
func (p ApprovalPrompt) Labels() (approve, reject string) {
	switch p.Kind {
	case PromptMenu:
		return "Approve (1. Yes)", "Reject (Esc)"
	case PromptTrust:
		return "Trust this folder", "Exit"
	default:
		return "Approve (y)", "Reject (n)"
	}
}

// DetectApprovalPrompt checks the bottom of the rendered screen for a pending prompt.
func DetectApprovalPrompt(lines []string) (ApprovalPrompt, bool) {
	tail := lines[max(0, len(lines)-menuScanLines):]
	for i, l := range tail {
		if trustRe.MatchString(l) {
			start := max(0, i-10)
			return ApprovalPrompt{Kind: PromptTrust, Context: strings.Join(tail[start:], "\n"), Key: strings.Join(tail[start:], "\n")}, true
		}
	}

	// A choice list first: its options may also start with "1. Yes".
	if p, ok := detectChoice(tail); ok {
		return p, true
	}

	hasYes, hasAnchor := false, false
	for _, l := range tail {
		hasYes = hasYes || menuYesRe.MatchString(l)
		hasAnchor = hasAnchor || menuAnchorRe.MatchString(l)
	}
	if hasYes && hasAnchor {
		anchor := lastAnchor(tail)
		return ApprovalPrompt{
			Kind:    PromptMenu,
			Context: strings.Join(tail[max(0, anchor-menuContextAbove):], "\n"),
			Key:     strings.Join(tail[anchor:], "\n"),
		}, true
	}

	if n := len(lines); n > 0 && yesNoRe.MatchString(lines[n-1]) {
		return ApprovalPrompt{Kind: PromptYesNo, Context: strings.Join(lines[max(0, n-5):], "\n"), Key: lines[n-1]}, true
	}
	return ApprovalPrompt{}, false
}

// lastAnchor finds the last "do you want to…"/"do you trust…" row; searching from
// the bottom avoids anchoring on matching text in tool output above the menu.
func lastAnchor(tail []string) int {
	for i := len(tail) - 1; i >= 0; i-- {
		lower := strings.ToLower(tail[i])
		if strings.Contains(lower, "do you want to") || strings.Contains(lower, "do you trust") {
			return i
		}
	}
	return 0
}
