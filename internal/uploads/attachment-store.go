// Package uploads stores files sent in chats so agent sessions can read them.
package uploads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// MaxBytes is the largest attachment saved (the Telegram Bot API download cap).
const MaxBytes = 20 << 20

// DownloadTimeout bounds one attachment download, body included.
const DownloadTimeout = 5 * time.Minute

// ErrTooLarge means the attachment exceeds MaxBytes.
var ErrTooLarge = errors.New("file is larger than 20 MB")

const maxNameLen = 80

// Store writes attachments under Dir/<project>/. Files are never executable
// and, on macOS, carry the quarantine attribute like a browser download.
type Store struct {
	Dir      string
	MaxBytes int64
}

// NewStore creates the uploads root (Claude may refuse a missing --add-dir).
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create uploads folder: %w", err)
	}
	return &Store{Dir: dir, MaxBytes: MaxBytes}, nil
}

// Save writes body to a new file named after the time and filename and
// returns its absolute path. A body over MaxBytes is discarded (ErrTooLarge).
func (s *Store) Save(ctx context.Context, projectName, filename string, body io.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := filepath.Join(s.Dir, sanitizeName(projectName, "project"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	name := time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(suffix) + "-" + sanitizeName(filename, "file")
	path := filepath.Join(dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(body, s.MaxBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > s.MaxBytes {
		err = ErrTooLarge
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = quarantine(path)
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return filepath.Abs(path)
}

// sanitizeName reduces a chat-supplied name to a safe single path element:
// no separators, control characters or leading dots, at most maxNameLen
// bytes with the extension kept. Empty results become fallback.
func sanitizeName(name, fallback string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || unicode.IsControl(r) || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" {
		return fallback
	}
	if len(name) > maxNameLen {
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		stem := name[:len(name)-len(ext)][:maxNameLen-len(ext)] // may cut a rune; ToValidUTF8 drops the tail
		name = strings.ToValidUTF8(stem, "") + ext
	}
	return name
}
