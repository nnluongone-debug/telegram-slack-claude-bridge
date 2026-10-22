// Package transcript follows Claude Code's session transcript
// (~/.claude/projects/<dir>/<session-id>.jsonl) and turns new entries into
// chat-friendly events: the assistant's markdown replies and tool calls.
package transcript

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Tail incrementally reads one session's transcript. Not safe for concurrent use.
type Tail struct {
	sessionID string
	path      string
	offset    int64
	partial   []byte // trailing line not yet terminated by '\n'
}

// NewTail starts following sessionID. If the transcript already exists
// (a resumed session) only entries written from now on are reported.
func NewTail(sessionID string) *Tail {
	t := &Tail{sessionID: sessionID}
	if path, ok := locate(sessionID); ok {
		t.path = path
		if info, err := os.Stat(path); err == nil {
			t.offset = info.Size()
		}
	}
	return t
}

// Found reports whether the transcript file has been located.
func (t *Tail) Found() bool { return t.path != "" }

// Poll returns events from entries appended since the previous call.
func (t *Tail) Poll() ([]Event, error) {
	if t.path == "" {
		path, ok := locate(t.sessionID)
		if !ok {
			return nil, nil
		}
		t.path = path // created after we started: read it from the beginning
	}

	f, err := os.Open(t.path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek transcript: %w", err)
	}
	chunk, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	t.offset += int64(len(chunk))

	data := append(t.partial, chunk...)
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		t.partial = data
		return nil, nil
	}
	t.partial = bytes.Clone(data[end+1:])

	var events []Event
	for _, line := range bytes.Split(data[:end], []byte{'\n'}) {
		events = append(events, ParseLine(line)...)
	}
	return events, nil
}

// locate finds <config>/projects/*/<sessionID>.jsonl. Searching by the unique
// session ID avoids re-implementing Claude's cwd→directory name encoding.
func locate(sessionID string) (string, bool) {
	matches, err := filepath.Glob(filepath.Join(configDir(), "projects", "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

func configDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}
