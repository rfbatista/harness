package tooling

import (
	"github.com/rfbatista/harnesskit/mcpserver"
	"github.com/rfbatista/harnesskit/mcptools"
	"github.com/rfbatista/harnesskit/skill"
	"github.com/rfbatista/harnesskit/tool"

	"operators-mcp/internal/ports"
)

// Catalog is what the built-in tool groups are built over: the catalog
// contexts' ports, and the capabilities context's harnesskit services, which
// harnesskit's ready-made tool groups take directly.
type Catalog struct {
	Projects     ports.ProjectCatalog
	Architecture ports.Architecture
	Agents       interface {
		ports.AgentCatalog
		ports.PromptCatalog
	}
	Settings     ports.SettingsEditor
	Capabilities interface {
		SkillService() *skill.Service
		MCPServerService() *mcpserver.Service
		ToolStore() tool.Store
	}
}

// Bootstrap registers all built-in tool groups into the tooling service.
// execSvc may be nil if the execution layer is not configured (e.g. no Genkit API key).
func Bootstrap(toolingSvc *Service, cat Catalog, planningSvc ports.Planning, execSvc ports.TaskRunner) {
	toolingSvc.Register(ProjectTools(cat.Projects)...)
	toolingSvc.Register(ZoneTools(cat.Architecture)...)
	toolingSvc.Register(BoundedContextTools(cat.Architecture)...)
	toolingSvc.Register(AgentTools(cat.Agents)...)
	toolingSvc.Register(PromptTools(cat.Agents)...)
	toolingSvc.Register(mcptools.SkillTools(cat.Capabilities.SkillService())...)
	toolingSvc.Register(SettingsTools(cat.Settings)...)
	toolingSvc.Register(mcptools.MCPServerTools(cat.Capabilities.MCPServerService())...)
	toolingSvc.Register(TreeTools(cat.Architecture)...)
	toolingSvc.Register(mcptools.ToolTools(toolingSvc, cat.Capabilities.ToolStore())...)
	toolingSvc.Register(FilesystemTools()...)
	toolingSvc.Register(RepoMapTools()...)

	if planningSvc != nil {
		toolingSvc.Register(TicketTools(planningSvc)...)
		toolingSvc.Register(DocumentTools(planningSvc)...)
	}

	if execSvc != nil {
		toolingSvc.Register(TaskTools(execSvc)...)
	}
}
