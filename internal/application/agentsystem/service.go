// Package agentsystem is the agent system settings store (ADR 0011): project
// defaults, per-repository overrides, harness defaults and the one resolver
// that gives each repository its effective configuration.
package agentsystem

import (
	"context"
	"encoding/json"
	"slices"

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
	// Detector reads each repository's stack for its gate defaults; nil: none
	// is detected and every required gate is missing until set.
	Detector interface {
		Detect(ctx context.Context, repo *domain.Repository) map[domain.GateKind]domain.GateDefault
	}
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
// A repository deleted since the caller checked it is not found.
func (s *Service) defaults(ctx context.Context, repositoryID string) (domain.AgentSystemHarnessDefaults, error) {
	d := domain.AgentSystemHarnessDefaults{ReviewModel: s.d.ReviewModel()}
	if repositoryID != "" && s.d.Detector != nil {
		repo := s.d.Repositories.Get(ctx, repositoryID)
		if repo == nil {
			return d, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found: " + repositoryID}
		}
		d.Gates = s.d.Detector.Detect(ctx, repo)
	}
	return d, nil
}

// checkRepository refuses a repository that is not in the project.
func (s *Service) checkRepository(ctx context.Context, projectID, repositoryID string) error {
	if r := s.d.Repositories.Get(ctx, repositoryID); r == nil || r.ProjectID != projectID {
		return &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found: " + repositoryID}
	}
	return nil
}

// Effective implements ports.AgentSystemSettings.
func (s *Service) Effective(ctx context.Context, projectID, repositoryID string) (domain.EffectiveAgentSystem, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.EffectiveAgentSystem{}, err
	}
	project, err := s.d.Store.Values(ctx, domain.SourceProject, projectID)
	if err != nil {
		return domain.EffectiveAgentSystem{}, err
	}
	levels := []domain.AgentSystemLevel{{Source: domain.SourceProject, Values: project}}
	if repositoryID != "" {
		if err := s.checkRepository(ctx, projectID, repositoryID); err != nil {
			return domain.EffectiveAgentSystem{}, err
		}
		own, err := s.d.Store.Values(ctx, domain.SourceRepository, repositoryID)
		if err != nil {
			return domain.EffectiveAgentSystem{}, err
		}
		levels = slices.Insert(levels, 0, domain.AgentSystemLevel{Source: domain.SourceRepository, Values: own})
	}
	defaults, err := s.defaults(ctx, repositoryID)
	if err != nil {
		return domain.EffectiveAgentSystem{}, err
	}
	eff := domain.ResolveAgentSystem(defaults, levels...)
	if repositoryID == "" {
		eff.MissingRequiredGates = nil // no checkout to detect from: a project has no gates to miss
	}
	return eff, nil
}

// ProjectView implements ports.AgentSystemSettings.
func (s *Service) ProjectView(ctx context.Context, projectID string) (domain.AgentSystemView, error) {
	return s.view(ctx, projectID, "")
}

// RepositoryView implements ports.AgentSystemSettings.
func (s *Service) RepositoryView(ctx context.Context, projectID, repositoryID string) (domain.AgentSystemView, error) {
	return s.view(ctx, projectID, repositoryID)
}

func (s *Service) view(ctx context.Context, projectID, repositoryID string) (domain.AgentSystemView, error) {
	eff, err := s.Effective(ctx, projectID, repositoryID)
	if err != nil {
		return domain.AgentSystemView{}, err
	}
	level, owner := domain.SourceProject, projectID
	if repositoryID != "" {
		level, owner = domain.SourceRepository, repositoryID
	}
	values, err := s.d.Store.Values(ctx, level, owner)
	return domain.AgentSystemView{Effective: eff, Values: values}, err
}

// SaveProject implements ports.AgentSystemSettings.
func (s *Service) SaveProject(ctx context.Context, projectID string, patch map[string]json.RawMessage) (domain.AgentSystemView, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.AgentSystemView{}, err
	}
	return s.save(ctx, projectID, "", patch)
}

// SaveRepository implements ports.AgentSystemSettings.
func (s *Service) SaveRepository(ctx context.Context, projectID, repositoryID string, patch map[string]json.RawMessage) (domain.AgentSystemView, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.AgentSystemView{}, err
	}
	if err := s.checkRepository(ctx, projectID, repositoryID); err != nil {
		return domain.AgentSystemView{}, err
	}
	return s.save(ctx, projectID, repositoryID, patch)
}

// save validates the whole patch, stores it in one transaction, announces it
// and answers the level's new view.
func (s *Service) save(ctx context.Context, projectID, repositoryID string, patch map[string]json.RawMessage) (domain.AgentSystemView, error) {
	set, reset, err := domain.ValidateAgentSystemPatch(patch)
	if err != nil {
		return domain.AgentSystemView{}, err
	}
	level, owner := domain.SourceProject, projectID
	if repositoryID != "" {
		level, owner = domain.SourceRepository, repositoryID
	}
	if err := s.d.Store.Apply(ctx, level, owner, projectID, set, reset); err != nil {
		return domain.AgentSystemView{}, err
	}
	s.announce(ctx, projectID, repositoryID)
	return s.view(ctx, projectID, repositoryID)
}

// Subscribe removes what a deleted project or repository set.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.ProjectDeleted) error {
		return s.d.Store.DeleteProject(ctx, ev.ProjectID)
	})
	ports.On(sub, func(ctx context.Context, ev domain.RepositoryDeleted) error {
		return s.d.Store.DeleteOwner(ctx, domain.SourceRepository, ev.RepositoryID)
	})
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
