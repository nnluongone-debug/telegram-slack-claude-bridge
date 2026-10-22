// Package session runs Claude CLI processes inside PTYs and bridges their I/O.
package session

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

// Sink receives everything a session produces. Calls come from one goroutine
// per session, in order.
type Sink interface {
	// Screen delivers the rendered terminal; fresh asks for a new message instead
	// of editing the last one. Returning false means "not delivered, retry".
	Screen(s *Session, text string, fresh bool) bool
	// ApprovalRequested posts approve/reject buttons; false means "not posted, retry".
	ApprovalRequested(s *Session, seq uint64, prompt terminal.ApprovalPrompt) bool
	// Transcript delivers the assistant's replies and tool calls read from Claude's transcript.
	Transcript(s *Session, events []transcript.Event)
	// Status shows what Claude is doing while busy: activity is its spinner row,
	// recent the latest output rows above it. busy=false clears it.
	// Returning false means "not delivered, retry".
	Status(s *Session, activity, recent string, busy bool) bool
	Exited(s *Session, err error)
}

// Options configures how the agent process is spawned.
type Options struct {
	Bin           string
	Args          []string
	Cols, Rows    uint16
	FlushInterval time.Duration
	// MirrorScreen posts the rendered terminal instead of transcript replies.
	MirrorScreen bool
	// Protected are the bridge's own files, which the agent may not touch.
	Protected ProtectedPaths
	// PlainCLI marks an agent that is not Claude Code's TUI (tests use a shell):
	// input is written without waiting for or verifying Claude's prompt box.
	PlainCLI bool
}

// Session is one Claude CLI process bound to one Slack thread.
type Session struct {
	ID        string // also Claude's own session ID (transcript ~/.claude/projects/<dir>/<ID>.jsonl)
	ChannelID string
	ThreadTS  string
	Project   config.Project
	Worktree  string // Claude worktree name under <workdir>/.claude/worktrees, "" = the folder itself
	StartedAt time.Time

	proc   agentProcess
	screen *terminal.VirtualScreen
	sink   Sink

	writeMu sync.Mutex // serialises PTY writes
	flushMu sync.Mutex // serialises flushes so Slack output stays ordered

	mu          sync.Mutex // guards the fields below
	dirty       bool
	lastRender  string
	lastAlt     bool
	fresh       bool
	approval    *terminal.ApprovalPrompt
	approvalSeq uint64
	answeredKey string
	answeredAt  time.Time
	inputSince  time.Time // first prompt sent; bounds the wait for the transcript file

	plainCLI   bool
	lastStatus string           // last status delivered to the sink; guarded by flushMu
	tail       *transcript.Tail // nil when mirroring the screen
	mirror     atomic.Bool

	replies    chan []byte
	readerDone chan struct{}
	done       chan struct{}
	exited     atomic.Bool
	stopOnce   sync.Once
}

// maxPromptArg is the longest first prompt passed as an argument. Windows
// caps a whole command line at 32,767 characters and Linux one argument at
// 128 KiB; a longer prompt is typed into the session instead.
const maxPromptArg = 16 << 10

func startSession(s *Session, initialPrompt string, resume bool, extraArgs []string, opts Options, onExit func(*Session)) error {
	args := slices.Concat(opts.Args, s.Project.Args, extraArgs)
	if !opts.PlainCLI { // --settings is a Claude Code flag
		args = append(args, protectSettingsArgs(opts.Protected)...)
	}
	if resume {
		args = append(args, "--resume", s.ID)
	} else {
		args = append(args, "--session-id", s.ID)
	}
	var typedPrompt string
	if len(initialPrompt) > maxPromptArg {
		typedPrompt, initialPrompt = initialPrompt, "" // too long for a command line: type it in
	}
	if initialPrompt != "" {
		if strings.HasPrefix(initialPrompt, "-") {
			initialPrompt = " " + initialPrompt // never let a prompt be parsed as a CLI flag
		}
		args = append(args, initialPrompt)
	}

	// TERM is replaced, not appended: Windows reads the first of duplicate variables.
	env := append(filterEnv(os.Environ(), append(slices.Clone(inheritedSessionEnv), "TERM")), "TERM=xterm-256color")
	proc, err := startAgentProcess(opts.Bin, args, s.Project.WorkDir, env, opts.Cols, opts.Rows)
	if err != nil {
		return fmt.Errorf("start %s in %s: %w", opts.Bin, s.Project.WorkDir, err)
	}
	s.proc = proc
	s.replies = make(chan []byte, replyQueueSize)
	s.screen = terminal.NewVirtualScreen(int(opts.Cols), int(opts.Rows), ptyReplyWriter{s})
	s.fresh = true
	s.mirror.Store(opts.MirrorScreen)
	s.plainCLI = opts.PlainCLI
	if !opts.MirrorScreen {
		s.tail = transcript.NewTail(s.ID)
	}
	if initialPrompt != "" {
		s.inputSince = time.Now()
	}
	s.readerDone = make(chan struct{})
	s.done = make(chan struct{})
	s.StartedAt = time.Now()

	go s.readLoop()
	go s.replyLoop()
	go s.flushLoop(opts.FlushInterval)
	go s.waitLoop(onExit)
	if typedPrompt != "" {
		go func() { _ = s.SendText(typedPrompt) }() // waits for Claude's input box
	}
	return nil
}

// Done is closed once the process has exited and its final output and exit notice were delivered.
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) readLoop() {
	defer close(s.readerDone)
	buf := make([]byte, 32*1024)
	for {
		n, err := s.proc.Read(buf)
		if n > 0 {
			_, _ = s.screen.Write(buf[:n])
			s.mu.Lock()
			s.dirty = true
			s.mu.Unlock()
		}
		if err != nil {
			return // EIO once the child exits, or closed file
		}
	}
}

func (s *Session) flushLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.relayTranscript() // replies first, so they precede any approval buttons
			s.flush()
		}
	}
}
