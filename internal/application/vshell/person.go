package vshell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
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
	Sessions interface {
		Get(id string) *domain.Session
	}
	// RootEvents says when a root of a session goes away; nil: no terminal
	// ends with its root.
	RootEvents interface {
		Subscribe(fn func(vfs.RootEvent)) (cancel func())
	}
	// Page is where the page's event stream hears of a terminal that ended
	// (live only); nil: nobody is told.
	Page interface {
		PublishEphemeral(sessionID string, ev ports.SessionEvent)
	}
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

// personTerm is one open terminal and what ended it, if something did.
type personTerm struct {
	ports.PersonTerminal
	kind vfs.RootKind
	// root is the real path of the root it was opened on.
	root string
	// reason and message are why it ends, when the harness ends it; silent
	// when the person closed it, so no event says so.
	reason, message string
	silent          bool
	notified        bool
}

// The reasons a terminal ends other than the person closing it, as the
// contract names them.
const (
	ReasonExited        = "exited"
	ReasonRootGone      = "root_gone"
	ReasonServerStopped = "server_stopped"
)

// EventPersonTerminalEnded is the type of the event the page hears of a
// terminal that ended.
const EventPersonTerminalEnded = "person_terminal_ended"

var _ ports.PersonTerminals = (*PersonTerminals)(nil)

// NewPersonTerminals returns the person's terminals on d.
func NewPersonTerminals(d PersonDeps) *PersonTerminals {
	if d.Now == nil {
		d.Now = time.Now
	}
	p := &PersonTerminals{PersonDeps: d}
	if d.RootEvents != nil {
		d.RootEvents.Subscribe(p.rootChanged)
	}
	return p
}

// rootChanged ends the terminals of a root that went away: its session's
// terminals opened on that root, at that path.
func (p *PersonTerminals) rootChanged(ev vfs.RootEvent) {
	if ev.Available {
		return
	}
	message := "The worktree was removed, and this terminal with it."
	if ev.Kind == vfs.ResourcesRoot {
		message = "The session ended, and its resources folder with it."
	}
	p.mu.Lock()
	var gone []*personTerm
	for _, t := range p.terms {
		if t.SessionID == ev.SessionID && t.kind == ev.Kind && (ev.Path == "" || t.root == ev.Path) {
			gone = append(gone, t)
		}
	}
	p.mu.Unlock()
	for _, t := range gone {
		p.end(t, ReasonRootGone, message)
	}
}

// end hangs up t for a reason of the harness's; its exit tells the page.
func (p *PersonTerminals) end(t *personTerm, reason, message string) {
	p.mu.Lock()
	if t.reason == "" && !t.silent {
		t.reason, t.message = reason, message
	}
	p.mu.Unlock()
	p.kill(t.ID)
}

func (p *PersonTerminals) kill(id string) {
	if term, err := p.Host.Attach(id); err == nil {
		_ = term.Kill()
	}
}

// Shutdown ends every terminal for the server stopping. The page is told
// first, best effort: once the server is down nobody can be.
func (p *PersonTerminals) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	terms := slices.Clone(p.terms)
	for _, t := range terms {
		if t.reason == "" && !t.silent {
			t.reason, t.message = ReasonServerStopped, "The harness stopped; this terminal ended."
		}
	}
	p.mu.Unlock()
	for _, t := range terms {
		p.notify(t, 0)
	}
	var wg sync.WaitGroup
	for _, t := range terms {
		wg.Go(func() { p.kill(t.ID) })
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

// exited forgets t, which is gone, and tells the page why, unless the person
// closed it or the page was told already.
func (p *PersonTerminals) exited(t *personTerm, code int) {
	p.forget(t)
	p.notify(t, code)
}

func (p *PersonTerminals) notify(t *personTerm, code int) {
	p.mu.Lock()
	reason, message := t.reason, t.message
	skip := t.silent || t.notified || p.Page == nil
	if reason == "" {
		reason, message = ReasonExited, "The shell exited."
		if code != 0 {
			message = fmt.Sprintf("The shell exited with code %d.", code)
		}
	}
	t.notified = true
	p.mu.Unlock()
	if skip {
		return
	}
	p.Page.PublishEphemeral(t.SessionID, ports.SessionEvent{
		Type: EventPersonTerminalEnded, SessionID: t.SessionID, ID: t.ID, Reason: reason, Message: message, At: p.Now(),
	})
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
	root, err := p.Folders.RealPath(ctx, sessionID, vfs.RootKind(req.Root), "")
	if err != nil {
		return ports.PersonTerminal{}, rootGone(err)
	}
	t := &personTerm{PersonTerminal: pt, kind: vfs.RootKind(req.Root), root: root}
	spec := ports.AgentSpec{Kind: ports.PersonTerminalKind, SessionID: pt.ID, Dir: dir}
	if t.kind == vfs.ResourcesRoot {
		// The resources root is the agent's, and its writes are stored only
		// through the edit proxy: the person reads it, never writes (ADR 0052).
		spec.DenyWrites = root
	}
	// Listed before it runs: a shell that exits at once must find itself there.
	p.mu.Lock()
	p.terms = append(p.terms, t)
	p.mu.Unlock()
	if err := p.Host.Spawn(ctx, pt.ID, spec, size, func(code int) { p.exited(t, code) }); err != nil {
		p.forget(t)
		return ports.PersonTerminal{}, err
	}
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
	p.mu.Lock()
	for _, t := range p.terms {
		if t.ID == id {
			t.silent = true
		}
	}
	p.mu.Unlock()
	p.kill(id)
	return nil
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
