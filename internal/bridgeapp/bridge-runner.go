package bridgeapp

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/slackbridge"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/telegrambridge"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/uploads"
)

const projectsPollInterval = 3 * time.Second

// Platform is a chat connection (Slack, Telegram).
type Platform interface {
	Name() string
	Run(ctx context.Context) error
	Connected() bool
	Err() string // why it stopped, "" while healthy
}

// Runtime exposes the live bridge to a UI (the menu bar app).
type Runtime struct {
	Config    *config.Config
	Sessions  *session.Manager
	Platforms []Platform
}

// Run loads config from the working directory and serves every configured
// platform until ctx is cancelled, then stops all agent sessions. A platform
// that fails (e.g. a bad token) does not take the others down. ready
// (optional) is called once everything is built.
func Run(ctx context.Context, logger *slog.Logger, ready func(*Runtime)) error {
	release, err := AcquireLock()
	if err != nil {
		return err
	}
	defer release()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.ClaudeBin, err = resolveClaudeBin(cfg.ClaudeBin); err != nil {
		return err
	}
	logger.Info("loaded config", "projects", cfg.Projects.Count(), "slack", cfg.SlackEnabled(), "telegram", cfg.TelegramEnabled(), "max_sessions", cfg.MaxSessions)
	cfg.Projects.LogProblems(logger)
	for _, p := range cfg.PlatformProblems {
		logger.Warn("platform disabled", "problem", p)
	}

	supportDir, err := SupportDir()
	if err != nil {
		return err
	}
	store, err := uploads.NewStore(filepath.Join(supportDir, "uploads"))
	if err != nil {
		return err
	}
	sessions := session.NewManager(session.Options{
		Bin:           cfg.ClaudeBin,
		Args:          cfg.ClaudeArgs,
		Cols:          cfg.PTYCols,
		Rows:          cfg.PTYRows,
		FlushInterval: cfg.FlushInterval,
		MirrorScreen:  cfg.MirrorScreen,
		Protected:     protectedPaths(cfg),
		UploadsDir:    store.Dir,
	}, cfg.MaxSessions)
	core := chatcore.New(sessions, chatcore.OpenSessionDirectory(directoryPath()), logger)

	var platforms []Platform
	if cfg.SlackEnabled() {
		platforms = append(platforms, slackbridge.New(cfg, core, store, logger))
	}
	if cfg.TelegramEnabled() {
		tg, err := telegrambridge.New(cfg, core, store, logger)
		if err != nil {
			return err
		}
		platforms = append(platforms, tg)
	}
	if ready != nil {
		ready(&Runtime{Config: cfg, Sessions: sessions, Platforms: platforms})
	}

	go cfg.Projects.Watch(ctx, projectsPollInterval, logger)
	results := make(chan error, len(platforms))
	for _, p := range platforms {
		go func() {
			err := p.Run(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("platform stopped", "platform", p.Name(), "err", err)
				results <- err
				return
			}
			results <- nil
		}()
	}
	var failures []error
	for range platforms {
		if err := <-results; err != nil {
			failures = append(failures, err)
		}
	}

	logger.Info("shutting down, stopping agent sessions", "live", len(sessions.List("")))
	sessions.StopAll()
	if ctx.Err() == nil { // every platform failed on its own
		return errors.Join(failures...)
	}
	return nil
}

// protectedPaths are the bridge's config, tokens and records: .env in the
// working directory, the projects file and the whole support folder. Only
// files are named in the working directory, which may also be a project.
func protectedPaths(cfg *config.Config) session.ProtectedPaths {
	p := session.ProtectedPaths{NoEdit: []string{cfg.Projects.Path()}, NoRead: []string{".env"}}
	if dir, err := SupportDir(); err == nil {
		p.NoEdit = append(p.NoEdit, dir+"/**")
		p.NoRead = append(p.NoRead, filepath.Join(dir, ".env"))
	}
	return p
}

// directoryPath is where thread→session links are kept for `!resume`.
func directoryPath() string {
	dir, err := SupportDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "thread-sessions.json")
}
