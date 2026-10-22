//go:build !windows

package session

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type unixProcess struct {
	cmd  *exec.Cmd
	ptmx *os.File
}

func startAgentProcess(bin string, args []string, dir string, env []string, cols, rows uint16) (agentProcess, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Env = dir, env
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return &unixProcess{cmd: cmd, ptmx: ptmx}, nil
}

func (p *unixProcess) Read(b []byte) (int, error)  { return p.ptmx.Read(b) }
func (p *unixProcess) Write(b []byte) (int, error) { return p.ptmx.Write(b) }
func (p *unixProcess) Wait() error                 { return p.cmd.Wait() }
func (p *unixProcess) Close() error                { return p.ptmx.Close() }

// pty.Start uses Setsid, so the agent leads its own process group and a
// signal to -pid reaches its background jobs too.
func (p *unixProcess) Terminate() { _ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM) }
func (p *unixProcess) Kill()      { _ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL) }
