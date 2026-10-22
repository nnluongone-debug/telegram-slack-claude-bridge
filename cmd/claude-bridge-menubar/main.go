//go:build darwin

// Command claude-bridge-menubar is the macOS menu bar app of the Slack ↔ Claude
// bridge. Config lives in ~/Library/Application Support/ClaudeBridge and the
// log in ~/Library/Logs/ClaudeBridge.
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fyne.io/systray"

	claudebridge "github.com/nnluongone-debug/telegram-slack-claude-bridge"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/assets"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/bridgeapp"
)

const (
	// Right after login the Mac can be busy enough that loading .zshrc takes
	// well over 15s; give it time and keep retrying in the background.
	shellEnvTimeout  = 60 * time.Second
	shellEnvAttempts = 5
	shellEnvRetry    = 30 * time.Second
)

// current is the running app; onExit uses it when macOS quits the app
// (logout, `osascript … quit`) so agent sessions still stop cleanly.
var current *app

func main() {
	systray.Run(onReady, func() {
		if current != nil {
			current.stop()
		}
	})
}

func onReady() {
	systray.SetTemplateIcon(assets.MenubarTemplate, assets.MenubarTemplate)
	systray.SetTooltip("Claude Bridge")

	logFile, logPath, err := bridgeapp.OpenLogFile()
	if err != nil {
		logFile, logPath = os.Stderr, ""
	}
	log := slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dir, err := bridgeapp.SupportDir()
	if err == nil {
		err = os.Chdir(dir) // .env and projects.json are read from here
	}
	if err == nil {
		var created []string
		if created, err = bridgeapp.WriteMissingConfig(dir, claudebridge.ExampleEnv, claudebridge.ExampleProjects); len(created) > 0 {
			log.Info("created example config, edit it from the menu", "files", created, "dir", dir)
		}
	}
	a := &app{log: log, dir: dir, logPath: logPath}
	current = a
	a.buildMenu()
	if err != nil {
		a.setErr("config folder: " + err.Error())
		return
	}

	go func() {
		imported := importShellEnv(log, 1)
		a.start() // works without the shell environment too: claude is also found in its usual install folders
		for attempt := 2; !imported && attempt <= shellEnvAttempts; attempt++ {
			time.Sleep(shellEnvRetry)
			if imported = importShellEnv(log, attempt); imported && a.err() != "" {
				a.retryNow() // a failed start may have been missing the shell's PATH
			}
		}
	}()
	go a.refreshLoop()

	// Logout/shutdown sends SIGTERM: stop agents cleanly first.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-sig
		a.quit()
	}()
}

func importShellEnv(log *slog.Logger, attempt int) bool {
	err := bridgeapp.ImportLoginShellEnv(shellEnvTimeout)
	if err != nil {
		log.Warn("could not import login shell environment yet", "attempt", attempt, "err", err)
		return false
	}
	log.Info("imported login shell environment", "attempt", attempt)
	return true
}
