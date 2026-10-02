// Package ptyunix is ports.PTY on a Unix pseudo-terminal (creack/pty).
package ptyunix

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sync"

	"github.com/creack/pty"

	"operators-mcp/internal/ports"
)

var _ ports.PTY = PTY{}

// PTY starts processes on a fresh pseudo-terminal each.
type PTY struct{}

// New returns the Unix PTY.
func New() PTY { return PTY{} }

// Start runs spec on a pseudo-terminal of the given size. TERM and COLORTERM
// are set last, so they describe this terminal rather than whichever one the
// caller runs in.
func (PTY) Start(spec ports.ProcessSpec, size ports.TermSize) (ports.PTYProcess, error) {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(slices.Clone(spec.Env), "TERM=xterm-256color", "COLORTERM=truecolor")
	f, err := pty.StartWithSize(cmd, winsize(size))
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.Path, err)
	}
	return &process{cmd: cmd, f: f}, nil
}

func winsize(s ports.TermSize) *pty.Winsize {
	return &pty.Winsize{Cols: uint16(max(1, s.Cols)), Rows: uint16(max(1, s.Rows))}
}

type process struct {
	cmd *exec.Cmd
	f   *os.File

	// mu keeps Resize, which reaches the device through its descriptor, from
	// racing Close.
	mu     sync.Mutex
	closed bool
}

func (p *process) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *process) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *process) Resize(s ports.TermSize) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	return pty.Setsize(p.f, winsize(s))
}

func (p *process) Signal(sig os.Signal) error { return p.cmd.Process.Signal(sig) }

func (p *process) Wait() (int, error) {
	err := p.cmd.Wait()
	var exit *exec.ExitError
	if err == nil || errors.As(err, &exit) {
		return p.cmd.ProcessState.ExitCode(), nil
	}
	return -1, err
}

func (p *process) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return p.f.Close()
}
