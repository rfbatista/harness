//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/xeipuuv/gojsonschema"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
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
