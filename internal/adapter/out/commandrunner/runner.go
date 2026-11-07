// Package commandrunner starts a command service's program as a process of
// its own process group, so stopping it reaches everything it started.
package commandrunner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"operators-mcp/internal/ports"
)

var _ ports.ServiceProcessRunner = Runner{}

// Runner implements ports.ServiceProcessRunner for a program on this machine.
type Runner struct{}

// Start runs spec.Path with spec.Args in spec.Dir under spec.Env. An error
// here is a spawn that failed (a missing directory or program).
func (Runner) Start(_ context.Context, spec ports.ServiceProcessSpec) (ports.ServiceProcessHandle, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir, cmd.Env = spec.Dir, spec.Env
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = cmd.Start()
	// Only the process holds the write end now; the read end sees EOF when it
	// and its children have closed it.
	_ = pw.Close()
	if err != nil {
		_ = pr.Close()
		return nil, err
	}
	h := &handle{cmd: cmd, out: pr, done: make(chan struct{})}
	go h.reap()
	return h, nil
}

type handle struct {
	cmd  *exec.Cmd
	out  *os.File
	done chan struct{}
	code int
}

func (h *handle) Output() io.ReadCloser { return h.out }

// reap waits for the process, then kills what it left behind: nothing would
// hold a handle on those.
func (h *handle) reap() {
	err := h.cmd.Wait()
	h.code = exitCode(h.cmd, err)
	_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGKILL)
	close(h.done)
}

func exitCode(cmd *exec.Cmd, err error) int {
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	if err != nil && cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

func (h *handle) Wait() int {
	<-h.done
	return h.code
}

func (h *handle) Stop(grace time.Duration) {
	pgid := h.cmd.Process.Pid
	select {
	case <-h.done:
		return
	default:
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-h.done:
	case <-time.After(grace):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-h.done
	}
}
