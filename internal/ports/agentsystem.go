package ports

import (
	"context"
	"encoding/json"

	"operators-mcp/internal/domain"
)

// AgentSystemSettings is the agent system settings store as the layers and the
// settings UI use it (ADR 0011; contract "Agent system settings store ↔ layers
// and web UI"). Saving is the person's: the save routes are person-only by
// convention, like answering a branch or service request, until the harness
// has a check of who calls.
type AgentSystemSettings interface {
	// Effective is the repository's effective configuration, each field with
	// its source, and the required gates it is missing. An empty repositoryID
	// asks for the project's defaults. The value is the caller's to keep: a
	// layer takes it when a session starts or resumes and keeps it for that
	// session, so a later save never changes a running session.
	Effective(ctx context.Context, projectID, repositoryID string) (domain.EffectiveAgentSystem, error)

	// ProjectView is the project's defaults: what it sets, and the effective
	// configuration with sources.
	ProjectView(ctx context.Context, projectID string) (domain.AgentSystemView, error)
	// SaveProject sets (a value) or resets (null) project fields by path, all
	// or none: a refused save is an *domain.AgentSystemValidationError and
	// stores nothing. It answers the project's new view.
	SaveProject(ctx context.Context, projectID string, patch map[string]json.RawMessage) (domain.AgentSystemView, error)
	// RepositoryView is the repository's effective configuration with sources
	// and missing required gates, and the overrides it sets itself.
	RepositoryView(ctx context.Context, projectID, repositoryID string) (domain.AgentSystemView, error)
	// SaveRepository sets or resets (null) repository overrides, as SaveProject.
	SaveRepository(ctx context.Context, projectID, repositoryID string, patch map[string]json.RawMessage) (domain.AgentSystemView, error)
}

// AgentSystemSettingsChange is a save of agent system settings on the project
// feed; RepositoryID is empty for a project-level save. The page re-reads.
type AgentSystemSettingsChange struct {
	ProjectID    string `json:"project_id"`
	RepositoryID string `json:"repository_id,omitempty"`
}

// AgentSystemStore persists the levels' values (driven; sqlite implements it).
// A level is domain.SourceProject or domain.SourceRepository, its owner the
// project or repository id.
type AgentSystemStore interface {
	Values(ctx context.Context, level domain.AgentSystemSource, ownerID string) (domain.AgentSystemValues, error)
	// Apply stores set and removes reset in one transaction.
	Apply(ctx context.Context, level domain.AgentSystemSource, ownerID, projectID string, set domain.AgentSystemValues, reset []string) error
	// DeleteOwner removes everything one project or repository set itself.
	DeleteOwner(ctx context.Context, level domain.AgentSystemSource, ownerID string) error
	// DeleteProject removes everything the project and its repositories set.
	DeleteProject(ctx context.Context, projectID string) error
}
