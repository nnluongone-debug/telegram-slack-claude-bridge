package chatcore

import (
	"encoding/json"
	"os"
	"sync"
)

// SessionDirectory remembers the last session (and its project) of each chat
// thread on disk, so `!resume` works after a restart even where history can't
// be read (Telegram), and multi-project chats know a topic's project.
type SessionDirectory struct {
	path string // "" keeps it in memory only

	mu      sync.Mutex
	entries map[string]directoryEntry // "<chat>:<thread>" → entry
}

type directoryEntry struct {
	Session string `json:"session"`
	Project string `json:"project,omitempty"`
}

// OpenSessionDirectory loads path if it exists; a missing or corrupt file starts empty.
func OpenSessionDirectory(path string) *SessionDirectory {
	d := &SessionDirectory{path: path, entries: map[string]directoryEntry{}}
	if path == "" {
		return d
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return d
	}
	var stored map[string]json.RawMessage
	if json.Unmarshal(raw, &stored) != nil {
		return d
	}
	for key, value := range stored {
		var e directoryEntry
		if json.Unmarshal(value, &e) != nil {
			_ = json.Unmarshal(value, &e.Session) // first format: just the session ID
		}
		d.entries[key] = e
	}
	return d
}

// Remember records the latest session of a thread and the project it runs in.
func (d *SessionDirectory) Remember(chatID, threadID, sessionID, project string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[chatID+":"+threadID] = directoryEntry{Session: sessionID, Project: project}
	if d.path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(d.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.path + ".tmp" // write-then-rename so a crash never leaves half a file
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, d.path)
}

// Last returns the latest session ID of a thread, or "".
func (d *SessionDirectory) Last(chatID, threadID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.entries[chatID+":"+threadID].Session
}

// ProjectOf returns the project name recorded for a thread, or "".
func (d *SessionDirectory) ProjectOf(chatID, threadID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.entries[chatID+":"+threadID].Project
}
