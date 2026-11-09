package orchestration

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
)

func servicesCfg(s *Service, agent *domain.Agent, project *domain.Project) llmkit.SessionConfig {
	var cfg llmkit.SessionConfig
	s.applyServicesServer(&cfg, "sess-9", agent, project)
	return cfg
}

// A session gets the services server when its agent or its project names it,
// with every tool allowed and the tools named in its brief; any other session
// gets nothing of it.
func TestApplyServicesServer_AttachesWhenTheAgentOrTheProjectNamesIt(t *testing.T) {
	s := &Service{ServicesServerURL: func(id string) string { return "http://127.0.0.1:8080/mcp/services/" + id }}
	withIt := &domain.Agent{MCPServerIDs: []string{"github", "services"}}
	without := &domain.Agent{MCPServerIDs: []string{"github"}}
	projectWith := &domain.Project{MCPServers: []string{"services"}}
	projectWithout := &domain.Project{MCPServers: []string{"github"}}

	for name, c := range map[string]struct {
		agent   *domain.Agent
		project *domain.Project
	}{
		"agent":             {withIt, projectWithout},
		"project":           {without, projectWith},
		"project, no agent": {nil, projectWith},
		"both":              {withIt, projectWith},
	} {
		cfg := servicesCfg(s, c.agent, c.project)
		if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "services" || cfg.MCPServers[0].Transport != "http" || cfg.MCPServers[0].URL != "http://127.0.0.1:8080/mcp/services/sess-9" {
			t.Fatalf("%s: servers = %+v", name, cfg.MCPServers)
		}
		for _, tool := range tooling.SessionServicesToolNames {
			if !slices.Contains(cfg.AllowedTools, "mcp__services__"+tool) {
				t.Fatalf("%s: mcp__services__%s is not allowed: %v", name, tool, cfg.AllowedTools)
			}
			if !strings.Contains(cfg.AppendSystem, "mcp__services__"+tool+" ") {
				t.Fatalf("%s: the brief does not name %s", name, tool)
			}
		}
	}
	for name, c := range map[string]struct {
		agent   *domain.Agent
		project *domain.Project
	}{
		"neither list":  {without, projectWithout},
		"no agent":      {nil, projectWithout},
		"nothing":       {nil, nil},
		"empty project": {&domain.Agent{}, &domain.Project{}},
	} {
		if cfg := servicesCfg(s, c.agent, c.project); len(cfg.MCPServers) != 0 || len(cfg.AllowedTools) != 0 || cfg.AppendSystem != "" {
			t.Fatalf("%s: got %+v", name, cfg)
		}
	}
	if cfg := servicesCfg(&Service{}, withIt, nil); len(cfg.MCPServers) != 0 {
		t.Fatalf("a host that serves no services endpoint attaches nothing: %+v", cfg)
	}
}

// The catalog's own servers skip the name quietly: the harness serves it.
func TestResolveAttachedMCPServers_LeavesTheBuiltInServicesServerToTheHarness(t *testing.T) {
	agent := &domain.Agent{MCPServerIDs: []string{"services", "m1"}}
	got := resolveAttachedMCPServers(agent, &domain.Project{MCPServers: []string{"services"}}, []*domain.MCPServer{{ID: "m1", Name: "github"}})
	if len(got) != 1 || got[0].ID != "m1" {
		t.Fatalf("got %+v", got)
	}
}
