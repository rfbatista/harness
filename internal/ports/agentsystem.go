package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// AgentSystemSettings is the agent system settings store as the layers and the
// settings UI use it (ADR 0011; contract "Agent system settings store ↔ layers
// and web UI").
type AgentSystemSettings interface {
	// Effective is the repository's effective configuration, each field with
	// its source, and the required gates it is missing. An empty repositoryID
	// asks for the project's defaults. The value is the caller's to keep: a
	// layer takes it when a session starts or resumes and keeps it for that
	// session, so a later save never changes a running session.
	Effective(ctx context.Context, projectID, repositoryID string) (domain.EffectiveAgentSystem, error)
}
