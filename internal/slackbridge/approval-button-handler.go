package slackbridge

import (
	"context"
	"strings"

	"github.com/slack-go/slack"
)

func (b *Bridge) handleInteraction(ctx context.Context, cb slack.InteractionCallback) {
	defer b.recoverPanic("interaction")

	if cb.Type != slack.InteractionTypeBlockActions {
		return
	}
	for _, action := range cb.ActionCallback.BlockActions {
		switch {
		case !b.cfg.AllowedUserIDs[cb.User.ID]:
			b.log.Warn("unauthorised button click", "user", cb.User.ID)
			b.poster.ephemeral(ctx, cb.Channel.ID, cb.User.ID, ":no_entry: You are not authorised to control agents.")
		case action.ActionID == actionApprove || action.ActionID == actionReject:
			b.handleApprovalClick(ctx, cb, action)
		case strings.HasPrefix(action.ActionID, actionChoice):
			b.resolveButton(ctx, cb, b.core.ResolveChoice(action.Value, "<@"+cb.User.ID+">"))
		case strings.HasPrefix(action.ActionID, actionPickProject):
			b.handleProjectPick(ctx, cb, action)
		}
	}
}

func (b *Bridge) handleApprovalClick(ctx context.Context, cb slack.InteractionCallback, action *slack.BlockAction) {
	approve := action.ActionID == actionApprove
	b.resolveButton(ctx, cb, b.core.ResolveApproval(action.Value, approve, "<@"+cb.User.ID+">"))
}

// resolveButton replaces a message's buttons with the click's outcome.
func (b *Bridge) resolveButton(ctx context.Context, cb slack.InteractionCallback, result string) {
	channel, msgTS := cb.Container.ChannelID, cb.Container.MessageTs
	if channel == "" {
		channel = cb.Channel.ID
	}
	b.log.Info("button click", "user", cb.User.ID, "result", result)
	blocks := resolvedBlocks(cb.Message.Blocks.BlockSet, result)
	if _, _, _, err := b.api.UpdateMessageContext(ctx, channel, msgTS,
		slack.MsgOptionText(result, false), slack.MsgOptionBlocks(blocks...)); err != nil {
		b.log.Error("update approval message failed", "err", err)
	}
}
