package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/mcp"
	"operators-mcp/internal/adapter/out/agentcatalog"
	"operators-mcp/internal/adapter/out/catalogstore"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
)

// legacyFixture serves the existing agent, skill and MCP server endpoints over
// the agent catalog, the way the server wires them.
func legacyFixture(t *testing.T) *catalogFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	f := &catalogFixture{t: t, env: map[string]string{}}
	f.dir = t.TempDir() + "/agents"
	f.projects = nil
	cat := agentcatalog.New(f.dir)
	deps := catalog.SQLiteDeps(db)
	deps.Agents = catalogstore.NewAgents(cat)
	deps.Skills = catalogstore.NewSkills(cat)
	deps.MCPServers = catalogstore.NewMCPServers(cat)
	app := catalog.New(deps)
	f.projects = app.Projects
	cat.UseProjects(app.Projects)
	f.router = NewRouter(NewHandler(Services{Catalog: cat, Agents: app.Agents, Capabilities: app.Capabilities, Projects: app.Projects}))
	return f
}

func (f *catalogFixture) rawGet(path string) (int, []byte) {
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.Bytes()
}

func TestLegacyReads_KeepTheirShapesOverTheCatalog(t *testing.T) {
	f := legacyFixture(t)
	f.write("agents.json", `{
  "bundles": { "core": ["tdd", "design"] },
  "agents": { "ux": { "description": "UX", "skills": ["@core", "ghost"], "mcp_servers": ["nb", "gone"], "aliases": ["u"] } }
}
`)
	f.skill("tdd", "Test first")
	f.skill("design", "Design")
	f.write("skills/tdd/refs/a.md", "a")
	f.write("mcp-servers.json", `{"mcpServers":{"nb":{"command":"npx","args":["x"],"env":{"K":"${V}"}},"lin":{"type":"http","url":"https://l/mcp","headers":{"A":"${T}"}}}}`)

	_, body := f.rawGet("/api/list_agents")
	var agents mcp.ListAgentsOut
	if err := json.Unmarshal(body, &agents); err != nil || len(agents.Agents) != 1 {
		t.Fatalf("list_agents = %s (%v)", body, err)
	}
	a := agents.Agents[0]
	if a.ID != "ux" || a.Name != "ux" || a.Description != "UX" {
		t.Fatalf("agent = %+v", a)
	}
	// Its resolved skills and its servers, by name; get_agent resolves the ones
	// that exist.
	if !reflect.DeepEqual(a.SkillIDs, []string{"tdd", "design", "ghost"}) || !reflect.DeepEqual(a.MCPServerIDs, []string{"nb", "gone"}) {
		t.Fatalf("agent refs = %v %v", a.SkillIDs, a.MCPServerIDs)
	}

	_, body = f.rawGet("/api/get_agent?agent_id=ux")
	var got mcp.GetAgentOut
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Agent.Skills) != 2 || got.Agent.Skills[0].Name != "tdd" {
		t.Fatalf("get_agent skills = %+v", got.Agent.Skills)
	}
	var tdd *mcp.SkillDTO
	for _, s := range got.Agent.Skills {
		if s.Name == "tdd" {
			tdd = s
		}
	}
	paths := []string{}
	for _, file := range tdd.Files {
		paths = append(paths, file.Path)
	}
	// What agent_launch.py --harness writes out: SKILL.md as it is, and the rest.
	if !reflect.DeepEqual(paths, []string{"SKILL.md", "refs", "refs/a.md"}) {
		t.Fatalf("tdd files = %v", paths)
	}
	if !strings.Contains(tdd.Files[0].Content, "description: Test first") {
		t.Fatalf("SKILL.md = %q", tdd.Files[0].Content)
	}
	if len(got.Agent.MCPServers) != 1 || got.Agent.MCPServers[0].Name != "nb" || got.Agent.MCPServers[0].Transport != "stdio" ||
		got.Agent.MCPServers[0].Env["K"] != "${V}" {
		t.Fatalf("get_agent servers = %+v", got.Agent.MCPServers)
	}

	_, body = f.rawGet("/api/list_skills")
	var skills mcp.ListSkillsOut
	if err := json.Unmarshal(body, &skills); err != nil || len(skills.Skills) != 2 {
		t.Fatalf("list_skills = %s (%v)", body, err)
	}
	_, body = f.rawGet("/api/get_skill?skill_id=tdd")
	var sk mcp.GetSkillOut
	if err := json.Unmarshal(body, &sk); err != nil || sk.Skill.ID != "tdd" || sk.Skill.Description != "Test first" {
		t.Fatalf("get_skill = %s (%v)", body, err)
	}
	_, body = f.rawGet("/api/list_mcp_servers")
	var servers mcp.ListMCPServersOut
	if err := json.Unmarshal(body, &servers); err != nil || len(servers.MCPServers) != 2 {
		t.Fatalf("list_mcp_servers = %s (%v)", body, err)
	}
	for _, s := range servers.MCPServers {
		if s.Name == "nb" && (s.AgentCount != 1 || !reflect.DeepEqual(s.AgentNames, []string{"ux"})) {
			t.Fatalf("nb usage = %+v", s)
		}
		if s.Name == "lin" && s.Transport != "streamable-http" {
			t.Fatalf("lin transport = %q", s.Transport)
		}
	}
	if code, _ := f.rawGet("/api/get_agent?agent_id=nope"); code != 404 {
		t.Fatalf("unknown agent = %d", code)
	}
}

func TestLegacyWrites_GoThroughTheCatalog(t *testing.T) {
	f := legacyFixture(t)
	f.skill("tdd", "Test first")
	f.skill("design", "Design")
	f.write("agents.json", `{
  "bundles": { "core": ["tdd", "design"] },
  "agents": { "ux": { "description": "UX", "skills": ["@core"], "aliases": ["u"], "allowed_tools": ["Read"] } }
}
`)

	f.post("/api/create_agent", map[string]any{"name": "new-agent", "description": "New", "skill_ids": []string{"tdd"}}, 200)
	if got := f.read("agents.json"); !strings.Contains(got, `"new-agent"`) {
		t.Fatalf("agents.json = %s", got)
	}

	// An update that leaves the skills as they resolve keeps the bundle entry,
	// the aliases and the allowed tools.
	f.post("/api/update_agent", map[string]any{"agent_id": "ux", "description": "UX 2"}, 200)
	cat := f.get("/api/get_agent_catalog", 200)
	ux := byName(t, cat["agents"], "ux")
	if ux["description"] != "UX 2" || !reflect.DeepEqual(strList(ux["entries"]), []string{"@core"}) ||
		!reflect.DeepEqual(strList(ux["aliases"]), []string{"u"}) || !reflect.DeepEqual(strList(ux["allowed_tools"]), []string{"Read"}) {
		t.Fatalf("ux = %v", ux)
	}
	// A different list replaces the entries.
	f.post("/api/update_agent", map[string]any{"agent_id": "ux", "skill_ids": []string{"design"}}, 200)
	ux = byName(t, f.get("/api/get_agent_catalog", 200)["agents"], "ux")
	if !reflect.DeepEqual(strList(ux["entries"]), []string{"design"}) {
		t.Fatalf("ux entries = %v", ux["entries"])
	}

	f.del("/api/delete_agent", map[string]any{"agent_id": "new-agent"})
	if strings.Contains(f.read("agents.json"), "new-agent") {
		t.Fatal("the agent was not deleted")
	}

	f.post("/api/create_mcp_server", map[string]any{"name": "nb", "transport": "stdio", "command": "npx", "env": map[string]string{"K": "${V}"}}, 200)
	if got := f.read("mcp-servers.json"); !strings.Contains(got, `"K": "${V}"`) {
		t.Fatalf("mcp-servers.json = %s", got)
	}
	f.post("/api/update_mcp_server", map[string]any{"mcp_server_id": "nb", "args": []string{"-y", "pkg"}}, 200)
	if got := f.read("mcp-servers.json"); !strings.Contains(got, `"pkg"`) || !strings.Contains(got, `"command": "npx"`) {
		t.Fatalf("mcp-servers.json = %s", got)
	}
	f.post("/api/update_agent", map[string]any{"agent_id": "ux", "mcp_server_ids": []string{"nb"}}, 200)
	f.del("/api/delete_mcp_server", map[string]any{"mcp_server_id": "nb"})
	if got := strList(byName(t, f.get("/api/get_agent_catalog", 200)["agents"], "ux")["mcp_servers"]); len(got) != 0 {
		t.Fatalf("ux still names the deleted server: %v", got)
	}

	f.post("/api/create_skill", map[string]any{"name": "review", "description": "Review", "files": []map[string]any{
		{"path": "SKILL.md", "content": "---\nname: review\ndescription: Review\n---\n\nSteps\n"},
		{"path": "refs/x.md", "content": "x"},
	}}, 200)
	if f.read("skills/review/refs/x.md") != "x" || !strings.Contains(f.read("skills/review/SKILL.md"), "Steps") {
		t.Fatal("the skill was not written")
	}
	f.post("/api/update_skill", map[string]any{"skill_id": "review", "description": "Review well"}, 200)
	if got := f.read("skills/review/SKILL.md"); !strings.Contains(got, "description: Review well") || !strings.Contains(got, "Steps") {
		t.Fatalf("SKILL.md = %q", got)
	}
	f.del("/api/delete_skill", map[string]any{"skill_id": "design"})
	if f.exists("skills/design") || strings.Contains(f.read("agents.json"), `"design"`) {
		t.Fatalf("design was not removed everywhere: %s", f.read("agents.json"))
	}
	// Deleting a skill must not flatten the bundles that held it.
	if !strings.Contains(f.read("agents.json"), `"core"`) {
		t.Fatal("the bundle is gone")
	}
}

func TestLegacyAgentWrites_RefuseWhatTheCatalogCannotHold(t *testing.T) {
	f := legacyFixture(t)
	f.write("agents.json", `{"agents":{"a":{"description":"d"}}}`)
	before := f.read("agents.json")
	for _, c := range []struct {
		path string
		body map[string]any
	}{
		{"create_agent", map[string]any{"name": "b", "description": "d", "prompt_id": "p1"}},
		{"update_agent", map[string]any{"agent_id": "a", "prompt_id": "p1"}},
	} {
		got := f.post("/api/"+c.path, c.body, 400)
		if got["code"] != "INVALID_INPUT" || got["field"] != "prompt_id" {
			t.Errorf("%s: body = %v", c.path, got)
		}
	}
	if f.read("agents.json") != before {
		t.Fatal("a refused write changed the catalog")
	}
}

// del calls a legacy delete endpoint, which answers 204.
func (f *catalogFixture) del(path string, body any) {
	f.t.Helper()
	if rec := f.postRaw(path, body); rec.Code != http.StatusNoContent {
		f.t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body.String())
	}
}

func TestListAgents_WithAProjectOffersOnlyItsEnabledAgents(t *testing.T) {
	f := legacyFixture(t)
	f.write("agents.json", `{"agents":{"a":{"description":"d"},"b":{"description":"d"}}}`)
	all, sel := f.project("All"), f.project("Selected")
	f.post("/api/set_project_agents", map[string]any{"project_id": sel, "mode": "selected", "agents": []string{"b"}}, 200)

	names := func(path string) []string {
		_, body := f.rawGet(path)
		var out mcp.ListAgentsOut
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, a := range out.Agents {
			n = append(n, a.Name)
		}
		return n
	}
	if got := names("/api/list_agents"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("unfiltered = %v", got)
	}
	if got := names("/api/list_agents?project_id=" + all); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("all project = %v", got)
	}
	if got := names("/api/list_agents?project_id=" + sel); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("selected project = %v", got)
	}
	if code, _ := f.rawGet("/api/list_agents?project_id=nope"); code != 404 {
		t.Fatalf("unknown project = %d", code)
	}
}
