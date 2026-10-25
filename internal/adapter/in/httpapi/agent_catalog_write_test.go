package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/agentcatalog"
	"operators-mcp/internal/ports"
)

// post calls a POST endpoint and decodes the answer.
func (f *catalogFixture) post(path string, body any, status int) map[string]any {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	if rec.Code != status {
		f.t.Fatalf("POST %s %s = %d, want %d: %s", path, raw, rec.Code, status, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		f.t.Fatalf("POST %s: %v: %s", path, err, rec.Body.String())
	}
	return out
}

func (f *catalogFixture) read(rel string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(rel)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func (f *catalogFixture) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(f.dir, filepath.FromSlash(rel)))
	return err == nil
}

// versions is the catalog read's versions, to send as base_versions.
func (f *catalogFixture) versions() map[string]any {
	return f.get("/api/get_agent_catalog", 200)["versions"].(map[string]any)
}

const handWrittenAgents = `{
  "settings": {
    "auto_sync": true
  },
  "extra": [
    1,
    2
  ],
  "bundles": {
    "core": [
      "tdd"
    ]
  },
  "agents": {
    "zeta": {
      "note": "kept",
      "description": "Last in the alphabet",
      "skills": [
        "tdd"
      ]
    }
  }
}
`

func catalogWithSkills(t *testing.T) *catalogFixture {
	f := newCatalogFixture(t)
	f.write("agents.json", handWrittenAgents)
	f.skill("tdd", "Test first")
	f.skill("design", "Design things")
	f.write("mcp-servers.json", `{"mcpServers":{"nb":{"command":"npx"}}}`)
	return f
}

func TestCatalogSave_CreatesAnAgentKeepingEverythingElseAsWritten(t *testing.T) {
	f := catalogWithSkills(t)
	out := f.post("/api/save_catalog_agent", map[string]any{
		"base_versions": f.versions(),
		"agent": map[string]any{
			"name": "alpha", "description": "First", "aliases": []string{"a"},
			"entries": []string{"@core", "design"}, "mcp_servers": []string{"nb"}, "allowed_tools": []string{"Read", "Bash"},
		},
	}, 200)

	want := `{
  "settings": {
    "auto_sync": true
  },
  "extra": [
    1,
    2
  ],
  "bundles": {
    "core": [
      "tdd"
    ]
  },
  "agents": {
    "zeta": {
      "note": "kept",
      "description": "Last in the alphabet",
      "skills": [
        "tdd"
      ]
    },
    "alpha": {
      "description": "First",
      "aliases": [
        "a"
      ],
      "skills": [
        "@core",
        "design"
      ],
      "mcp_servers": [
        "nb"
      ],
      "allowed_tools": [
        "Read",
        "Bash"
      ]
    }
  }
}
`
	if got := f.read("agents.json"); got != want {
		t.Fatalf("agents.json =\n%s\nwant\n%s", got, want)
	}
	change := out["change"].(map[string]any)
	if change["id"] == "" || !strings.Contains(change["summary"].(string), "alpha now has 2 skills") {
		t.Fatalf("change = %v", change)
	}
	w := list(change["writes"])[0].(map[string]any)
	if w["path"] != "agents.json" || w["before"] != handWrittenAgents || w["after"] != want {
		t.Fatalf("write = %v", w)
	}
	if out["versions"].(map[string]any)["agents.json"] != f.versions()["agents.json"] {
		t.Fatalf("versions = %v", out["versions"])
	}
}

func TestCatalogSave_CreatesTheFileWhenThereIsNoCatalogYet(t *testing.T) {
	f := newCatalogFixture(t)
	out := f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "first", "description": "d"}}, 200)
	if got, want := f.read("agents.json"), "{\n  \"agents\": {\n    \"first\": {\n      \"description\": \"d\"\n    }\n  }\n}\n"; got != want {
		t.Fatalf("agents.json = %q, want %q", got, want)
	}
	if w := list(out["change"].(map[string]any)["writes"])[0].(map[string]any); w["before"] != nil {
		t.Fatalf("before = %v, want null", w["before"])
	}
}

func TestCatalogSave_UpdatesAndRenamesAnAgentInPlace(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/save_catalog_agent", map[string]any{
		"previous_name": "zeta",
		"agent":         map[string]any{"name": "omega", "description": "Renamed", "entries": []string{"tdd", "design"}},
	}, 200)
	cat := f.get("/api/get_agent_catalog", 200)
	if len(list(cat["agents"])) != 1 {
		t.Fatalf("agents = %v", cat["agents"])
	}
	omega := byName(t, cat["agents"], "omega")
	if omega["description"] != "Renamed" || len(strList(omega["resolved_skills"])) != 2 {
		t.Fatalf("omega = %v", omega)
	}
	if !strings.Contains(f.read("agents.json"), `"note": "kept"`) {
		t.Fatal("the agent's unknown key was lost")
	}
}

func TestCatalogSave_RefusesWhatItCannotSave(t *testing.T) {
	f := catalogWithSkills(t)
	cases := []struct {
		name   string
		path   string
		body   map[string]any
		status int
		code   string
		field  string
	}{
		{"bad name", "save_catalog_agent", map[string]any{"agent": map[string]any{"name": "Bad Name", "description": "d"}}, 400, "INVALID_INPUT", "name"},
		{"no description", "save_catalog_agent", map[string]any{"agent": map[string]any{"name": "ok", "description": " "}}, 400, "INVALID_INPUT", "description"},
		{"name taken", "save_catalog_agent", map[string]any{"agent": map[string]any{"name": "zeta", "description": "d"}}, 409, "NAME_TAKEN", "name"},
		{"unknown previous", "save_catalog_agent", map[string]any{"previous_name": "nope", "agent": map[string]any{"name": "x", "description": "d"}}, 404, "AGENT_NOT_FOUND", ""},
		{"unknown skill", "save_catalog_agent", map[string]any{"agent": map[string]any{"name": "x", "description": "d", "entries": []string{"ghost"}}}, 400, "UNKNOWN_REFERENCE", ""},
		{"unknown server", "save_catalog_agent", map[string]any{"agent": map[string]any{"name": "x", "description": "d", "mcp_servers": []string{"ghost"}}}, 400, "UNKNOWN_REFERENCE", ""},
		{"unknown bundle", "save_catalog_agent", map[string]any{"agent": map[string]any{"name": "x", "description": "d", "entries": []string{"@ghost"}}}, 400, "UNKNOWN_REFERENCE", ""},
		{"delete unknown agent", "delete_catalog_agent", map[string]any{"name": "nope"}, 404, "AGENT_NOT_FOUND", ""},
		{"delete unknown bundle", "delete_catalog_bundle", map[string]any{"name": "nope"}, 404, "BUNDLE_NOT_FOUND", ""},
		{"bundle name taken", "save_catalog_bundle", map[string]any{"bundle": map[string]any{"name": "core"}}, 409, "NAME_TAKEN", "name"},
		{"bundle cycle", "save_catalog_bundle", map[string]any{"previous_name": "core", "bundle": map[string]any{"name": "core", "entries": []string{"@core"}}}, 400, "BUNDLE_CYCLE", ""},
	}
	before := f.read("agents.json")
	for _, c := range cases {
		got := f.post("/api/"+c.path, c.body, c.status)
		if got["code"] != c.code || got["error"] == "" {
			t.Errorf("%s: body = %v, want code %s", c.name, got, c.code)
		}
		if c.field != "" && got["field"] != c.field {
			t.Errorf("%s: field = %v, want %s", c.name, got["field"], c.field)
		}
	}
	if f.read("agents.json") != before {
		t.Fatal("a refused save wrote the file")
	}

	taken := f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "zeta", "description": "d"}}, 409)
	if taken["suggestion"] != "zeta-2" {
		t.Fatalf("suggestion = %v", taken["suggestion"])
	}
	unknown := f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "x", "description": "d", "entries": []string{"ghost", "@spirit"}}}, 400)
	if got := strList(unknown["names"]); !reflect.DeepEqual(got, []string{"ghost", "@spirit"}) {
		t.Fatalf("names = %v", got)
	}
}

func TestCatalogBundles_RenameRewritesEveryReferenceAndDeleteRemovesThem(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("agents.json", `{
  "bundles": { "core": ["tdd"], "wide": ["@core", "design"] },
  "agents": { "a": { "description": "d", "skills": ["@core", "design"] } }
}
`)
	out := f.post("/api/save_catalog_bundle", map[string]any{
		"previous_name": "core", "bundle": map[string]any{"name": "base", "entries": []string{"tdd", "design"}},
	}, 200)
	if !strings.Contains(out["change"].(map[string]any)["summary"].(string), "2 references updated") {
		t.Fatalf("summary = %v", out["change"])
	}
	cat := f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "a")["entries"]); !reflect.DeepEqual(got, []string{"@base", "design"}) {
		t.Fatalf("agent entries = %v", got)
	}
	if got := strList(byName(t, cat["bundles"], "wide")["entries"]); !reflect.DeepEqual(got, []string{"@base", "design"}) {
		t.Fatalf("wide entries = %v", got)
	}
	if got := strList(byName(t, cat["bundles"], "base")["entries"]); !reflect.DeepEqual(got, []string{"tdd", "design"}) {
		t.Fatalf("base entries = %v", got)
	}
	if got := []string{}; len(list(cat["bundles"])) != 2 {
		t.Fatalf("bundles = %v %v", cat["bundles"], got)
	}

	f.post("/api/delete_catalog_bundle", map[string]any{"name": "base"}, 200)
	cat = f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "a")["entries"]); !reflect.DeepEqual(got, []string{"design"}) {
		t.Fatalf("agent entries after delete = %v", got)
	}
	if got := strList(byName(t, cat["bundles"], "wide")["entries"]); !reflect.DeepEqual(got, []string{"design"}) {
		t.Fatalf("wide entries after delete = %v", got)
	}
}

func TestCatalogDelete_RemovesTheAgentAndKeepsTheRest(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/delete_catalog_agent", map[string]any{"name": "zeta"}, 200)
	got := f.read("agents.json")
	if strings.Contains(got, "zeta") || !strings.Contains(got, `"auto_sync": true`) || !strings.Contains(got, `"extra"`) {
		t.Fatalf("agents.json = %s", got)
	}
}

func TestCatalogDryRun_AnswersTheChangeAndWritesNothing(t *testing.T) {
	f := catalogWithSkills(t)
	before := f.read("agents.json")
	out := f.post("/api/save_catalog_agent", map[string]any{
		"dry_run": true, "agent": map[string]any{"name": "alpha", "description": "d"},
	}, 200)
	change := out["change"].(map[string]any)
	if change["id"] != "" {
		t.Fatalf("a dry run has id %v", change["id"])
	}
	w := list(change["writes"])[0].(map[string]any)
	if w["before"] != before || !strings.Contains(w["after"].(string), `"alpha"`) {
		t.Fatalf("write = %v", w)
	}
	if f.read("agents.json") != before {
		t.Fatal("a dry run wrote the file")
	}
	f.post("/api/undo_catalog_change", map[string]any{"change_id": ""}, 409)
}

func TestCatalogWrites_RefuseAChangeMadeSinceTheEditorOpenedUnlessForced(t *testing.T) {
	f := catalogWithSkills(t)
	opened := f.versions()
	outside := strings.Replace(handWrittenAgents, "Last in the alphabet", "Edited by hand", 1)
	f.write("agents.json", outside)

	body := map[string]any{"base_versions": opened, "agent": map[string]any{"name": "alpha", "description": "d"}}
	got := f.post("/api/save_catalog_agent", body, 409)
	if got["code"] != "CATALOG_CHANGED" {
		t.Fatalf("body = %v", got)
	}
	if cur := got["current_versions"].(map[string]any)["agents.json"]; cur == opened["agents.json"] || cur == "" {
		t.Fatalf("current_versions = %v", got["current_versions"])
	}
	changed := list(got["changed"])
	if len(changed) != 1 || changed[0].(map[string]any)["path"] != "agents.json" || changed[0].(map[string]any)["theirs"] != outside {
		t.Fatalf("changed = %v", changed)
	}
	if f.read("agents.json") != outside {
		t.Fatal("a refused save wrote the file")
	}

	body["force"] = true
	f.post("/api/save_catalog_agent", body, 200)
	if !strings.Contains(f.read("agents.json"), `"alpha"`) {
		t.Fatal("the forced save did not write")
	}
}

func TestCatalogWrites_AreRefusedWhileTheFileIsBroken(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("agents.json", "{ \"agents\": ")
	got := f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "alpha", "description": "d"}}, 409)
	if got["code"] != "CATALOG_BROKEN" || got["path"] != "agents.json" {
		t.Fatalf("body = %v", got)
	}
	if f.read("agents.json") != "{ \"agents\": " {
		t.Fatal("the broken file was written")
	}
}

func TestCatalogUndo_RestoresTheLatestChangeOnly(t *testing.T) {
	f := catalogWithSkills(t)
	first := f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "one", "description": "d"}}, 200)
	second := f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "two", "description": "d"}}, 200)
	afterSecond := f.read("agents.json")

	old := f.post("/api/undo_catalog_change", map[string]any{"change_id": first["change"].(map[string]any)["id"]}, 409)
	if old["code"] != "CHANGE_NOT_UNDOABLE" {
		t.Fatalf("body = %v", old)
	}
	unknown := f.post("/api/undo_catalog_change", map[string]any{"change_id": "nope"}, 409)
	if unknown["code"] != "CHANGE_NOT_UNDOABLE" {
		t.Fatalf("body = %v", unknown)
	}

	// A file changed since the change refuses the undo.
	f.write("agents.json", afterSecond+"\n")
	changed := f.post("/api/undo_catalog_change", map[string]any{"change_id": second["change"].(map[string]any)["id"]}, 409)
	if changed["code"] != "CATALOG_CHANGED" {
		t.Fatalf("body = %v", changed)
	}
	f.write("agents.json", afterSecond)

	undone := f.post("/api/undo_catalog_change", map[string]any{"change_id": second["change"].(map[string]any)["id"]}, 200)
	if strings.Contains(f.read("agents.json"), `"two"`) || !strings.Contains(f.read("agents.json"), `"one"`) {
		t.Fatalf("agents.json = %s", f.read("agents.json"))
	}
	if undone["change"].(map[string]any)["id"] == "" {
		t.Fatal("an undo has no change id")
	}
}

func TestCatalogWrites_PublishTheirPathsOnTheProjectEventStream(t *testing.T) {
	f := catalogWithSkills(t)
	cat := agentcatalog.New(f.dir)
	srv := httptest.NewServer(NewRouter(NewHandler(Services{Catalog: cat, ProjectFeed: quietProjectFeed{}})))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/project_events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				lines <- data
			}
		}
		close(lines)
	}()
	// The stream is open once its headers came; give the follow a moment.
	time.Sleep(100 * time.Millisecond)

	raw, _ := json.Marshal(map[string]any{"agent": map[string]any{"name": "alpha", "description": "d"}})
	if r, err := http.Post(srv.URL+"/api/save_catalog_agent", "application/json", bytes.NewReader(raw)); err != nil || r.StatusCode != 200 {
		t.Fatalf("save: %v %v", r, err)
	}
	select {
	case got := <-lines:
		if got != `{"agent_catalog_changed":{"paths":["agents.json"]}}` {
			t.Fatalf("event = %s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no agent_catalog_changed event")
	}
}

// quietProjectFeed is a project feed with no changes.
type quietProjectFeed struct{}

func (quietProjectFeed) FollowProjects(ctx context.Context) (<-chan ports.ProjectCatalogChange, error) {
	ch := make(chan ports.ProjectCatalogChange)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}
