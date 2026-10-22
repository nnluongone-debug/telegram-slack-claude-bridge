// Package terminal renders a full-screen TUI (Claude Code) into plain text
// via a virtual terminal emulator, and detects confirmation prompts on it.
package terminal

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/hinshun/vt10x"
)

// ruleOnlyChars are box-drawing glyphs used for separators; rows made only of
// these carry no information and waste space on a phone screen.
const ruleOnlyChars = "─━│┃╭╮╰╯┌┐└┘├┤┬┴┼═║╌╍┄┅  "

// VirtualScreen feeds raw PTY bytes into a VT emulator and exposes the visible screen.
// Claude Code redraws with absolute cursor positioning on the alternate screen,
// so the only faithful text is what an emulator would display.
type VirtualScreen struct {
	vt vt10x.Terminal

	mu    sync.Mutex
	carry []byte // trailing partial UTF-8 rune from the previous Write
}

// NewVirtualScreen creates a cols×rows emulator. Terminal query replies
// (cursor position, status) are written to replies, which should be the PTY.
func NewVirtualScreen(cols, rows int, replies io.Writer) *VirtualScreen {
	return &VirtualScreen{vt: vt10x.New(vt10x.WithSize(cols, rows), vt10x.WithWriter(replies))}
}

// Write feeds PTY output to the emulator.
func (v *VirtualScreen) Write(p []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	data := append(v.carry, p...)
	data, v.carry = splitIncompleteRune(data)
	if _, err := v.vt.Write(data); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Lines returns the visible screen as trimmed text rows, without separator
// rows and with runs of blank rows collapsed.
func (v *VirtualScreen) Lines() []string {
	var out []string
	blank := true // also drops leading blank rows
	for _, row := range strings.Split(v.vt.String(), "\n") {
		row = strings.TrimRight(row, "  ")
		if strings.Trim(row, ruleOnlyChars) == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, row)
		blank = false
	}
	if n := len(out); n > 0 && out[n-1] == "" {
		out = out[:n-1]
	}
	return out
}

// AltScreen reports whether the TUI is on the alternate screen (Claude Code
// leaves it on exit, revealing a different main screen).
func (v *VirtualScreen) AltScreen() bool {
	v.vt.Lock() // Mode() itself reads without the emulator's lock
	defer v.vt.Unlock()
	return v.vt.Mode()&vt10x.ModeAltScreen != 0
}

// splitIncompleteRune holds back a trailing partial UTF-8 sequence.
func splitIncompleteRune(data []byte) (complete, carry []byte) {
	for back := 1; back <= utf8.UTFMax && back <= len(data); back++ {
		start := len(data) - back
		if utf8.RuneStart(data[start]) {
			if !utf8.FullRune(data[start:]) {
				return data[:start], bytes.Clone(data[start:])
			}
			break
		}
	}
	return data, nil
}
