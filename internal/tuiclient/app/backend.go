// Package app is tui-client's terminal UI: projects, then a project's
// tasks, then a task's agent sessions, each one interactive claude running in
// a term pane. It is a client of the coding_pool HTTP API, so the server
// (`make air`) must be running; the server provisions, records and ends the
// sessions, and the client runs their CLIs.
package app

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Agent is the slice of an agent template the picker shows.
type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionFilter narrows ListSessions; empty fields match everything.
type SessionFilter struct {
	ProjectID string
	TicketID  string
	Statuses  []domain.SessionStatus
}

// Backend is everything the client asks of the server. HTTPClient is the
// real one; Fake stands in for tests.
type Backend interface {
	Health(ctx context.Context) error
	ListProjects(ctx context.Context) ([]domain.Project, error)
	ListRepositories(ctx context.Context, projectID string) ([]domain.Repository, error)
	ListTickets(ctx context.Context, projectID string) ([]domain.Ticket, error)
	ListAgents(ctx context.Context) ([]Agent, error)
	ListSessions(ctx context.Context, f SessionFilter) ([]domain.Session, error)
	StartSession(ctx context.Context, req ports.InteractiveRequest) (domain.Session, ports.Launch, error)
	ResumeSession(ctx context.Context, sessionID string) (domain.Session, ports.Launch, error)
	EndSession(ctx context.Context, sessionID string, exitCode int, closed bool) error
}

// APIError is an error response from the server. Code is the stable error
// code screens branch on; Message is for people.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return e.Code + ": " + e.Message
	}
	return e.Message
}
