// Package catalog wires the five catalog bounded contexts — projects,
// architecture, agents, capabilities, settings — over one event bus. It is
// plain construction with no fx, so the fx graph and tests build the contexts
// exactly the same way.
//
// Context map (who reads whom, who reacts to whom):
//
//	architecture ──reads──▶ projects (root dir), agents (zone rule prompts)
//	agents       ──reads──▶ capabilities (skills, MCP servers)
//	capabilities ──reads──▶ settings (publish root), agents (MCP server usage),
//	                        projects (an activation's project and repositories)
//
//	ProjectDeleted   ──▶ architecture drops the project's zones and bounded contexts
//	PromptDeleted    ──▶ architecture drops it from zone rules
//	AgentDeleted     ──▶ architecture drops it from zone assignments
//	SkillDeleted     ──▶ agents unlink it (a failure cancels the delete)
//	MCPServerDeleted ──▶ agents unlink it (a failure cancels the delete)
//	SettingsChanged  ──▶ capabilities migrates published skills to a new root
//	ProjectDeleted, RepositoryDeleted, TicketDeleted
//	                 ──▶ capabilities drops the owner's token saver activations
package catalog

import (
	"context"
	"fmt"
	"sync"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/application/agents"
	"operators-mcp/internal/application/architecture"
	"operators-mcp/internal/application/capabilities"
	"operators-mcp/internal/application/designsystem"
	"operators-mcp/internal/application/projects"
	"operators-mcp/internal/application/repoarch"
	"operators-mcp/internal/application/settings"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Deps are the driven ports the contexts are built on. Any may be nil where a
// context tolerates it (repositories, bounded contexts, settings, publisher).
type Deps struct {
	Projects        ports.ProjectRepository
	Repositories    ports.RepositoryRepository
	Zones           ports.ZoneRepository
	BoundedContexts ports.BoundedContextRepository
	// ArchRecords keeps repositories' architecture records; nil disables them.
	ArchRecords ports.ArchitectureRecordStore
	// ArchGit reads checkouts for previews and discovery; nil disables them.
	ArchGit ports.ArchitectureGit
	// ArchEvents keeps the architecture rule events the API lists.
	ArchEvents ports.ArchitectureRuleEventStore
	// DesignRecords keeps projects' design system versions; nil disables them.
	DesignRecords ports.DesignSystemStore
	// DesignExports keeps each repository's last design system export; nil
	// disables the export.
	DesignExports ports.DesignExportStore
	Agents        ports.AgentRepository
	Prompts       ports.PromptRepository
	Skills        ports.SkillRepository
	MCPServers    ports.MCPServerRepository
	Tools         ports.ToolRepository
	Settings      ports.SettingsRepository
	Publisher     ports.SkillPublisher
	PathMatcher   ports.PathMatcher
	TreeLister    ports.TreeLister
	// RepositoryFinder lets projects discover git checkouts; nil disables it.
	RepositoryFinder ports.RepositoryFinder
	// EnvFiles and EnvFileIO keep repositories' env files; nil disables them.
	EnvFiles  ports.EnvFileRepository
	EnvFileIO ports.EnvFileIO
	// RunCommands keeps repositories' saved run commands; nil disables them.
	RunCommands ports.RunCommandRepository
	// Activations keeps token saver activations; nil disables them.
	Activations ports.TokenSaverActivationRepository
	DefaultRoot string
	// Bus carries the domain events; nil creates one.
	Bus *eventbus.Bus
}

// Catalog is the five contexts, wired.
type Catalog struct {
	Bus          *eventbus.Bus
	Projects     *projects.Service
	Architecture *architecture.Service
	// RepositoryArchitectures is nil without Deps.ArchRecords.
	RepositoryArchitectures *repoarch.Service
	// DesignSystems is nil without Deps.DesignRecords.
	DesignSystems *designsystem.Service
	Agents        *agents.Service
	Capabilities  *capabilities.Service
	Settings      *settings.Service
}

// New builds the contexts, subscribes each to the events it reacts to, and
// binds the one late read port (agents ↔ capabilities read each other).
func New(d Deps) Catalog {
	bus := d.Bus
	if bus == nil {
		bus = eventbus.New()
	}

	st := settings.NewService(d.Settings, bus)
	pr := projects.NewService(d.Projects, d.Repositories, bus)
	caps := capabilities.NewService(capabilities.Deps{
		Skills:      d.Skills,
		MCPServers:  d.MCPServers,
		Tools:       d.Tools,
		Settings:    st,
		Publisher:   d.Publisher,
		Events:      bus,
		Activations: d.Activations,
		Projects:    pr,
	})
	ag := agents.NewService(d.Agents, d.Prompts, caps, bus)
	caps.UseMCPServerUsage(ag)
	if d.RepositoryFinder != nil {
		pr.UseFinder(d.RepositoryFinder)
	}
	if d.EnvFiles != nil {
		pr.UseEnvFiles(d.EnvFiles, d.EnvFileIO)
	}
	if d.RunCommands != nil {
		pr.UseRunCommands(d.RunCommands)
	}
	arch := architecture.NewService(architecture.Deps{
		Zones:           d.Zones,
		BoundedContexts: d.BoundedContexts,
		PathMatcher:     d.PathMatcher,
		TreeLister:      d.TreeLister,
		DefaultRoot:     d.DefaultRoot,
		Projects:        pr,
		Prompts:         ag,
	})

	var repoArch *repoarch.Service
	if d.ArchRecords != nil {
		repoArch = repoarch.NewService(repoarch.Deps{Store: d.ArchRecords, Repositories: pr, Agents: ag, Contexts: arch, Git: d.ArchGit, MCPServers: caps, Events: d.ArchEvents})
		repoArch.Subscribe(bus)
	}

	var designs *designsystem.Service
	if d.DesignRecords != nil {
		designs = designsystem.NewService(designsystem.Deps{Store: d.DesignRecords, Projects: pr})
		designs.Subscribe(bus)
		if d.ArchGit != nil {
			designs.UseImport(d.ArchGit, pr)
			if git, ok := d.ArchGit.(ports.DesignExportGit); ok && d.DesignExports != nil {
				designs.UseExport(git, pr, d.DesignExports)
			}
		}
		var inUse sync.Mutex // apply and delete of a design system exclude each other
		pr.UseDesignSystems(d.DesignRecords, &inUse)
		designs.UseLock(&inUse)
		designs.GuardDelete(func(ctx context.Context, designSystemID string) error {
			used, err := pr.RepositoriesUsingDesignSystem(ctx, designSystemID)
			if err != nil {
				return err
			}
			if len(used) == 0 {
				return nil
			}
			rows := make([]map[string]string, len(used))
			for i, r := range used {
				rows[i] = map[string]string{"repository_id": r.ID, "name": r.DisplayName()}
			}
			return &domain.DetailedError{
				StructuredError: &domain.StructuredError{Code: "DESIGN_SYSTEM_IN_USE", Message: fmt.Sprintf("%d of the project's repositories use this design system; remove or move them first", len(used))},
				Details:         map[string]any{"repositories": rows},
			}
		})
	}

	arch.Subscribe(bus)
	ag.Subscribe(bus)
	caps.Subscribe(bus)

	return Catalog{Bus: bus, Projects: pr, Architecture: arch, RepositoryArchitectures: repoArch, DesignSystems: designs, Agents: ag, Capabilities: caps, Settings: st}
}

// Tooling is the catalog as the built-in tool groups need it.
func (c Catalog) Tooling() tooling.Catalog {
	return tooling.Catalog{
		Projects:     c.Projects,
		Architecture: c.Architecture,
		Agents:       c.Agents,
		Settings:     c.Settings,
		Capabilities: c.Capabilities,
	}
}
