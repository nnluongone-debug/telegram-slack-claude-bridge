package bridgeapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// claudeInstallDirs are where the claude CLI is usually installed, relative to
// $HOME when not absolute. Apps opened at login may start with a bare PATH.
var claudeInstallDirs = []string{".local/bin", ".claude/local", ".npm-global/bin", "/opt/homebrew/bin", "/usr/local/bin"}

// resolveClaudeBin returns an absolute path for bin: as given if it is a path,
// else from PATH, else from the usual install locations.
func resolveClaudeBin(bin string) (string, error) {
	if strings.ContainsAny(bin, `/\`) {
		if isExecutable(bin) {
			return bin, nil
		}
		return "", fmt.Errorf("CLAUDE_BIN %q is not an executable file", bin)
	}
	if path, err := exec.LookPath(bin); err == nil && !isScriptShim(path) {
		return path, nil
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" && filepath.Ext(bin) == "" {
		bin += ".exe" // the native installer puts claude.exe in %USERPROFILE%\.local\bin
	}
	for _, dir := range claudeInstallDirs {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(home, dir)
		}
		if path := filepath.Join(dir, bin); isExecutable(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found on PATH or in ~/.local/bin, /opt/homebrew/bin, /usr/local/bin (set CLAUDE_BIN to its full path)", bin)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0 // Windows has no execute bit
}

// isScriptShim spots the npm claude.cmd on Windows: the agent runner refuses
// scripts, so the search goes on to the native claude.exe.
func isScriptShim(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat")
}
