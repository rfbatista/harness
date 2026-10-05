// Package catalog wires the five catalog bounded contexts — projects,
// architecture, agents, capabilities, settings — over one event bus. It is
// plain construction with no fx, so the fx graph and tests build the contexts
// exactly the same way.
//
// Context map (who reads whom, who reacts to whom):
//
//	architecture ──reads──▶ projects (root dir), agents (zone rule prompts)
//	agents       ──reads──▶ capabilities (skills, MCP servers)
//	capabilities ──reads──▶ settings (publish root), agents (MCP server usage)
//
//	ProjectDeleted   ──▶ architecture drops the project's zones and bounded contexts
//	PromptDeleted    ──▶ architecture drops it from zone rules
//	AgentDeleted     ──▶ architecture drops it from zone assignments
//	SkillDeleted     ──▶ agents unlink it (a failure cancels the delete)
//	MCPServerDeleted ──▶ agents unlink it (a failure cancels the delete)
//	SettingsChanged  ──▶ capabilities migrates published skills to a new root
package catalog

import (
	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/application/agents"
	"operators-mcp/internal/application/architecture"
	"operators-mcp/internal/application/capabilities"
	"operators-mcp/internal/application/projects"
	"operators-mcp/internal/application/settings"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/ports"
)

// Deps are the driven ports the contexts are built on. Any may be nil where a
// context tolerates it (repositories, bounded contexts, settings, publisher).
type Deps struct {
	Projects        ports.ProjectRepository
	Repositories    ports.RepositoryRepository
	Zones           ports.ZoneRepository
	BoundedContexts ports.BoundedContextRepository
	Agents          ports.AgentRepository
	Prompts         ports.PromptRepository
	Skills          ports.SkillRepository
	MCPServers      ports.MCPServerRepository
	Tools           ports.ToolRepository
	Settings        ports.SettingsRepository
	Publisher       ports.SkillPublisher
	PathMatcher     ports.PathMatcher
	TreeLister      ports.TreeLister
	// RepositoryFinder lets projects discover git checkouts; nil disables it.
	RepositoryFinder ports.RepositoryFinder
	// EnvFiles and EnvFileIO keep repositories' env files; nil disables them.
	EnvFiles    ports.EnvFileRepository
	EnvFileIO   ports.EnvFileIO
	DefaultRoot string
	// Bus carries the domain events; nil creates one.
	Bus *eventbus.Bus
}

// Catalog is the five contexts, wired.
type Catalog struct {
	Bus          *eventbus.Bus
	Projects     *projects.Service
	Architecture *architecture.Service
	Agents       *agents.Service
	Capabilities *capabilities.Service
	Settings     *settings.Service
}

// New builds the contexts, subscribes each to the events it reacts to, and
// binds the one late read port (agents ↔ capabilities read each other).
func New(d Deps) Catalog {
	bus := d.Bus
	if bus == nil {
		bus = eventbus.New()
	}

	st := settings.NewService(d.Settings, bus)
	caps := capabilities.NewService(capabilities.Deps{
		Skills:     d.Skills,
		MCPServers: d.MCPServers,
		Tools:      d.Tools,
		Settings:   st,
		Publisher:  d.Publisher,
		Events:     bus,
	})
	ag := agents.NewService(d.Agents, d.Prompts, caps, bus)
	caps.UseMCPServerUsage(ag)
	pr := projects.NewService(d.Projects, d.Repositories, bus)
	if d.RepositoryFinder != nil {
		pr.UseFinder(d.RepositoryFinder)
	}
	if d.EnvFiles != nil {
		pr.UseEnvFiles(d.EnvFiles, d.EnvFileIO)
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

	arch.Subscribe(bus)
	ag.Subscribe(bus)
	caps.Subscribe(bus)

	return Catalog{Bus: bus, Projects: pr, Architecture: arch, Agents: ag, Capabilities: caps, Settings: st}
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
