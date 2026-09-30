// Package capabilities is the capabilities bounded context: what an agent can
// be equipped with — skills, MCP servers and user-defined tools.
//
// The skill and MCP server use cases live in harnesskit; this context supplies
// what the library deliberately does not know: where the publish root is set
// (the settings context), and that agents reference skills and servers (the
// agents context, reached through events and the MCPServerUsage read port).
package capabilities

import (
	"context"
	"sync/atomic"

	"github.com/rfbatista/harnesskit/mcpprobe"
	"github.com/rfbatista/harnesskit/mcpserver"
	"github.com/rfbatista/harnesskit/skill"
	"github.com/rfbatista/harnesskit/tool"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.Capabilities     = (*Service)(nil)
	_ ports.CapabilityReader = (*Service)(nil)
)

// Service implements the capabilities use cases.
type Service struct {
	// skills and mcp are built once in NewService and never rebuilt: two
	// instances would diverge.
	skills *skill.Service
	mcp    *mcpserver.Service
	tools  ports.ToolRepository

	settings ports.SettingsReader // nil: publishing is not configured
	events   ports.EventPublisher // nil: deletes are not announced
	usage    atomic.Pointer[ports.MCPServerUsage]
}

// Deps are the Service's collaborators. Settings and Publisher are optional:
// without both, publishing is unavailable and skill mutations never touch the
// filesystem.
type Deps struct {
	Skills     ports.SkillRepository
	MCPServers ports.MCPServerRepository
	Tools      ports.ToolRepository
	Settings   ports.SettingsReader
	Publisher  ports.SkillPublisher
	Events     ports.EventPublisher
}

// NewService returns the capabilities context.
func NewService(d Deps) *Service {
	s := &Service{tools: d.Tools, settings: d.Settings, events: d.Events}
	s.skills = skill.NewService(d.Skills).WithHooks(skill.Hooks{
		BeforeDeleteRow: func(sk *domain.Skill) error {
			return s.publish(domain.SkillDeleted{SkillID: sk.ID})
		},
	})
	if d.Publisher != nil {
		s.skills.WithPublishing(s.publishRoot, d.Publisher)
	}
	s.mcp = mcpserver.NewService(d.MCPServers).
		WithProber(mcpprobe.NewProber()).
		WithHooks(mcpserver.Hooks{
			Decorate: s.decorateUsage,
			BeforeDelete: func(m *domain.MCPServer) error {
				return s.publish(domain.MCPServerDeleted{ServerID: m.ID})
			},
		})
	return s
}

// UseMCPServerUsage connects the usage read model. The agents context
// implements it, and agents also read capabilities, so the two cannot be built
// in one order; the host binds this once both exist. Until then servers are
// returned without usage.
func (s *Service) UseMCPServerUsage(u ports.MCPServerUsage) { s.usage.Store(&u) }

// Subscribe registers the context's reactions to other contexts' events.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	// A new publish root republishes every published skill under it and
	// removes their folders from the old one.
	ports.On(sub, func(_ context.Context, ev domain.SettingsChanged) error {
		if ev.Key != domain.SettingSkillsPublishRoot {
			return nil
		}
		oldRoot, newRoot := domain.ExpandUserPath(ev.Old), domain.ExpandUserPath(ev.New)
		if oldRoot != newRoot {
			s.skills.MigratePublishRoot(oldRoot, newRoot)
		}
		return nil
	})
}

// SkillService returns the skill use cases, for callers that build tool groups
// directly on top of them.
func (s *Service) SkillService() *skill.Service { return s.skills }

// MCPServerService returns the MCP server use cases.
func (s *Service) MCPServerService() *mcpserver.Service { return s.mcp }

// ToolStore returns the store backing user-defined tools.
func (s *Service) ToolStore() tool.Store { return s.tools }

// publish announces a delete. The hooks that call it have no context of their
// own: harnesskit runs them inside its delete, which a subscriber error cancels.
func (s *Service) publish(ev domain.Event) error {
	if s.events == nil {
		return nil
	}
	return s.events.Publish(context.Background(), ev)
}

// publishRoot returns the configured publish root with "~" expanded, or "" when
// publishing is not configured. It backs skill.Service's root function, so it
// is read on every operation rather than captured once.
func (s *Service) publishRoot() string {
	if s.settings == nil {
		return ""
	}
	return domain.ExpandUserPath(s.settings.Setting(domain.SettingSkillsPublishRoot))
}

// decorateUsage fills the denormalized usage read model on a server being
// returned: how many agents, and which, reference it.
func (s *Service) decorateUsage(m *domain.MCPServer) {
	u := s.usage.Load()
	if m == nil || u == nil {
		return
	}
	names := (*u).AgentsUsingMCPServer(m.ID)
	m.AgentCount += len(names)
	m.AgentNames = append(m.AgentNames, names...)
}
