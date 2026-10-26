package httpapi

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/mcpserver"
	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/adapter/out/agentcatalog"
	"operators-mcp/internal/domain"
)

// importFixture is a copy of the shape of the repository's agents/ folder:
// agents that own a .mcp.json ("self"), agents that share another's, and
// agents with none.
func importFixture(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	put := func(rel, content string, mode os.FileMode) {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	put("agents.json", `{
  "settings": {
    "auto_sync": true,
    "plans_dir": ".claude/plans"
  },
  "bundles": {
    "core": [
      "tdd",
      "design"
    ]
  },
  "agents": {
    "software-architect": {
      "skills": [
        "tdd"
      ],
      "mcp": "self",
      "aliases": [
        "architect"
      ]
    },
    "backend-developer": {
      "skills": [
        "@core"
      ],
      "mcp": "self"
    },
    "node-developer": {
      "skills": [
        "tdd"
      ],
      "mcp": "backend-developer"
    },
    "designer": {
      "skills": [
        "design"
      ],
      "mcp": "designer",
      "note": "unknown key"
    },
    "go-developer": {
      "skills": [
        "tdd"
      ]
    }
  }
}
`, 0o644)
	put("skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first\n---\n\nbody\n", 0o644)
	put("skills/tdd/scripts/run.sh", "#!/bin/sh\necho hi\n", 0o755)
	put("skills/design/SKILL.md", "---\nname: design\ndescription: Design\n---\n\nbody\n", 0o644)
	put("software-architect/.mcp.json", `{"mcpServers":{"notebooklm":{"command":"npx","args":["notebooklm-mcp@latest"]}}}`, 0o644)
	// backend-developer and designer both define github: the same, and
	// postgres/figma apart. Postman differs between them.
	put("backend-developer/.mcp.json", `{"mcpServers":{
  "github":{"type":"http","url":"https://api.githubcopilot.com/mcp/","headers":{"Authorization":"Bearer ${GITHUB_TOKEN}"}},
  "Postman":{"type":"http","url":"https://mcp.postman.com/minimal"},
  "postgres":{"command":"uv","args":["run","postgres-mcp"]}}}`, 0o644)
	put("designer/.mcp.json", `{"mcpServers":{
  "github":{"type":"http","url":"https://api.githubcopilot.com/mcp/","headers":{"Authorization":"Bearer ${GITHUB_TOKEN}"}},
  "Postman":{"type":"http","url":"https://mcp.postman.com/other"},
  "figma":{"command":"bunx","args":["figma-mcp"]}}}`, 0o644)
	return repo
}

func importInto(t *testing.T, src agentcatalog.ImportSource) (*catalogFixture, agentcatalog.ImportReport) {
	t.Helper()
	f := newCatalogFixture(t)
	cat := agentcatalog.New(f.dir)
	rep, err := cat.Import(src)
	if err != nil {
		t.Fatal(err)
	}
	return f, rep
}

func TestCatalogImport_CarriesTheRepositoryFolderIntoTheCatalog(t *testing.T) {
	f, rep := importInto(t, agentcatalog.ImportSource{RepoDir: importFixture(t)})
	if rep.Skipped || rep.Agents != 5 || rep.Bundles != 1 || rep.Skills != 2 || rep.Servers != 5 {
		t.Fatalf("report = %+v", rep)
	}
	cat := f.get("/api/get_agent_catalog", 200)
	if cat["broken"] != nil {
		t.Fatalf("broken = %v", cat["broken"])
	}

	servers := func(agent string) []string { return strList(byName(t, cat["agents"], agent)["mcp_servers"]) }
	// "self": its own file's servers, in file order.
	if got := servers("software-architect"); !reflect.DeepEqual(got, []string{"notebooklm"}) {
		t.Fatalf("software-architect servers = %v", got)
	}
	if got := servers("backend-developer"); !reflect.DeepEqual(got, []string{"github", "Postman", "postgres"}) {
		t.Fatalf("backend-developer servers = %v", got)
	}
	// Sharing another agent's file: the same servers.
	if got := servers("node-developer"); !reflect.DeepEqual(got, servers("backend-developer")) {
		t.Fatalf("node-developer servers = %v", got)
	}
	if got := servers("designer"); !reflect.DeepEqual(got, []string{"github", "Postman", "figma"}) {
		t.Fatalf("designer servers = %v", got)
	}
	if got := servers("go-developer"); len(got) != 0 {
		t.Fatalf("go-developer servers = %v", got)
	}
	if got := strList(byName(t, cat["agents"], "software-architect")["aliases"]); !reflect.DeepEqual(got, []string{"architect"}) {
		t.Fatalf("aliases = %v", got)
	}

	// Two files defined Postman differently: the first in agent-key order
	// (backend-developer) is kept, and the log says which was dropped.
	pm := byName(t, cat["mcp_servers"], "Postman")
	if pm["url"] != "https://mcp.postman.com/minimal" {
		t.Fatalf("Postman = %v", pm)
	}
	if len(rep.DroppedServers) != 1 || !strings.Contains(rep.DroppedServers[0], "Postman") || !strings.Contains(rep.DroppedServers[0], "designer") {
		t.Fatalf("dropped = %v", rep.DroppedServers)
	}
	// An identical definition is no conflict.
	if len(list(byName(t, cat["mcp_servers"], "github")["agents"])) != 3 {
		t.Fatalf("github agents = %v", byName(t, cat["mcp_servers"], "github")["agents"])
	}

	agents := f.read("agents.json")
	for _, want := range []string{`"auto_sync": true`, `"plans_dir": ".claude/plans"`, `"note": "unknown key"`, `"mcp_servers"`} {
		if !strings.Contains(agents, want) {
			t.Fatalf("agents.json lost %s:\n%s", want, agents)
		}
	}
	if strings.Contains(agents, `"mcp":`) {
		t.Fatalf("agents.json still has the old mcp field:\n%s", agents)
	}
	if got := strList(byName(t, cat["bundles"], "core")["resolved_skills"]); !reflect.DeepEqual(got, []string{"tdd", "design"}) {
		t.Fatalf("core = %v", got)
	}

	if f.read("skills/tdd/scripts/run.sh") != "#!/bin/sh\necho hi\n" {
		t.Fatal("a skill file did not copy")
	}
	if info, _ := os.Stat(filepath.Join(f.dir, "skills/tdd/scripts/run.sh")); info.Mode().Perm() != 0o755 {
		t.Fatalf("the script lost its mode: %v", info.Mode())
	}
	// Nothing was left in the repository's folder changed.
	if _, err := os.Stat(f.dir + ".importing"); err == nil {
		t.Fatal("the import left its working folder behind")
	}
}

func TestCatalogImport_AddsTheDatabaseCatalogAndTheRepositoryWinsAClash(t *testing.T) {
	src := agentcatalog.ImportSource{
		RepoDir: importFixture(t),
		Skills: []*skill.Skill{
			{ID: "s1", Name: "tdd", Description: "from the database", Files: []skill.File{{Path: "SKILL.md", Content: "---\nname: tdd\ndescription: from the database\n---\n\ndb body\n"}}},
			{ID: "s2", Name: "Db Only Skill", Description: "only in the database", Files: []skill.File{
				{Path: "SKILL.md", Content: "---\nname: Db Only Skill\ndescription: only in the database\n---\n\nbody\n"},
				{Path: "bin/x", Content: "//4A", Encoding: "base64"},
			}},
		},
		MCPServers: []*mcpserver.Server{
			{ID: "m1", Name: "github", Transport: "stdio", Command: "from-db"},
			{ID: "m2", Name: "db-server", Transport: "streamable-http", URL: "https://db.example/mcp", Headers: map[string]string{"X": "${DB_TOKEN}"}},
		},
		Agents: []*domain.Agent{
			{ID: "a1", Name: "go-developer", Description: "db go", SkillIDs: []string{"s2"}},
			{ID: "a2", Name: "DB Agent", Description: "only in the database", SkillIDs: []string{"s1", "s2"}, MCPServerIDs: []string{"m2"}, PromptID: "p1"},
		},
	}
	f, rep := importInto(t, src)
	cat := f.get("/api/get_agent_catalog", 200)

	if got := byName(t, cat["skills"], "tdd")["description"]; got != "Test first" {
		t.Fatalf("the database's tdd won: %v", got)
	}
	if byName(t, cat["mcp_servers"], "github")["command"] != "" {
		t.Fatal("the database's github won")
	}
	if got := byName(t, cat["agents"], "go-developer")["description"]; got == "db go" {
		t.Fatal("the database's go-developer won")
	}
	if got := strList(byName(t, cat["agents"], "go-developer")["entries"]); !reflect.DeepEqual(got, []string{"tdd"}) {
		t.Fatalf("go-developer entries = %v", got)
	}

	only := byName(t, cat["skills"], "db-only-skill")
	if only["description"] != "only in the database" || only["file_count"] != float64(2) {
		t.Fatalf("db-only-skill = %v", only)
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "skills/db-only-skill/bin/x")); string(b) != "\xff\xfe\x00" {
		t.Fatalf("binary file = %q", b)
	}
	ds := byName(t, cat["mcp_servers"], "db-server")
	if ds["transport"] != "http" || ds["url"] != "https://db.example/mcp" {
		t.Fatalf("db-server = %v", ds)
	}
	dbAgent := byName(t, cat["agents"], "db-agent")
	if got := strList(dbAgent["entries"]); !reflect.DeepEqual(got, []string{"tdd", "db-only-skill"}) {
		t.Fatalf("db-agent entries = %v", got)
	}
	if got := strList(dbAgent["mcp_servers"]); !reflect.DeepEqual(got, []string{"db-server"}) {
		t.Fatalf("db-agent servers = %v", got)
	}

	clashes := strings.Join(rep.Clashes, "; ")
	for _, want := range []string{"skill tdd", "MCP server github", "agent go-developer"} {
		if !strings.Contains(clashes, want) {
			t.Fatalf("clashes = %q, want %q", clashes, want)
		}
	}
	if len(rep.Dropped) != 1 || !strings.Contains(rep.Dropped[0], "prompt") {
		t.Fatalf("dropped = %v", rep.Dropped)
	}
}

func TestCatalogImport_NeverTouchesACatalogThatHasContent(t *testing.T) {
	f := newCatalogFixture(t)
	f.write("agents.json", handWrittenAgents)
	rep, err := agentcatalog.New(f.dir).Import(agentcatalog.ImportSource{RepoDir: importFixture(t)})
	if err != nil || !rep.Skipped {
		t.Fatalf("import = %+v, %v", rep, err)
	}
	if f.read("agents.json") != handWrittenAgents || f.exists("skills") {
		t.Fatal("the import changed a non-empty catalog")
	}

	// An empty folder is still empty.
	empty := newCatalogFixture(t)
	if err := os.MkdirAll(empty.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err = agentcatalog.New(empty.dir).Import(agentcatalog.ImportSource{RepoDir: importFixture(t)})
	if err != nil || rep.Skipped || rep.Agents != 5 {
		t.Fatalf("import into an empty folder = %+v, %v", rep, err)
	}
	// Run again: now it has content.
	rep, _ = agentcatalog.New(empty.dir).Import(agentcatalog.ImportSource{RepoDir: importFixture(t)})
	if !rep.Skipped {
		t.Fatal("a second import ran")
	}
}

func TestCatalogImport_NothingToImportLeavesNoFolder(t *testing.T) {
	f := newCatalogFixture(t)
	rep, err := agentcatalog.New(f.dir).Import(agentcatalog.ImportSource{RepoDir: filepath.Join(t.TempDir(), "missing")})
	if err != nil || rep.Agents != 0 {
		t.Fatalf("import = %+v, %v", rep, err)
	}
	if f.exists("") && f.exists("agents.json") {
		t.Fatal("an import of nothing wrote files")
	}
}

// The real agents/ folder of this repository imports without error, and every
// agent keeps the skills it had. It goes away with the folder.
func TestCatalogImport_TheRepositoryAgentsFolder(t *testing.T) {
	repo, err := filepath.Abs("../../../../agents")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "agents.json")); err != nil {
		t.Skip("the repository has no agents/ folder any more")
	}
	f, rep := importInto(t, agentcatalog.ImportSource{RepoDir: repo})
	if rep.Agents == 0 || rep.Skills < 400 {
		t.Fatalf("report = %+v", rep)
	}
	cat := f.get("/api/get_agent_catalog", 200)
	if cat["broken"] != nil {
		t.Fatalf("broken = %v", cat["broken"])
	}
	for _, a := range list(cat["agents"]) {
		ag := a.(map[string]any)
		if len(list(ag["missing_skills"])) != 0 {
			t.Errorf("agent %v misses skills %v", ag["name"], ag["missing_skills"])
		}
		for _, p := range list(ag["problems"]) {
			if code := p.(map[string]any)["code"]; code != "NO_DESCRIPTION" {
				t.Errorf("agent %v has problem %v", ag["name"], p)
			}
		}
	}
	// The 4 "self" agents keep their servers; the sharers keep theirs.
	servers := func(agent string) []string { return strList(byName(t, cat["agents"], agent)["mcp_servers"]) }
	if got := servers("django-developer"); !reflect.DeepEqual(got, servers("backend-developer")) || len(got) == 0 {
		t.Fatalf("django-developer servers = %v", got)
	}
	if got := servers("ux-designer"); !reflect.DeepEqual(got, servers("designer")) || len(got) == 0 {
		t.Fatalf("ux-designer servers = %v", got)
	}
	if got := servers("software-architect"); !reflect.DeepEqual(got, []string{"notebooklm"}) {
		t.Fatalf("software-architect servers = %v", got)
	}
	if got := servers("go-developer"); len(got) != 0 {
		t.Fatalf("go-developer servers = %v", got)
	}
	if !strings.Contains(f.read("agents.json"), `"auto_sync": true`) {
		t.Fatal("settings.auto_sync was lost")
	}
}
