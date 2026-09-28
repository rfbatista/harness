package ports

import (
	"operators-mcp/internal/domain"

	"github.com/rfbatista/harnesskit/mcpserver"
	"github.com/rfbatista/harnesskit/skill"
)

// ProjectRepository is the outbound port for persisting and retrieving projects.
// A project defines the directory root that everything is based on.
type ProjectRepository interface {
	Get(id string) *domain.Project
	List() []*domain.Project
	Create(name, rootDir string) (*domain.Project, error)
	Update(id, name, rootDir string) (*domain.Project, error)
	Delete(projectID string) error
	AddIgnoredPath(projectID, path string) (*domain.Project, error)
	RemoveIgnoredPath(projectID, path string) (*domain.Project, error)
}

// RepositoryRepository is the outbound port for persisting and retrieving
// repositories. A repository is a code repository scoped to a project; a project
// can hold many repositories (one-to-many via ProjectID).
type RepositoryRepository interface {
	Get(id string) *domain.Repository
	ListByProject(projectID string) []*domain.Repository
	Create(projectID, name, description, url, rootDir string) (*domain.Repository, error)
	Update(id, name, description, url, rootDir string) (*domain.Repository, error)
	Delete(id string) error
	DeleteByProject(projectID string) error
	AddIgnoredPath(repositoryID, path string) (*domain.Repository, error)
	RemoveIgnoredPath(repositoryID, path string) (*domain.Repository, error)
}

// ZoneRepository is the outbound port for persisting and retrieving zones.
// Zones are scoped to a project. Implemented by adapters (e.g. in-memory store, future DB).
type ZoneRepository interface {
	Get(id string) *domain.Zone
	ListByProject(projectID string) []*domain.Zone
	Create(projectID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error)
	Update(id, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error)
	AssignPath(zoneID, path string) (*domain.Zone, error)
	UnassignPath(zoneID, path string) (*domain.Zone, error)
	// SetBoundedContext links the zone to a bounded context; an empty
	// boundedContextID clears the link.
	SetBoundedContext(zoneID, boundedContextID string) (*domain.Zone, error)
	// ClearBoundedContext unlinks every zone pointing at the given bounded context.
	ClearBoundedContext(boundedContextID string) error
	DeleteByProject(projectID string) error
}

// BoundedContextRepository is the outbound port for persisting and retrieving
// bounded contexts (DDD): named areas of a project's domain that group zones
// and carry purpose plus ubiquitous language.
type BoundedContextRepository interface {
	Get(id string) *domain.BoundedContext
	ListByProject(projectID string) []*domain.BoundedContext
	Create(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error)
	Update(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error)
	Delete(id string) error
	DeleteByProject(projectID string) error
}

// PathMatcher is the outbound port for listing paths under a root that match a regex pattern.
// Implemented by the filesystem adapter.
type PathMatcher interface {
	ListMatchingPaths(root, pattern string) ([]string, error)
}

// TreeLister is the outbound port for building a directory tree from a root path.
// Implemented by the filesystem adapter.
type TreeLister interface {
	ListTree(root string) (*domain.TreeNode, error)
}

// AgentRepository is the outbound port for persisting and retrieving agents.
// Agents can be assigned to zones and can reference prompts and skills.
type AgentRepository interface {
	Get(id string) *domain.Agent
	List() []*domain.Agent
	Create(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	Delete(id string) error
}

// PromptRepository is the outbound port for persisting and retrieving prompts.
// Prompts can be referenced by agents via PromptID.
type PromptRepository interface {
	Get(id string) *domain.Prompt
	List() []*domain.Prompt
	Create(name, description, content string) (*domain.Prompt, error)
	Update(id, name, description, content string) (*domain.Prompt, error)
	Delete(id string) error
}

// SkillRepository persists skills and their file trees.
// It is an alias for skill.Store, which defines the contract; verify an
// implementation against skilltest.StoreConformance.
type SkillRepository = skill.Store

// SettingsRepository is the outbound port for global key/value app settings.
// Get returns "" for an unset key rather than an error.
type SettingsRepository interface {
	All() (map[string]string, error)
	Get(key string) (string, error)
	Set(key, value string) error
}

// MCPServerRepository persists MCP server configurations.
// It is an alias for mcpserver.Store, which defines the contract.
type MCPServerRepository = mcpserver.Store

// ToolRepository is the outbound port for persisting and retrieving user-defined tools.
type ToolRepository interface {
	Get(id string) *domain.Tool
	List() []*domain.Tool
	Create(name, description string, inputSchema map[string]any) (*domain.Tool, error)
	Update(id, name, description string, inputSchema map[string]any) (*domain.Tool, error)
	Delete(id string) error
}

// TaskFilter constrains which tasks are returned by List.
type TaskFilter struct {
	ZoneID    string
	AgentID   string
	ProjectID string
	Status    domain.TaskStatus
}

// TaskRepository is the outbound port for persisting and retrieving execution tasks.
type TaskRepository interface {
	Get(id string) *domain.Task
	List(filter TaskFilter) []*domain.Task
	Create(task *domain.Task) (*domain.Task, error)
	UpdateStatus(id string, status domain.TaskStatus, result, errMsg string) error
}

// SkillPublisher projects a stored skill tree onto <root>/<slug>.
// It is an alias for skill.Publisher.
type SkillPublisher = skill.Publisher

// WorkspaceRepository is the outbound port for persisting and retrieving
// repository workspaces (git worktrees tracked in the database).
type WorkspaceRepository interface {
	Get(id string) *domain.Workspace
	ListByRepository(repositoryID string) []*domain.Workspace
	Create(repositoryID, name, branch, path string) (*domain.Workspace, error)
	Delete(id string) error
}
