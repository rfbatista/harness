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
	GetRepositoryAgents(ctx context.Context, repositoryID string) (*domain.RepositoryAgentsView, error)
	UnassignAgent(ctx context.Context, repositoryID, agent, listVersion string) (*domain.UnassignedAnswer, error)
	AssignAgentAgain(ctx context.Context, repositoryID, agent, listVersion string) (*domain.RepositoryAgentsView, error)
	AssignAllAgentsAgain(ctx context.Context, repositoryID, listVersion string) (*domain.RepositoryAgentsView, error)
	// FollowRepositoryAgents delivers each change until ctx ends; the channel
	// closes when ctx ends or the follower fell behind.
	FollowRepositoryAgents(ctx context.Context) (<-chan domain.RepositoryAgentsChanged, error)
}
