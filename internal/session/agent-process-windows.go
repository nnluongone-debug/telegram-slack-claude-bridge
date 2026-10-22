//go:build windows

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsProcess runs the agent in a ConPTY. The agent and everything it
// starts live in a job object, so stopping the job reaches background jobs
// too and never hits an unrelated process that reused a PID.
type windowsProcess struct {
	in, out *os.File // our ends of the console's input and output pipes
	console windows.Handle
	process windows.Handle
	job     windows.Handle

	consoleOnce sync.Once  // ClosePseudoConsole can block while output drains: never under mu
	mu          sync.Mutex // guards the job and process handles against use after Close
	closed      bool
}

func startAgentProcess(bin string, args []string, dir string, env []string, cols, rows uint16) (agentProcess, error) {
	// A .cmd/.bat shim (npm install) would run through cmd.exe, which
	// re-parses arguments and could treat parts of a prompt as commands.
	if ext := strings.ToLower(filepath.Ext(bin)); ext == ".cmd" || ext == ".bat" {
		return nil, fmt.Errorf("%s is a %s script; install the native claude.exe (https://docs.claude.com/en/docs/claude-code/setup) or point CLAUDE_BIN at it", bin, ext)
	}
	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{bin}, args...)))
	if err != nil {
		return nil, err
	}
	workDir, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}

	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("create input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		closeHandles(inRead, inWrite)
		return nil, fmt.Errorf("create output pipe: %w", err)
	}
	p := &windowsProcess{in: os.NewFile(uintptr(inWrite), "conpty-in"), out: os.NewFile(uintptr(outRead), "conpty-out")}
	err = windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &p.console)
	// The console holds its own copies; closing ours lets reads end with EOF
	// once the console is closed.
	closeHandles(inRead, outWrite)
	if err != nil {
		p.in.Close()
		p.out.Close()
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}
	if err := p.spawn(bin, cmdLine, workDir, env); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// spawn starts the agent suspended, puts it in a kill-on-close job, then lets it run.
func (p *windowsProcess) spawn(bin string, cmdLine, workDir *uint16, env []string) error {
	appName, err := windows.UTF16PtrFromString(bin) // no search of the current folder
	if err != nil {
		return err
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	// The attribute's value is the console handle itself, not a pointer to it.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&p.console)), unsafe.Sizeof(p.console)); err != nil {
		return fmt.Errorf("attach pseudo console: %w", err)
	}
	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.Flags = windows.STARTF_USESTDHANDLES // no handles: the console provides stdio

	if p.job, err = windows.CreateJobObject(nil, nil); err != nil {
		return fmt.Errorf("create job object: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(p.job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return fmt.Errorf("configure job object: %w", err)
	}

	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(appName, cmdLine, nil, nil, false, flags, envBlock(env), workDir, &si.StartupInfo, &pi); err != nil {
		return err
	}
	defer windows.CloseHandle(pi.Thread)
	p.process = pi.Process
	if err := windows.AssignProcessToJobObject(p.job, pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return fmt.Errorf("assign job object: %w", err)
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return fmt.Errorf("resume agent: %w", err)
	}
	return nil
}

func (p *windowsProcess) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *windowsProcess) Write(b []byte) (int, error) { return p.in.Write(b) }

// Wait returns when the agent exits. It then closes the console, which
// flushes its last output and ends pending reads with EOF.
func (p *windowsProcess) Wait() error {
	if _, err := windows.WaitForSingleObject(p.process, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	err := windows.GetExitCodeProcess(p.process, &code)
	p.closeConsole()
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("exit status %d", code)
	}
	return nil
}

// Windows has no SIGTERM for console programs: stop the whole job.
func (p *windowsProcess) Terminate() { p.Kill() }

func (p *windowsProcess) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed && p.job != 0 {
		_ = windows.TerminateJobObject(p.job, 1)
	}
}

func (p *windowsProcess) closeConsole() {
	p.consoleOnce.Do(func() {
		if p.console != 0 {
			windows.ClosePseudoConsole(p.console)
		}
	})
}

// Close releases the pipes, console and handles; closing the job also ends
// any process the agent left behind. The output pipe is closed first (which
// also cancels a pending read), so closing the console never waits for a
// reader.
func (p *windowsProcess) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	p.in.Close()
	p.out.Close()
	p.closeConsole()
	closeHandles(p.process, p.job)
	return nil
}

func closeHandles(handles ...windows.Handle) {
	for _, h := range handles {
		if h != 0 {
			_ = windows.CloseHandle(h)
		}
	}
}

// envBlock encodes env as a CreateProcess environment block: "k=v\0…\0".
func envBlock(env []string) *uint16 {
	var block []uint16
	for _, kv := range env {
		if strings.IndexByte(kv, 0) < 0 {
			block = append(block, utf16.Encode([]rune(kv))...)
			block = append(block, 0)
		}
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	block = append(block, 0)
	return &block[0]
}
