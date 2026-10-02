package httpclient

import (
	"context"
	"net/url"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.SessionReader       = (*Sessions)(nil)
	_ ports.InteractiveSessions = (*Sessions)(nil)
)

// Sessions is ports.SessionReader and ports.InteractiveSessions over the HTTP
// API.
type Sessions struct{ c *Client }

// NewSessions returns the sessions adapter over c.
func NewSessions(c *Client) *Sessions { return &Sessions{c: c} }

func (s *Sessions) Get(ctx context.Context, id string) (*domain.Session, error) {
	var out struct {
		Session *domain.Session `json:"session"`
	}
	return out.Session, s.c.get(ctx, "/api/sessions/"+url.PathEscape(id), nil, &out)
}

func (s *Sessions) List(ctx context.Context, f ports.SessionFilter) ([]*domain.Session, error) {
	q := url.Values{}
	if f.ProjectID != "" {
		q.Set("project_id", f.ProjectID)
	}
	if f.AgentID != "" {
		q.Set("agent_id", f.AgentID)
	}
	if f.TicketID != "" {
		q.Set("ticket_id", f.TicketID)
	}
	if len(f.Statuses) > 0 {
		st := make([]string, len(f.Statuses))
		for i, x := range f.Statuses {
			st[i] = string(x)
		}
		q.Set("status", strings.Join(st, ","))
	}
	var out struct {
		Sessions []*domain.Session `json:"sessions"`
	}
	return out.Sessions, s.c.get(ctx, "/api/sessions", q, &out)
}

type startedOut struct {
	Session *domain.Session `json:"session"`
	Agent   ports.AgentSpec `json:"agent"`
}

func (s *Sessions) StartInteractive(ctx context.Context, req ports.InteractiveRequest) (*domain.Session, ports.AgentSpec, error) {
	var out startedOut
	err := s.c.post(ctx, "/api/start_interactive_session", req, &out)
	return out.Session, out.Agent, err
}

func (s *Sessions) ResumeInteractive(ctx context.Context, req ports.ResumeRequest) (*domain.Session, ports.AgentSpec, error) {
	var out startedOut
	err := s.c.post(ctx, "/api/resume_interactive_session", req, &out)
	return out.Session, out.Agent, err
}

func (s *Sessions) EndInteractive(ctx context.Context, id string, exitCode int, closedByUser bool) (*domain.Session, error) {
	var out struct {
		Session *domain.Session `json:"session"`
	}
	err := s.c.post(ctx, "/api/end_interactive_session", map[string]any{
		"session_id": id, "exit_code": exitCode, "closed": closedByUser,
	}, &out)
	return out.Session, err
}
