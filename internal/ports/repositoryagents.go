package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// RepositoryAgentListStore keeps each repository's unassigned agents (driven,
// ADR 0061).
type RepositoryAgentListStore interface {
	// Get is a repository's list; a repository never written has an empty
	// list with version "".
	Get(ctx context.Context, repositoryID string) (*domain.RepositoryAgentList, error)
	// Put replaces the repository's list.
	Put(ctx context.Context, list *domain.RepositoryAgentList) error
	// DeleteByRepository forgets a deleted repository's list.
	DeleteByRepository(ctx context.Context, repositoryID string) error
}

// RepositoryAgents assigns agents to repositories. *repoagents.Service
// satisfies it. Errors: REPOSITORY_NOT_FOUND, AGENT_NOT_FOUND,
// REPOSITORY_AGENTS_CHANGED.
type RepositoryAgents interface {
	RepositoryPrompts
	AgentPlacements
	GetRepositoryAgents(ctx context.Context, repositoryID string) (*domain.RepositoryAgentsView, error)
	UnassignAgent(ctx context.Context, repositoryID, agent, listVersion string) (*domain.UnassignedAnswer, error)
	AssignAgentAgain(ctx context.Context, repositoryID, agent, listVersion string) (*domain.RepositoryAgentsView, error)
	AssignAllAgentsAgain(ctx context.Context, repositoryID, listVersion string) (*domain.RepositoryAgentsView, error)
	// FollowRepositoryAgents delivers each change until ctx ends; the channel
	// closes when ctx ends or the follower fell behind.
	FollowRepositoryAgents(ctx context.Context) (<-chan domain.RepositoryAgentsChanged, error)
}

// AgentAssignments is what a session start and a session read ask about a
// repository's assigned agents (ADR 0061). *repoagents.Service satisfies it.
type AgentAssignments interface {
	// CheckAssigned is nil when the agent is assigned to the repository, else
	// AGENT_NOT_ASSIGNED, whose message names the agents that are.
	CheckAssigned(ctx context.Context, repositoryID, agent string) error
	// IsUnassigned says whether the repository's list holds the agent.
	IsUnassigned(ctx context.Context, repositoryID, agent string) bool
}

// RepositoryPromptStore keeps the repository prompts (driven, ADR 0061).
type RepositoryPromptStore interface {
	// Get is the entry, or one with empty content and version "" when none.
	Get(ctx context.Context, repositoryID, agent string) (*domain.RepositoryPrompt, error)
	// ListByRepository is the repository's entries that are set.
	ListByRepository(ctx context.Context, repositoryID string) ([]*domain.RepositoryPrompt, error)
	// Put creates or replaces the entry.
	Put(ctx context.Context, p *domain.RepositoryPrompt) error
	// Delete removes the entry; none is not an error.
	Delete(ctx context.Context, repositoryID, agent string) error
}

// RepositoryPrompts writes and reads the repository prompts. *repoagents.Service
// satisfies it. Errors: REPOSITORY_NOT_FOUND, AGENT_NOT_FOUND,
// REPOSITORY_PROMPT_CHANGED, REPOSITORY_PROMPT_TOO_LONG.
type RepositoryPrompts interface {
	GetRepositoryPrompt(ctx context.Context, repositoryID, agent string) (*domain.RepositoryPromptAnswer, error)
	// SaveRepositoryPrompt stores content; blank content removes the entry.
	SaveRepositoryPrompt(ctx context.Context, repositoryID, agent, content, version string) (*domain.RepositoryPromptAnswer, error)
	// PreviewRepositoryPrompt builds what the agent would receive for content,
	// saved or not, and stores nothing.
	PreviewRepositoryPrompt(ctx context.Context, repositoryID, agent, content string) (*domain.RepositoryPromptPreview, error)
}

// RepositoryPromptSections is what a session start and resume read: the text
// to append after an agent's own prompt in a repository. *repoagents.Service
// satisfies it.
type RepositoryPromptSections interface {
	// RepositoryPromptSection is the heading and the repository prompt of the
	// agent in the repository, "" when there is none. A prompt that cannot be
	// read is none: a session does not fail on it.
	RepositoryPromptSection(ctx context.Context, repositoryID, agent string) string
}

// AgentPlacements says where agents are assigned, for the reads that list
// agents. *repoagents.Service satisfies it.
type AgentPlacements interface {
	// UnassignedFrom names, for each agent, the repositories of the project it
	// is unassigned from; an empty projectID reads every project.
	UnassignedFrom(ctx context.Context, projectID string) (map[string][]domain.RepositoryRef, error)
	// AgentStates is every catalog agent's standing in the repository.
	AgentStates(ctx context.Context, repositoryID string) (map[string]domain.AgentRepositoryState, error)
	// DecorateCatalog returns the catalog view with each agent's repositories.
	DecorateCatalog(ctx context.Context, view *domain.CatalogView) (*domain.CatalogView, error)
}
