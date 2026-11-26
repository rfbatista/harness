// Package agentsystem is the agent system settings store (ADR 0011): project
// defaults, per-repository overrides, harness defaults and the one resolver
// that gives each repository its effective configuration.
package agentsystem

import (
	"context"
	"encoding/json"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.AgentSystemSettings = (*Service)(nil)

// Deps are what the service uses.
type Deps struct {
	Store    ports.AgentSystemStore
	Projects interface {
		Get(ctx context.Context, id string) *domain.Project
	}
	Repositories interface {
		Get(ctx context.Context, id string) *domain.Repository
	}
	// ReviewModel is the review-mode session model, the critic's harness default.
	ReviewModel func() string
	// Events and Feed announce a save; nil: nobody listens.
	Events ports.EventPublisher
	Feed   ports.ProjectChangeSink
}

// Service is the store.
type Service struct{ d Deps }

// New returns the service.
func New(d Deps) *Service { return &Service{d: d} }

func (s *Service) checkProject(ctx context.Context, projectID string) error {
	if s.d.Projects.Get(ctx, projectID) == nil {
		return &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found: " + projectID}
	}
	return nil
}

// defaults are the harness defaults for a repository ("" for the project).
func (s *Service) defaults(_ context.Context, _ string) domain.AgentSystemHarnessDefaults {
	return domain.AgentSystemHarnessDefaults{ReviewModel: s.d.ReviewModel()}
}

// Effective implements ports.AgentSystemSettings.
func (s *Service) Effective(ctx context.Context, projectID, repositoryID string) (domain.EffectiveAgentSystem, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.EffectiveAgentSystem{}, err
	}
	if repositoryID != "" {
		if r := s.d.Repositories.Get(ctx, repositoryID); r == nil || r.ProjectID != projectID {
			return domain.EffectiveAgentSystem{}, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found: " + repositoryID}
		}
	}
	project, err := s.d.Store.Values(ctx, domain.SourceProject, projectID)
	if err != nil {
		return domain.EffectiveAgentSystem{}, err
	}
	eff := domain.ResolveAgentSystem(s.defaults(ctx, repositoryID), domain.AgentSystemLevel{Source: domain.SourceProject, Values: project})
	if repositoryID == "" {
		eff.MissingRequiredGates = nil // no checkout to detect from: a project has no gates to miss
	}
	return eff, nil
}

// ProjectView implements ports.AgentSystemSettings.
func (s *Service) ProjectView(ctx context.Context, projectID string) (domain.AgentSystemView, error) {
	eff, err := s.Effective(ctx, projectID, "")
	if err != nil {
		return domain.AgentSystemView{}, err
	}
	values, err := s.d.Store.Values(ctx, domain.SourceProject, projectID)
	return domain.AgentSystemView{Effective: eff, Values: values}, err
}

// SaveProject implements ports.AgentSystemSettings.
func (s *Service) SaveProject(ctx context.Context, projectID string, patch map[string]json.RawMessage) (domain.AgentSystemView, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.AgentSystemView{}, err
	}
	set, reset, err := domain.ValidateAgentSystemPatch(patch)
	if err != nil {
		return domain.AgentSystemView{}, err
	}
	if err := s.d.Store.Apply(ctx, domain.SourceProject, projectID, projectID, set, reset); err != nil {
		return domain.AgentSystemView{}, err
	}
	s.announce(ctx, projectID, "")
	return s.ProjectView(ctx, projectID)
}

// announce tells the event bus and the project feed that settings changed.
func (s *Service) announce(ctx context.Context, projectID, repositoryID string) {
	if s.d.Events != nil {
		_ = s.d.Events.Publish(ctx, domain.AgentSystemSettingsChanged{ProjectID: projectID, RepositoryID: repositoryID})
	}
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{AgentSystemSettings: &ports.AgentSystemSettingsChange{ProjectID: projectID, RepositoryID: repositoryID}})
	}
}
