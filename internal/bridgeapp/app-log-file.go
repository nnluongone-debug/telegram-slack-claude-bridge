package bridgeapp

import (
	"os"
	"path/filepath"
)

const maxLogBytes = 10 << 20

// LogPath is ~/Library/Logs/ClaudeBridge/bridge.log (Console.app shows it).
func LogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Logs", "ClaudeBridge")
	return filepath.Join(dir, "bridge.log"), os.MkdirAll(dir, 0o700)
}

// OpenLogFile opens the app log for appending, keeping one rotated copy
// (bridge.log.1) once it grows past 10 MB.
func OpenLogFile() (*os.File, string, error) {
	path, err := LogPath()
	if err != nil {
		return nil, "", err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	return f, path, err
}
