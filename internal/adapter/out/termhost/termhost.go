// Package termhost is ports.TerminalHost: it runs interactive agents on
// terminals, composing the runtime stack — an Agent builds the command, a
// Shell turns it into a process, a PTY runs it — and keeps each terminal's
// authoritative screen in a VT emulator that clients attach to.
//
// It depends on ports and the emulator only, so the server can host
// terminals and tui-client can host its own with the same code.
package termhost

import (
	"context"
	"fmt"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TerminalHost = (*Host)(nil)

// Host owns the terminals it spawned, by id.
type Host struct {
	agents    map[string]ports.Agent
	launchers map[string]ports.Launcher
	shell     ports.Shell
	pty       ports.PTY
	killAfter time.Duration
	subBuffer int

	mu    sync.Mutex
	terms map[string]*terminal
}

// New returns a host running agents through shell on pty.
func New(shell ports.Shell, pty ports.PTY, agents ...ports.Agent) *Host {
	h := &Host{
		agents:    map[string]ports.Agent{},
		launchers: map[string]ports.Launcher{},
		shell:     shell,
		pty:       pty,
		killAfter: 2 * time.Second,
		subBuffer: defaultSubscriberBuffer,
		terms:     map[string]*terminal{},
	}
	for _, a := range agents {
		h.agents[a.Kind()] = a
	}
	return h
}

// WithLaunchers adds agents that start themselves on a terminal; a
// launcher's kind wins over an agent's.
func (h *Host) WithLaunchers(launchers ...ports.Launcher) *Host {
	for _, l := range launchers {
		h.launchers[l.Kind()] = l
	}
	return h
}

// Spawn starts spec on a new terminal. A spec with no kind runs on the only
// agent or launcher, when there is exactly one.
func (h *Host) Spawn(ctx context.Context, id string, spec ports.AgentSpec, size ports.TermSize, onExit func(code int)) error {
	start, err := h.starter(ctx, spec)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.terms[id]; ok && !t.exited() {
		return &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "a terminal already runs " + id}
	}
	proc, err := start(size)
	if err != nil {
		return err
	}
	t := newTerminal(proc, size, h.killAfter, h.subBuffer)
	h.terms[id] = t
	t.run(func(code int) {
		h.mu.Lock()
		if h.terms[id] == t {
			delete(h.terms, id)
		}
		h.mu.Unlock()
		if onExit != nil {
			onExit(code)
		}
	})
	return nil
}

// starter checks what spec needs before it starts, and answers how to start
// it: through its launcher, or as its agent's command prepared by the shell
// on the PTY.
func (h *Host) starter(ctx context.Context, spec ports.AgentSpec) (func(ports.TermSize) (ports.PTYProcess, error), error) {
	kind := spec.Kind
	if kind == "" && len(h.agents)+len(h.launchers) == 1 {
		for k := range h.agents {
			kind = k
		}
		for k := range h.launchers {
			kind = k
		}
	}
	if l, ok := h.launchers[kind]; ok {
		if spec.Dir != "" {
			if err := h.shell.CheckDir(ctx, spec.Dir); err != nil {
				return nil, err
			}
		}
		return func(size ports.TermSize) (ports.PTYProcess, error) { return l.Launch(ctx, spec, size, h.shell) }, nil
	}
	agent, ok := h.agents[kind]
	if !ok {
		return nil, fmt.Errorf("termhost: no agent adapter for kind %q", spec.Kind)
	}
	cmd, err := agent.Command(spec)
	if err != nil {
		return nil, err
	}
	if cmd.Dir != "" {
		if err := h.shell.CheckDir(ctx, cmd.Dir); err != nil {
			return nil, err
		}
	}
	ps, err := h.shell.Prepare(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return func(size ports.TermSize) (ports.PTYProcess, error) { return h.pty.Start(ps, size) }, nil
}

// Attach returns the running terminal under id.
func (h *Host) Attach(id string) (ports.Terminal, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.terms[id]; ok {
		return t, nil
	}
	return nil, &domain.StructuredError{Code: "TERMINAL_NOT_FOUND", Message: "no terminal runs " + id}
}

// Shutdown kills every terminal, in parallel, and returns when they are gone
// or ctx ends.
func (h *Host) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	terms := make([]*terminal, 0, len(h.terms))
	for _, t := range h.terms {
		terms = append(terms, t)
	}
	h.mu.Unlock()

	var wg sync.WaitGroup
	for _, t := range terms {
		wg.Go(func() { _ = t.Kill() })
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
