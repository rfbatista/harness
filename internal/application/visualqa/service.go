// Package visualqa starts visual QA for a session: a qa-mode session on the
// repository's declared UI (Glossary "Visual QA"; ADR 0011). The qa session
// files the service request for the app itself.
package visualqa

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.VisualQA = (*Service)(nil)

// Deps are what a visual QA start reads and drives.
type Deps struct {
	Sessions interface {
		Find(ctx context.Context, id string) (*domain.Session, error)
	}
	Settings ports.AgentSystemSettings
	Starter  interface {
		StartInteractive(ctx context.Context, req ports.InteractiveRequest) (*domain.Session, ports.AgentSpec, error)
	}
}

// Service starts visual QA.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) *Service { return &Service{d: d} }

// Start implements ports.VisualQA.
func (s *Service) Start(ctx context.Context, sessionID string) (*domain.Session, error) {
	sess, err := s.d.Sessions.Find(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	eff, err := s.d.Settings.Effective(ctx, sess.ProjectID, sess.RepositoryID)
	if err != nil {
		return nil, err
	}
	if eff.Review.VisualQA.UI.Value == nil {
		return nil, &domain.StructuredError{Code: "VISUAL_QA_NOT_DECLARED",
			Message: "this repository declares no UI for visual QA: set its start command, URL, routes and viewports in the agent system settings (review.visual_qa.ui)"}
	}
	// The qa session joins the session's worktree, read-only: it walks the
	// running app and changes no code. Its brief carries the declaration.
	qa, _, err := s.d.Starter.StartInteractive(ctx, ports.InteractiveRequest{
		ProjectID: sess.ProjectID, RepositoryID: sess.RepositoryID, TicketID: sess.TicketID,
		Mode: string(domain.SessionModeQA), RunsOn: domain.RunnerServer,
		WorktreeID: sess.WorkspaceID, Access: string(domain.AccessReadOnly),
	})
	return qa, err
}
