package telegrambridge

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// commandMenu is what Telegram suggests when you type "/" in a group. Each
// command maps to the core's "!" command of the same name.
var commandMenu = []models.BotCommand{
	{Command: "sessions", Description: "List live sessions in this group"},
	{Command: "status", Description: "This topic's session: ID, project, folder, uptime"},
	{Command: "resume", Description: "Reopen this topic's last session (or /resume <id> in General)"},
	{Command: "restart", Description: "Start a fresh session in this topic (/restart [prompt])"},
	{Command: "stop", Description: "Stop this topic's session"},
	{Command: "model", Description: "Switch model, conversation kept (/model sonnet)"},
	{Command: "mode", Description: "Show or switch permission mode (/mode plan|auto|accept|manual)"},
	{Command: "interrupt", Description: "Send Ctrl-C to Claude"},
	{Command: "esc", Description: "Send Esc to Claude"},
	{Command: "enter", Description: "Send Enter to Claude"},
	{Command: "screen", Description: "Snapshot of Claude's terminal"},
	{Command: "help", Description: "How to use the bridge"},
}

// registerCommands publishes commandMenu for all group chats. A failure only
// loses the suggestions, so it is logged rather than fatal.
func (b *Bridge) registerCommands(ctx context.Context) {
	if _, err := b.bot.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: commandMenu,
		Scope:    &models.BotCommandScopeAllGroupChats{},
	}); err != nil {
		b.log.Warn("could not register telegram command menu", "err", err)
	}
}
