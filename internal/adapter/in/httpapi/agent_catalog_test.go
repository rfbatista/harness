package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/mcpserver"

	"operators-mcp/internal/adapter/out/agentcatalog"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/projects"
)

// catalogFixture is an agent catalog folder in a temporary directory, served
// over the HTTP API.
type catalogFixture struct {
	t        *testing.T
	dir      string
	router   http.Handler
	env      map[string]string
	projects *projects.Service
	prober   *fakeProber
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	app := catalog.New(catalog.SQLiteDeps(db))
	f := &catalogFixture{t: t, dir: filepath.Join(t.TempDir(), "agents"), env: map[string]string{}, projects: app.Projects, prober: &fakeProber{}}
	cat := agentcatalog.New(f.dir,
		agentcatalog.WithEnv(func(k string) (string, bool) {
			v, ok := f.env[k]
			return v, ok
		}),
		agentcatalog.WithProjects(app.Projects),
		agentcatalog.WithProber(f.prober))
	f.router = NewRouter(NewHandler(Services{Catalog: cat}))
	return f
}

// fakeProber answers a probe with what a test set, and keeps what it was asked.
type fakeProber struct {
	got    mcpserver.Server
	result mcpserver.ProbeResult
	err    error
}

func (p *fakeProber) Probe(_ context.Context, s mcpserver.Server) (mcpserver.ProbeResult, error) {
	p.got = s
	return p.result, p.err
}

func (f *catalogFixture) write(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *catalogFixture) skill(name, description string) {
	f.write("skills/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: "+description+"\n---\n\n# "+name+"\n")
}

// get calls a GET endpoint and decodes the answer.
func (f *catalogFixture) get(path string, status int) map[string]any {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != status {
		f.t.Fatalf("GET %s = %d, want %d: %s", path, rec.Code, status, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		f.t.Fatalf("GET %s: %v: %s", path, err, rec.Body.String())
	}
	return out
}

func list(v any) []any { l, _ := v.([]any); return l }

func strList(v any) []string {
	out := []string{}
	for _, x := range list(v) {
		out = append(out, x.(string))
	}
	return out
}

// byName finds the item with the given name in a list of objects.
func byName(t *testing.T, items any, name string) map[string]any {
	t.Helper()
	for _, it := range list(items) {
		if m := it.(map[string]any); m["name"] == name {
			return m
		}
	}
	t.Fatalf("no item named %q in %v", name, items)
	return nil
}

func problemCodes(item map[string]any) []string {
	out := []string{}
	for _, p := range list(item["problems"]) {
		out = append(out, p.(map[string]any)["code"].(string))
	}
	return out
}

const catalogAgentsJSON = `{
  "settings": { "auto_sync": true },
  "bundles": {
    "ux": ["design", "@core"],
    "core": ["tdd", "design"],
    "loop-a": ["@loop-b"],
    "loop-b": ["@loop-a"]
  },
  "agents": {
    "ux-designer": {
      "description": "Designs screens",
      "aliases": ["ux"],
      "skills": ["@ux", "tdd", "ghost"],
      "mcp_servers": ["notebooklm", "nowhere"],
      "allowed_tools": ["Read"]
    },
    "plain": { "skills": ["@missing", "@loop-a"] }
  }
}
`

func TestCatalog_ReadsAgentsBundlesSkillsAndServersWithTheirProblems(t *testing.T) {
	f := newCatalogFixture(t)
	f.write("agents.json", catalogAgentsJSON)
	f.skill("tdd", "Test first")
	f.skill("design", "Design things")
	f.write("skills/design/refs/notes.md", "notes")
	f.write("skills/nodesc/SKILL.md", "---\nname: nodesc\n---\nbody\n")
	f.write("skills/bad/readme.txt", "no skill file")
	f.write("mcp-servers.json", `{
  "mcpServers": {
    "notebooklm": { "command": "npx", "args": ["-y", "nb"], "env": { "TOKEN": "${NB_TOKEN}", "OTHER": "${NB_OTHER}" } },
    "linear": { "type": "http", "url": "https://x/mcp", "headers": { "Authorization": "Bearer ${LINEAR_TOKEN}" } }
  }
}
`)
	f.env["NB_TOKEN"] = "super-secret-value"

	cat := f.get("/api/get_agent_catalog", 200)

	if cat["broken"] != nil {
		t.Fatalf("broken = %v", cat["broken"])
	}
	if cat["root"] != f.dir {
		t.Fatalf("root = %v, want %v", cat["root"], f.dir)
	}

	ux := byName(t, cat["agents"], "ux-designer")
	if got, want := strList(ux["entries"]), []string{"@ux", "tdd", "ghost"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	// @ux = design + @core(tdd, design); then tdd again; then ghost.
	if got, want := strList(ux["resolved_skills"]), []string{"design", "tdd", "ghost"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved_skills = %v, want %v", got, want)
	}
	if got, want := strList(ux["dropped_duplicates"]), []string{"design", "tdd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dropped_duplicates = %v, want %v", got, want)
	}
	if got, want := strList(ux["missing_skills"]), []string{"ghost"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing_skills = %v, want %v", got, want)
	}
	if got, want := strList(ux["allowed_tools"]), []string{"Read"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_tools = %v", got)
	}
	if got, want := problemCodes(ux), []string{"MISSING_SKILL", "UNKNOWN_MCP_SERVER"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ux problems = %v, want %v", got, want)
	}

	plain := byName(t, cat["agents"], "plain")
	if got, want := problemCodes(plain), []string{"UNKNOWN_BUNDLE", "BUNDLE_CYCLE", "NO_DESCRIPTION"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plain problems = %v, want %v", got, want)
	}

	core := byName(t, cat["bundles"], "core")
	if got, want := strList(core["used_by_bundles"]), []string{"ux"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("core used_by_bundles = %v", got)
	}
	ux2 := byName(t, cat["bundles"], "ux")
	if got, want := strList(ux2["used_by_agents"]), []string{"ux-designer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ux used_by_agents = %v", got)
	}
	if got, want := strList(ux2["resolved_skills"]), []string{"design", "tdd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ux bundle resolved = %v", got)
	}

	design := byName(t, cat["skills"], "design")
	if design["file_count"] != float64(2) || design["has_extra_files"] != true {
		t.Fatalf("design files = %v extra=%v", design["file_count"], design["has_extra_files"])
	}
	if got, want := strList(design["bundles"]), []string{"ux", "core"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("design bundles = %v, want %v", got, want)
	}
	if design["agent_count"] != float64(1) || len(list(design["agents"])) != 0 {
		t.Fatalf("design reach = %v / %v", design["agent_count"], design["agents"])
	}
	tdd := byName(t, cat["skills"], "tdd")
	if got, want := strList(tdd["agents"]), []string{"ux-designer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tdd agents = %v", got)
	}
	if got := problemCodes(byName(t, cat["skills"], "nodesc")); !reflect.DeepEqual(got, []string{"NO_DESCRIPTION"}) {
		t.Fatalf("nodesc problems = %v", got)
	}
	if got := problemCodes(byName(t, cat["skills"], "bad")); !reflect.DeepEqual(got, []string{"INVALID_SKILL"}) {
		t.Fatalf("bad problems = %v", got)
	}

	nb := byName(t, cat["mcp_servers"], "notebooklm")
	if nb["transport"] != "stdio" || nb["command"] != "npx" {
		t.Fatalf("notebooklm = %v", nb)
	}
	if nb["env"].(map[string]any)["TOKEN"] != "${NB_TOKEN}" {
		t.Fatalf("env was expanded: %v", nb["env"])
	}
	if got, want := strList(nb["agents"]), []string{"ux-designer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("notebooklm agents = %v", got)
	}
	refs := list(nb["env_refs"])
	if len(refs) != 2 || refs[0].(map[string]any)["name"] != "NB_OTHER" || refs[0].(map[string]any)["set"] != false ||
		refs[1].(map[string]any)["name"] != "NB_TOKEN" || refs[1].(map[string]any)["set"] != true {
		t.Fatalf("env_refs = %v", refs)
	}
	if got := problemCodes(nb); !reflect.DeepEqual(got, []string{"SECRET_NOT_SET"}) {
		t.Fatalf("notebooklm problems = %v", got)
	}
	if lin := byName(t, cat["mcp_servers"], "linear"); lin["transport"] != "http" {
		t.Fatalf("linear transport = %v", lin["transport"])
	}

	if len(list(cat["built_in_tools"])) == 0 {
		t.Fatal("no built-in tools")
	}
	versions := cat["versions"].(map[string]any)
	if !strings.HasPrefix(versions["agents.json"].(string), "sha256:") || !strings.HasPrefix(versions["mcp-servers.json"].(string), "sha256:") {
		t.Fatalf("versions = %v", versions)
	}
}

func TestCatalog_NeverReturnsAVariableValue(t *testing.T) {
	f := newCatalogFixture(t)
	f.write("mcp-servers.json", `{"mcpServers":{"a":{"command":"x","env":{"K":"${SECRET_ONE}"}}}}`)
	f.env["SECRET_ONE"] = "hunter2-value"
	for _, path := range []string{"/api/get_agent_catalog", "/api/check_env_vars?names=SECRET_ONE,SECRET_TWO"} {
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if strings.Contains(rec.Body.String(), "hunter2-value") {
			t.Fatalf("%s returned a value: %s", path, rec.Body.String())
		}
	}
	got := f.get("/api/check_env_vars?names=SECRET_ONE,SECRET_TWO", 200)
	if got["SECRET_ONE"] != true || got["SECRET_TWO"] != false {
		t.Fatalf("check_env_vars = %v", got)
	}
}

func TestCatalog_AMissingFolderIsAnEmptyCatalog(t *testing.T) {
	f := newCatalogFixture(t)
	cat := f.get("/api/get_agent_catalog", 200)
	for _, k := range []string{"agents", "bundles", "skills", "mcp_servers"} {
		if l, ok := cat[k].([]any); !ok || len(l) != 0 {
			t.Fatalf("%s = %v, want an empty list", k, cat[k])
		}
	}
	if cat["broken"] != nil {
		t.Fatalf("broken = %v", cat["broken"])
	}
}

func TestCatalog_ReadsASkillWithItsBodyFilesAndVersion(t *testing.T) {
	f := newCatalogFixture(t)
	f.write("agents.json", `{"agents":{"a":{"description":"d","skills":["tdd"]}}}`)
	f.write("skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first\n---\n\n# TDD\n\nbody text\n")
	f.write("skills/tdd/refs/one.md", "one")
	f.write("skills/tdd/bin.dat", string([]byte{0xff, 0xfe, 0x00}))

	out := f.get("/api/get_catalog_skill?name=tdd", 200)
	sk := out["skill"].(map[string]any)
	if sk["description"] != "Test first" {
		t.Fatalf("description = %v", sk["description"])
	}
	body := sk["body"].(string)
	if strings.Contains(body, "---") || !strings.Contains(body, "body text") {
		t.Fatalf("body = %q", body)
	}
	files := map[string]map[string]any{}
	for _, x := range list(sk["files"]) {
		m := x.(map[string]any)
		files[m["path"].(string)] = m
	}
	if _, ok := files["SKILL.md"]; ok {
		t.Fatal("SKILL.md is listed among the files")
	}
	if files["refs"]["dir"] != true {
		t.Fatalf("refs = %v", files["refs"])
	}
	if files["refs/one.md"]["content"] != "one" || files["refs/one.md"]["encoding"] != "utf-8" {
		t.Fatalf("one.md = %v", files["refs/one.md"])
	}
	if files["bin.dat"]["encoding"] != "base64" || files["bin.dat"]["content"] != "//4A" {
		t.Fatalf("bin.dat = %v", files["bin.dat"])
	}
	if got := strList(sk["agents"]); !reflect.DeepEqual(got, []string{"a"}) || sk["agent_count"] != float64(1) {
		t.Fatalf("reach = %v / %v", got, sk["agent_count"])
	}
	v1 := out["versions"].(map[string]any)["skills/tdd"].(string)
	f.write("skills/tdd/refs/one.md", "changed")
	v2 := f.get("/api/get_catalog_skill?name=tdd", 200)["versions"].(map[string]any)["skills/tdd"].(string)
	if v1 == "" || v1 == v2 {
		t.Fatalf("version did not change with a file: %q %q", v1, v2)
	}

	f.get("/api/get_catalog_skill?name=nope", 404)
	bad := f.get("/api/get_catalog_skill?name=../x", 400)
	if bad["code"] != "INVALID_INPUT" || bad["field"] != "name" {
		t.Fatalf("bad name = %v", bad)
	}
}

func TestCatalog_ABrokenFileIsReportedAndTheLastGoodListsStay(t *testing.T) {
	f := newCatalogFixture(t)
	f.write("agents.json", `{"agents":{"a":{"description":"d"}}}`)
	if got := list(f.get("/api/get_agent_catalog", 200)["agents"]); len(got) != 1 {
		t.Fatalf("agents = %v", got)
	}

	f.write("agents.json", "{\n  \"agents\": {\n    \"a\": {\"description\": }\n  }\n}\n")
	cat := f.get("/api/get_agent_catalog", 200)
	br, _ := cat["broken"].(map[string]any)
	if br == nil {
		t.Fatalf("broken = nil: %v", cat)
	}
	if br["path"] != "agents.json" || br["line"] != float64(3) || br["column"] != float64(26) || br["message"] == "" {
		t.Fatalf("broken = %v", br)
	}
	if got := list(cat["agents"]); len(got) != 1 {
		t.Fatalf("the last good agents were lost: %v", got)
	}
}

func TestCatalog_ReadingFrontmatterOnlyKeepsAHugeCatalogFast(t *testing.T) {
	f := newCatalogFixture(t)
	big := strings.Repeat("x", 200_000)
	for i := 0; i < 475; i++ {
		name := "skill-" + strings.Repeat("a", i%5) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
		f.write("skills/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: d\n---\n"+big)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/get_agent_catalog", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Body.Len() > 500_000 {
		t.Fatalf("the catalog read returned %d bytes: it carries skill content", rec.Body.Len())
	}
}
