package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ Backend = (*HTTPClient)(nil)

// HTTPClient is Backend over the server's /api routes.
type HTTPClient struct {
	base string
	http *http.Client
}

// NewHTTPClient talks to the server at base, e.g. http://localhost:8080.
func NewHTTPClient(base string) *HTTPClient {
	return &HTTPClient{
		base: strings.TrimRight(base, "/"),
		// Starting a session runs `git worktree add`, which can take a while
		// on a large repository.
		http: &http.Client{Timeout: 2 * time.Minute},
	}
}

func (c *HTTPClient) Health(ctx context.Context) error {
	return c.get(ctx, "/api/health", nil, nil)
}

func (c *HTTPClient) ListProjects(ctx context.Context) ([]domain.Project, error) {
	var out struct {
		Projects []domain.Project `json:"projects"`
	}
	return out.Projects, c.get(ctx, "/api/list_projects", nil, &out)
}

func (c *HTTPClient) ListRepositories(ctx context.Context, projectID string) ([]domain.Repository, error) {
	var out struct {
		Repositories []domain.Repository `json:"repositories"`
	}
	return out.Repositories, c.get(ctx, "/api/list_repositories", url.Values{"project_id": {projectID}}, &out)
}

func (c *HTTPClient) ListTickets(ctx context.Context, projectID string) ([]domain.Ticket, error) {
	var out struct {
		Tickets []domain.Ticket `json:"tickets"`
	}
	return out.Tickets, c.get(ctx, "/api/list_tickets", url.Values{"project_id": {projectID}}, &out)
}

func (c *HTTPClient) ListAgents(ctx context.Context) ([]Agent, error) {
	var out struct {
		Agents []Agent `json:"agents"`
	}
	return out.Agents, c.get(ctx, "/api/list_agents", nil, &out)
}

func (c *HTTPClient) ListSessions(ctx context.Context, f SessionFilter) ([]domain.Session, error) {
	q := url.Values{}
	if f.ProjectID != "" {
		q.Set("project_id", f.ProjectID)
	}
	if f.TicketID != "" {
		q.Set("ticket_id", f.TicketID)
	}
	if len(f.Statuses) > 0 {
		s := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			s[i] = string(st)
		}
		q.Set("status", strings.Join(s, ","))
	}
	var out struct {
		Sessions []domain.Session `json:"sessions"`
	}
	return out.Sessions, c.get(ctx, "/api/sessions", q, &out)
}

type launchResponse struct {
	Session domain.Session `json:"session"`
	Launch  ports.Launch   `json:"launch"`
}

func (c *HTTPClient) StartSession(ctx context.Context, req ports.InteractiveRequest) (domain.Session, ports.Launch, error) {
	var out launchResponse
	err := c.post(ctx, "/api/start_interactive_session", req, &out)
	return out.Session, out.Launch, err
}

func (c *HTTPClient) ResumeSession(ctx context.Context, sessionID string) (domain.Session, ports.Launch, error) {
	var out launchResponse
	err := c.post(ctx, "/api/resume_interactive_session", map[string]string{"session_id": sessionID}, &out)
	return out.Session, out.Launch, err
}

func (c *HTTPClient) EndSession(ctx context.Context, sessionID string, exitCode int, closed bool) error {
	return c.post(ctx, "/api/end_interactive_session", map[string]any{
		"session_id": sessionID, "exit_code": exitCode, "closed": closed,
	}, nil)
}

func (c *HTTPClient) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *HTTPClient) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *HTTPClient) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("coding_pool server at %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		var e struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			apiErr.Message, apiErr.Code = e.Error, e.Code
		}
		return apiErr
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", req.URL.Path, err)
	}
	return nil
}
