package session

import "io"

// agentProcess is an agent running in a pseudo-terminal: a Unix PTY, or a
// ConPTY on Windows. Reads return the terminal output, writes are keystrokes.
type agentProcess interface {
	io.ReadWriter
	// Wait blocks until the agent exits.
	Wait() error
	// Terminate asks the agent and every process it started to exit.
	Terminate()
	// Kill force-stops the agent and every process it started.
	Kill()
	// Close releases the terminal; pending reads return an error.
	Close() error
}
