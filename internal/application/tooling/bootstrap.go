package tooling

import (
	"github.com/rfbatista/harnesskit/mcptools"

	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/ports"
)

// Bootstrap registers all built-in tool groups into the tooling service.
// execSvc may be nil if the execution layer is not configured (e.g. no Genkit API key).
func Bootstrap(toolingSvc *Service, bpSvc *blueprint.Service, planningSvc ports.Planning, execSvc ports.TaskRunner) {
	toolingSvc.Register(ProjectTools(bpSvc)...)
	toolingSvc.Register(ZoneTools(bpSvc)...)
	toolingSvc.Register(BoundedContextTools(bpSvc)...)
	toolingSvc.Register(AgentTools(bpSvc)...)
	toolingSvc.Register(PromptTools(bpSvc)...)
	toolingSvc.Register(mcptools.SkillTools(bpSvc.SkillService())...)
	toolingSvc.Register(SettingsTools(bpSvc)...)
	toolingSvc.Register(mcptools.MCPServerTools(bpSvc.MCPServerService())...)
	toolingSvc.Register(TreeTools(bpSvc)...)
	toolingSvc.Register(mcptools.ToolTools(toolingSvc, bpSvc.ToolStore())...)
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
