package slackbridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
)

const (
	actionPickProject = "pick_project"
	maxPromptShown    = 200
)

// askProject posts "Which project?" buttons in the thread under the message.
func (b *Bridge) askProject(conv *slackConversation) func(pendingID, prompt string, members []config.Project) {
	return func(pendingID, prompt string, members []config.Project) {
		var buttons []slack.BlockElement
		for i, p := range members {
			buttons = append(buttons, slack.NewButtonBlockElement(
				fmt.Sprintf("%s:%d", actionPickProject, i), fmt.Sprintf("%s:%d", pendingID, i), plain(p.Name)))
		}
		text := ":file_folder: *Which project?*\n_" + escapeSlack(truncateRunes(prompt, maxPromptShown)) + "_"
		b.poster.post(conv.channel, conv.messageTS, slack.MsgOptionText("Which project?", false),
			slack.MsgOptionBlocks(markdownSection(text), slack.NewActionBlock("", buttons...)))
	}
}

// handleProjectPick starts the waiting prompt in the clicked project.
func (b *Bridge) handleProjectPick(ctx context.Context, cb slack.InteractionCallback, action *slack.BlockAction) {
	channel, msgTS := cb.Container.ChannelID, cb.Container.MessageTs
	if channel == "" {
		channel = cb.Channel.ID
	}
	id, rawIndex, _ := strings.Cut(action.Value, ":")
	index, err := strconv.Atoi(rawIndex)
	update := func(text string) {
		if _, _, _, err := b.api.UpdateMessageContext(ctx, channel, msgTS,
			slack.MsgOptionText(text, false), slack.MsgOptionBlocks(markdownSection(text))); err != nil {
			b.log.Warn("update project picker failed", "err", err)
		}
	}
	ok := err == nil && b.core.PickProject(ctx, id, index, func(project config.Project, prompt string) {
		update(":file_folder: → *" + escapeSlack(project.Name) + "*\n_" + escapeSlack(truncateRunes(prompt, maxPromptShown)) + "_")
	})
	if !ok {
		update(":hourglass: This choice expired; send your message again.")
	}
}
