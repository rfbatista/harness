// Package branchgate is the harness's guard on an agent session's branch
// actions: the git shim asks it for a branch grant before it runs one
// (ADR 0069).
package branchgate

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BranchGate = (*Service)(nil)

// Deps are the Service's collaborators.
type Deps struct {
	Sessions interface {
		Get(id string) *domain.Session
	}
}

// Service decides branch grants.
type Service struct{ d Deps }

// NewService returns the branch gate.
func NewService(d Deps) *Service { return &Service{d: d} }

// Grant implements ports.BranchGate.
func (s *Service) Grant(_ context.Context, sessionID string, req ports.BranchGrantRequest) ports.BranchGrantAnswer {
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil {
		return ports.BranchGrantAnswer{Reason: "no such session"}
	}
	if req.Kind == domain.BranchSwitch && req.Branch != "" && req.Branch == sess.Branch {
		return ports.BranchGrantAnswer{Allow: true}
	}
	return ports.BranchGrantAnswer{}
}
