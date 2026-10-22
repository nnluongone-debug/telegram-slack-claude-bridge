package bridgeapp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWriteMissingConfigKeepsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "projects.json"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := WriteMissingConfig(dir, []byte("ENV"), []byte("example"))
	if err != nil || !slices.Equal(created, []string{".env"}) {
		t.Fatalf("created %q, err %v", created, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "projects.json")); string(got) != "mine" {
		t.Fatalf("existing projects.json was overwritten: %q", got)
	}
	if info, err := os.Stat(filepath.Join(dir, ".env")); err != nil || (os.PathSeparator == '/' && info.Mode().Perm() != 0o600) {
		t.Fatalf(".env must be private: %v %v", info.Mode(), err)
	}
	if again, err := WriteMissingConfig(dir, nil, nil); err != nil || len(again) != 0 {
		t.Fatalf("second run must create nothing: %q %v", again, err)
	}
}
