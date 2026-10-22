package session

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestProtectSettingsArgs(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	args := protectSettingsArgs(ProtectedPaths{
		NoEdit: []string{filepath.Join(dir, "projects.json"), dir + "/**"},
		NoRead: []string{filepath.Join(dir, ".env")},
	})
	if len(args) != 2 || args[0] != "--settings" {
		t.Fatalf("want --settings <json>, got %q", args)
	}
	var settings struct {
		Permissions struct{ Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(args[1]), &settings); err != nil {
		t.Fatal(err)
	}
	root := absRule(dir)
	want := append(slices.Clone(claudeConfigRules),
		"Edit("+root+"/projects.json)",
		"Edit("+root+"/**)",
		"Edit("+root+"/.env)",
		"Read("+root+"/.env)",
	)
	if !slices.Equal(settings.Permissions.Deny, want) {
		t.Fatalf("deny rules:\n got %q\nwant %q", settings.Permissions.Deny, want)
	}
}

func TestAbsRule(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got := absRule(`C:\Users\a\cfg`); got != "//c/Users/a/cfg" {
			t.Fatalf("got %q", got)
		}
		return
	}
	if got := absRule("/nonexistent-bridge-dir/cfg/**"); got != "//nonexistent-bridge-dir/cfg/**" {
		t.Fatalf("got %q", got)
	}
}
