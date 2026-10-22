// Command claude-bridge drives local Claude Code agents from Slack and Telegram.
// It reads .env and projects.json from the working directory.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/bridgeapp"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bridgeapp.Run(ctx, logger, nil); err != nil {
		logger.Error("bridge stopped with error", "err", err)
		os.Exit(1)
	}
}
