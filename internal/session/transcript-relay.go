package session

import (
	"strings"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

// transcriptWait is how long after the first prompt we wait for Claude's
// transcript file before falling back to mirroring the screen.
const transcriptWait = 45 * time.Second

// relayTranscript forwards new assistant replies and tool calls to the sink.
func (s *Session) relayTranscript() {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if s.tail == nil {
		return
	}

	events, err := s.tail.Poll()
	if err == nil && len(events) > 0 {
		s.sink.Transcript(s, events)
		return
	}
	if s.tail.Found() {
		return
	}

	s.mu.Lock()
	since := s.inputSince
	s.mu.Unlock()
	if since.IsZero() || time.Since(since) < transcriptWait {
		return
	}
	s.tail = nil // transcript saving may be disabled: show the terminal instead
	s.mirror.Store(true)
	s.mu.Lock()
	s.fresh, s.dirty, s.lastRender = true, true, ""
	s.mu.Unlock()
	s.sink.Transcript(s, []transcript.Event{{Kind: transcript.EventNotice,
		Text: "Claude's transcript was not found, so this thread now mirrors the terminal screen instead."}})
}

// ScreenText returns the currently rendered terminal screen.
func (s *Session) ScreenText() string {
	return strings.Join(s.screen.Lines(), "\n")
}
