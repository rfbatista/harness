package orchestration

import (
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// catalogWorld is a session service whose catalog has three MCP servers, an
// agent that names two of them and allows two tools, and a project that
// names two of them.
func catalogWorld(t *testing.T) (*Service, *fakeResolver) {
	t.Helper()
	svc, prov := newInteractiveService(t)
	_ = prov
	res := svc.catalog.Agents.(*fakeResolver)
	res.servers = []*domain.MCPServer{
		{ID: "github", Name: "github", Transport: "http", URL: "https://api/mcp", Headers: map[string]string{"Authorization": "Bearer ${S2_GITHUB_TOKEN}"}},
		{ID: "postgres", Name: "postgres", Transport: "stdio", Command: "uv", Args: []string{"run", "postgres-mcp"}},
		{ID: "linear", Name: "linear", Transport: "http", URL: "https://linear/mcp"},
	}
	res.agents = map[string]*domain.Agent{"a1": {
		ID: "a1", Name: "backend", MCPServerIDs: []string{"github", "postgres"}, AllowedTools: []string{"Read", "Bash"},
	}}
	res.proj.MCPServers = []string{"postgres", "linear"}
	return svc, res
}

// serversOf are the catalog servers a launched session has, in its order.
func serversOf(l launched) []string {
	var names []string
	for _, sv := range l.Spec.MCPServers {
		if slices.Contains([]string{"github", "postgres", "linear", "ghost"}, sv.Name) {
			names = append(names, sv.Name)
		}
	}
	return names
}

func TestSessionStart_GetsItsAgentsServersAndItsProjectsOnceEach(t *testing.T) {
	svc, _ := catalogWorld(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if got, want := serversOf(launch), []string{"github", "postgres", "linear"}; !slices.Equal(got, want) {
		t.Fatalf("servers = %v, want %v (postgres is the agent's and the project's, once)", got, want)
	}
	// Its details reach the spec as the catalog has them: ${VAR} unexpanded.
	for _, sv := range launch.Spec.MCPServers {
		if sv.Name == "github" && sv.Headers["Authorization"] != "Bearer ${S2_GITHUB_TOKEN}" {
			t.Fatalf("github headers = %v", sv.Headers)
		}
	}
}

func TestSessionStart_WithNoAgentGetsItsProjectsServersOnly(t *testing.T) {
	svc, res := catalogWorld(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1"})
	if got, want := serversOf(launch), []string{"postgres", "linear"}; !slices.Equal(got, want) {
		t.Fatalf("servers = %v, want %v", got, want)
	}
	res.proj.MCPServers = nil
	_, launch = startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1"})
	if got := serversOf(launch); len(got) != 0 {
		t.Fatalf("servers = %v, want none", got)
	}
}

func TestSessionStart_AnAgentWithNoProjectServersGetsItsOwn(t *testing.T) {
	svc, res := catalogWorld(t)
	res.proj.MCPServers = nil
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if got, want := serversOf(launch), []string{"github", "postgres"}; !slices.Equal(got, want) {
		t.Fatalf("servers = %v, want %v", got, want)
	}
}

func TestSessionStart_SkipsAServerTheCatalogNoLongerHasAndStarts(t *testing.T) {
	svc, res := catalogWorld(t)
	res.agents["a1"].MCPServerIDs = []string{"github", "ghost"}
	res.proj.MCPServers = []string{"ghost", "linear"}
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if got, want := serversOf(launch), []string{"github", "linear"}; !slices.Equal(got, want) {
		t.Fatalf("servers = %v, want %v", got, want)
	}
}

func TestSessionStart_PassesTheAgentsAllowedToolsToClaude(t *testing.T) {
	svc, res := catalogWorld(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	tools := allowedToolsOf(launch.Args)
	for _, want := range []string{"Read", "Bash", "mcp__task__get_task"} {
		if !slices.Contains(tools, want) {
			t.Errorf("--allowedTools = %v, lacks %s", tools, want)
		}
	}
	// An agent with none leaves claude's default: nothing of its own is added.
	res.agents["a1"].AllowedTools = nil
	_, launch = startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	tools = allowedToolsOf(launch.Args)
	if slices.Contains(tools, "Read") || slices.Contains(tools, "Bash") {
		t.Errorf("--allowedTools = %v, want no built-in tools for an agent without any", tools)
	}
}

// claude expands ${VAR} in its MCP config from its own environment. The
// harness server's environment reaches the process, so the variables a
// session's servers name are there with their values, and nothing is passed
// by name.
func TestSessionStart_TheVariablesItsServersNameReachTheClaudeProcess(t *testing.T) {
	t.Setenv("S2_GITHUB_TOKEN", "s2-secret-value")
	svc, _ := catalogWorld(t)
	svc.catalog.Agents.(*fakeResolver).proj.MCPServers = nil
	_, spec, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	l := launchOf(t, spec)
	if !slices.Contains(l.Env, "S2_GITHUB_TOKEN=s2-secret-value") {
		t.Fatalf("the claude process env lacks the variable the github server names")
	}
	// The value appears nowhere in the command line or the MCP config.
	for _, a := range l.Args {
		if strings.Contains(a, "s2-secret-value") {
			t.Fatalf("the value is in the arguments: %q", a)
		}
	}
}
