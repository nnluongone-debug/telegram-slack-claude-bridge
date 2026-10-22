package terminal

import (
	"regexp"
	"strings"
)

// PromptChoice is a numbered list picked by digit, e.g. Claude's AskUserQuestion
// dialog ("Which color? ❯ 1. Red 2. Green … Enter to select") and its
// "Ready to submit your answers? 1. Submit answers 2. Cancel" review page.
const PromptChoice PromptKind = 100

var (
	choiceAnchorRe = regexp.MustCompile(`(?i)enter to select|✔\s*submit`)
	choiceOptionRe = regexp.MustCompile(`^\s*(?:❯\s*)?([1-9])\.\s+(\S.*?)\s*$`)
	typeOwnRe      = regexp.MustCompile(`(?i)^type something`)
	tabBarRe       = regexp.MustCompile(`[☐☒]|✔\s*submit|^\s*←|→\s*$`)
)

// ChoiceOption is one numbered option of a PromptChoice.
type ChoiceOption struct {
	Number string // the digit that selects it
	Label  string
}

// detectChoice finds a numbered choice list at the bottom of the screen.
func detectChoice(tail []string) (ApprovalPrompt, bool) {
	anchored := false
	for _, l := range tail {
		anchored = anchored || choiceAnchorRe.MatchString(l)
	}
	if !anchored {
		return ApprovalPrompt{}, false
	}

	// The last "1." row starts the current list; options continue 2, 3…
	// with indented description rows in between.
	first := -1
	for i := len(tail) - 1; i >= 0; i-- {
		if m := choiceOptionRe.FindStringSubmatch(tail[i]); m != nil && m[1] == "1" {
			first = i
			break
		}
	}
	if first < 0 {
		return ApprovalPrompt{}, false
	}
	var options []ChoiceOption
	for _, l := range tail[first:] {
		if m := choiceOptionRe.FindStringSubmatch(l); m != nil && m[1] == string(rune('1'+len(options))) {
			options = append(options, ChoiceOption{Number: m[1], Label: m[2]})
		}
	}
	if len(options) < 2 {
		return ApprovalPrompt{}, false
	}

	question, start := "", first
	for i := first - 1; i >= 0; i-- {
		if l := strings.TrimSpace(tail[i]); l != "" && !tabBarRe.MatchString(l) && !strings.HasPrefix(l, "❯") {
			question, start = l, i
			break
		}
	}
	// On the review page, include the answers summary ("● question", "→ answer").
	for start > 0 && isSummaryRow(tail[start-1]) {
		start--
	}
	key := question
	for _, o := range options {
		key += "\n" + o.Number + ". " + o.Label
	}
	return ApprovalPrompt{Kind: PromptChoice, Context: strings.Join(tail[start:first], "\n"), Key: key, Question: question, Options: options}, true
}

func isSummaryRow(l string) bool {
	l = strings.TrimSpace(l)
	return strings.HasPrefix(l, "●") || strings.HasPrefix(l, "→")
}

// OwnAnswerOption returns the "Type something." option, if the list has one.
func (p ApprovalPrompt) OwnAnswerOption() (ChoiceOption, bool) {
	for _, o := range p.Options {
		if typeOwnRe.MatchString(o.Label) {
			return o, true
		}
	}
	return ChoiceOption{}, false
}
