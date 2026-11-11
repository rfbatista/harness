package httpapi

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/mcpserver"
)

func (f *catalogFixture) project(name string) string {
	f.t.Helper()
	p, err := f.projects.CreateProject(context.Background(), name, f.t.TempDir())
	if err != nil {
		f.t.Fatal(err)
	}
	return p.ID
}

func stdioServer(name string) map[string]any {
	return map[string]any{"name": name, "transport": "stdio", "command": "npx", "args": []string{"-y", "pkg"}, "env": map[string]string{"TOKEN": "${NB_TOKEN}"}}
}

func TestCatalogMCPServers_CreateWritesTheClaudeShapeAndSetsWhoGetsIt(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	out := f.post("/api/save_catalog_mcp_server", map[string]any{
		"base_versions": f.versions(),
		"server":        stdioServer("github"),
		"agents":        []string{"zeta"},
		"projects":      []string{p},
	}, 200)

	want := `{
  "mcpServers": {
    "nb": {
      "command": "npx"
    },
    "github": {
      "command": "npx",
      "args": [
        "-y",
        "pkg"
      ],
      "env": {
        "TOKEN": "${NB_TOKEN}"
      }
    }
  }
}
`
	if got := f.read("mcp-servers.json"); got != want {
		t.Fatalf("mcp-servers.json =\n%s\nwant\n%s", got, want)
	}
	cat := f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "zeta")["mcp_servers"]); !reflect.DeepEqual(got, []string{"github"}) {
		t.Fatalf("zeta servers = %v", got)
	}
	gh := byName(t, cat["mcp_servers"], "github")
	projs := list(gh["projects"])
	if len(projs) != 1 || projs[0].(map[string]any)["id"] != p || projs[0].(map[string]any)["name"] != "Widgets" {
		t.Fatalf("projects = %v", gh["projects"])
	}
	got := f.get("/api/get_project_mcp_servers?project_id="+p, 200)
	if !reflect.DeepEqual(strList(got["mcp_servers"]), []string{"github"}) {
		t.Fatalf("project servers = %v", got)
	}
	ch := out["change"].(map[string]any)
	if pc := list(ch["projects"]); len(pc) != 1 || !reflect.DeepEqual(strList(pc[0].(map[string]any)["after"]), []string{"github"}) {
		t.Fatalf("change.projects = %v", ch["projects"])
	}
	if !strings.Contains(ch["summary"].(string), "github is on 1 agents and 1 projects") {
		t.Fatalf("summary = %v", ch["summary"])
	}
}

func TestCatalogMCPServers_HTTPServerKeepsTypeUrlAndHeaders(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": map[string]any{
		"name": "linear", "transport": "http", "url": "https://x/mcp", "headers": map[string]string{"Authorization": "Bearer ${LINEAR_TOKEN}"},
	}}, 200)
	if got := f.read("mcp-servers.json"); !strings.Contains(got, `"type": "http"`) || !strings.Contains(got, `"Authorization": "Bearer ${LINEAR_TOKEN}"`) {
		t.Fatalf("mcp-servers.json = %s", got)
	}
	lin := byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "linear")
	if lin["transport"] != "http" || lin["url"] != "https://x/mcp" {
		t.Fatalf("linear = %v", lin)
	}
}

func TestCatalogMCPServers_RenameAndDeleteRewriteAgentsAndProjects(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("github"), "agents": []string{"zeta"}, "projects": []string{p}}, 200)

	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("gh"), "agents": []string{"zeta"}, "projects": []string{p}}, 200)
	cat := f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "zeta")["mcp_servers"]); !reflect.DeepEqual(got, []string{"gh"}) {
		t.Fatalf("zeta servers = %v", got)
	}
	if got := f.get("/api/get_project_mcp_servers?project_id="+p, 200); !reflect.DeepEqual(strList(got["mcp_servers"]), []string{"gh"}) {
		t.Fatalf("project servers = %v", got)
	}

	out := f.post("/api/delete_catalog_mcp_server", map[string]any{"name": "gh"}, 200)
	cat = f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "zeta")["mcp_servers"]); len(got) != 0 {
		t.Fatalf("zeta servers after delete = %v", got)
	}
	if got := f.get("/api/get_project_mcp_servers?project_id="+p, 200); len(strList(got["mcp_servers"])) != 0 {
		t.Fatalf("project servers after delete = %v", got)
	}
	if strings.Contains(f.read("mcp-servers.json"), `"gh"`) {
		t.Fatal("the server is still in the file")
	}

	// Undo brings the server, the agent's name for it and the project's back.
	f.post("/api/undo_catalog_change", map[string]any{"change_id": out["change"].(map[string]any)["id"]}, 200)
	cat = f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "zeta")["mcp_servers"]); !reflect.DeepEqual(got, []string{"gh"}) {
		t.Fatalf("zeta servers after undo = %v", got)
	}
	if got := f.get("/api/get_project_mcp_servers?project_id="+p, 200); !reflect.DeepEqual(strList(got["mcp_servers"]), []string{"gh"}) {
		t.Fatalf("project servers after undo = %v", got)
	}
	if byName(t, cat["mcp_servers"], "gh")["command"] != "npx" {
		t.Fatal("the server is not back")
	}
}

func TestCatalogMCPServers_AWriteThatTakesAnAgentOrProjectAwayNamesThemInTheList(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("github"), "agents": []string{"zeta"}, "projects": []string{p}}, 200)
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("github"), "agents": []string{}, "projects": []string{}}, 200)
	cat := f.get("/api/get_agent_catalog", 200)
	gh := byName(t, cat["mcp_servers"], "github")
	if len(list(gh["agents"])) != 0 || len(list(gh["projects"])) != 0 {
		t.Fatalf("github = %v", gh)
	}
	// Left out entirely, who gets it stays.
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("github"), "agents": []string{"zeta"}}, 200)
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("github")}, 200)
	if got := strList(byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "github")["agents"]); !reflect.DeepEqual(got, []string{"zeta"}) {
		t.Fatalf("agents = %v", got)
	}
}

func TestCatalogMCPServers_RefuseBadInput(t *testing.T) {
	f := catalogWithSkills(t)
	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
		field  string
	}{
		{"bad name", map[string]any{"server": map[string]any{"name": "bad name", "command": "x"}}, 400, "INVALID_INPUT", "name"},
		{"stdio without command", map[string]any{"server": map[string]any{"name": "a", "transport": "stdio"}}, 400, "INVALID_INPUT", "command"},
		{"http without url", map[string]any{"server": map[string]any{"name": "a", "transport": "http"}}, 400, "INVALID_INPUT", "url"},
		{"unknown transport", map[string]any{"server": map[string]any{"name": "a", "transport": "pigeon", "command": "x"}}, 400, "INVALID_INPUT", "transport"},
		{"name taken", map[string]any{"server": map[string]any{"name": "nb", "command": "x"}}, 409, "NAME_TAKEN", "name"},
		{"unknown previous", map[string]any{"previous_name": "ghost", "server": map[string]any{"name": "a", "command": "x"}}, 404, "MCP_SERVER_NOT_FOUND", ""},
		{"unknown agent", map[string]any{"server": map[string]any{"name": "a", "command": "x"}, "agents": []string{"ghost"}}, 400, "UNKNOWN_REFERENCE", ""},
		{"unknown project", map[string]any{"server": map[string]any{"name": "a", "command": "x"}, "projects": []string{"ghost"}}, 400, "UNKNOWN_REFERENCE", ""},
	}
	before := f.read("mcp-servers.json")
	for _, c := range cases {
		got := f.post("/api/save_catalog_mcp_server", c.body, c.status)
		if got["code"] != c.code || (c.field != "" && got["field"] != c.field) {
			t.Errorf("%s: body = %v", c.name, got)
		}
	}
	if f.read("mcp-servers.json") != before {
		t.Fatal("a refused save wrote the file")
	}
	f.post("/api/delete_catalog_mcp_server", map[string]any{"name": "ghost"}, 404)
}

func TestCatalogProjectMCPServers_RoundTripAndRefuseNamesNotInTheCatalog(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	got := f.post("/api/set_project_mcp_servers", map[string]any{"project_id": p, "mcp_servers": []string{"nb", "nb"}}, 200)
	if !reflect.DeepEqual(strList(got["mcp_servers"]), []string{"nb"}) {
		t.Fatalf("answer = %v", got)
	}
	if got := f.get("/api/get_project_mcp_servers?project_id="+p, 200); !reflect.DeepEqual(strList(got["mcp_servers"]), []string{"nb"}) {
		t.Fatalf("read = %v", got)
	}
	bad := f.post("/api/set_project_mcp_servers", map[string]any{"project_id": p, "mcp_servers": []string{"ghost"}}, 400)
	if bad["code"] != "UNKNOWN_REFERENCE" || !reflect.DeepEqual(strList(bad["names"]), []string{"ghost"}) {
		t.Fatalf("body = %v", bad)
	}
	if got := f.get("/api/get_project_mcp_servers?project_id="+p, 200); !reflect.DeepEqual(strList(got["mcp_servers"]), []string{"nb"}) {
		t.Fatalf("a refused set changed the list: %v", got)
	}
	f.post("/api/set_project_mcp_servers", map[string]any{"project_id": "nope", "mcp_servers": []string{}}, 404)
	f.get("/api/get_project_mcp_servers?project_id=nope", 404)
}

func TestCatalogMCPServers_TestExpandsVariablesForTheProbeOnly(t *testing.T) {
	f := catalogWithSkills(t)
	f.env["NB_TOKEN"] = "tok-123456-secret"
	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a", "b"}}
	out := f.post("/api/test_catalog_mcp_server", map[string]any{"server": stdioServer("")}, 200)
	probe := out["probe"].(map[string]any)
	if probe["status"] != "ok" || len(list(probe["tools"])) != 2 || list(probe["tools"])[0].(map[string]any)["name"] != "a" {
		t.Fatalf("probe = %v", probe)
	}
	if f.prober.got.Env["TOKEN"] != "tok-123456-secret" {
		t.Fatalf("the probe was not given the value: %v", f.prober.got.Env)
	}
	if f.read("mcp-servers.json") != `{"mcpServers":{"nb":{"command":"npx"}}}` {
		t.Fatal("a test stored something")
	}

	// A failure that echoes the value comes back with the value hidden.
	f.prober.result = mcpserver.ProbeResult{Status: "error", Error: "initialize: bad token tok-123456-secret at https://x"}
	out = f.post("/api/test_catalog_mcp_server", map[string]any{"server": stdioServer("")}, 200)
	probe = out["probe"].(map[string]any)
	if probe["status"] != "error" || strings.Contains(probe["error"].(string), "tok-123456-secret") || !strings.Contains(probe["error"].(string), "${NB_TOKEN}") {
		t.Fatalf("probe = %v", probe)
	}
}

func TestCatalogMCPServers_TestHintsAtWhyItFailed(t *testing.T) {
	f := catalogWithSkills(t)
	f.prober.result = mcpserver.ProbeResult{Status: "error", Error: "initialize: boom"}
	out := f.post("/api/test_catalog_mcp_server", map[string]any{"server": stdioServer("")}, 200)
	if hint := out["probe"].(map[string]any)["hint"]; !strings.Contains(hint.(string), "NB_TOKEN is not set") {
		t.Fatalf("hint = %v", hint)
	}
	f.env["NB_TOKEN"] = "tok-654321"
	f.prober.result = mcpserver.ProbeResult{Status: "error", Error: `fork/exec npx: executable file not found in $PATH`}
	out = f.post("/api/test_catalog_mcp_server", map[string]any{"server": stdioServer("")}, 200)
	if hint := out["probe"].(map[string]any)["hint"]; !strings.Contains(hint.(string), "npx is installed") {
		t.Fatalf("hint = %v", hint)
	}

	f.prober.err = errors.New("invalid")
	f.prober.result = mcpserver.ProbeResult{}
	f.post("/api/test_catalog_mcp_server", map[string]any{"server": map[string]any{"name": "x", "transport": "stdio"}}, 400)
}

func TestCatalogMCPServers_SaveProbesAndKeepsTheKnownToolsApartFromTheServerFile(t *testing.T) {
	f := catalogWithSkills(t)
	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"create_issue", "search_code"}}
	out := f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("github"), "disabled_tools": []string{"create_issue"}}, 200)
	if probe := out["probe"].(map[string]any); probe["status"] != "ok" {
		t.Fatalf("probe = %v", probe)
	}
	gh := byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "github")
	if got := strList(gh["known_tools"]); !reflect.DeepEqual(got, []string{"create_issue", "search_code"}) {
		t.Fatalf("known_tools = %v", gh["known_tools"])
	}
	if s, _ := gh["probed_at"].(string); s == "" {
		t.Fatalf("probed_at = %v", gh["probed_at"])
	}
	if got := strList(gh["disabled_tools"]); !reflect.DeepEqual(got, []string{"create_issue"}) {
		t.Fatalf("disabled_tools = %v", gh["disabled_tools"])
	}
	if file := f.read("mcp-servers.json"); strings.Contains(file, "create_issue") || strings.Contains(file, "known_tools") {
		t.Fatalf("the tool data leaked into mcp-servers.json: %s", file)
	}

	// Absent keeps the default, [] clears it.
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("github")}, 200)
	gh = byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "github")
	if got := strList(gh["disabled_tools"]); !reflect.DeepEqual(got, []string{"create_issue"}) {
		t.Fatalf("an absent list changed the default: %v", got)
	}
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("github"), "disabled_tools": []string{}}, 200)
	gh = byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "github")
	if got := strList(gh["disabled_tools"]); len(got) != 0 {
		t.Fatalf("an empty list did not clear the default: %v", got)
	}
}

func TestCatalogMCPServers_AFailedProbeOnSaveKeepsTheServerAndItsLastKnownTools(t *testing.T) {
	f := catalogWithSkills(t)
	f.env["NB_TOKEN"] = "tok-123456-secret"
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("github")}, 200)
	gh := byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "github")
	if gh["known_tools"] != nil || gh["probed_at"] != nil || len(strList(gh["disabled_tools"])) != 0 {
		t.Fatalf("a never probed server = %v", gh)
	}

	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a"}}
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("github")}, 200)
	before := byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "github")

	f.prober.result = mcpserver.ProbeResult{Status: "error", Error: "initialize: bad token tok-123456-secret"}
	out := f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("github")}, 200)
	probe := out["probe"].(map[string]any)
	if probe["status"] != "error" || strings.Contains(probe["error"].(string), "tok-123456-secret") {
		t.Fatalf("probe = %v", probe)
	}
	after := byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "github")
	if !reflect.DeepEqual(after["known_tools"], before["known_tools"]) || after["probed_at"] != before["probed_at"] {
		t.Fatalf("a failed probe changed the tools: before %v after %v", before, after)
	}
}

func TestCatalogMCPServers_RefreshProbesASavedServerAndWritesOnlyTheToolData(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("github")}, 200)
	file := f.read("mcp-servers.json")

	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a", "b"}}
	out := f.post("/api/refresh_catalog_mcp_server_tools", map[string]any{"name": "github"}, 200)
	if out["probe"].(map[string]any)["status"] != "ok" {
		t.Fatalf("answer = %v", out)
	}
	srv := out["server"].(map[string]any)
	if srv["name"] != "github" || !reflect.DeepEqual(strList(srv["known_tools"]), []string{"a", "b"}) {
		t.Fatalf("server = %v", srv)
	}
	if f.read("mcp-servers.json") != file {
		t.Fatal("a refresh rewrote mcp-servers.json")
	}

	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{}}
	out = f.post("/api/refresh_catalog_mcp_server_tools", map[string]any{"name": "github"}, 200)
	if got := out["server"].(map[string]any)["known_tools"]; got == nil || len(list(got)) != 0 {
		t.Fatalf("a server probed with no tools = %v", got)
	}

	missing := f.post("/api/refresh_catalog_mcp_server_tools", map[string]any{"name": "ghost"}, 404)
	if missing["code"] != "UNKNOWN_REFERENCE" || !reflect.DeepEqual(strList(missing["names"]), []string{"ghost"}) {
		t.Fatalf("body = %v", missing)
	}
}

func TestCatalogMCPServers_RenameCarriesTheToolDataAndDeleteRemovesIt(t *testing.T) {
	f := catalogWithSkills(t)
	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a", "b"}}
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("github"), "disabled_tools": []string{"a", "gone"}}, 200)

	f.prober.result = mcpserver.ProbeResult{Status: "error", Error: "down"}
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("gh")}, 200)
	gh := byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "gh")
	if !reflect.DeepEqual(strList(gh["known_tools"]), []string{"a", "b"}) || !reflect.DeepEqual(strList(gh["disabled_tools"]), []string{"a", "gone"}) {
		t.Fatalf("renamed server = %v", gh)
	}

	f.post("/api/delete_catalog_mcp_server", map[string]any{"name": "gh"}, 200)
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("gh")}, 200)
	gh = byName(t, f.get("/api/get_agent_catalog", 200)["mcp_servers"], "gh")
	if gh["known_tools"] != nil || len(strList(gh["disabled_tools"])) != 0 {
		t.Fatalf("a deleted server's tool data came back: %v", gh)
	}
}

func TestCatalogMCPServers_AnUnparseableToolDataFileIsCatalogBroken(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("mcp-tools.json", "{not json")
	cat := f.get("/api/get_agent_catalog", 200)
	if b, _ := cat["broken"].(map[string]any); b == nil || b["path"] != "mcp-tools.json" {
		t.Fatalf("broken = %v", cat["broken"])
	}
	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a"}}
	got := f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("github")}, 409)
	if got["code"] != "CATALOG_BROKEN" {
		t.Fatalf("body = %v", got)
	}
}

// projectServer is one entry of a project's servers read.
func projectServer(t *testing.T, got map[string]any, name string) map[string]any {
	t.Helper()
	return byName(t, got["servers"], name)
}

func toolOf(t *testing.T, srv map[string]any, name string) map[string]any {
	t.Helper()
	return byName(t, srv["tools"], name)
}

// githubWithTools saves a github server whose probe found a and b, a disabled
// by default, used by the agent zeta.
func githubWithTools(f *catalogFixture, projectIDs ...string) {
	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a", "b"}}
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("github"), "agents": []string{"zeta"}, "disabled_tools": []string{"a"}}, 200)
}

func TestProjectMCPSettings_EveryServerReadsAgentsChoiceUntilTheProjectSetsIt(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	githubWithTools(f)
	got := f.get("/api/get_project_mcp_servers?project_id="+p, 200)
	if len(strList(got["mcp_servers"])) != 0 {
		t.Fatalf("mcp_servers = %v", got["mcp_servers"])
	}
	gh := projectServer(t, got, "github")
	if gh["state"] != "agent" || !reflect.DeepEqual(strList(gh["used_by_agents"]), []string{"zeta"}) {
		t.Fatalf("github = %v", gh)
	}
	a, b := toolOf(t, gh, "a"), toolOf(t, gh, "b")
	if a["default"] != "disabled" || a["override"] != "default" || a["effective"] != "disabled" || b["effective"] != "enabled" {
		t.Fatalf("tools = %v %v", a, b)
	}
	// nb was never probed: its tools are unknown.
	if nb := projectServer(t, got, "nb"); nb["tools"] != nil || nb["state"] != "agent" {
		t.Fatalf("nb = %v", nb)
	}

	// A project that listed servers before this existed has them Added.
	f.post("/api/set_project_mcp_servers", map[string]any{"project_id": p, "mcp_servers": []string{"nb"}}, 200)
	got = f.get("/api/get_project_mcp_servers?project_id="+p, 200)
	if projectServer(t, got, "nb")["state"] != "added" || !reflect.DeepEqual(strList(got["mcp_servers"]), []string{"nb"}) {
		t.Fatalf("read = %v", got)
	}
}

func TestProjectMCPSettings_SetReplacesTheWholeSettingsAndAToolFollowsTheGlobalDefault(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	githubWithTools(f)
	out := f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{
		{"name": "nb", "state": "banned"},
		{"name": "github", "state": "added", "tool_overrides": map[string]string{"a": "enabled", "b": "disabled", "vanished": "disabled"}},
	}}, 200)
	if projectServer(t, out, "nb")["state"] != "banned" || !reflect.DeepEqual(strList(out["mcp_servers"]), []string{"github"}) {
		t.Fatalf("answer = %v", out)
	}
	gh := projectServer(t, f.get("/api/get_project_mcp_servers?project_id="+p, 200), "github")
	if a := toolOf(t, gh, "a"); a["override"] != "enabled" || a["effective"] != "enabled" || a["default"] != "disabled" {
		t.Fatalf("a = %v", a)
	}
	if b := toolOf(t, gh, "b"); b["effective"] != "disabled" {
		t.Fatalf("b = %v", b)
	}
	if len(list(gh["tools"])) != 2 {
		t.Fatalf("a tool the server does not know is shown: %v", gh["tools"])
	}

	// Left at Default, a tool follows the global default.
	f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{{"name": "github", "state": "agent"}}}, 200)
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("github"), "disabled_tools": []string{"b"}}, 200)
	got := f.get("/api/get_project_mcp_servers?project_id="+p, 200)
	gh = projectServer(t, got, "github")
	if toolOf(t, gh, "a")["effective"] != "enabled" || toolOf(t, gh, "b")["effective"] != "disabled" || gh["state"] != "agent" {
		t.Fatalf("github = %v", gh)
	}
	if projectServer(t, got, "nb")["state"] != "agent" {
		t.Fatal("a server left out of the replacement did not go back to Agent's choice")
	}

	// A tool the server lost keeps its override for when it comes back.
	f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{{"name": "github", "state": "agent", "tool_overrides": map[string]string{"vanished": "disabled"}}}}, 200)
	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a", "b", "vanished"}}
	f.post("/api/refresh_catalog_mcp_server_tools", map[string]any{"name": "github"}, 200)
	gh = projectServer(t, f.get("/api/get_project_mcp_servers?project_id="+p, 200), "github")
	if v := toolOf(t, gh, "vanished"); v["override"] != "disabled" || v["effective"] != "disabled" {
		t.Fatalf("vanished = %v", v)
	}
}

func TestProjectMCPSettings_RefusalsChangeNothing(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	githubWithTools(f)
	f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{{"name": "nb", "state": "banned"}}}, 200)
	f.post("/api/set_project_mcp_settings", map[string]any{"project_id": "nope", "servers": []map[string]any{}}, 404)
	unknown := f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{{"name": "ghost", "state": "added"}}}, 400)
	if unknown["code"] != "UNKNOWN_REFERENCE" || !reflect.DeepEqual(strList(unknown["names"]), []string{"ghost"}) {
		t.Fatalf("body = %v", unknown)
	}
	for name, servers := range map[string][]map[string]any{
		"bad state":    {{"name": "github", "state": "pigeon"}},
		"bad override": {{"name": "github", "state": "agent", "tool_overrides": map[string]string{"a": "default"}}},
		"twice":        {{"name": "github", "state": "agent"}, {"name": "github", "state": "added"}},
	} {
		if got := f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": servers}, 400); got["code"] != "INVALID_INPUT" {
			t.Errorf("%s: body = %v", name, got)
		}
	}
	got := f.get("/api/get_project_mcp_servers?project_id="+p, 200)
	if projectServer(t, got, "nb")["state"] != "banned" || projectServer(t, got, "github")["state"] != "agent" {
		t.Fatalf("a refusal changed the settings: %v", got)
	}
}

func TestProjectMCPSettings_TheListWriteOnlyMovesTheAddedServers(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	githubWithTools(f)
	f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{
		{"name": "nb", "state": "banned"},
		{"name": "github", "state": "added", "tool_overrides": map[string]string{"a": "enabled"}},
	}}, 200)
	// Naming nothing turns the Added server into Agent's choice and keeps the ban and the override.
	f.post("/api/set_project_mcp_servers", map[string]any{"project_id": p, "mcp_servers": []string{}}, 200)
	got := f.get("/api/get_project_mcp_servers?project_id="+p, 200)
	gh := projectServer(t, got, "github")
	if gh["state"] != "agent" || projectServer(t, got, "nb")["state"] != "banned" || toolOf(t, gh, "a")["override"] != "enabled" {
		t.Fatalf("read = %v", got)
	}
	// Naming a banned server does not lift the ban.
	f.post("/api/set_project_mcp_servers", map[string]any{"project_id": p, "mcp_servers": []string{"nb"}}, 200)
	if got := f.get("/api/get_project_mcp_servers?project_id="+p, 200); projectServer(t, got, "nb")["state"] != "banned" || len(strList(got["mcp_servers"])) != 0 {
		t.Fatalf("read = %v", got)
	}
}

func TestProjectMCPSettings_RenameAndDeleteRewriteEveryProjectsSettings(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	githubWithTools(f)
	f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{
		{"name": "github", "state": "banned", "tool_overrides": map[string]string{"a": "enabled"}},
	}}, 200)

	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "github", "server": stdioServer("gh")}, 200)
	gh := projectServer(t, f.get("/api/get_project_mcp_servers?project_id="+p, 200), "gh")
	if gh["state"] != "banned" || toolOf(t, gh, "a")["override"] != "enabled" {
		t.Fatalf("gh = %v", gh)
	}

	f.post("/api/delete_catalog_mcp_server", map[string]any{"name": "gh"}, 200)
	f.post("/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("gh")}, 200)
	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a", "b"}}
	f.post("/api/refresh_catalog_mcp_server_tools", map[string]any{"name": "gh"}, 200)
	gh = projectServer(t, f.get("/api/get_project_mcp_servers?project_id="+p, 200), "gh")
	if gh["state"] != "agent" || toolOf(t, gh, "a")["override"] != "default" {
		t.Fatalf("a deleted server left its project settings behind: %v", gh)
	}
}

func TestProjectMCPSettings_TheBuiltInServicesServerIsNotListedAndIsRefused(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	got := f.get("/api/get_project_mcp_servers?project_id="+p, 200)
	for _, s := range list(got["servers"]) {
		if s.(map[string]any)["name"] == "services" {
			t.Fatalf("the built-in server is listed: %v", got["servers"])
		}
	}
	for _, state := range []string{"banned", "agent", "added"} {
		out := f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{{"name": "services", "state": state}}}, 400)
		if out["code"] != "INVALID_INPUT" || !strings.Contains(out["error"].(string), "built in") {
			t.Fatalf("%s: body = %v", state, out)
		}
	}
}

func TestProjectMCPSettings_AReplacementThatOmitsAVanishedToolKeepsItsOverride(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	githubWithTools(f)
	f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{
		{"name": "github", "state": "agent", "tool_overrides": map[string]string{"a": "enabled", "vanished": "disabled"}},
	}}, 200)
	// The page shows only known tools, so it sends no "vanished"; a is cleared.
	f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{{"name": "github", "state": "agent"}}}, 200)
	f.prober.result = mcpserver.ProbeResult{Status: "ok", ToolNames: []string{"a", "b", "vanished"}}
	f.post("/api/refresh_catalog_mcp_server_tools", map[string]any{"name": "github"}, 200)
	gh := projectServer(t, f.get("/api/get_project_mcp_servers?project_id="+p, 200), "github")
	if toolOf(t, gh, "vanished")["override"] != "disabled" || toolOf(t, gh, "a")["override"] != "default" {
		t.Fatalf("github = %v", gh)
	}
}

func TestProjectMCPSettings_AServerHasOneStateAndMCPServersListsExactlyTheAddedOnes(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	githubWithTools(f)
	set := func(state string) map[string]any {
		return f.post("/api/set_project_mcp_settings", map[string]any{"project_id": p, "servers": []map[string]any{{"name": "github", "state": state}, {"name": "nb", "state": "added"}}}, 200)
	}
	for state, want := range map[string][]string{"added": {"github", "nb"}, "banned": {"nb"}, "agent": {"nb"}} {
		got := set(state)
		if !reflect.DeepEqual(strList(got["mcp_servers"]), want) || projectServer(t, got, "github")["state"] != state {
			t.Fatalf("%s: answer = %v, want mcp_servers %v", state, got, want)
		}
		if read := f.get("/api/get_project_mcp_servers?project_id="+p, 200); !reflect.DeepEqual(strList(read["mcp_servers"]), want) {
			t.Fatalf("%s: read = %v", state, read)
		}
	}
}
