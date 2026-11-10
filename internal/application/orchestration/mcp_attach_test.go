package orchestration

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
)

func TestResolveAttachedMCPServers_AttachNoneWhenEmpty(t *testing.T) {
	agent := &domain.Agent{ID: "a1", MCPServerIDs: nil}
	all := []*domain.MCPServer{
		{ID: "m1", Name: "github"},
		{ID: "m2", Name: "linear"},
	}
	got := resolveAttachedMCPServers(agent, nil, all)
	if len(got) != 0 {
		t.Fatalf("want no MCPs when agent has empty ids, got %d", len(got))
	}
}

func TestResolveAttachedMCPServers_FiltersByID(t *testing.T) {
	agent := &domain.Agent{ID: "a1", MCPServerIDs: []string{"m2"}}
	all := []*domain.MCPServer{
		{ID: "m1", Name: "github"},
		{ID: "m2", Name: "linear"},
	}
	got := resolveAttachedMCPServers(agent, nil, all)
	if len(got) != 1 || got[0].ID != "m2" {
		t.Fatalf("want only m2, got %+v", got)
	}
}

func TestResolveAttachedMCPServers_NilAgent(t *testing.T) {
	got := resolveAttachedMCPServers(nil, nil, []*domain.MCPServer{{ID: "m1"}})
	if len(got) != 0 {
		t.Fatalf("want nil for nil agent, got %d", len(got))
	}
}

func serversNamed(names ...string) []*domain.MCPServer {
	var out []*domain.MCPServer
	for _, n := range names {
		out = append(out, &domain.MCPServer{ID: n, Name: n})
	}
	return out
}

func namesOf(servers []*domain.MCPServer) []string {
	out := []string{}
	for _, s := range servers {
		out = append(out, s.Name)
	}
	return out
}

func TestResolveMCP_ABannedServerIsNotAttachedWhateverTheAgentOrProjectListsIt(t *testing.T) {
	agent := &domain.Agent{MCPServerIDs: []string{"github", "linear"}}
	project := &domain.Project{MCPServers: []string{"github", "notion"}, MCPBanned: []string{"github", "ghost"}}
	got := resolveMCP(agent, project, serversNamed("github", "linear", "notion"), nil)
	if want := []string{"linear", "notion"}; !reflect.DeepEqual(namesOf(got.Servers), want) {
		t.Fatalf("servers = %v, want %v", namesOf(got.Servers), want)
	}
	// With no agent the ban still holds, and a server in no list stays out.
	got = resolveMCP(nil, project, serversNamed("github", "linear", "notion"), nil)
	if want := []string{"notion"}; !reflect.DeepEqual(namesOf(got.Servers), want) {
		t.Fatalf("servers = %v, want %v", namesOf(got.Servers), want)
	}
}

func TestResolveMCP_DeniesTheToolsWhoseEffectiveSettingIsDisabled(t *testing.T) {
	agent := &domain.Agent{MCPServerIDs: []string{"github"}}
	defaults := map[string][]string{"github": {"a", "b"}}
	cases := []struct {
		name      string
		overrides map[string]map[string]string
		want      []string
	}{
		{"no override follows the global default", nil, []string{"mcp__github__a", "mcp__github__b"}},
		{"enabled beats a global disable", map[string]map[string]string{"github": {"a": domain.ToolEnabled}}, []string{"mcp__github__b"}},
		{"disabled beats a global enable", map[string]map[string]string{"github": {"c": domain.ToolDisabled}}, []string{"mcp__github__a", "mcp__github__b", "mcp__github__c"}},
		{"every tool enabled", map[string]map[string]string{"github": {"a": domain.ToolEnabled, "b": domain.ToolEnabled}}, []string{}},
		{"another server's override is not this one's", map[string]map[string]string{"linear": {"x": domain.ToolDisabled}}, []string{"mcp__github__a", "mcp__github__b"}},
	}
	for _, c := range cases {
		project := &domain.Project{MCPToolOverrides: c.overrides}
		got := resolveMCP(agent, project, serversNamed("github", "linear"), defaults)
		if !reflect.DeepEqual(append([]string{}, got.Denied...), c.want) {
			t.Errorf("%s: denied = %v, want %v", c.name, got.Denied, c.want)
		}
	}
}

func TestResolveMCP_AServerWithEveryToolDisabledStaysAttached(t *testing.T) {
	agent := &domain.Agent{MCPServerIDs: []string{"github"}}
	got := resolveMCP(agent, &domain.Project{}, serversNamed("github"), map[string][]string{"github": {"a", "b"}})
	if !reflect.DeepEqual(namesOf(got.Servers), []string{"github"}) || len(got.Denied) != 2 {
		t.Fatalf("resolution = %+v", got)
	}
}

func TestResolveMCP_OnlyAttachedServersAreTrimmedAndTheBuiltInOnesAreNeverNamed(t *testing.T) {
	project := &domain.Project{
		MCPBanned:        []string{"task", "browser", "codegraph"},
		MCPToolOverrides: map[string]map[string]string{"task": {"x": domain.ToolDisabled}, "ghost": {"y": domain.ToolDisabled}, "linear": {"z": domain.ToolDisabled}},
	}
	got := resolveMCP(&domain.Agent{MCPServerIDs: []string{"ghost"}}, project, serversNamed("github", "linear"), nil)
	if len(got.Servers) != 0 || len(got.Denied) != 0 {
		t.Fatalf("a server nobody lists, or one the catalog lacks, was trimmed: %+v", got)
	}
}

func TestResolveMCP_TheBuiltInServicesServerIsNeverBannedOrTrimmed(t *testing.T) {
	agent := &domain.Agent{MCPServerIDs: []string{servicesServerName}}
	project := &domain.Project{
		MCPBanned:        []string{servicesServerName},
		MCPToolOverrides: map[string]map[string]string{servicesServerName: {"stop_service": domain.ToolDisabled}},
	}
	got := resolveMCP(agent, project, nil, map[string][]string{servicesServerName: {"start_service"}})
	if len(got.Servers) != 0 || len(got.Denied) != 0 {
		t.Fatalf("resolution = %+v", got)
	}
	if !namesServicesServer(agent, project) {
		t.Fatal("a ban took the services server from the session")
	}
}

func TestWithDeniedMCPTools_AddsThemBesideWhatTheSettingsAlreadyHold(t *testing.T) {
	cfg := llmkit.SessionConfig{Driver: claude.Config{Settings: &claude.Settings{Extra: map[string]json.RawMessage{
		"sandbox":     json.RawMessage(`{"enabled":false}`),
		"permissions": json.RawMessage(`{"deny":["Bash(rm:*)"],"allow":["Read"]}`),
	}}}}
	withDeniedMCPTools(&cfg, []string{"mcp__github__a"})
	extra := cfg.Driver.(claude.Config).Settings.Extra
	if string(extra["sandbox"]) != `{"enabled":false}` {
		t.Fatalf("sandbox = %s", extra["sandbox"])
	}
	var perms struct {
		Deny  []string `json:"deny"`
		Allow []string `json:"allow"`
	}
	if err := json.Unmarshal(extra["permissions"], &perms); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(perms.Deny, []string{"Bash(rm:*)", "mcp__github__a"}) || !reflect.DeepEqual(perms.Allow, []string{"Read"}) {
		t.Fatalf("permissions = %+v", perms)
	}
	// Nothing denied leaves the config as it was.
	plain := llmkit.SessionConfig{}
	withDeniedMCPTools(&plain, nil)
	if plain.Driver != nil {
		t.Fatalf("driver = %v", plain.Driver)
	}
}
