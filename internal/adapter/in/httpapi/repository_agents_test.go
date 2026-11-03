package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/agentcatalog"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/repoagents"
	"operators-mcp/internal/domain"
)

// repoAgentsFixture is the catalog fixture plus the repository agents service
// on the same database, behind the real router.
type repoAgentsFixture struct {
	*catalogFixture
	svc *repoagents.Service
}

func newRepoAgentsFixture(t *testing.T) *repoAgentsFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	app := catalog.New(catalog.SQLiteDeps(db))
	f := &catalogFixture{t: t, dir: filepath.Join(t.TempDir(), "agents"), env: map[string]string{}, projects: app.Projects, prober: &fakeProber{}, db: db}
	cat := agentcatalog.New(f.dir, agentcatalog.WithProjects(app.Projects), agentcatalog.WithUsage(sqlite.NewAgentUsageRepository(db)), agentcatalog.WithProber(f.prober))
	svc := repoagents.New(repoagents.Deps{
		Lists:        sqlite.NewRepositoryAgentListRepository(db),
		Projects:     sqlite.NewProjectRepository(db),
		Repositories: sqlite.NewRepositoryRepository(db),
		Catalog:      cat,
		Sessions:     sqlite.NewSessionRepository(db),
	})
	f.router = NewRouter(NewHandler(Services{Catalog: cat, RepositoryAgents: svc, ProjectFeed: app.Projects}))
	f.write("agents.json", handWrittenAgents)
	for _, n := range []string{"alpha", "beta"} {
		f.post("/api/save_catalog_agent", map[string]any{"base_versions": f.versions(), "agent": map[string]any{"name": n, "description": n + " agent"}}, 200)
	}
	return &repoAgentsFixture{catalogFixture: f, svc: svc}
}

func (f *repoAgentsFixture) repository(projectID, name string) string {
	f.t.Helper()
	r, err := f.projects.CreateRepository(context.Background(), projectID, name, "", "https://example.test/"+name+".git", f.t.TempDir())
	if err != nil {
		f.t.Fatal(err)
	}
	return r.ID
}

func (f *repoAgentsFixture) tab(repo string) map[string]any {
	return f.get("/api/get_repository_agents?repository_id="+repo, 200)
}

func stateOf(t *testing.T, tab map[string]any, agent string) string {
	t.Helper()
	return byName(t, tab["agents"], agent)["state"].(string)
}

func TestRepositoryAgents_ARepositoryThatWasNeverWrittenAssignsEveryEnabledAgent(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	tab := f.tab(r)
	if tab["list_version"] != "" {
		t.Fatalf("list_version = %v", tab["list_version"])
	}
	if repo := tab["repository"].(map[string]any); repo["id"] != r || repo["name"] != "atlas-api" {
		t.Fatalf("repository = %v", repo)
	}
	if pr := tab["project"].(map[string]any); pr["name"] != "atlas" || pr["agents_mode"] != "all" || pr["enabled_count"] != float64(3) || pr["catalog_count"] != float64(3) {
		t.Fatalf("project = %v", pr)
	}
	for _, a := range []string{"alpha", "beta", "zeta"} {
		row := byName(t, tab["agents"], a)
		if row["state"] != "assigned" || row["unassigned"] != false || row["running_sessions"] != float64(0) {
			t.Fatalf("%s = %v", a, row)
		}
	}
	f.get("/api/get_repository_agents?repository_id=nope", 404)
}

func TestRepositoryAgents_UnassignKeepsEveryOtherAgentAndRepositoryAssigned(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	api, web := f.repository(p, "atlas-api"), f.repository(p, "atlas-web")

	out := f.post("/api/unassign_agent", map[string]any{"repository_id": api, "agent": "beta", "list_version": ""}, 200)
	if stateOf(t, out, "beta") != "unassigned" || stateOf(t, out, "alpha") != "assigned" {
		t.Fatalf("answer = %v", out["agents"])
	}
	if out["running_sessions"] != float64(0) || out["list_version"] == "" {
		t.Fatalf("answer = %v", out)
	}
	if got := stateOf(t, f.tab(web), "beta"); got != "assigned" {
		t.Fatalf("other repository: beta = %s", got)
	}
}

func TestRepositoryAgents_AssignAgainAndAssignAllAgainRestoreTheState(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	v := f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "alpha", "list_version": ""}, 200)["list_version"]
	v = f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": v}, 200)["list_version"]

	back := f.post("/api/assign_agent_again", map[string]any{"repository_id": r, "agent": "alpha", "list_version": v}, 200)
	if stateOf(t, back, "alpha") != "assigned" || stateOf(t, back, "beta") != "unassigned" || back["list_version"] == v {
		t.Fatalf("assign again = %v", back)
	}
	all := f.post("/api/assign_all_agents_again", map[string]any{"repository_id": r, "list_version": back["list_version"]}, 200)
	if stateOf(t, all, "beta") != "assigned" {
		t.Fatalf("assign all again = %v", all["agents"])
	}
}

func TestRepositoryAgents_AWriteThatChangesNothingKeepsTheVersion(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	v1 := f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "alpha", "list_version": ""}, 200)["list_version"]
	if v2 := f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "alpha", "list_version": v1}, 200)["list_version"]; v2 != v1 {
		t.Fatalf("unassign twice moved the version: %v -> %v", v1, v2)
	}
	v3 := f.post("/api/assign_agent_again", map[string]any{"repository_id": r, "agent": "alpha", "list_version": v1}, 200)["list_version"]
	if v4 := f.post("/api/assign_agent_again", map[string]any{"repository_id": r, "agent": "alpha", "list_version": v3}, 200)["list_version"]; v4 != v3 {
		t.Fatalf("assign again twice moved the version: %v -> %v", v3, v4)
	}
}

func TestRepositoryAgents_AStaleListVersionIsRefusedWithTheCurrentState(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "alpha", "list_version": ""}, 200)

	bad := f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": ""}, 409)
	if bad["code"] != "REPOSITORY_AGENTS_CHANGED" {
		t.Fatalf("body = %v", bad)
	}
	cur := bad["current"].(map[string]any)
	if stateOf(t, cur, "alpha") != "unassigned" || stateOf(t, cur, "beta") != "assigned" {
		t.Fatalf("current = %v", cur["agents"])
	}
	if got := stateOf(t, f.tab(r), "beta"); got != "assigned" {
		t.Fatalf("a refused write applied: beta = %s", got)
	}
	f.post("/api/assign_agent_again", map[string]any{"repository_id": r, "agent": "alpha", "list_version": ""}, 409)
	f.post("/api/assign_all_agents_again", map[string]any{"repository_id": r, "list_version": ""}, 409)
}

func TestRepositoryAgents_ANotEnabledAgentReadsNotEnabledAndStaysListed(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "selected", "agents": []string{"alpha"}}, 200)

	tab := f.tab(r)
	if pr := tab["project"].(map[string]any); pr["agents_mode"] != "selected" || pr["enabled_count"] != float64(1) {
		t.Fatalf("project = %v", pr)
	}
	// Only enabled or unassigned agents are listed.
	if got := len(list(tab["agents"])); got != 1 {
		t.Fatalf("agents = %v", tab["agents"])
	}
	// An agent can be unassigned although the project does not enable it.
	out := f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": ""}, 200)
	row := byName(t, out["agents"], "beta")
	if row["state"] != "not_enabled" || row["unassigned"] != true {
		t.Fatalf("beta = %v", row)
	}
}

func TestRepositoryAgents_RefusesAnUnknownAgentOrRepository(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "ghost", "list_version": ""}, 404)
	f.post("/api/unassign_agent", map[string]any{"repository_id": "nope", "agent": "alpha", "list_version": ""}, 404)
}

func TestRepositoryAgents_EachWritePublishesOneEvent(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := f.svc.FollowRepositoryAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": ""}, 200)
	select {
	case ev := <-events:
		want := domain.RepositoryAgentsChanged{ProjectID: p, RepositoryID: r, Agents: []string{"beta"}}
		if !reflect.DeepEqual(ev, want) {
			t.Fatalf("event = %+v, want %+v", ev, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestRepositoryAgents_TheEventReachesTheProjectStream(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(f.router)
	defer srv.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/project_events", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": ""}, 200)
	buf := make([]byte, 4096)
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		n, _ := resp.Body.Read(buf)
		got += string(buf[:n])
		if contains(got, "repository_agents_changed") {
			return
		}
	}
	t.Fatalf("stream = %q", got)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRepositoryAgents_TheRefusalMessageCarriesTheCodeAndTheAssignedAgents(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	if err := f.svc.CheckAssigned(context.Background(), r, "beta"); err != nil {
		t.Fatalf("an assigned agent was refused: %v", err)
	}
	v := f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": ""}, 200)["list_version"]

	err := f.svc.CheckAssigned(context.Background(), r, "beta")
	if err == nil {
		t.Fatal("an unassigned agent was accepted")
	}
	msg := err.Error()
	for _, want := range []string{"AGENT_NOT_ASSIGNED", "beta", "atlas-api", "zeta, alpha", "Only the person assigns agents"} {
		if !contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	var ce *domain.CatalogError
	if !errors.As(err, &ce) || !reflect.DeepEqual(ce.Fields["assigned"], []string{"zeta", "alpha"}) || ce.Fields["repository_name"] != "atlas-api" {
		t.Fatalf("fields = %+v", ce)
	}
	// With no agent left the message says so.
	for _, a := range []string{"alpha", "zeta"} {
		v = f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": a, "list_version": v}, 200)["list_version"]
	}
	if msg := f.svc.CheckAssigned(context.Background(), r, "beta").Error(); !contains(msg, "No agent is assigned there") {
		t.Fatalf("message = %q", msg)
	}
}
