package session

import (
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	stopGracePeriod = 5 * time.Second
	readerDrainWait = 2 * time.Second
)

// inheritedSessionEnv are markers a parent Claude Code session sets; a child
// agent that inherits them believes it is nested and degrades its behaviour.
var inheritedSessionEnv = []string{
	"CLAUDECODE", "CLAUDE_PID", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_CODE_EXECPATH",
	// The bridge's own credentials must never reach the agent or the tools it runs.
	"SLACK_BOT_TOKEN", "SLACK_APP_TOKEN", "TELEGRAM_BRIDGE_BOT_TOKEN",
}

// Stop terminates the agent and its child processes, force-killing them after a grace period.
func (s *Session) Stop() {
	if s.exited.Load() {
		return // the group ID may already be reused by an unrelated process
	}
	s.stopOnce.Do(func() {
		s.proc.Terminate()
		go func() {
			select {
			case <-s.done:
			case <-time.After(stopGracePeriod):
				if !s.exited.Load() { // once reaped, its PID may belong to another process
					s.proc.Kill()
				}
			}
		}()
	})
}

func (s *Session) waitLoop(onExit func(*Session)) {
	err := s.proc.Wait()
	s.exited.Store(true)
	s.proc.Terminate() // reap leftover background jobs of this agent
	select {           // let the reader drain buffered output before closing the PTY
	case <-s.readerDone:
	case <-time.After(readerDrainWait):
	}
	_ = s.proc.Close()
	<-s.readerDone

	s.mu.Lock()
	s.fresh = true // the exit screen must not overwrite the last live view
	s.mu.Unlock()
	s.relayTranscript()
	s.flush()

	onExit(s) // unregister before Done fires so `!restart` can reuse the thread
	s.sink.Exited(s, err)
	close(s.done)
}

// filterEnv removes the drop variables from env. Windows variable names are
// case-insensitive, so there "slack_bot_token" is dropped too.
func filterEnv(env, drop []string) []string {
	return slices.DeleteFunc(env, func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		return slices.ContainsFunc(drop, func(d string) bool {
			return d == key || runtime.GOOS == "windows" && strings.EqualFold(d, key)
		})
	})
}
