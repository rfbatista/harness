package workbench

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

var _ Backend = (*Fake)(nil)

// Fake is an in-memory Backend for screen tests. Started sessions launch
// Command with Args; a test points them at /bin/sh to stand in for claude.
// It answers with the same error codes the server does.
type Fake struct {
	mu sync.Mutex

	Projects     []domain.Project
	Repositories []domain.Repository
	Tickets      []domain.Ticket
	Agents       []Agent
	Sessions     []domain.Session

	// Launch builds the command line a started or resumed session gets.
	Launch func(s domain.Session, resume bool) orchestration.Launch
	// Err, when set, is returned by the next call to the named method.
	Err map[string]error

	Ends []EndCall
	seq  int
}

// EndCall records one EndSession call.
type EndCall struct {
	SessionID string
	ExitCode  int
	Closed    bool
}

func (f *Fake) fail(method string) error {
	if err := f.Err[method]; err != nil {
		delete(f.Err, method)
		return err
	}
	return nil
}

func (f *Fake) Health(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fail("Health")
}

func (f *Fake) ListProjects(context.Context) ([]domain.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Projects), f.fail("ListProjects")
}

func (f *Fake) ListRepositories(_ context.Context, projectID string) ([]domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Repository
	for _, r := range f.Repositories {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, f.fail("ListRepositories")
}

func (f *Fake) ListTickets(_ context.Context, projectID string) ([]domain.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Ticket
	for _, t := range f.Tickets {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, f.fail("ListTickets")
}

func (f *Fake) ListAgents(context.Context) ([]Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Agents), f.fail("ListAgents")
}

func (f *Fake) ListSessions(_ context.Context, flt SessionFilter) ([]domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Session
	for _, s := range f.Sessions {
		if (flt.ProjectID == "" || s.ProjectID == flt.ProjectID) &&
			(flt.TicketID == "" || s.TicketID == flt.TicketID) &&
			(len(flt.Statuses) == 0 || slices.Contains(flt.Statuses, s.Status)) {
			out = append(out, s)
		}
	}
	return out, f.fail("ListSessions")
}

func (f *Fake) StartSession(_ context.Context, req orchestration.InteractiveRequest) (domain.Session, orchestration.Launch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("StartSession"); err != nil {
		return domain.Session{}, orchestration.Launch{}, err
	}
	if req.TicketID == "" {
		return domain.Session{}, orchestration.Launch{}, &APIError{Status: 400, Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	f.seq++
	s := domain.Session{
		ID:           fmt.Sprintf("s%d", f.seq),
		ProjectID:    req.ProjectID,
		RepositoryID: req.RepositoryID,
		TicketID:     req.TicketID,
		AgentID:      req.AgentID,
		Task:         req.Prompt,
		Branch:       fmt.Sprintf("agent/s%d", f.seq),
		Status:       domain.SessionRunning,
		Interactive:  true,
		CreatedAt:    time.Now(),
	}
	s.ClaudeSessionID = s.ID
	f.Sessions = append(f.Sessions, s)
	return s, f.launch(s, false), nil
}

func (f *Fake) ResumeSession(_ context.Context, id string) (domain.Session, orchestration.Launch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("ResumeSession"); err != nil {
		return domain.Session{}, orchestration.Launch{}, err
	}
	i := f.index(id)
	if i < 0 {
		return domain.Session{}, orchestration.Launch{}, &APIError{Status: 404, Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	if !f.Sessions[i].Status.IsTerminal() {
		return domain.Session{}, orchestration.Launch{}, &APIError{Status: 409, Code: "SESSION_ALREADY_RUNNING", Message: "session is still running"}
	}
	f.Sessions[i].Status = domain.SessionRunning
	return f.Sessions[i], f.launch(f.Sessions[i], true), nil
}

func (f *Fake) EndSession(_ context.Context, id string, exitCode int, closed bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ends = append(f.Ends, EndCall{SessionID: id, ExitCode: exitCode, Closed: closed})
	if err := f.fail("EndSession"); err != nil {
		return err
	}
	i := f.index(id)
	if i < 0 {
		return &APIError{Status: 404, Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	if f.Sessions[i].Status.IsTerminal() {
		return nil
	}
	switch {
	case closed:
		f.Sessions[i].Status = domain.SessionStopped
	case exitCode != 0:
		f.Sessions[i].Status = domain.SessionFailed
	default:
		f.Sessions[i].Status = domain.SessionDone
	}
	return nil
}

// EndCalls returns a copy of the EndSession calls so far.
func (f *Fake) EndCalls() []EndCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Ends)
}

// Session returns the session with the given id.
func (f *Fake) Session(id string) (domain.Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.index(id); i >= 0 {
		return f.Sessions[i], true
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

func (f *Fake) launch(s domain.Session, resume bool) orchestration.Launch {
	if f.Launch != nil {
		return f.Launch(s, resume)
	}
	return orchestration.Launch{SessionID: s.ID, Args: []string{"-c", "cat"}}
}
