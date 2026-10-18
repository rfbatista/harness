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

// TokenSaverActivationReader reads one owner's token saver activations: each
// saver's own value there and what it resolves to. An unknown owner is
// PROJECT_NOT_FOUND, REPOSITORY_NOT_FOUND or TICKET_NOT_FOUND.
type TokenSaverActivationReader interface {
	ListTokenSaverActivations(ctx context.Context, owner domain.ActivationOwner) ([]domain.TokenSaverActivation, error)
}

// TokenSaverActivations reads and sets token saver activations at project,
// repository and task level. Setting returns the owner's activations as
// they now read; a bad saver, level or value is INVALID_INPUT.
type TokenSaverActivations interface {
	TokenSaverActivationReader
	SetTokenSaverActivation(ctx context.Context, owner domain.ActivationOwner, saver domain.TokenSaver, value domain.ActivationValue) ([]domain.TokenSaverActivation, error)
}

// TokenSaverResolver resolves every token saver for a session's task,
// repository and project (task → repository → project, the most specific
// level not inheriting wins). An empty repository or task id skips that
// level.
type TokenSaverResolver interface {
	ResolveTokenSavers(ctx context.Context, projectID, repositoryID, taskID string) ([]domain.ResolvedTokenSaver, error)
}

// SessionTokenSavers is what Orchestration asks Capabilities when it
// prepares an agent session: how each saver resolves for it, and which are
// installed. *capabilities.Service satisfies it.
type SessionTokenSavers interface {
	TokenSaverResolver
	TokenSaverInstallations
	// CavemanIntensity is the caveman intensity sessions get, full by
	// default.
	CavemanIntensity(ctx context.Context) domain.CavemanIntensity
}

// TokenSaverActivationRepository stores activations (driven). Inherit is
// never stored: it is the absence of a row.
type TokenSaverActivationRepository interface {
	// List returns the owner's stored activations.
	List(owner domain.ActivationOwner) ([]domain.Activation, error)
	// ListSaver returns saver's stored activations, at every level.
	ListSaver(saver domain.TokenSaver) ([]domain.Activation, error)
	// Put creates or replaces the activation of a.Saver for a.Owner.
	Put(a domain.Activation) error
	// Delete removes one saver's activation for owner, if stored.
	Delete(saver domain.TokenSaver, owner domain.ActivationOwner) error
	// DeleteOwner removes every activation of owner.
	DeleteOwner(owner domain.ActivationOwner) error
	// DeleteProject removes every activation in the project, at every level.
	DeleteProject(projectID string) error
	// SaversOn returns the savers with an activation stored on at some
	// level, each once.
	SaversOn() ([]domain.TokenSaver, error)
}
