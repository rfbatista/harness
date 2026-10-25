//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/xeipuuv/gojsonschema"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// archRecordServer is the server as it runs, on a real git repository, with
// the repository architecture API on a test server.
type archRecordServer struct {
	*archE2E
	cat      catalog.Catalog
	orch     *orchestration.Service
	dir      string
	checkout string
	repo     *domain.Repository
}

func startArchRecordServer(t *testing.T) *archRecordServer {
	t.Helper()
	dir := t.TempDir()
	s := &archRecordServer{dir: dir, checkout: filepath.Join(dir, "widgets")}
	gitInit(t, s.checkout)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&s.cat, &s.orch),
		fx.NopLogger,
	)
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: s.cat.Projects, Agents: s.cat.Agents, Architecture: s.cat.Architecture,
		RepositoryArchitectures: s.cat.RepositoryArchitectures, Sessions: s.orch,
	})))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	ctx := context.Background()
	p, err := s.cat.Projects.CreateProject(ctx, "widgets", s.checkout)
	if err != nil {
		t.Fatal(err)
	}
	if s.repo, err = s.cat.Projects.CreateRepository(ctx, p.ID, "widgets", "", "file://"+s.checkout, s.checkout); err != nil {
		t.Fatal(err)
	}
	s.archE2E = &archE2E{t: t, ctx: ctx, srv: srv, project: p.ID, repo: s.repo.ID}
	s.follow()
	return s
}

func (s *archRecordServer) agent(name string) {
	s.t.Helper()
	if _, err := s.cat.Agents.CreateAgent(context.Background(), name, "", "", nil, nil); err != nil {
		s.t.Fatal(err)
	}
}

// post calls POST /api/<verb>, answers the status and the JSON body.
func (s *archRecordServer) post(verb string, body any) (int, map[string]any) {
	s.t.Helper()
	raw, _ := json.Marshal(body)
	res, err := http.Post(s.srv.URL+"/api/"+verb, "application/json", bytes.NewReader(raw))
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	b, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func (s *archRecordServer) mustPost(verb string, body any) map[string]any {
	s.t.Helper()
	status, out := s.post(verb, body)
	if status >= 300 {
		s.t.Fatalf("%s: %d %v", verb, status, out)
	}
	return out
}

func (s *archRecordServer) get() map[string]any {
	return s.mustPost("get_repository_architecture", map[string]any{"repository_id": s.repo.ID})
}

func (s *archRecordServer) save(base int, record map[string]any) (int, map[string]any) {
	return s.post("save_repository_architecture", map[string]any{"repository_id": s.repo.ID, "base_version": base, "record": record})
}

// layerZones are the repository's zones as the zone catalog lists them.
func (s *archRecordServer) layerZones() []*domain.Zone {
	var out []*domain.Zone
	for _, z := range s.cat.Architecture.ListZones(context.Background(), s.repo.ProjectID) {
		if z.RepositoryID == s.repo.ID {
			out = append(out, z)
		}
	}
	return out
}

func sample() map[string]any {
	return map[string]any{
		"version": 1, "repository": "widgets", "pattern": "hexagonal",
		"instructions": []any{map[string]any{"path": "CLAUDE.md", "kind": "rules", "applies_to": []any{"all"}, "loaded": "always"}},
		"tools":        []any{map[string]any{"name": "codegraph", "kind": "mcp", "used_by": []any{"all"}, "writes": false, "usage": "Query the code graph."}},
		"sandboxes":    []any{map[string]any{"name": "seatbelt", "runs": "Every session.", "can_reach": []any{"the worktree"}, "cannot_reach": []any{"~/.ssh"}, "used_by": []any{"all"}}},
		"orchestration": map[string]any{
			"routing":  []any{map[string]any{"agent": "go-developer", "model": "claude-opus-5-5"}},
			"handoffs": []any{map[string]any{"from": "software-architect", "to": "go-developer", "when": "A spec is approved."}},
		},
		"layers": []any{
			map[string]any{"name": "domain", "match": map[string]any{"folder": "internal/domain"}, "purpose": "Pure domain types.",
				"access": map[string]any{"default": "read", "agents": map[string]any{"software-architect": "change", "plain": "read"}}},
			map[string]any{"name": "ports", "match": map[string]any{"regex": "^internal/(domain|ports)/"},
				"access": map[string]any{"default": "read", "agents": map[string]any{"go-developer": "change"}}},
		},
		"unlayered": map[string]any{"default": "read", "agents": map[string]any{"software-architect": "change", "devops": "change"}},
		"rules": []any{
			map[string]any{"id": "stay-in-your-layer", "kind": "edit_outside_layer", "mode": "blocking", "message": "Ask the architect."},
			map[string]any{"id": "archtest", "kind": "command", "at": "before_commit", "command": "go test ./...", "mode": "warning", "message": "The architecture test failed."},
		},
	}
}

func clone(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func errorPaths(body map[string]any) map[string]string {
	out := map[string]string{}
	list, _ := body["errors"].([]any)
	for _, e := range list {
		m := e.(map[string]any)
		out[m["path"].(string)] = m["code"].(string)
	}
	return out
}

func TestRepositoryArchitecture_StarterRecordWhenNoneIsSaved(t *testing.T) {
	s := startArchRecordServer(t)
	s.agent("software-architect")
	s.agent("devops")
	got := s.get()
	if got["exists"] != false || got["version"] != float64(0) {
		t.Fatalf("exists/version = %v/%v", got["exists"], got["version"])
	}
	rec := got["record"].(map[string]any)
	un := rec["unlayered"].(map[string]any)
	agents := un["agents"].(map[string]any)
	if un["default"] != "read" || agents["software-architect"] != "change" || agents["devops"] != "change" {
		t.Errorf("unlayered = %v", un)
	}
	rules := rec["rules"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["kind"] != "edit_outside_layer" || rules[0].(map[string]any)["mode"] != "warning" {
		t.Errorf("rules = %v", rules)
	}
	if l, _ := rec["layers"].([]any); len(l) != 0 {
		t.Errorf("layers = %v", l)
	}
	// The starter can be saved as it is.
	if status, body := s.save(0, rec); status != http.StatusOK {
		t.Fatalf("saving the starter: %d %v", status, body)
	}
}

func TestRepositoryArchitecture_UnknownRepositoryIsNotFound(t *testing.T) {
	s := startArchRecordServer(t)
	status, body := s.post("get_repository_architecture", map[string]any{"repository_id": "nope"})
	if status != http.StatusNotFound || body["code"] != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("%d %v", status, body)
	}
}

func TestRepositoryArchitecture_EachSaveIsANewVersionAndAStaleOneIsRefused(t *testing.T) {
	s := startArchRecordServer(t)
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		s.agent(a)
	}
	status, saved := s.save(0, sample())
	if status != http.StatusOK || saved["version"] != float64(1) || saved["exists"] != true {
		t.Fatalf("first save: %d %v", status, saved)
	}
	if saved["export"].(map[string]any)["state"] != "never_exported" {
		t.Errorf("export = %v", saved["export"])
	}
	again := clone(sample())
	again["pattern"] = "layered"
	status, saved = s.save(1, again)
	if status != http.StatusOK || saved["version"] != float64(2) {
		t.Fatalf("second save: %d %v", status, saved)
	}

	stale := clone(sample())
	stale["pattern"] = "library"
	status, body := s.save(1, stale)
	if status != http.StatusConflict || body["code"] != "ARCHITECTURE_CHANGED" {
		t.Fatalf("stale save: %d %v", status, body)
	}
	if got := s.get(); got["version"] != float64(2) || got["record"].(map[string]any)["pattern"] != "layered" {
		t.Errorf("a refused save changed the record: %v", got)
	}
	// Creating over an existing record is stale too.
	if status, body = s.save(0, stale); status != http.StatusConflict || body["code"] != "ARCHITECTURE_CHANGED" {
		t.Errorf("save with base_version 0: %d %v", status, body)
	}
}

func TestRepositoryArchitecture_AnInvalidRecordListsEveryProblemByField(t *testing.T) {
	s := startArchRecordServer(t)
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		s.agent(a)
	}
	bad := clone(sample())
	layers := bad["layers"].([]any)
	layers = append(layers,
		map[string]any{"name": "domain", "match": map[string]any{"folder": "x"}},                                                                                           // 2: duplicate
		map[string]any{"name": "rx", "match": map[string]any{"regex": "("}},                                                                                                // 3: bad regex
		map[string]any{"name": "abs", "match": map[string]any{"folder": "/etc"}},                                                                                           // 4: absolute folder
		map[string]any{"name": "dots", "match": map[string]any{"folder": "a/../b"}},                                                                                        // 5: ..
		map[string]any{"name": "git", "match": map[string]any{"folder": ".git/hooks"}},                                                                                     // 6: inside .git
		map[string]any{"name": "ghost", "match": map[string]any{"folder": "g"}, "access": map[string]any{"default": "read", "agents": map[string]any{"nobody": "change"}}}, // 7: unknown agent
		map[string]any{"name": "ctx", "match": map[string]any{"folder": "c"}, "bounded_context": "no-such-context"},                                                        // 8
		map[string]any{"name": "nomatch"}, // 9: missing match
		map[string]any{"name": "lvl", "match": map[string]any{"folder": "l"}, "access": map[string]any{"default": "write"}}, // 10: bad enum
	)
	bad["layers"] = layers
	bad["rules"] = append(bad["rules"].([]any),
		map[string]any{"id": "archtest", "kind": "command", "at": "before_commit", "command": "x", "mode": "warning", "message": "m"}, // 2: duplicate id
		map[string]any{"id": "nomsg", "kind": "command", "at": "before_commit", "command": "x", "mode": "warning"},                    // 3: no message
		map[string]any{"id": "nocmd", "kind": "command", "at": "after_edit", "mode": "warning", "message": "m"},                       // 4: no command
		map[string]any{"id": "slow", "kind": "command", "at": "after_edit", "command": "x", "mode": "warning", "message": "m", "timeout_seconds": 301},
		map[string]any{"id": "badrx", "kind": "command", "at": "after_edit", "command": "x", "mode": "warning", "message": "m", "paths": "("},
		map[string]any{"id": "second", "kind": "edit_outside_layer", "mode": "warning", "message": "m"}, // a second edit_outside_layer
		map[string]any{"id": "mode", "kind": "command", "at": "after_edit", "command": "x", "mode": "loud", "message": "m"},
	)
	bad["tools"] = []any{map[string]any{"name": "writer", "kind": "mcp", "writes": true}}

	status, body := s.save(0, bad)
	if status != http.StatusBadRequest || body["code"] != "ARCHITECTURE_INVALID" {
		t.Fatalf("%d %v", status, body)
	}
	want := map[string]string{
		"/layers/2/name":                 "DUPLICATE_NAME",
		"/layers/3/match/regex":          "INVALID_REGEX",
		"/layers/4/match/folder":         "INVALID_FOLDER",
		"/layers/5/match/folder":         "INVALID_FOLDER",
		"/layers/6/match/folder":         "INVALID_FOLDER",
		"/layers/7/access/agents/nobody": "UNKNOWN_AGENT",
		"/layers/8/bounded_context":      "UNKNOWN_BOUNDED_CONTEXT",
		"/layers/9/match":                "MISSING_FIELD",
		"/layers/10/access/default":      "INVALID_VALUE",
		"/rules/2/id":                    "DUPLICATE_NAME",
		"/rules/3/message":               "MISSING_FIELD",
		"/rules/4/command":               "MISSING_FIELD",
		"/rules/5/timeout_seconds":       "INVALID_VALUE",
		"/rules/6/paths":                 "INVALID_REGEX",
		"/rules/7":                       "TOO_MANY",
		"/rules/8/mode":                  "INVALID_VALUE",
		"/tools/0/usage":                 "MISSING_FIELD",
	}
	got := errorPaths(body)
	for path, code := range want {
		if got[path] != code {
			t.Errorf("%s: got %q, want %s (all: %v)", path, got[path], code, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d errors, want %d: %v", len(got), len(want), got)
	}
	if s.get()["exists"] != false {
		t.Error("an invalid save stored a record")
	}
	if len(s.layerZones()) != 0 {
		t.Error("an invalid save stored zones")
	}

	tooMany := clone(sample())
	var many []any
	for i := 0; i < 101; i++ {
		many = append(many, map[string]any{"name": "l" + string(rune('a'+i%26)) + string(rune('a'+i/26)), "match": map[string]any{"folder": "f" + string(rune('a'+i%26)) + string(rune('a'+i/26))}})
	}
	tooMany["layers"] = many
	_, body = s.save(0, tooMany)
	if got := errorPaths(body); got["/layers"] != "TOO_MANY" {
		t.Errorf("101 layers: %v", got)
	}
}

func TestRepositoryArchitecture_LayersBecomeTheRepositorysZones(t *testing.T) {
	s := startArchRecordServer(t)
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		s.agent(a)
	}
	other, err := s.cat.Architecture.CreateZone(s.repo.ProjectID, "Legacy", "^legacy/", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status, body := s.save(0, sample()); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	zones := s.layerZones()
	if len(zones) != 2 {
		t.Fatalf("layer zones = %d", len(zones))
	}
	byName := map[string]*domain.Zone{}
	for _, z := range zones {
		byName[z.Name] = z
	}
	d, p := byName["domain"], byName["ports"]
	if d == nil || p == nil {
		t.Fatalf("zones = %v", byName)
	}
	if !slices.Equal(d.ExplicitPaths, []string{"internal/domain"}) || d.Pattern != "" {
		t.Errorf("a folder layer is an explicit path: %+v", d)
	}
	if p.Pattern != "^internal/(domain|ports)/" || len(p.ExplicitPaths) != 0 {
		t.Errorf("a regex layer is a pattern: %+v", p)
	}
	if d.Access == nil || d.Access.Default != domain.AccessRead || d.Access.Agents["software-architect"] != domain.AccessChange {
		t.Errorf("access = %+v", d.Access)
	}

	// A re-save replaces the layers; zones with no repository stay.
	next := clone(sample())
	next["layers"] = next["layers"].([]any)[:1]
	if status, body := s.save(1, next); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	if zones = s.layerZones(); len(zones) != 1 || zones[0].Name != "domain" {
		t.Errorf("after the re-save: %+v", zones)
	}
	if z := s.cat.Architecture.GetZone(other.ID); z == nil || z.RepositoryID != "" {
		t.Errorf("a zone with no repository changed: %+v", z)
	}

	// Delete removes the record and its zones.
	s.mustPost("delete_repository_architecture", map[string]any{"repository_id": s.repo.ID})
	if s.get()["exists"] != false || len(s.layerZones()) != 0 {
		t.Error("delete left the record or its zones")
	}
	if s.cat.Architecture.GetZone(other.ID) == nil {
		t.Error("delete removed a zone with no repository")
	}

	// Deleting the repository does the same.
	if status, body := s.save(0, sample()); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	if err := s.cat.Projects.DeleteRepository(context.Background(), s.repo.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.layerZones()) != 0 {
		t.Error("deleting the repository left its zones")
	}
}

func TestRepositoryArchitecture_ARenamedAgentIsRefusedOnTheNextSaveButTheRecordStillReads(t *testing.T) {
	s := startArchRecordServer(t)
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		s.agent(a)
	}
	if status, body := s.save(0, sample()); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	agents, err := s.cat.Agents.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.Name == "go-developer" {
			if _, err := s.cat.Agents.UpdateAgent(context.Background(), a.ID, "golang-developer", "", "", nil, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := s.get()
	if got["exists"] != true || got["version"] != float64(1) {
		t.Fatalf("the stored record no longer reads: %v", got)
	}
	status, body := s.save(1, got["record"].(map[string]any))
	if status != http.StatusBadRequest || body["code"] != "ARCHITECTURE_INVALID" {
		t.Fatalf("%d %v", status, body)
	}
	if code := errorPaths(body)["/layers/1/access/agents/go-developer"]; code != "UNKNOWN_AGENT" {
		t.Errorf("errors = %v", errorPaths(body))
	}
}

func TestRepositoryArchitecture_SchemaIsServedAndValidatesARecord(t *testing.T) {
	s := startArchRecordServer(t)
	res, err := http.Get(s.srv.URL + "/api/architecture.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	schema, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	check := func(doc any) *gojsonschema.Result {
		r, err := gojsonschema.Validate(gojsonschema.NewBytesLoader(schema), gojsonschema.NewGoLoader(doc))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := check(sample()); !r.Valid() {
		t.Errorf("the contract's example does not satisfy the schema: %v", r.Errors())
	}
	broken := clone(sample())
	broken["layers"].([]any)[0].(map[string]any)["match"] = map[string]any{"folder": "a", "regex": "b"}
	if r := check(broken); r.Valid() {
		t.Error("a layer with both a folder and a regex satisfies the schema")
	}
}

func TestRepositoryArchitecture_SavesAndDeletesAreOnTheProjectFeed(t *testing.T) {
	s := startArchRecordServer(t)
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		s.agent(a)
	}
	if status, body := s.save(0, sample()); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	s.sawOnFeed("repository_architecture_changed", `"repository_id":"`+s.repo.ID+`"`, `"version":1`)
	s.mustPost("delete_repository_architecture", map[string]any{"repository_id": s.repo.ID})
	s.sawOnFeed("repository_architecture_changed", `"repository_id":"`+s.repo.ID+`"`, `"version":0`)
}

// commitFiles writes files into the checkout and commits them on main.
func (s *archRecordServer) commitFiles(files map[string]string) {
	s.t.Helper()
	for name, body := range files {
		path := filepath.Join(s.checkout, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			s.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			s.t.Fatal(err)
		}
	}
	gitE2E(s.t, s.checkout, "add", ".")
	gitE2E(s.t, s.checkout, "commit", "-q", "-m", "add files")
}

func numbered(prefix string, n int) map[string]string {
	out := map[string]string{}
	for i := 0; i < n; i++ {
		out[prefix+string(rune('a'+i/26))+string(rune('a'+i%26))+".go"] = "package x\n"
	}
	return out
}

func merge(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func TestRepositoryArchitecture_PreviewCountsSamplesAndOverlapsOfADraft(t *testing.T) {
	s := startArchRecordServer(t)
	s.commitFiles(merge(
		numbered("internal/domain/", 60),
		numbered("internal/ports/", 3),
		map[string]string{"cmd/main.go": "package main\n", "README.md": "# widgets\n", "vendor/lib/a.go": "package a\n"},
	))
	if _, err := s.cat.Projects.AddRepositoryIgnoredPath(context.Background(), s.repo.ID, "vendor"); err != nil {
		t.Fatal(err)
	}
	// Uncommitted files are not in the default branch.
	if err := os.WriteFile(filepath.Join(s.checkout, "scratch.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	layers := []any{
		map[string]any{"name": "domain", "match": map[string]any{"folder": "internal/domain"}},
		map[string]any{"name": "ports", "match": map[string]any{"regex": "^internal/(domain|ports)/"}},
		map[string]any{"name": "cmd", "match": map[string]any{"folder": "cmd"}},
		map[string]any{"name": "nothing", "match": map[string]any{"folder": "no/such"}},
		map[string]any{"name": "broken", "match": map[string]any{"regex": "("}},
		map[string]any{"name": "escape", "match": map[string]any{"folder": "../x"}},
	}
	got := s.mustPost("preview_layers", map[string]any{
		"repository_id": s.repo.ID, "layers": layers, "unlayered": map[string]any{"default": "read"},
	})

	byName := map[string]map[string]any{}
	for _, l := range got["layers"].([]any) {
		m := l.(map[string]any)
		byName[m["name"].(string)] = m
	}
	count := func(name string) float64 { return byName[name]["file_count"].(float64) }
	if count("domain") != 0 || count("ports") != 63 || count("cmd") != 1 || count("nothing") != 0 {
		t.Errorf("counts: domain %v ports %v cmd %v nothing %v", count("domain"), count("ports"), count("cmd"), count("nothing"))
	}
	if sample := byName["ports"]["sample"].([]any); len(sample) != 20 {
		t.Errorf("a sample has at most 20 paths: %d", len(sample))
	}
	if sample := byName["cmd"]["sample"].([]any); len(sample) != 1 || sample[0] != "cmd/main.go" {
		t.Errorf("cmd sample = %v", sample)
	}
	if byName["ports"]["error"] != nil || byName["cmd"]["error"] != nil {
		t.Error("a good layer has an error")
	}
	if e, _ := byName["broken"]["error"].(map[string]any); e["code"] != "INVALID_REGEX" || count("broken") != 0 {
		t.Errorf("broken = %v", byName["broken"])
	}
	if e, _ := byName["escape"]["error"].(map[string]any); e["code"] != "INVALID_FOLDER" || count("escape") != 0 {
		t.Errorf("escape = %v", byName["escape"])
	}

	// Paths in no layer: the README and the root's .gitignore-free files, not
	// the ignored vendor folder and not the uncommitted file.
	un := got["unlayered"].(map[string]any)
	if un["file_count"] != float64(1) || un["sample"].([]any)[0] != "README.md" {
		t.Errorf("unlayered = %v", un)
	}

	overlaps := got["overlaps"].([]any)
	if len(overlaps) != 50 {
		t.Fatalf("overlaps are capped at 50: %d", len(overlaps))
	}
	o := overlaps[0].(map[string]any)
	if o["winner"] != "ports" || len(o["layers"].([]any)) != 2 || o["layers"].([]any)[0] != "ports" || o["layers"].([]any)[1] != "domain" {
		t.Errorf("overlap = %v", o)
	}
	if p, _ := o["path"].(string); len(p) < len("internal/domain/") || p[:16] != "internal/domain/" {
		t.Errorf("overlap path = %v", o["path"])
	}
	if s.get()["exists"] != false {
		t.Error("a preview stored something")
	}
}

func TestRepositoryArchitecture_DiscoveryFindsInstructionsToolsAndSandboxesWithoutWriting(t *testing.T) {
	s := startArchRecordServer(t)
	s.agent("go-developer")
	s.agent("software-architect")
	s.cat.RepositoryArchitectures.UseSandbox(func() ports.SandboxDriver { return fakeSandboxDriver{} }, fakeSandboxSettings{})
	if _, err := s.cat.Capabilities.CreateMCPServer(domain.MCPServerInput{Name: "playwright", Transport: domain.MCPTransportStdio, Command: "npx"}); err != nil {
		t.Fatal(err)
	}
	s.commitFiles(map[string]string{
		"CLAUDE.md":                      "x",
		"AGENTS.md":                      "x",
		"web/GEMINI.md":                  "x",
		"services/api/AGENTS.md":         "x",
		".claude/skills/review/SKILL.md": "x",
		".claude/agents/reviewer.md":     "x",
		".claude/agents/notes.txt":       "x",
		"docs/CLAUDE.md.bak":             "x",
		".mcp.json":                      `{"mcpServers":{"codegraph":{"command":"codegraph"},"browser":{"url":"http://localhost:1"}}}`,
		"main.go":                        "package main\n",
	})
	before := gitE2E(t, s.checkout, "rev-parse", "HEAD")

	got := s.mustPost("discover_repository_architecture", map[string]any{"repository_id": s.repo.ID})

	type cand struct{ kind, loaded, source string }
	instr := map[string]cand{}
	for _, c := range got["instructions"].([]any) {
		m := c.(map[string]any)
		instr[m["path"].(string)] = cand{m["kind"].(string), m["loaded"].(string), ""}
	}
	want := map[string]cand{
		"CLAUDE.md": {"rules", "always", ""}, "AGENTS.md": {"rules", "always", ""}, "web/GEMINI.md": {"rules", "always", ""},
		"services/api/AGENTS.md": {"rules", "always", ""}, ".claude/skills/review/SKILL.md": {"skill", "on_demand", ""},
		".claude/agents/reviewer.md": {"subagent_prompt", "on_demand", ""},
	}
	for path, c := range want {
		if instr[path] != c {
			t.Errorf("instruction %s = %+v, want %+v", path, instr[path], c)
		}
	}
	if _, ok := instr["docs/CLAUDE.md.bak"]; ok || len(instr) < len(want) {
		t.Errorf("instructions = %v", instr)
	}

	tools := map[string]string{}
	for _, c := range got["tools"].([]any) {
		m := c.(map[string]any)
		tools[m["name"].(string)] = m["kind"].(string) + "/" + m["source"].(string)
	}
	for name, w := range map[string]string{"codegraph": "mcp/.mcp.json", "browser": "mcp/.mcp.json", "playwright": "mcp/mcp catalog"} {
		if tools[name] != w {
			t.Errorf("tool %s = %q, want %q (all: %v)", name, tools[name], w, tools)
		}
	}
	if sb := got["sandboxes"].([]any); len(sb) != 1 || sb[0].(map[string]any)["name"] != "fake-sandbox" || sb[0].(map[string]any)["source"] != "sandbox settings" {
		t.Errorf("sandboxes = %v", sb)
	}
	agents := got["agents"].([]any)
	if len(agents) != 2 || agents[0] != "go-developer" || agents[1] != "software-architect" {
		t.Errorf("agents = %v", agents)
	}

	if after := gitE2E(t, s.checkout, "rev-parse", "HEAD"); after != before || gitE2E(t, s.checkout, "status", "--porcelain") != "" {
		t.Error("discovery changed the checkout")
	}
	if s.get()["exists"] != false {
		t.Error("discovery stored a record")
	}
}

type fakeSandboxDriver struct{}

func (fakeSandboxDriver) Name() string { return "fake-sandbox" }
func (fakeSandboxDriver) Wrap(_ ports.SandboxPolicy, cmd ports.SandboxCommand) (ports.SandboxCommand, error) {
	return cmd, nil
}

type fakeSandboxSettings struct{}

func (fakeSandboxSettings) Enabled() bool { return true }
func (fakeSandboxSettings) Paths() []ports.SandboxPath {
	return []ports.SandboxPath{{Path: "/opt/tools", Write: false}}
}

// exportServer is the architecture server on a checkout whose origin is a
// local bare repository, so a test can prove an export never pushes.
func startExportServer(t *testing.T) (*archRecordServer, string) {
	t.Helper()
	s := startArchRecordServer(t)
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		s.agent(a)
	}
	bare := filepath.Join(s.dir, "origin.git")
	gitE2E(t, s.dir, "init", "-q", "--bare", bare)
	gitE2E(t, s.checkout, "remote", "add", "origin", bare)
	gitE2E(t, s.checkout, "push", "-q", "origin", "main")
	return s, bare
}

func (s *archRecordServer) export(version int) (int, map[string]any) {
	return s.post("export_repository_architecture", map[string]any{"repository_id": s.repo.ID, "version": version})
}

func exportState(got map[string]any) string {
	return got["export"].(map[string]any)["state"].(string)
}

func TestRepositoryArchitecture_ExportCommitsTheFileAndTheSchemaOnTheDefaultBranchWithoutPushing(t *testing.T) {
	s, bare := startExportServer(t)
	if status, body := s.save(0, sample()); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	before := gitE2E(t, s.checkout, "rev-parse", "HEAD")
	remoteBefore := gitE2E(t, bare, "rev-parse", "main")

	status, got := s.export(1)
	if status != http.StatusOK {
		t.Fatalf("%d %v", status, got)
	}
	head := gitE2E(t, s.checkout, "rev-parse", "HEAD")
	if got["commit"] != head || got["path"] != ".harness/architecture.yaml" || head == before {
		t.Errorf("commit %v path %v head %s before %s", got["commit"], got["path"], head, before)
	}
	if n := gitE2E(t, s.checkout, "rev-list", "--count", before+"..HEAD"); n != "1" {
		t.Errorf("%s commits", n)
	}
	if msg := gitE2E(t, s.checkout, "log", "-1", "--format=%s"); msg != "chore(architecture): export repository architecture v1" {
		t.Errorf("message = %q", msg)
	}
	if who := gitE2E(t, s.checkout, "log", "-1", "--format=%an <%ae>|%cn"); who != "harness <harness@harness.local>|harness" {
		t.Errorf("author = %q", who)
	}
	if files := gitE2E(t, s.checkout, "show", "--name-only", "--format=", "HEAD"); files != ".harness/architecture.schema.json\n.harness/architecture.yaml" {
		t.Errorf("files = %q", files)
	}
	yamlText, err := os.ReadFile(filepath.Join(s.checkout, ".harness", "architecture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	const header = "# Generated by the harness from the repository architecture. Edit it in the harness; changes here are not read back yet.\n"
	if len(yamlText) < len(header) || string(yamlText[:len(header)]) != header {
		t.Errorf("the file starts %q", yamlText[:min(len(yamlText), 60)])
	}
	res, err := http.Get(s.srv.URL + "/api/architecture.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if written, _ := os.ReadFile(filepath.Join(s.checkout, ".harness", "architecture.schema.json")); !bytes.Equal(written, served) {
		t.Error("the schema written is not the schema served")
	}
	if remote := gitE2E(t, bare, "rev-parse", "main"); remote != remoteBefore {
		t.Error("the export pushed")
	}
	if gitE2E(t, s.checkout, "status", "--porcelain") != "" {
		t.Error("the checkout is not clean after the export")
	}
	if exp := got["export"].(map[string]any); exp["state"] != "in_sync" || exp["exported_commit"] != head || exp["exported_version"] != float64(1) {
		t.Errorf("export = %v", exp)
	}
	s.sawOnFeed("repository_architecture_changed", `"repository_id":"`+s.repo.ID+`"`, `"version":1`)
}

func TestRepositoryArchitecture_ExportIsRefusedWithAReasonAndLeavesTheRepositoryAsItWas(t *testing.T) {
	s, _ := startExportServer(t)
	if status, body := s.export(1); status != http.StatusNotFound || body["code"] != "ARCHITECTURE_NOT_FOUND" {
		t.Errorf("no record: %d %v", status, body)
	}
	if status, body := s.save(0, sample()); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	head := gitE2E(t, s.checkout, "rev-parse", "HEAD")
	unchanged := func(step string) {
		t.Helper()
		if gitE2E(t, s.checkout, "rev-parse", "HEAD") != head {
			t.Errorf("%s: HEAD moved", step)
		}
		if _, err := os.Stat(filepath.Join(s.checkout, ".harness")); err == nil {
			t.Errorf("%s: .harness was written", step)
		}
	}

	if status, body := s.export(7); status != http.StatusConflict || body["code"] != "ARCHITECTURE_CHANGED" {
		t.Errorf("stale: %d %v", status, body)
	}
	unchanged("stale")

	if err := os.WriteFile(filepath.Join(s.checkout, "mine.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, body := s.export(1); status != http.StatusConflict || body["code"] != "CHECKOUT_DIRTY" {
		t.Errorf("dirty: %d %v", status, body)
	}
	unchanged("dirty")
	if err := os.Remove(filepath.Join(s.checkout, "mine.txt")); err != nil {
		t.Fatal(err)
	}

	gitE2E(t, s.checkout, "checkout", "-q", "-b", "feature")
	if status, body := s.export(1); status != http.StatusConflict || body["code"] != "WRONG_BRANCH" {
		t.Errorf("wrong branch: %d %v", status, body)
	}
	unchanged("wrong branch")
	gitE2E(t, s.checkout, "checkout", "-q", "main")

	if status, body := s.export(1); status != http.StatusOK {
		t.Errorf("after the refusals: %d %v", status, body)
	}
}

func TestRepositoryArchitecture_ExportingAnUnchangedFileCommitsNothing(t *testing.T) {
	s, _ := startExportServer(t)
	if status, body := s.save(0, sample()); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	_, first := s.export(1)
	count := gitE2E(t, s.checkout, "rev-list", "--count", "HEAD")

	_, again := s.export(1)
	if again["commit"] != first["commit"] || gitE2E(t, s.checkout, "rev-list", "--count", "HEAD") != count {
		t.Errorf("a second export of the same version committed: %v then %v", first["commit"], again["commit"])
	}

	// A save that changes nothing in the file's content is a new version with
	// the same YAML: exporting it commits nothing either.
	status, body := s.save(1, sample())
	if status != http.StatusOK || body["version"] != float64(2) {
		t.Fatalf("%d %v", status, body)
	}
	_, third := s.export(2)
	if third["commit"] != first["commit"] || gitE2E(t, s.checkout, "rev-list", "--count", "HEAD") != count {
		t.Errorf("an unchanged file was committed again: %v", third["commit"])
	}
	if exp := third["export"].(map[string]any); exp["state"] != "in_sync" || exp["exported_version"] != float64(2) {
		t.Errorf("export = %v", exp)
	}
}

func TestRepositoryArchitecture_ExportStateTellsWhetherTheFileMatchesTheRecord(t *testing.T) {
	s, _ := startExportServer(t)
	if state := exportState(s.get()); state != "never_exported" {
		t.Errorf("no record: %s", state)
	}
	if status, body := s.save(0, sample()); status != http.StatusOK || exportState(body) != "never_exported" {
		t.Fatalf("after a save: %d %v", status, body)
	}
	s.mustPost("export_repository_architecture", map[string]any{"repository_id": s.repo.ID, "version": 1})
	if state := exportState(s.get()); state != "in_sync" {
		t.Errorf("after the export: %s", state)
	}

	next := clone(sample())
	next["pattern"] = "layered"
	if status, body := s.save(1, next); status != http.StatusOK || exportState(body) != "database_ahead" {
		t.Fatalf("after a new save: %d %v", status, exportState(body))
	}

	file := filepath.Join(s.checkout, ".harness", "architecture.yaml")
	original, _ := os.ReadFile(file)
	edit := func(content string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitE2E(t, s.checkout, "commit", "-q", "-am", "edit")
	}
	edit(string(original) + "# edited by hand\n")
	if state := exportState(s.get()); state != "edited_outside" {
		t.Errorf("after a manual edit: %s", state)
	}
	edit("layers: [unterminated\n")
	if state := exportState(s.get()); state != "file_invalid" {
		t.Errorf("after an unparsable edit: %s", state)
	}
	edit("version: 1\nlayers: 3\n")
	if state := exportState(s.get()); state != "file_invalid" {
		t.Errorf("after an edit that breaks the schema: %s", state)
	}
	edit(string(original))
	if state := exportState(s.get()); state != "database_ahead" {
		t.Errorf("after restoring the exported file: %s", state)
	}
}
