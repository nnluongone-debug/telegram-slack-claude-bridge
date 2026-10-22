//go:build windows

package session

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// The ConPTY backend runs a console program, returns its output, and ends
// reads with EOF once the program has exited.
func TestWindowsAgentProcessRoundTrip(t *testing.T) {
	proc, err := startAgentProcess(`C:\Windows\System32\cmd.exe`, []string{"/c", "echo bridge-ok"}, t.TempDir(), os.Environ(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	read := make(chan error, 1)
	go func() {
		_, err := io.Copy(&out, proc) // until EOF
		read <- err
	}()
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	select {
	case <-read:
	case <-time.After(15 * time.Second):
		t.Fatal("read still blocked after the program exited")
	}
	if !strings.Contains(out.String(), "bridge-ok") {
		t.Fatalf("output %q lacks bridge-ok", out.String())
	}
	if err := proc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := proc.Write([]byte("x")); err == nil {
		t.Fatal("write after Close must fail")
	}
	proc.Kill() // after Close: must not touch released handles
}

func TestWindowsRefusesScriptShims(t *testing.T) {
	if _, err := startAgentProcess(`C:\npm\claude.cmd`, nil, t.TempDir(), nil, 80, 24); err == nil {
		t.Fatal("a .cmd shim must be refused")
	}
}
