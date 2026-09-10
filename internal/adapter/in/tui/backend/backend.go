// Package backend is the seam between the TUI screens and the application
// services. Screens depend on the Backend interface only; the real
// implementation wraps the services, and Fake backs tests without a database.
package backend

import (
	"context"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Backend is everything the TUI asks of the application layer, one method per
// use case, grouped by the screen that owns them.
type Backend interface {
	Reader
	ProjectWriter
	AgentWriter
	SkillWriter
	MCPWriter
	SettingsWriter
	TaskWriter
	DocumentReader
	Spawner
	SessionAccess
}

// Reader is every list the snapshot needs.
type Reader interface {
	ListProjects() []*domain.Project
	ListRepositories(projectID string) []*domain.Repository
	ListBoundedContexts(projectID string) []*domain.BoundedContext
	ListAgents() []*domain.Agent
	ListSkills() []*domain.Skill
	GetSkill(id string) *domain.Skill
	ListMCPServers() []*domain.MCPServer
	// ListTickets returns every ticket when projectID is empty.
	ListTickets(projectID string) []*domain.Ticket
	ListSessions(f ports.SessionFilter) []*domain.Session
	GetSettings() (map[string]string, error)
}

// ProjectWriter covers the Settings section: projects, their repositories,
// bounded contexts and ignored paths.
type ProjectWriter interface {
	CreateProject(name, rootDir string) (*domain.Project, error)
	UpdateProject(id, name, rootDir string) (*domain.Project, error)
	DeleteProject(id string) error
	AddIgnoredPath(projectID, path string) (*domain.Project, error)
	RemoveIgnoredPath(projectID, path string) (*domain.Project, error)
	CreateRepository(projectID, name, description, url, rootDir string) (*domain.Repository, error)
	UpdateRepository(id, name, description, url, rootDir string) (*domain.Repository, error)
	DeleteRepository(id string) error
	CreateBoundedContext(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error)
	UpdateBoundedContext(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error)
	DeleteBoundedContext(id string) error
}

// AgentWriter covers agent profiles and their capability links.
type AgentWriter interface {
	CreateAgent(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	UpdateAgent(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	DeleteAgent(id string) error
}

// SkillWriter covers skills, their file trees, import and publishing.
type SkillWriter interface {
	CreateSkill(in domain.SkillInput) (*domain.Skill, error)
	UpdateSkill(id string, in domain.SkillInput) (*domain.Skill, error)
	DeleteSkill(id string) error
	PutSkillFile(id string, f domain.SkillFile) (*domain.Skill, error)
	RenameSkillFile(id, oldPath, newPath string) (*domain.Skill, error)
	DeleteSkillFile(id, path string) (*domain.Skill, error)
	ValidateSkillPath(path string) (*domain.SkillPathValidation, error)
	ImportSkillFromPath(path string) (*domain.Skill, error)
	PublishSkill(id string, force bool) (*domain.Skill, error)
	UnpublishSkill(id string) (*domain.Skill, error)
}

// MCPWriter covers MCP server configuration, probing and import.
type MCPWriter interface {
	CreateMCPServer(in domain.MCPServerInput) (*domain.MCPServer, error)
	UpdateMCPServer(id string, in domain.MCPServerInput) (*domain.MCPServer, error)
	DeleteMCPServer(id string) error
	// ProbeMCPServer connects to the server and persists the result on it.
	ProbeMCPServer(ctx context.Context, id string) (domain.MCPProbeResult, error)
	// ImportMCPServers parses Cursor-style {"mcpServers": {...}} JSON;
	// onDuplicate is skip, update or rename.
	ImportMCPServers(content, onDuplicate string) ([]*domain.MCPServer, error)
}

// SettingsWriter merges keys into the stored settings.
type SettingsWriter interface {
	UpdateSettings(values map[string]string) (map[string]string, error)
}

// TaskWriter covers tickets (the UI's tasks).
type TaskWriter interface {
	CreateTicket(projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	UpdateTicket(id, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	DeleteTicket(id string) error
}

// DocumentReader covers the documents a task detail shows.
type DocumentReader interface {
	ListDocuments(projectID string) []*domain.Document
	GetDocument(id string) *domain.Document
	ListTicketDocuments(ticketID string) []*domain.Document
}

// Spawner covers what the spawn wizard needs to start a session.
type Spawner interface {
	ListBranches(repositoryID string) ([]domain.GitBranch, error)
	StartSession(ctx context.Context, req orchestration.StartRequest) (*domain.Session, error)
}

// SessionAccess is what the live session screen needs: the event stream and
// the controls that answer the agent.
type SessionAccess interface {
	GetSession(id string) *domain.Session
	// Subscribe returns a live channel, the replay buffer of recent persisted
	// events, and a cancel func that closes the channel.
	Subscribe(id string) (<-chan orchestration.SessionEvent, []orchestration.SessionEvent, func())
	// History returns the durable events after fromSeq.
	History(id string, fromSeq int64) []orchestration.SessionEvent
	Send(ctx context.Context, id, text string) error
	Resolve(ctx context.Context, id, reqID string, allow bool, message string) error
	Answer(ctx context.Context, id, reqID string, answers, notes map[string]string) error
	SetAutoRun(ctx context.Context, id string, enabled bool) error
	Stop(ctx context.Context, id string) error
	DeleteSession(ctx context.Context, id string) error
}
