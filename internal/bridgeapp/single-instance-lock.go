// Package bridgeapp wires config, sessions and Slack into a runnable bridge,
// shared by the CLI and the macOS menu bar app.
package bridgeapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrAlreadyRunning means another bridge (CLI or app) holds the lock. Two
// bridges on one Slack app would split events between them at random.
var ErrAlreadyRunning = errors.New("another Claude bridge is already running on this computer")

// AcquireLock takes a machine-wide lock; the OS frees it if the process dies.
func AcquireLock() (release func(), err error) {
	dir, err := SupportDir()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "bridge.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		unlockFile(f)
		f.Close()
	}, nil
}

// SupportDir is the bridge's folder for records and the app's config (created
// if missing): ~/Library/Application Support/ClaudeBridge on macOS,
// ~/.config/ClaudeBridge on Linux, %AppData%\ClaudeBridge on Windows.
func SupportDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "ClaudeBridge")
	return dir, os.MkdirAll(dir, 0o700)
}
