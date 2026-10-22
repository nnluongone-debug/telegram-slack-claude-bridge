package slackbridge

import (
	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
)

const (
	actionApprove   = "approval_approve"
	actionReject    = "approval_reject"
	approvalBlockID = "approval_actions"
	actionChoice    = "choice:"
	maxButtonChars  = 75   // Slack button text limit
	maxSectionChars = 2900 // Slack section text limit is 3000
)

// approvalBlocks renders the prompt context plus [Approve]/[Reject] buttons.
// Button value = "<sessionID>|<seq>" so stale buttons can be rejected.
func approvalBlocks(sessionID string, seq uint64, prompt terminal.ApprovalPrompt) []slack.Block {
	if prompt.Kind == terminal.PromptChoice {
		return choiceBlocks(sessionID, seq, prompt)
	}
	approveLabel, rejectLabel := prompt.Labels()
	value := chatcore.ApprovalValue(sessionID, seq)

	approve := slack.NewButtonBlockElement(actionApprove, value, plain(approveLabel)).WithStyle(slack.StylePrimary)
	reject := slack.NewButtonBlockElement(actionReject, value, plain(rejectLabel)).WithStyle(slack.StyleDanger)

	return []slack.Block{
		markdownSection(":raised_hand: *Claude is asking for confirmation*"),
		markdownSection(fitCodeBlock(prompt.Context, maxSectionChars)),
		slack.NewActionBlock(approvalBlockID, approve, reject),
	}
}

// choiceBlocks renders a question (e.g. Claude's AskUserQuestion) with one
// button per option.
func choiceBlocks(sessionID string, seq uint64, prompt terminal.ApprovalPrompt) []slack.Block {
	blocks := []slack.Block{markdownSection(":question: *" + escapeSlack(prompt.Question) + "*")}
	if prompt.Context != "" && prompt.Context != prompt.Question {
		blocks = append(blocks, markdownSection(fitCodeBlock(prompt.Context, maxSectionChars)))
	}
	var buttons []slack.BlockElement
	for _, o := range prompt.Options {
		buttons = append(buttons, slack.NewButtonBlockElement(actionChoice+o.Number,
			chatcore.ChoiceValue(sessionID, seq, o.Number), plain(truncateRunes(o.Number+". "+o.Label, maxButtonChars))))
	}
	blocks = append(blocks, slack.NewActionBlock("", buttons...))
	if _, ok := prompt.OwnAnswerOption(); ok {
		blocks = append(blocks, slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, ":writing_hand: Or reply in this thread with your own answer.", false, false)))
	}
	return blocks
}

// resolvedBlocks keeps the original prompt context but swaps the buttons for a result line.
func resolvedBlocks(original []slack.Block, result string) []slack.Block {
	out := make([]slack.Block, 0, len(original)+1)
	for _, b := range original {
		if b.BlockType() != slack.MBTAction {
			out = append(out, b)
		}
	}
	return append(out, slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, result, false, false)))
}

func markdownSection(text string) *slack.SectionBlock {
	return slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil)
}

func plain(text string) *slack.TextBlockObject {
	return slack.NewTextBlockObject(slack.PlainTextType, text, false, false)
}

// fitCodeBlock renders text as a code block within limit chars, dropping rows
// from the top: the command being approved and the menu are at the bottom.
// Trimming happens before escaping so entities like &amp; are never cut.
func fitCodeBlock(text string, limit int) string {
	block := codeBlock(text)
	for keep := limit; len(block) > limit && keep > 0; keep /= 2 {
		block = codeBlock(tailText(text, keep))
	}
	return block
}
