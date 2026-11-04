package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// Driving ports of the agents context: agent templates and the prompts they
// are built from. *agents.Service satisfies them.

// PromptCatalog manages reusable prompts.
type PromptCatalog interface {
	ListPrompts() []*domain.Prompt
	GetPrompt(id string) *domain.Prompt
	CreatePrompt(name, description, content string) (*domain.Prompt, error)
	UpdatePrompt(id, name, description, content string) (*domain.Prompt, error)
	DeletePrompt(id string) error
}

// AgentLister reads agent templates: what a page offering agents to pick
// from needs. AgentCatalog embeds it.
type AgentLister interface {
	ListAgents(ctx context.Context) ([]*domain.Agent, error)
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
}

// AgentNameLister reads what a page needs to name and offer the agents: each
// one's id, name and description, with no prompt, skill or MCP server
// resolved. A page request shares one catalog read among its callers, so
// asking for the names of the agents costs one read however often it is
// asked. *agents.Service satisfies it.
type AgentNameLister interface {
	ListAgentNames(ctx context.Context) ([]*domain.Agent, error)
}

// AgentCatalog manages agent templates. It is network-safe: tui-client
// implements it over HTTP, so every method takes a context and reports
// failure, AGENT_NOT_FOUND included, as an error.
type AgentCatalog interface {
	AgentLister
	CreateAgent(ctx context.Context, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	UpdateAgent(ctx context.Context, id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	DeleteAgent(ctx context.Context, id string) error
}

// PromptReader is what other contexts read about prompts.
type PromptReader interface {
	GetPrompt(id string) *domain.Prompt
}

// AgentResolver loads an agent ready to run: GetAgent returns it with its
// prompt, skills and MCP servers resolved; the Resolve methods fill an agent a
// caller already holds.
type AgentResolver interface {
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
	ResolveAgentPrompt(a *domain.Agent)
	ResolveAgentRelations(a *domain.Agent)
}

// MCPServerUsage reports which agents use an MCP server, for the usage read
// model the capabilities context shows on each server.
type MCPServerUsage interface {
	AgentsUsingMCPServer(serverID string) []string
}

// Agents is the whole agents context.
type Agents interface {
	PromptCatalog
	AgentCatalog
	AgentResolver
	MCPServerUsage
}

// AgentOffer is what a repository offers a new session: the agents assigned
// there, and the rest of the catalog with why it is not offered (ADR 0061).
type AgentOffer struct {
	Agents     []*domain.Agent
	NotOffered []domain.NotOfferedAgent
}

// AgentOffers reads the agents a new session in a repository can run as.
// tui-client implements it over HTTP.
type AgentOffers interface {
	ListAgentsFor(ctx context.Context, projectID, repositoryID string) (*AgentOffer, error)
}
