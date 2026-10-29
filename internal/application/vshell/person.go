package vshell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Folders answers where a folder of a session's virtual file system really is.
type Folders interface {
	RealPath(ctx context.Context, sessionID string, kind vfs.RootKind, folder string) (string, error)
}

// PersonDeps are what a person's terminals stand on.
type PersonDeps struct {
	// Host runs the shells. It is the person's own: no session, run or agent
	// terminal is on it, so their ids never meet.
	Host     ports.TerminalHost
	Folders  Folders
	Sessions interface{ Get(id string) *domain.Session }
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// PersonTerminals are the virtual shells of the person, opened on the virtual
// file systems of sessions (ADR 0050). Agents have no way to reach them.
type PersonTerminals struct {
	PersonDeps

	mu    sync.Mutex
	terms []*personTerm // oldest first
}

type personTerm struct{ ports.PersonTerminal }

var _ ports.PersonTerminals = (*PersonTerminals)(nil)

// NewPersonTerminals returns the person's terminals on d.
func NewPersonTerminals(d PersonDeps) *PersonTerminals {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &PersonTerminals{PersonDeps: d}
}

const (
	defaultCols = 80
	defaultRows = 24
)

// Open starts the person's login shell on the folder.
func (p *PersonTerminals) Open(ctx context.Context, sessionID string, req ports.PersonTerminalOpen) (ports.PersonTerminal, error) {
	if err := p.session(sessionID); err != nil {
		return ports.PersonTerminal{}, err
	}
	dir, err := p.Folders.RealPath(ctx, sessionID, vfs.RootKind(req.Root), req.Dir)
	if err != nil {
		return ports.PersonTerminal{}, rootGone(err)
	}
	size := req.Size
	if size.Cols <= 0 || size.Rows <= 0 {
		size = ports.TermSize{Cols: defaultCols, Rows: defaultRows}
	}
	pt := ports.PersonTerminal{
		ID: newID(), SessionID: sessionID, Root: req.Root, Dir: req.Dir, Label: label(req.Root, req.Dir),
		State: ports.PersonTerminalRunning, OpenedAt: p.Now(),
	}
	t := &personTerm{pt}
	spec := ports.AgentSpec{Kind: ports.PersonTerminalKind, SessionID: pt.ID, Dir: dir}
	if err := p.Host.Spawn(ctx, pt.ID, spec, size, func(int) { p.forget(t) }); err != nil {
		return ports.PersonTerminal{}, err
	}
	p.mu.Lock()
	p.terms = append(p.terms, t)
	p.mu.Unlock()
	return pt, nil
}

// List is the session's open terminals, oldest first.
func (p *PersonTerminals) List(_ context.Context, sessionID string) ([]ports.PersonTerminal, error) {
	if err := p.session(sessionID); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []ports.PersonTerminal{}
	for _, t := range p.terms {
		if t.SessionID == sessionID {
			out = append(out, t.PersonTerminal)
		}
	}
	return out, nil
}

// Attach returns the terminal's screen, or TERMINAL_NOT_FOUND.
func (p *PersonTerminals) Attach(id string) (ports.Terminal, error) { return p.Host.Attach(id) }

// Close hangs up the shell; one that is already gone is not an error.
func (p *PersonTerminals) Close(id string) error {
	t, err := p.Host.Attach(id)
	if err != nil {
		return nil
	}
	return t.Kill()
}

func (p *PersonTerminals) session(id string) error {
	if p.Sessions.Get(id) == nil {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no session " + id}
	}
	return nil
}

func (p *PersonTerminals) forget(t *personTerm) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, x := range p.terms {
		if x == t {
			p.terms = append(p.terms[:i], p.terms[i+1:]...)
			return
		}
	}
}

// rootGone gives the files contract's name to a root that is not there.
func rootGone(err error) error {
	if se, ok := err.(*domain.StructuredError); ok && se.Code == "ROOT_UNAVAILABLE" {
		return &domain.StructuredError{Code: "ROOT_GONE", Message: se.Message}
	}
	return err
}

// label is a terminal's tab: its folder, or the root's name for the root.
func label(root, dir string) string {
	if dir != "" {
		return dir
	}
	if root == string(vfs.ResourcesRoot) {
		return "Resources"
	}
	return "Worktree"
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "pt_" + hex.EncodeToString(b[:])
}
