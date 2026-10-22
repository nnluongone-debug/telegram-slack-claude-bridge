package session

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

var (
	// ErrSessionExists means the thread already has a live agent.
	ErrSessionExists = errors.New("a session is already running in this thread")
	// ErrTooManySessions means MaxSessions was reached.
	ErrTooManySessions = errors.New("maximum number of concurrent sessions reached")
	// ErrSessionOpenElsewhere means the Claude session being resumed is live in another thread.
	ErrSessionOpenElsewhere = errors.New("that Claude session is already open in another thread")
	// ErrSessionOtherProject means the session being resumed ran in another project's folder.
	ErrSessionOtherProject = errors.New("that session belongs to another project")
	// ErrShuttingDown means StopAll has been called.
	ErrShuttingDown = errors.New("bridge is shutting down")
)

// Manager owns all live sessions, indexed by Slack thread and by session ID.
type Manager struct {
	opts        Options
	maxSessions int

	mu       sync.RWMutex
	byThread map[string]*Session
	byID     map[string]*Session
	closed   bool
}

// NewManager creates an empty session registry.
func NewManager(opts Options, maxSessions int) *Manager {
	return &Manager{
		opts:        opts,
		maxSessions: maxSessions,
		byThread:    map[string]*Session{},
		byID:        map[string]*Session{},
	}
}

func threadKey(channelID, threadTS string) string { return channelID + ":" + threadTS }

// Start spawns a new agent for the given thread. The session ID is also passed
// to Claude (--session-id), so it names the transcript and can be resumed later.
func (m *Manager) Start(channelID, threadTS string, project config.Project, initialPrompt string, sink Sink) (*Session, error) {
	return m.start(channelID, threadTS, project, uuid.NewString(), initialPrompt, false, nil, sink)
}

// Resume reopens an existing Claude conversation (claude --resume <id>) in the given thread.
// The conversation must belong to the project's workdir. extraArgs (e.g. --model)
// apply to this process only and are never saved as Claude defaults.
func (m *Manager) Resume(channelID, threadTS string, project config.Project, claudeSessionID string, extraArgs []string, sink Sink) (*Session, error) {
	id, err := uuid.Parse(claudeSessionID)
	if err != nil {
		return nil, fmt.Errorf("invalid session ID %q", claudeSessionID)
	}
	if transcript.Exists(id.String()) && !transcript.SessionBelongsTo(id.String(), project.WorkDir) {
		return nil, ErrSessionOtherProject
	}
	return m.start(channelID, threadTS, project, id.String(), "", true, extraArgs, sink)
}

func (m *Manager) start(channelID, threadTS string, project config.Project, id, initialPrompt string, resume bool, extraArgs []string, sink Sink) (*Session, error) {
	key := threadKey(channelID, threadTS)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrShuttingDown
	}
	if _, ok := m.byThread[key]; ok {
		return nil, ErrSessionExists
	}
	if _, ok := m.byID[id]; ok {
		return nil, ErrSessionOpenElsewhere
	}
	if len(m.byID) >= m.maxSessions {
		return nil, fmt.Errorf("%w (%d)", ErrTooManySessions, m.maxSessions)
	}

	s := &Session{ID: id, ChannelID: channelID, ThreadTS: threadTS, Project: project, sink: sink}
	switch {
	case resume:
		s.Worktree = transcript.WorktreeName(id, project.WorkDir) // claude --resume returns to it by itself
	case m.wantsWorktreeLocked(project):
		s.Worktree = "bridge-" + id[:8]
		extraArgs = append(slices.Clone(extraArgs), "--worktree", s.Worktree)
	}
	if err := startSession(s, initialPrompt, resume, extraArgs, m.opts, m.remove); err != nil {
		return nil, err
	}
	m.byThread[key] = s
	m.byID[s.ID] = s
	return s, nil
}

// wantsWorktreeLocked decides whether a new session gets its own git worktree:
// always/never per project, otherwise only when another live session already
// works in the project folder itself. Caller holds m.mu.
func (m *Manager) wantsWorktreeLocked(project config.Project) bool {
	switch {
	case project.Worktree == "never":
		return false
	case project.Worktree != "always" && len(m.inFolderLocked(project.WorkDir, "")) == 0:
		return false
	default:
		return isGitRepo(project.WorkDir)
	}
}

// SharingFolder lists other live sessions working directly in s's folder
// (empty when s runs in its own worktree).
func (m *Manager) SharingFolder(s *Session) []*Session {
	if s.Worktree != "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.inFolderLocked(s.Project.WorkDir, s.ID)
}

func (m *Manager) inFolderLocked(workDir, exceptID string) []*Session {
	var out []*Session
	for _, s := range m.byID {
		if s.ID != exceptID && s.Worktree == "" && s.Project.WorkDir == workDir {
			out = append(out, s)
		}
	}
	return out
}

// ByThread finds the live session bound to a Slack thread.
func (m *Manager) ByThread(channelID, threadTS string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byThread[threadKey(channelID, threadTS)]
	return s, ok
}

// ByID finds a live session by its UUID (used by button callbacks).
func (m *Manager) ByID(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byID[id]
	return s, ok
}

// List returns live sessions, optionally filtered to one channel, oldest first.
func (m *Manager) List(channelID string) []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.byID))
	for _, s := range m.byID {
		if channelID == "" || s.ChannelID == channelID {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b *Session) int { return a.StartedAt.Compare(b.StartedAt) })
	return out
}

// StopAll terminates every session and waits for them to exit.
func (m *Manager) StopAll() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	sessions := m.List("")
	for _, s := range sessions {
		s.Stop()
	}
	for _, s := range sessions {
		<-s.Done()
	}
}

func (m *Manager) remove(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key := threadKey(s.ChannelID, s.ThreadTS); m.byThread[key] == s {
		delete(m.byThread, key)
	}
	delete(m.byID, s.ID)
}
