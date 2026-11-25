package orchestration

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func servicesCfg(s *Service, agent *domain.Agent, project *domain.Project) llmkit.SessionConfig {
	var cfg llmkit.SessionConfig
	s.applyServicesServer(&cfg, "sess-9", domain.SessionModeDefault, agent, project)
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

const testServicesBase = "http://127.0.0.1:8080/mcp/services/"

func withServicesURL(svc *Service) {
	svc.ServicesServerURL = func(id string) string { return testServicesBase + id }
}

// servicesOf reads what a launched session got of the services server: its
// entry in --mcp-config, its allowed tools and its system prompt.
func servicesOf(t *testing.T, launch launched) (servers int, tools int, brief bool) {
	t.Helper()
	if mcp, ok := flag(launch.Args, "--mcp-config"); ok {
		var cfg struct {
			MCPServers map[string]struct{ Type, URL string } `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(mcp), &cfg); err != nil {
			t.Fatalf("--mcp-config is not JSON: %v", err)
		}
		if _, ok := cfg.MCPServers["services"]; ok {
			servers = 1
		}
	}
	allowed, _ := flag(launch.Args, "--allowedTools")
	for _, tool := range tooling.SessionServicesToolNames {
		if slices.Contains(strings.Split(allowed, ","), "mcp__services__"+tool) {
			tools++
		}
	}
	system, _ := flag(launch.Args, "--append-system-prompt")
	return servers, tools, strings.Contains(system, "## Services")
}

func requireServices(t *testing.T, name string, launch launched, want bool) {
	t.Helper()
	servers, tools, brief := servicesOf(t, launch)
	if want && (servers != 1 || tools != len(tooling.SessionServicesToolNames) || !brief) {
		t.Errorf("%s: servers=%d tools=%d brief=%v, want the services server, every tool and the brief", name, servers, tools, brief)
	}
	if !want && (servers != 0 || tools != 0 || brief) {
		t.Errorf("%s: servers=%d tools=%d brief=%v, want nothing of the services server", name, servers, tools, brief)
	}
}

// An operate or qa session gets the services server whatever its agent and
// project list, a peer started from the form as well as any other start; a
// project's bans on other servers do not touch it.
func TestStartInteractive_OperateAndQAAlwaysGetTheServicesServer(t *testing.T) {
	svc, _ := newInteractiveService(t)
	withServicesURL(svc)
	for _, mode := range []string{"operate", "qa"} {
		_, launch := startInteractive(t, svc, InteractiveRequest{Mode: mode})
		requireServices(t, mode, launch, true)
	}
	project, err := svc.catalog.Projects.GetProject(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	project.MCPBanned = []string{"github"}
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "operate"})
	requireServices(t, "operate, project bans github", launch, true)
}

// Other modes keep ADR 0075's rule: no listing, no server.
func TestStartInteractive_OtherModesGetNoServicesServerUnlessListed(t *testing.T) {
	svc, _ := newInteractiveService(t)
	withServicesURL(svc)
	modes := []string{"", "architect", "implement", "review", "test", "product"}
	for _, mode := range modes {
		_, launch := startInteractive(t, svc, InteractiveRequest{Mode: mode})
		requireServices(t, "mode "+mode, launch, false)
	}
	project, err := svc.catalog.Projects.GetProject(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	project.MCPServers = []string{"services"}
	for _, mode := range modes {
		_, launch := startInteractive(t, svc, InteractiveRequest{Mode: mode})
		requireServices(t, "listed, mode "+mode, launch, true)
	}
}

// Listing the server as well as being in operate mode attaches it once.
func TestApplyServicesServer_AnOperateSessionWhoseAgentListsItGetsItOnce(t *testing.T) {
	s := &Service{ServicesServerURL: func(id string) string { return testServicesBase + id }}
	var cfg llmkit.SessionConfig
	s.applyServicesServer(&cfg, "sess-9", domain.SessionModeOperate, &domain.Agent{MCPServerIDs: []string{"services"}}, nil)
	if len(cfg.MCPServers) != 1 || strings.Count(cfg.AppendSystem, "## Services") != 1 {
		t.Fatalf("got %+v", cfg)
	}
}

// With no services endpoint configured, operate and qa start as they do today.
func TestStartInteractive_NoServicesEndpointMeansNoServerEvenInOperate(t *testing.T) {
	svc, _ := newInteractiveService(t)
	for _, mode := range []string{"operate", "qa"} {
		_, launch := startInteractive(t, svc, InteractiveRequest{Mode: mode})
		requireServices(t, mode, launch, false)
	}
}

// A resume follows the rule of a new start.
func TestResumeInteractive_OperateAndQAKeepTheServicesServer(t *testing.T) {
	svc, _ := newInteractiveService(t)
	withServicesURL(svc)
	for _, mode := range []string{"operate", "qa"} {
		sess, _ := startInteractive(t, svc, InteractiveRequest{Mode: mode})
		if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
			t.Fatal(err)
		}
		_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
		if err != nil {
			t.Fatal(err)
		}
		requireServices(t, "resumed "+mode, launchOf(t, spec), true)
	}
}
