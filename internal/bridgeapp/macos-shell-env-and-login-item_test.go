//go:build darwin

package bridgeapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportLoginShellEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("ZDOTDIR", home)
	// An rc file that prints noise and extends PATH, like a real .zshrc.
	rc := "echo 'welcome banner'\nexport PATH=\"/opt/custom/bin:$PATH\"\nexport BRIDGE_TEST_VAR='a b=c'\n"
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("BRIDGE_TEST_VAR", "")

	if err := ImportLoginShellEnv(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(os.Getenv("PATH"), "/opt/custom/bin:") || os.Getenv("BRIDGE_TEST_VAR") != "a b=c" {
		t.Fatalf("PATH=%q BRIDGE_TEST_VAR=%q", os.Getenv("PATH"), os.Getenv("BRIDGE_TEST_VAR"))
	}
}

func TestLaunchAtLoginPlist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if LaunchAtLoginEnabled() {
		t.Fatal("should start disabled")
	}
	if err := SetLaunchAtLogin(true, "/Applications/Claude Bridge.app"); err != nil {
		t.Fatal(err)
	}
	path, _ := launchAgentPath()
	raw, _ := os.ReadFile(path)
	if !LaunchAtLoginEnabled() || !strings.Contains(string(raw), "<string>/Applications/Claude Bridge.app</string>") {
		t.Fatalf("plist:\n%s", raw)
	}
	if err := SetLaunchAtLogin(false, ""); err != nil || LaunchAtLoginEnabled() {
		t.Fatalf("disable failed: %v", err)
	}
}

func TestResolveClaudeBinFallsBackToInstallDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin") // like an app started at login
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "claude-test-bin")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveClaudeBin("claude-test-bin"); err != nil || got != fake {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := resolveClaudeBin("definitely-missing-bin"); err == nil {
		t.Fatal("missing binary must be an error")
	}
	if got, err := resolveClaudeBin(fake); err != nil || got != fake {
		t.Fatalf("absolute path: %q %v", got, err)
	}
}
