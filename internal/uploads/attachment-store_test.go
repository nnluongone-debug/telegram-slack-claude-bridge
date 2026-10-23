package uploads

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveWritesPrivateFileUnderProject(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.Save(context.Background(), "web-app", "notes.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Dir(path) != filepath.Join(store.Dir, "web-app") || !strings.HasSuffix(path, "-notes.txt") {
		t.Fatalf("unexpected path %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "hello" {
		t.Fatalf("content %q, err %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("file mode %v, want 0600", info.Mode().Perm())
		}
		dirInfo, _ := os.Stat(filepath.Dir(path))
		if dirInfo.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode %v, want 0700", dirInfo.Mode().Perm())
		}
	}
}

func TestSaveRejectsOversizedBody(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	store.MaxBytes = 4
	_, err := store.Save(context.Background(), "p", "big.bin", strings.NewReader("12345"))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	left, _ := os.ReadDir(filepath.Join(store.Dir, "p"))
	if len(left) != 0 {
		t.Fatalf("partial file left behind: %v", left)
	}
	if _, err := store.Save(context.Background(), "p", "ok.bin", strings.NewReader("1234")); err != nil {
		t.Fatalf("exact limit should pass: %v", err)
	}
}

func TestSaveKeepsPathsInsideStore(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	path, err := store.Save(context.Background(), "../../etc", `..\..\..\evil.sh`, strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(store.Dir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("escaped the store: %q", path)
	}
	if filepath.Base(filepath.Dir(path)) != "etc" || !strings.HasSuffix(path, "-evil.sh") {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestSanitizeName(t *testing.T) {
	long := strings.Repeat("a", 100) + ".png"
	cases := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"../../.hidden", "hidden"},
		{"C:\\Users\\me\\a:b.txt", "ab.txt"},
		{"bad\x00\nname.txt", "badname.txt"},
		{"  ", "file"},
		{"..", "file"},
		{"", "file"},
		{long, strings.Repeat("a", 76) + ".png"},
		{"tiếng việt.md", "tiếng việt.md"},
	}
	for _, c := range cases {
		if got := sanitizeName(c.in, "file"); got != c.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
