// Package tuitest provides Fake, an in-memory stand-in for the coding_pool
// server behind the driving ports tui-client's screens use.
package tuitest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.ProjectReader       = (*Fake)(nil)
	_ ports.RepositoryCatalog   = (*Fake)(nil)
	_ ports.TicketBoard         = (*Fake)(nil)
	_ ports.AgentCatalog        = (*Fake)(nil)
	_ ports.SessionReader       = (*Fake)(nil)
	_ ports.InteractiveSessions = (*Fake)(nil)
	_ ports.TerminalAccess      = (*Fake)(nil)
	_ ports.SessionFeed         = (*Fake)(nil)
)

// errReadOnly answers the catalog writes no screen makes yet.
var errReadOnly = errors.New("tuitest: the fake catalog is read-only")

// Fake answers the screens' ports from memory, with the error codes the
// server uses. Started and resumed sessions come back with Launch's
// AgentSpec; a test makes it a runtimetest.Script to stand in for claude.
type Fake struct {
	mu sync.Mutex

	Projects     []*domain.Project
	Repositories []*domain.Repository
	Tickets      []*domain.Ticket
	Agents       []*domain.Agent
	Sessions     []*domain.Session

	// Launch builds the AgentSpec a started or resumed session gets.
	Launch func(s *domain.Session, resume bool) ports.AgentSpec
	// Server plays the server's terminal host: RunnerServer sessions are
	// spawned there, end themselves when their process exits, and are
	// attached to through AttachTerminal. Nil answers them with
	// SERVER_HOSTING_UNAVAILABLE.
	Server ports.TerminalHost
	// Err, when set, is returned by the next call to the named method.
	Err map[string]error

	Ends []EndCall
	seq  int

	followers map[string]map[chan ports.SessionChange]struct{} // by project
}

// FollowProject follows a project's session changes until ctx ends, like the
// server's feed.
func (f *Fake) FollowProject(ctx context.Context, projectID string) (<-chan ports.SessionChange, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("FollowProject"); err != nil {
		return nil, err
	}
	c := make(chan ports.SessionChange, 64)
	if f.followers == nil {
		f.followers = map[string]map[chan ports.SessionChange]struct{}{}
	}
	if f.followers[projectID] == nil {
		f.followers[projectID] = map[chan ports.SessionChange]struct{}{}
	}
	f.followers[projectID][c] = struct{}{}
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.unfollow(projectID, c)
	}()
	return c, nil
}

// unfollow closes a follower once. Called with f.mu held.
func (f *Fake) unfollow(projectID string, c chan ports.SessionChange) {
	if _, ok := f.followers[projectID][c]; ok {
		delete(f.followers[projectID], c)
		close(c)
	}
}

// notify puts s on its project's feed. Called with f.mu held.
func (f *Fake) notify(s *domain.Session, deleted bool) {
	c := *s
	for ch := range f.followers[s.ProjectID] {
		select {
		case ch <- ports.SessionChange{Session: &c, Deleted: deleted}:
		default:
			f.unfollow(s.ProjectID, ch)
		}
	}
}

// Put records s as another client would, adding or replacing it, and puts it
// on the feed.
func (f *Fake) Put(s *domain.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *s
	if i := f.index(s.ID); i >= 0 {
		f.Sessions[i] = &c
	} else {
		f.Sessions = append(f.Sessions, &c)
	}
	f.notify(&c, false)
}

// DropFollowers ends every follow of projectID, as a lost connection would.
func (f *Fake) DropFollowers(projectID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.followers[projectID] {
		f.unfollow(projectID, c)
	}
}

// Following reports how many follows of projectID are open.
func (f *Fake) Following(projectID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.followers[projectID])
}

// EndCall records one EndInteractive call.
type EndCall struct {
	SessionID string
	ExitCode  int
	Closed    bool
}

func notFound(code, what string) error {
	return &domain.StructuredError{Code: code, Message: what + " not found"}
}

func (f *Fake) fail(method string) error {
	if err := f.Err[method]; err != nil {
		delete(f.Err, method)
		return err
	}
	return nil
}

// --- projects -------------------------------------------------------------------

func (f *Fake) ListProjects(context.Context) ([]*domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Projects), f.fail("ListProjects")
}

func (f *Fake) GetProject(_ context.Context, id string) (*domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.Projects {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, notFound("PROJECT_NOT_FOUND", "project")
}

func (f *Fake) ListRepositories(_ context.Context, projectID string) ([]*domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Repository
	for _, r := range f.Repositories {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, f.fail("ListRepositories")
}

func (f *Fake) GetRepository(_ context.Context, id string) (*domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.Repositories {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, notFound("REPOSITORY_NOT_FOUND", "repository")
}

func (f *Fake) CreateRepository(context.Context, string, string, string, string, string) (*domain.Repository, error) {
	return nil, errReadOnly
}

func (f *Fake) UpdateRepository(context.Context, string, string, string, string, string) (*domain.Repository, error) {
	return nil, errReadOnly
}

func (f *Fake) DeleteRepository(context.Context, string) error { return errReadOnly }

func (f *Fake) AddRepositoryIgnoredPath(context.Context, string, string) (*domain.Repository, error) {
	return nil, errReadOnly
}

func (f *Fake) RemoveRepositoryIgnoredPath(context.Context, string, string) (*domain.Repository, error) {
	return nil, errReadOnly
}

// --- tickets --------------------------------------------------------------------

func (f *Fake) ListTickets(_ context.Context, projectID string) ([]*domain.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Ticket
	for _, t := range f.Tickets {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, f.fail("ListTickets")
}

func (f *Fake) GetTicket(_ context.Context, id string) (*domain.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.Tickets {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, notFound("TICKET_NOT_FOUND", "ticket")
}

func (f *Fake) CreateTicket(context.Context, string, string, string, domain.TicketStatus) (*domain.Ticket, error) {
	return nil, errReadOnly
}

func (f *Fake) UpdateTicket(context.Context, string, string, string, domain.TicketStatus) (*domain.Ticket, error) {
	return nil, errReadOnly
}

func (f *Fake) DeleteTicket(context.Context, string) error { return errReadOnly }

// --- agents ---------------------------------------------------------------------

func (f *Fake) ListAgents(context.Context) ([]*domain.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Agents), f.fail("ListAgents")
}

func (f *Fake) GetAgent(_ context.Context, id string) (*domain.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.Agents {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, notFound("AGENT_NOT_FOUND", "agent")
}

func (f *Fake) CreateAgent(context.Context, string, string, string, []string, []string) (*domain.Agent, error) {
	return nil, errReadOnly
}

func (f *Fake) UpdateAgent(context.Context, string, string, string, string, []string, []string) (*domain.Agent, error) {
	return nil, errReadOnly
}

func (f *Fake) DeleteAgent(context.Context, string) error { return errReadOnly }

// --- sessions -------------------------------------------------------------------

func (f *Fake) Get(_ context.Context, id string) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.index(id); i >= 0 {
		s := *f.Sessions[i]
		return &s, nil
	}
	return nil, notFound("SESSION_NOT_FOUND", "session")
}

func (f *Fake) List(_ context.Context, flt ports.SessionFilter) ([]*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Session
	for _, s := range f.Sessions {
		if (flt.ProjectID == "" || s.ProjectID == flt.ProjectID) &&
			(flt.TicketID == "" || s.TicketID == flt.TicketID) &&
			(flt.AgentID == "" || s.AgentID == flt.AgentID) &&
			(len(flt.Statuses) == 0 || slices.Contains(flt.Statuses, s.Status)) {
			c := *s
			out = append(out, &c)
		}
	}
	return out, f.fail("ListSessions")
}

func (f *Fake) StartInteractive(ctx context.Context, req ports.InteractiveRequest) (*domain.Session, ports.AgentSpec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("StartInteractive"); err != nil {
		return nil, ports.AgentSpec{}, err
	}
	runsOn, err := f.runner(req.RunsOn)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	if req.TicketID == "" {
		return nil, ports.AgentSpec{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	f.seq++
	s := &domain.Session{
		ID:           fmt.Sprintf("s%d", f.seq),
		ProjectID:    req.ProjectID,
		RepositoryID: req.RepositoryID,
		TicketID:     req.TicketID,
		AgentID:      req.AgentID,
		Task:         req.Prompt,
		Branch:       fmt.Sprintf("agent/s%d", f.seq),
		Status:       domain.SessionRunning,
		Interactive:  true,
		RunsOn:       runsOn,
		CreatedAt:    time.Now(),
	}
	if runsOn == domain.RunnerTUI {
		s.RunnerHost = req.RunnerHost
	}
	s.ClaudeSessionID = s.ID
	f.Sessions = append(f.Sessions, s)
	f.notify(s, false)
	spec := f.launch(s, false)
	if err := f.spawn(ctx, s, spec, req.Size); err != nil {
		return nil, ports.AgentSpec{}, err
	}
	c := *s
	return &c, spec, nil
}

func (f *Fake) runner(r domain.Runner) (domain.Runner, error) {
	switch {
	case r == "":
		return domain.RunnerTUI, nil
	case r == domain.RunnerServer && f.Server == nil:
		return "", &domain.StructuredError{Code: "SERVER_HOSTING_UNAVAILABLE", Message: "this server does not run agents itself"}
	}
	return r, nil
}

// spawn runs a RunnerServer session on the Server host, the way the server
// does: its exit ends it. Called with f.mu held.
func (f *Fake) spawn(ctx context.Context, s *domain.Session, spec ports.AgentSpec, size ports.TermSize) error {
	if s.RunsOn != domain.RunnerServer {
		return nil
	}
	id := s.ID
	err := f.Server.Spawn(ctx, id, spec, size, func(code int) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.finish(id, code, false)
	})
	if err != nil {
		f.finish(id, 127, false)
	}
	return err
}

// finish records an end the way the server does: once. Called with f.mu held.
func (f *Fake) finish(id string, exitCode int, closed bool) {
	i := f.index(id)
	if i < 0 || f.Sessions[i].Status.IsTerminal() {
		return
	}
	switch {
	case closed:
		f.Sessions[i].Status = domain.SessionStopped
	case exitCode != 0:
		f.Sessions[i].Status = domain.SessionFailed
	default:
		f.Sessions[i].Status = domain.SessionDone
	}
	f.notify(f.Sessions[i], false)
}

// AttachTerminal attaches to a RunnerServer session on the Server host. The
// terminal's Kill records the session as stopped first, as the server's stop
// does.
func (f *Fake) AttachTerminal(_ context.Context, id string) (ports.Terminal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.index(id)
	if i < 0 {
		return nil, notFound("SESSION_NOT_FOUND", "session")
	}
	s := f.Sessions[i]
	if s.RunsOn != domain.RunnerServer {
		return nil, &domain.StructuredError{Code: "SESSION_RUNS_ON_TUI", Message: "this session runs in the client that started it"}
	}
	if s.Status.IsTerminal() || f.Server == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "session is not running"}
	}
	t, err := f.Server.Attach(id)
	if err != nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "session is not running"}
	}
	return serverTerminal{Terminal: t, f: f, id: id}, nil
}

type serverTerminal struct {
	ports.Terminal
	f  *Fake
	id string
}

func (t serverTerminal) Kill() error {
	t.f.mu.Lock()
	t.f.finish(t.id, 0, true)
	t.f.mu.Unlock()
	return t.Terminal.Kill()
}

func (f *Fake) ResumeInteractive(ctx context.Context, req ports.ResumeRequest) (*domain.Session, ports.AgentSpec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("ResumeInteractive"); err != nil {
		return nil, ports.AgentSpec{}, err
	}
	runsOn, err := f.runner(req.RunsOn)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	i := f.index(req.SessionID)
	if i < 0 {
		return nil, ports.AgentSpec{}, notFound("SESSION_NOT_FOUND", "session")
	}
	s := f.Sessions[i]
	if !s.Status.IsTerminal() {
		return nil, ports.AgentSpec{}, &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session is still running"}
	}
	s.Status = domain.SessionRunning
	defer f.notify(s, false)
	s.RunsOn, s.RunnerHost = runsOn, ""
	if runsOn == domain.RunnerTUI {
		s.RunnerHost = req.RunnerHost
	}
	spec := f.launch(s, true)
	if err := f.spawn(ctx, s, spec, req.Size); err != nil {
		return nil, ports.AgentSpec{}, err
	}
	c := *s
	return &c, spec, nil
}

func (f *Fake) EndInteractive(_ context.Context, id string, exitCode int, closed bool) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ends = append(f.Ends, EndCall{SessionID: id, ExitCode: exitCode, Closed: closed})
	if err := f.fail("EndInteractive"); err != nil {
		return nil, err
	}
	i := f.index(id)
	if i < 0 {
		return nil, notFound("SESSION_NOT_FOUND", "session")
	}
	if f.Sessions[i].RunsOn == domain.RunnerServer {
		return nil, &domain.StructuredError{Code: "SESSION_RUNS_ON_SERVER", Message: "the server runs this session and ends it itself"}
	}
	f.finish(id, exitCode, closed)
	c := *f.Sessions[i]
	return &c, nil
}

// EndCalls returns a copy of the EndInteractive calls so far.
func (f *Fake) EndCalls() []EndCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Ends)
}

// Session returns a copy of the session with the given id.
func (f *Fake) Session(id string) (domain.Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.index(id); i >= 0 {
		return *f.Sessions[i], true
	}
	return domain.Session{}, false
}

func (f *Fake) index(id string) int {
	for i, s := range f.Sessions {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func (f *Fake) launch(s *domain.Session, resume bool) ports.AgentSpec {
	if f.Launch != nil {
		return f.Launch(s, resume)
	}
	return ports.AgentSpec{SessionID: s.ID, Conversation: ports.Conversation{ID: s.ClaudeSessionID, Resume: resume}}
}
