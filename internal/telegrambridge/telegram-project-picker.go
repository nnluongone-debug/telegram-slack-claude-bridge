package telegrambridge

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
)

const (
	callbackPick   = "pj:"
	buttonsPerRow  = 2
	maxPromptShown = 200
)

// askProject replies in General with one button per project.
func (b *Bridge) askProject(chatID int64) func(pendingID, prompt string, members []config.Project) {
	return func(pendingID, prompt string, members []config.Project) {
		var rows [][]models.InlineKeyboardButton
		for i, p := range members {
			if i%buttonsPerRow == 0 {
				rows = append(rows, nil)
			}
			rows[len(rows)-1] = append(rows[len(rows)-1], models.InlineKeyboardButton{
				Text: p.Name, CallbackData: fmt.Sprintf("%s%s:%d", callbackPick, pendingID, i),
			})
		}
		text := "📁 <b>Which project?</b>\n<i>" + html.EscapeString(truncateRunes(prompt, maxPromptShown)) + "</i>"
		b.poster.send(chatID, 0, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
	}
}

// pickProject handles a project button: start the waiting prompt there.
func (b *Bridge) pickProject(ctx context.Context, cq *models.CallbackQuery, data string, answer func(string)) {
	id, rawIndex, _ := strings.Cut(data, ":")
	index, err := strconv.Atoi(rawIndex)
	ok := err == nil && b.core.PickProject(ctx, id, index, func(project config.Project, prompt string) {
		answer("")
		if msg := cq.Message.Message; msg != nil {
			text := "📁 → <b>" + html.EscapeString(project.Name) + "</b>\n<i>" + html.EscapeString(truncateRunes(prompt, maxPromptShown)) + "</i>"
			_ = b.poster.replace(msg.Chat.ID, msg.ID, text)
		}
	})
	if !ok {
		answer("This choice expired; send your message again.")
	}
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
