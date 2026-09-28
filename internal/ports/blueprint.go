package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// Driving ports of the blueprint service: the catalog of projects and what
// they are made of — repositories, zones, bounded contexts — and of the agent
// building blocks: prompts, agents, tools, skills, MCP servers, settings.
// *blueprint.Service satisfies them all.

// ProjectCatalog manages projects.
type ProjectCatalog interface {
	ListProjects() []*domain.Project
	GetProject(projectID string) *domain.Project
	CreateProject(name, rootDir string) (*domain.Project, error)
	UpdateProject(projectID, name, rootDir string) (*domain.Project, error)
	DeleteProject(projectID string) error
	AddIgnoredPath(projectID, path string) (*domain.Project, error)
	RemoveIgnoredPath(projectID, path string) (*domain.Project, error)
}

// RepositoryCatalog manages the git repositories of a project.
type RepositoryCatalog interface {
	ListRepositories(projectID string) []*domain.Repository
	GetRepository(id string) *domain.Repository
	CreateRepository(projectID, name, description, url, rootDir string) (*domain.Repository, error)
	UpdateRepository(id, name, description, url, rootDir string) (*domain.Repository, error)
	DeleteRepository(id string) error
	AddRepositoryIgnoredPath(repositoryID, path string) (*domain.Repository, error)
	RemoveRepositoryIgnoredPath(repositoryID, path string) (*domain.Repository, error)
}

// ZoneCatalog manages a project's zones and the paths assigned to them.
type ZoneCatalog interface {
	ListZones(projectID string) []*domain.Zone
	GetZone(zoneID string) *domain.Zone
	CreateZone(projectID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error)
	UpdateZone(zoneID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error)
	AssignPathToZone(zoneID, path string) (*domain.Zone, error)
	UnassignPathFromZone(zoneID, path string) (*domain.Zone, error)
}

// BoundedContextCatalog manages a project's bounded contexts and which zones
// belong to them.
type BoundedContextCatalog interface {
	ListBoundedContexts(projectID string) []*domain.BoundedContext
	GetBoundedContext(id string) *domain.BoundedContext
	CreateBoundedContext(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error)
	UpdateBoundedContext(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error)
	DeleteBoundedContext(id string) error
	AssignZoneToBoundedContext(zoneID, boundedContextID string) (*domain.Zone, error)
	UnassignZoneFromBoundedContext(zoneID string) (*domain.Zone, error)
}

// PathExplorer reads a project's file tree. root overrides the project's root
// directory when set.
type PathExplorer interface {
	ListMatchingPaths(root, projectID, pattern string) ([]string, error)
	ListTree(root, projectID string) (*domain.TreeNode, error)
}

// PromptCatalog manages reusable prompts.
type PromptCatalog interface {
	ListPrompts() []*domain.Prompt
	GetPrompt(id string) *domain.Prompt
	CreatePrompt(name, description, content string) (*domain.Prompt, error)
	UpdatePrompt(id, name, description, content string) (*domain.Prompt, error)
	DeletePrompt(id string) error
}

// AgentCatalog manages agent templates.
type AgentCatalog interface {
	ListAgents() []*domain.Agent
	GetAgent(id string) *domain.Agent
	CreateAgent(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	UpdateAgent(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	DeleteAgent(id string) error
}

// ToolCatalog manages user-defined tools.
type ToolCatalog interface {
	ListTools() []*domain.Tool
	GetTool(id string) *domain.Tool
	CreateTool(name, description string, inputSchema map[string]any) (*domain.Tool, error)
	UpdateTool(id, name, description string, inputSchema map[string]any) (*domain.Tool, error)
	DeleteTool(id string) error
}

// SkillCatalog manages skills, their files, and their publication.
type SkillCatalog interface {
	ListSkills() []*domain.Skill
	GetSkill(id string) *domain.Skill
	ValidateSkillPath(path string) (*domain.SkillPathValidation, error)
	InspectSkill(path string) (*domain.SkillInspection, error)
	CreateSkill(input domain.SkillInput) (*domain.Skill, error)
	UpdateSkill(id string, input domain.SkillInput) (*domain.Skill, error)
	PutSkillFile(id string, f domain.SkillFile) (*domain.Skill, error)
	RenameSkillFile(id, oldPath, newPath string) (*domain.Skill, error)
	DeleteSkillFile(id, path string) (*domain.Skill, error)
	ListSkillFiles(id string) ([]domain.SkillFile, error)
	ImportSkillFromPath(path string) (*domain.Skill, error)
	DeleteSkill(id string) error
	PublishSkill(id string, force bool) (*domain.Skill, error)
	UnpublishSkill(id string) (*domain.Skill, error)
}

// MCPServerCatalog manages MCP server configurations.
type MCPServerCatalog interface {
	ListMCPServers() []*domain.MCPServer
	GetMCPServer(id string) *domain.MCPServer
	CreateMCPServer(in domain.MCPServerInput) (*domain.MCPServer, error)
	UpdateMCPServer(id string, in domain.MCPServerInput) (*domain.MCPServer, error)
	UpdateMCPServerProbeResult(id string, result domain.MCPProbeResult) (*domain.MCPServer, error)
	ImportMCPServers(content, onDuplicate string) ([]*domain.MCPServer, error)
	ProbeMCPServer(ctx context.Context, server domain.MCPServer) (domain.MCPProbeResult, error)
	DeleteMCPServer(id string) error
}

// SettingsEditor reads and writes application settings.
type SettingsEditor interface {
	GetSettings() (map[string]string, error)
	UpdateSettings(values map[string]string) (map[string]string, error)
}

// Blueprint is the whole catalog, for an adapter that serves all of it (the
// HTTP API). Take the narrowest role that covers what you call.
type Blueprint interface {
	ProjectCatalog
	RepositoryCatalog
	ZoneCatalog
	BoundedContextCatalog
	PathExplorer
	PromptCatalog
	AgentCatalog
	ToolCatalog
	SkillCatalog
	MCPServerCatalog
	SettingsEditor
}
