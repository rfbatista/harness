// Package agentsystem is the agent system settings store (ADR 0011): project
// defaults, per-repository overrides, harness defaults and the one resolver
// that gives each repository its effective configuration.
package agentsystem

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.AgentSystemSettings = (*Service)(nil)

// Deps are what the service reads.
type Deps struct {
	Projects interface {
		Get(ctx context.Context, id string) *domain.Project
	}
	Repositories interface {
		Get(ctx context.Context, id string) *domain.Repository
	}
	// ReviewModel is the review-mode session model, the critic's harness default.
	ReviewModel func() string
}

// Service is the store.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) *Service { return &Service{d: d} }

// Effective implements ports.AgentSystemSettings.
func (s *Service) Effective(ctx context.Context, projectID, repositoryID string) (domain.EffectiveAgentSystem, error) {
	if s.d.Projects.Get(ctx, projectID) == nil {
		return domain.EffectiveAgentSystem{}, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found: " + projectID}
	}
	if repositoryID != "" {
		if r := s.d.Repositories.Get(ctx, repositoryID); r == nil || r.ProjectID != projectID {
			return domain.EffectiveAgentSystem{}, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found: " + repositoryID}
		}
	}
	eff := domain.ResolveAgentSystem(domain.AgentSystemHarnessDefaults{ReviewModel: s.d.ReviewModel()})
	if repositoryID == "" {
		eff.MissingRequiredGates = nil // no checkout to detect from: a project has no gates to miss
	}
	return eff, nil
}
