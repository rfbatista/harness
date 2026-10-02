// Package panes keeps every task's deck of agent panes for tui-client, and
// the sessions they show. It outlives the screens: leaving a task keeps its
// panes drawing, and a session that finishes starting after the user moved on
// still gets its pane.
//
// A pane shows one of two kinds of session. A RunnerTUI session runs here, on
// the client's terminal host: the registry spawns it, ends it when it exits,
// and stops it when the client quits. A RunnerServer session runs on the
// server: the registry only attaches to it, the server records its end, and
// quitting detaches — it keeps running, and the next client reattaches.
//
// The root owns one Registry and hands the same pointer to the screens that
// show or count panes. Bubble Tea runs Update on one goroutine, so it needs no
// lock.
package panes

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/tuiclient/nav"
	"operators-mcp/internal/tuiclient/term"
)

type (
	// LaunchMsg carries a session the server started or resumed and the
	// AgentSpec to run it. The root hands it to Update, whichever screen is
	// showing.
	LaunchMsg struct {
		ProjectID string
		TicketID  string
		Label     string
		Session   *domain.Session
		Spec      ports.AgentSpec
		Err       error
	}
	// AttachMsg opens a pane on a RunnerServer session already running.
	AttachMsg struct{ Session *domain.Session }
	// ChangedMsg says a session started or ended; screens listing sessions
	// reload them.
	ChangedMsg struct{ ProjectID string }

	// NewSessionMsg, ShowSessionsMsg and BackMsg are the deck commands of a
	// task: the prefix key, then c, s or b.
	NewSessionMsg   struct{}
	ShowSessionsMsg struct{}
	BackMsg         struct{}
)

// Config is where the sessions this client starts run, and how it reaches
// each kind.
type Config struct {
	// RunsOn is where sessions this client starts or resumes run.
	RunsOn domain.Runner
	// RunnerHost names this machine on its RunnerTUI sessions.
	RunnerHost string
	// Host runs RunnerTUI sessions; nil when this client runs none.
	Host ports.TerminalHost
	// Terminals attaches to RunnerServer sessions.
	Terminals ports.TerminalAccess
	// Sessions ends RunnerTUI sessions.
	Sessions ports.InteractiveSessions
}

// ref is what a pane shows.
type ref struct {
	projectID string
	ticketID  string
	sessionID string
	runsOn    domain.Runner
}

// Registry holds the decks, by ticket.
type Registry struct {
	cfg Config

	width, height int
	decks         map[string]term.Deck // by ticket ID
	panes         map[int64]ref        // by pane ID
	projectOf     map[string]string    // ticket ID → project ID
}

// New returns a registry for cfg.
func New(cfg Config) *Registry {
	if cfg.RunsOn == "" {
		cfg.RunsOn = domain.RunnerTUI
	}
	return &Registry{
		cfg:       cfg,
		width:     80,
		height:    24,
		decks:     map[string]term.Deck{},
		panes:     map[int64]ref{},
		projectOf: map[string]string{},
	}
}

// Runner is where sessions this client starts run, and this machine's name
// for RunnerTUI ones.
func (r *Registry) Runner() (domain.Runner, string) { return r.cfg.RunsOn, r.cfg.RunnerHost }

// Resize sizes every deck to the body area.
func (r *Registry) Resize(width, height int) {
	r.width, r.height = width, height
	for id, d := range r.decks {
		r.decks[id], _ = d.Update(tea.WindowSizeMsg{Width: width, Height: height})
	}
}

// Deck is the deck of a task, created empty on first use.
func (r *Registry) Deck(ticketID string) term.Deck {
	d, ok := r.decks[ticketID]
	if !ok {
		d = term.NewDeck(
			term.Command{Key: "c", Label: "new session", Msg: func() tea.Msg { return NewSessionMsg{} }},
			term.Command{Key: "s", Label: "sessions", Msg: func() tea.Msg { return ShowSessionsMsg{} }},
			term.Command{Key: "b", Label: "back", Msg: func() tea.Msg { return BackMsg{} }},
		)
		d, _ = d.Update(tea.WindowSizeMsg{Width: r.width, Height: r.height})
		r.decks[ticketID] = d
	}
	return d
}

// PaneSize is the size a new pane of the task gets.
func (r *Registry) PaneSize(ticketID string) ports.TermSize {
	w, h := r.Deck(ticketID).PaneSize()
	return ports.TermSize{Cols: w, Rows: h}
}

// Input hands a key press or paste to a task's deck, and so to its focused
// claude.
func (r *Registry) Input(ticketID string, msg tea.Msg) tea.Cmd {
	d, cmd := r.Deck(ticketID).Update(msg)
	r.decks[ticketID] = d
	return cmd
}

// Focus shows the pane of sessionID and reports whether one is open.
func (r *Registry) Focus(sessionID string) bool {
	for id, p := range r.panes {
		if p.sessionID == sessionID {
			r.decks[p.ticketID] = r.decks[p.ticketID].Focus(id)
			return true
		}
	}
	return false
}

// Open reports whether a pane shows sessionID.
func (r *Registry) Open(sessionID string) bool {
	for _, p := range r.panes {
		if p.sessionID == sessionID {
			return true
		}
	}
	return false
}

// LiveIn counts the running panes of a task.
func (r *Registry) LiveIn(ticketID string) int {
	if d, ok := r.decks[ticketID]; ok {
		return d.Live()
	}
	return 0
}

// LiveInProject counts the running panes of every task of a project.
func (r *Registry) LiveInProject(projectID string) int {
	n := 0
	for ticketID, d := range r.decks {
		if r.projectOf[ticketID] == projectID {
			n += d.Live()
		}
	}
	return n
}

// Update handles the registry's own messages — launches, attaches and pane
// events — and reports whether msg was one.
func (r *Registry) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case LaunchMsg:
		return r.launch(msg), true
	case AttachMsg:
		return r.attach(msg.Session), true
	case term.FrameMsg:
		return r.route(msg.ID, msg), true
	case term.ExitedMsg:
		return r.exited(msg), true
	case term.ClosedMsg:
		return r.closed(msg), true
	}
	return nil, false
}

// launch opens a pane on a session the server just started or resumed:
// spawning it here for RunnerTUI, attaching to it for RunnerServer. A
// RunnerTUI session that cannot start is ended, so it does not linger as
// running; the server does the same for its own.
func (r *Registry) launch(msg LaunchMsg) tea.Cmd {
	if msg.Err != nil {
		return nav.Fail(msg.Err)
	}
	sess := msg.Session
	cmd, err := r.open(msg.ProjectID, msg.TicketID, msg.Label, sess.ID, sess.RunsOn, func(size ports.TermSize) (ports.Terminal, error) {
		if sess.RunsOn == domain.RunnerServer {
			return r.attachRemote(sess.ID)
		}
		return r.spawnLocal(sess.ID, msg.Spec, size)
	})
	if err != nil {
		if sess.RunsOn == domain.RunnerServer {
			return nav.Fail(err)
		}
		return tea.Batch(nav.Fail(err), r.end(msg.ProjectID, sess.ID, 127, false))
	}
	return tea.Batch(cmd, nav.Note("started "+sess.Branch), changed(msg.ProjectID))
}

// attach opens a pane on a RunnerServer session that is already running,
// unless one shows it.
func (r *Registry) attach(sess *domain.Session) tea.Cmd {
	if r.Open(sess.ID) {
		return nil
	}
	label := strings.TrimPrefix(sess.Branch, "agent/")
	cmd, err := r.open(sess.ProjectID, sess.TicketID, label, sess.ID, domain.RunnerServer, func(ports.TermSize) (ports.Terminal, error) {
		return r.attachRemote(sess.ID)
	})
	if err != nil {
		return nav.Fail(err)
	}
	return cmd
}

// open adds a pane on the terminal terminal returns to the task's deck.
func (r *Registry) open(projectID, ticketID, label, sessionID string, runsOn domain.Runner, terminal func(ports.TermSize) (ports.Terminal, error)) (tea.Cmd, error) {
	r.projectOf[ticketID] = projectID
	d := r.Deck(ticketID)
	w, h := d.PaneSize()
	t, err := terminal(ports.TermSize{Cols: w, Rows: h})
	if err != nil {
		return nil, err
	}
	pane := term.New(term.Options{Terminal: t, Name: label, Width: w, Height: h})
	if err := pane.Start(); err != nil {
		if runsOn == domain.RunnerTUI {
			_ = t.Kill()
		}
		return nil, err
	}
	r.panes[pane.ID()] = ref{projectID: projectID, ticketID: ticketID, sessionID: sessionID, runsOn: runsOn}
	d, cmd := d.Add(pane)
	r.decks[ticketID] = d
	return cmd, nil
}

// spawnLocal runs a RunnerTUI session's spec on this client's host. The
// registry learns of the exit from the pane, so it gives the host no
// callback.
func (r *Registry) spawnLocal(sessionID string, spec ports.AgentSpec, size ports.TermSize) (ports.Terminal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if r.cfg.Host == nil {
		return nil, &domain.StructuredError{Code: "SESSION_RUNS_ON_TUI", Message: "this client does not run sessions itself (--run server)"}
	}
	if err := r.cfg.Host.Spawn(ctx, sessionID, spec, size, nil); err != nil {
		return nil, err
	}
	return r.cfg.Host.Attach(sessionID)
}

func (r *Registry) attachRemote(sessionID string) (ports.Terminal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return r.cfg.Terminals.AttachTerminal(ctx, sessionID)
}

// route delivers a pane's message to whichever deck holds the pane, shown or
// not, so background agents keep drawing.
func (r *Registry) route(id int64, msg tea.Msg) tea.Cmd {
	p, ok := r.panes[id]
	if !ok {
		return nil
	}
	d, cmd := r.decks[p.ticketID].Update(msg)
	r.decks[p.ticketID] = d
	return cmd
}

// exited handles a pane whose process ended on its own. A RunnerTUI session
// is ended here by its exit code; the server ends its own. A pane the user
// closed is no longer in its deck by then; ClosedMsg handles that one.
func (r *Registry) exited(msg term.ExitedMsg) tea.Cmd {
	p, ok := r.panes[msg.ID]
	if !ok {
		return nil
	}
	cmd := r.route(msg.ID, msg)
	if !r.decks[p.ticketID].Owns(msg.ID) {
		return cmd
	}
	if p.runsOn == domain.RunnerServer {
		return tea.Batch(cmd, changed(p.projectID))
	}
	return tea.Batch(cmd, r.end(p.projectID, p.sessionID, msg.Code, false))
}

// closed handles a pane the user closed. Closing stopped its process — here,
// or on the server through a stop — so a RunnerTUI session is ended as
// stopped, and a RunnerServer one was already recorded so by the server.
func (r *Registry) closed(msg term.ClosedMsg) tea.Cmd {
	p, ok := r.panes[msg.ID]
	if !ok {
		return nil
	}
	delete(r.panes, msg.ID)
	if p.runsOn == domain.RunnerServer {
		return changed(p.projectID)
	}
	return r.end(p.projectID, p.sessionID, 0, true)
}

func (r *Registry) end(projectID, sessionID string, code int, closed bool) tea.Cmd {
	sessions := r.cfg.Sessions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := sessions.EndInteractive(ctx, sessionID, code, closed); err != nil {
			return nav.Fail(err)()
		}
		return ChangedMsg{ProjectID: projectID}
	}
}

func changed(projectID string) tea.Cmd {
	return func() tea.Msg { return ChangedMsg{ProjectID: projectID} }
}

// Shutdown leaves for good: RunnerServer panes detach and their sessions keep
// running; RunnerTUI panes are stopped and their sessions ended as closed.
// The host calls it after the program exits.
func (r *Registry) Shutdown(ctx context.Context) {
	var wg sync.WaitGroup
	for _, d := range r.decks {
		for _, pane := range d.Panes() {
			p, ok := r.panes[pane.ID()]
			if ok && p.runsOn == domain.RunnerServer {
				pane.Detach()
				continue
			}
			wg.Go(func() { _ = pane.Close() })
		}
	}
	wg.Wait()
	for _, p := range r.panes {
		if p.runsOn != domain.RunnerServer {
			_, _ = r.cfg.Sessions.EndInteractive(ctx, p.sessionID, 0, true)
		}
	}
}
