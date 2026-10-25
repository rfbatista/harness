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
