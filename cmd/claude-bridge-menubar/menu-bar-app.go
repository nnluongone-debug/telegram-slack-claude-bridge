//go:build darwin

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/bridgeapp"
)

const (
	refreshInterval = 2 * time.Second
	retryDelay      = 30 * time.Second
	stopTimeout     = 20 * time.Second
)

type app struct {
	log          *slog.Logger
	dir, logPath string

	runtime atomic.Pointer[bridgeapp.Runtime]
	lastErr atomic.Pointer[string]

	mu     sync.Mutex // guards cancel/done/retry of the current bridge run
	cancel context.CancelFunc
	done   chan struct{}
	retry  chan struct{}

	status, problems, login *systray.MenuItem
}

func (a *app) buildMenu() {
	a.status = systray.AddMenuItem("Starting…", "")
	a.status.Disable()
	a.problems = systray.AddMenuItem("", "Fix the folder in projects.json; it reloads automatically")
	a.problems.Hide()
	systray.AddSeparator()
	projects := systray.AddMenuItem("Edit projects…", "Channel → folder mapping; changes apply automatically")
	settings := systray.AddMenuItem("Edit settings (.env)…", "Tokens and options; use Restart bridge after editing")
	folder := systray.AddMenuItem("Open config folder", a.dir)
	logs := systray.AddMenuItem("Open log", a.logPath)
	systray.AddSeparator()
	restart := systray.AddMenuItem("Restart bridge", "Reload .env and reconnect (running sessions are stopped; use !resume)")
	a.login = systray.AddMenuItemCheckbox("Open at login", "", bridgeapp.LaunchAtLoginEnabled())
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Stop all agent sessions and quit")

	go func() {
		for {
			select {
			case <-projects.ClickedCh:
				a.open("-t", filepath.Join(a.dir, "projects.json"))
			case <-settings.ClickedCh:
				a.open("-t", filepath.Join(a.dir, ".env"))
			case <-folder.ClickedCh:
				a.open(a.dir)
			case <-logs.ClickedCh:
				a.open("-a", "Console", a.logPath)
			case <-restart.ClickedCh:
				go func() { a.stop(); a.start() }()
			case <-a.login.ClickedCh:
				a.toggleLogin()
			case <-quit.ClickedCh:
				go a.quit()
			}
		}
	}()
}

// start runs the bridge in the background. If it stops with an error (no
// network yet after login, a config typo…) it retries every retryDelay until
// stopped, so the bridge recovers on its own; the error shows in the menu.
func (a *app) start() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	retry := make(chan struct{}, 1)
	a.mu.Lock()
	a.cancel, a.done, a.retry = cancel, done, retry
	a.mu.Unlock()
	a.setErr("")

	go func() {
		defer close(done)
		for {
			err := bridgeapp.Run(ctx, a.log, func(rt *bridgeapp.Runtime) { a.runtime.Store(rt) })
			a.runtime.Store(nil)
			if err == nil || ctx.Err() != nil {
				return
			}
			a.log.Error("bridge stopped with error; retrying", "err", err, "in", retryDelay)
			a.setErr(err.Error() + " (retrying…)")
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryDelay):
			case <-retry:
			}
			a.setErr("")
		}
	}()
}

// retryNow skips the wait before the next start attempt.
func (a *app) retryNow() {
	a.mu.Lock()
	retry := a.retry
	a.mu.Unlock()
	select {
	case retry <- struct{}{}:
	default:
	}
}

// stop cancels the bridge and waits for agent sessions to shut down.
func (a *app) stop() {
	a.mu.Lock()
	cancel, done := a.cancel, a.done
	a.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(stopTimeout):
		a.log.Warn("bridge did not stop in time")
	}
}

func (a *app) quit() {
	a.status.SetTitle("Stopping sessions…")
	a.stop()
	systray.Quit()
}

func (a *app) refreshLoop() {
	for range time.Tick(refreshInterval) {
		title, text := "", "Starting…" // title: text next to the menu bar icon
		switch rt := a.runtime.Load(); {
		case a.err() != "":
			title, text = "⚠️", "⚠️ "+truncate(a.err(), 70)
		case rt == nil:
		default:
			var marks []string
			for _, p := range rt.Platforms {
				mark := "✓"
				if !p.Connected() {
					mark, title = "…", "…"
				}
				marks = append(marks, p.Name()+" "+mark)
			}
			text = fmt.Sprintf("%s · %d session(s) · %d project(s)", strings.Join(marks, " · "), len(rt.Sessions.List("")), rt.Config.Projects.Count())
		}
		systray.SetTitle(title)
		a.status.SetTitle(text)
		a.showProblems(a.runtime.Load())
	}
}

// showProblems surfaces skipped projects, e.g. "⚠️ mobile: folder not found (…)".
func (a *app) showProblems(rt *bridgeapp.Runtime) {
	var problems []string
	if rt != nil {
		for _, p := range rt.Platforms {
			if err := p.Err(); err != "" {
				problems = append(problems, err)
			}
		}
		problems = append(problems, rt.Config.PlatformProblems...)
		problems = append(problems, rt.Config.Projects.Problems()...)
	}
	if len(problems) == 0 {
		a.problems.Hide()
		return
	}
	text := "⚠️ " + truncate(problems[0], 70)
	if len(problems) > 1 {
		text += fmt.Sprintf(" (+%d more, see log)", len(problems)-1)
	}
	a.problems.SetTitle(text)
	a.problems.Show()
}

func (a *app) toggleLogin() {
	appPath := bridgeapp.AppBundlePath()
	if appPath == "" {
		a.log.Warn("open at login needs the packaged app (make install-app)")
		return
	}
	enable := !a.login.Checked()
	if err := bridgeapp.SetLaunchAtLogin(enable, appPath); err != nil {
		a.log.Error("open at login", "err", err)
		return
	}
	if enable {
		a.login.Check()
	} else {
		a.login.Uncheck()
	}
}

func (a *app) open(args ...string) {
	if err := exec.Command("open", args...).Run(); err != nil {
		a.log.Error("open failed", "args", args, "err", err)
	}
}

func (a *app) setErr(msg string) { a.lastErr.Store(&msg) }

func (a *app) err() string {
	if p := a.lastErr.Load(); p != nil {
		return *p
	}
	return ""
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
