package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// Driving ports of the capabilities context: what an agent can be equipped
// with — skills, MCP servers and user-defined tools. *capabilities.Service
// satisfies them.

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

// CapabilityReader is what other contexts read about capabilities.
type CapabilityReader interface {
	GetSkill(id string) *domain.Skill
	GetMCPServer(id string) *domain.MCPServer
}

// Capabilities is the whole capabilities context.
type Capabilities interface {
	SkillCatalog
	MCPServerCatalog
	ToolCatalog
}
