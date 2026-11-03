package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/agentcatalog"
	"operators-mcp/internal/adapter/out/httpclient"
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
		Prompts:      sqlite.NewRepositoryPromptRepository(db),
	})
	cat.UseAgentRenames(svc)
	svc.Subscribe(app.Bus)
	f.router = NewRouter(NewHandler(Services{Catalog: cat, RepositoryAgents: svc, ProjectFeed: app.Projects, Projects: app.Projects, Agents: app.Agents}))
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

func (f *repoAgentsFixture) prompt(repo, agent string) map[string]any {
	return f.get("/api/get_repository_prompt?repository_id="+repo+"&agent="+agent, 200)
}

func (f *repoAgentsFixture) save(repo, agent, content, version string, status int) map[string]any {
	return f.post("/api/save_repository_prompt", map[string]any{"repository_id": repo, "agent": agent, "content": content, "version": version}, status)
}

func TestRepositoryPrompt_SaveReadAndSaveAgainWithTheNewVersion(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	if got := f.prompt(r, "alpha"); got["content"] != "" || got["version"] != "" || got["limit"] != float64(8000) || got["unassigned"] != false || got["agent"] != "alpha" || got["repository_id"] != r {
		t.Fatalf("never saved = %v", got)
	}
	saved := f.save(r, "alpha", "Run make check before every commit.", "", 200)
	if saved["content"] != "Run make check before every commit." || saved["version"] == "" {
		t.Fatalf("saved = %v", saved)
	}
	if got := f.prompt(r, "alpha"); got["content"] != saved["content"] || got["version"] != saved["version"] {
		t.Fatalf("read = %v", got)
	}
	again := f.save(r, "alpha", "Second text", saved["version"].(string), 200)
	if again["version"] == saved["version"] || again["content"] != "Second text" {
		t.Fatalf("again = %v", again)
	}
	// Another agent in the same repository is untouched.
	if got := f.prompt(r, "beta"); got["content"] != "" {
		t.Fatalf("beta = %v", got)
	}
}

func TestRepositoryPrompt_AStaleVersionIsRefusedWithTheCurrentPrompt(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	saved := f.save(r, "alpha", "first", "", 200)
	bad := f.save(r, "alpha", "mine", "", 409)
	if bad["code"] != "REPOSITORY_PROMPT_CHANGED" {
		t.Fatalf("body = %v", bad)
	}
	cur := bad["current"].(map[string]any)
	if cur["content"] != "first" || cur["version"] != saved["version"] {
		t.Fatalf("current = %v", cur)
	}
	if got := f.prompt(r, "alpha"); got["content"] != "first" {
		t.Fatalf("a refused save applied: %v", got)
	}
}

func TestRepositoryPrompt_ABlankSaveRemovesTheEntry(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	saved := f.save(r, "alpha", "text", "", 200)
	gone := f.save(r, "alpha", " \n\t ", saved["version"].(string), 200)
	if gone["version"] != "" || gone["content"] != "" {
		t.Fatalf("blank save = %v", gone)
	}
	if row := byName(t, f.tab(r)["agents"], "alpha"); row["prompt"].(map[string]any)["set"] != false {
		t.Fatalf("row = %v", row)
	}
	// Removing an entry that was never saved is accepted.
	f.save(r, "beta", "", "", 200)
}

func TestRepositoryPrompt_TheLimitIs8000Characters(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	over := f.save(r, "alpha", strings.Repeat("é", 8001), "", 400)
	if over["code"] != "REPOSITORY_PROMPT_TOO_LONG" || over["limit"] != float64(8000) || over["length"] != float64(8001) {
		t.Fatalf("body = %v", over)
	}
	f.save(r, "alpha", strings.Repeat("é", 8000), "", 200)
}

func TestRepositoryPrompt_IsKeptWhileTheAgentIsUnassignedOrNotEnabled(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	saved := f.save(r, "alpha", "kept", "", 200)
	v := f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "alpha", "list_version": ""}, 200)["list_version"]
	if got := f.prompt(r, "alpha"); got["content"] != "kept" || got["unassigned"] != true {
		t.Fatalf("unassigned = %v", got)
	}
	f.post("/api/assign_agent_again", map[string]any{"repository_id": r, "agent": "alpha", "list_version": v}, 200)
	if got := f.prompt(r, "alpha"); got["content"] != "kept" || got["version"] != saved["version"] || got["unassigned"] != false {
		t.Fatalf("assigned again = %v", got)
	}
	// A prompt can be saved for an agent the project does not enable.
	f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "selected", "agents": []string{"alpha"}}, 200)
	f.save(r, "beta", "unused", "", 200)
}

func TestRepositoryPrompt_RowsSummariseTheFirstLine(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	first := strings.Repeat("x", 130)
	saved := f.save(r, "alpha", first+"\nsecond line", "", 200)
	row := byName(t, f.tab(r)["agents"], "alpha")["prompt"].(map[string]any)
	if row["set"] != true || row["summary"] != strings.Repeat("x", 120) || row["length"] != float64(len(first)+len("\nsecond line")) || row["version"] != saved["version"] {
		t.Fatalf("prompt = %v", row)
	}
}

func TestRepositoryPrompt_RefusesAnUnknownAgentOrRepository(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	f.get("/api/get_repository_prompt?repository_id="+r+"&agent=ghost", 404)
	f.get("/api/get_repository_prompt?repository_id=nope&agent=alpha", 404)
	f.save(r, "ghost", "x", "", 404)
}

func TestRepositoryPrompt_PreviewBuildsTheSectionAfterTheAgentsOwnPromptAndStoresNothing(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	out := f.post("/api/preview_repository_prompt", map[string]any{"repository_id": r, "agent": "alpha", "content": "Use the gateway."}, 200)
	if out["heading"] != "## Repository prompt: atlas-api" || out["repository_section"] != "## Repository prompt: atlas-api\n\nUse the gateway." {
		t.Fatalf("preview = %v", out)
	}
	if _, ok := out["agent_prompt"].(string); !ok {
		t.Fatalf("agent_prompt = %v", out["agent_prompt"])
	}
	if blank := f.post("/api/preview_repository_prompt", map[string]any{"repository_id": r, "agent": "alpha", "content": "  "}, 200); blank["repository_section"] != "" {
		t.Fatalf("blank preview = %v", blank)
	}
	if got := f.prompt(r, "alpha"); got["version"] != "" {
		t.Fatalf("preview stored something: %v", got)
	}
}

func TestRepositoryPrompt_ASaveAnnouncesTheAgent(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, _ := f.svc.FollowRepositoryAgents(ctx)
	f.save(r, "alpha", "text", "", 200)
	select {
	case ev := <-events:
		if want := (domain.RepositoryAgentsChanged{ProjectID: p, RepositoryID: r, Agents: []string{"alpha"}}); !reflect.DeepEqual(ev, want) {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestRepositoryPrompt_TheTextASessionAppendsIsTheTextThePreviewShows(t *testing.T) {
	f := newRepoAgentsFixture(t)
	r := f.repository(f.project("atlas"), "atlas-api")
	f.save(r, "alpha", "Use the gateway.\n", "", 200)
	preview := f.post("/api/preview_repository_prompt", map[string]any{"repository_id": r, "agent": "alpha", "content": "Use the gateway.\n"}, 200)
	if got := f.svc.RepositoryPromptSection(context.Background(), r, "alpha"); got == "" || got != preview["repository_section"] {
		t.Fatalf("session section = %q, preview = %q", got, preview["repository_section"])
	}
	if got := f.svc.RepositoryPromptSection(context.Background(), r, "beta"); got != "" {
		t.Fatalf("an agent with no prompt got %q", got)
	}
}

// listAgents creates the agents the list reads and answers GET /api/list_agents.
func (f *repoAgentsFixture) listAgents(query string) map[string]any {
	return f.get("/api/list_agents?"+query, 200)
}

func TestListAgents_EachAgentSaysWhereItIsUnassigned(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	api, web := f.repository(p, "atlas-api"), f.repository(p, "atlas-web")
	f.post("/api/create_agent", map[string]any{"name": "alpha"}, 200)
	f.post("/api/create_agent", map[string]any{"name": "beta"}, 200)
	f.post("/api/unassign_agent", map[string]any{"repository_id": api, "agent": "alpha", "list_version": ""}, 200)
	f.post("/api/unassign_agent", map[string]any{"repository_id": web, "agent": "alpha", "list_version": ""}, 200)

	out := f.listAgents("project_id=" + p)
	alpha, beta := byName(t, out["agents"], "alpha"), byName(t, out["agents"], "beta")
	var got []string
	for _, r := range list(alpha["unassigned_from"]) {
		m := r.(map[string]any)
		got = append(got, m["repository_id"].(string)+"="+m["repository_name"].(string))
	}
	slices.Sort(got)
	want := []string{api + "=atlas-api", web + "=atlas-web"}
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("alpha unassigned_from = %v, want %v", got, want)
	}
	if l, ok := beta["unassigned_from"].([]any); !ok || len(l) != 0 {
		t.Fatalf("beta unassigned_from = %v, want an empty list", beta["unassigned_from"])
	}
	if _, ok := alpha["repository_prompt_set"]; ok {
		t.Fatalf("repository_prompt_set without a repository: %v", alpha)
	}
	if _, ok := out["not_offered"]; ok {
		t.Fatalf("not_offered without a repository: %v", out["not_offered"])
	}
}

func TestListAgents_WithARepositoryHoldsOnlyTheAssignedAgentsAndNamesTheRest(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	for _, n := range []string{"alpha", "beta", "zeta"} {
		f.post("/api/create_agent", map[string]any{"name": n}, 200)
	}
	f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "selected", "agents": []string{"alpha", "beta"}}, 200)
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": ""}, 200)
	f.save(r, "alpha", "Run make check.", "", 200)

	out := f.listAgents("project_id=" + p + "&repository_id=" + r)
	if len(list(out["agents"])) != 1 {
		t.Fatalf("agents = %v", out["agents"])
	}
	if alpha := byName(t, out["agents"], "alpha"); alpha["repository_prompt_set"] != true {
		t.Fatalf("alpha = %v", alpha)
	}
	reasons := map[string]string{}
	for _, n := range list(out["not_offered"]) {
		m := n.(map[string]any)
		reasons[m["name"].(string)] = m["reason"].(string)
	}
	if !reflect.DeepEqual(reasons, map[string]string{"beta": "unassigned", "zeta": "not_enabled"}) {
		t.Fatalf("not_offered = %v", reasons)
	}
	f.get("/api/list_agents?repository_id=nope", 404)
}

func TestAgentCatalog_EachAgentListsItsRepositoriesInTheProjectsThatEnableIt(t *testing.T) {
	f := newRepoAgentsFixture(t)
	atlas, other := f.project("atlas"), f.project("other")
	api, web := f.repository(atlas, "atlas-api"), f.repository(atlas, "atlas-web")
	f.repository(other, "other-repo")
	f.post("/api/set_project_agents", map[string]any{"project_id": other, "mode": "selected", "agents": []string{"beta"}}, 200)
	v := f.post("/api/unassign_agent", map[string]any{"repository_id": api, "agent": "alpha", "list_version": ""}, 200)["list_version"]
	f.save(web, "alpha", "text", "", 200)

	rows := map[string]map[string]any{}
	for _, r := range list(byName(t, f.get("/api/get_agent_catalog", 200)["agents"], "alpha")["repositories"]) {
		m := r.(map[string]any)
		rows[m["repository_id"].(string)] = m
	}
	if len(rows) != 2 {
		t.Fatalf("alpha repositories = %v, want the two of atlas only", rows)
	}
	if rows[api]["unassigned"] != true || rows[api]["prompt_set"] != false || rows[api]["list_version"] != v || rows[api]["project_id"] != atlas || rows[api]["repository_name"] != "atlas-api" {
		t.Fatalf("api = %v", rows[api])
	}
	if rows[web]["unassigned"] != false || rows[web]["prompt_set"] != true {
		t.Fatalf("web = %v", rows[web])
	}
}

func (f *repoAgentsFixture) rename(from, to string) {
	f.post("/api/save_catalog_agent", map[string]any{"base_versions": f.versions(), "previous_name": from, "agent": map[string]any{"name": to, "description": to + " agent"}}, 200)
}

func TestRenamingAnAgentMovesItsUnassignedEntriesAndItsPrompts(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	api, web := f.repository(p, "atlas-api"), f.repository(p, "atlas-web")
	v := f.post("/api/unassign_agent", map[string]any{"repository_id": api, "agent": "alpha", "list_version": ""}, 200)["list_version"]
	f.save(web, "alpha", "Web rules.", "", 200)
	untouched := f.tab(web)["list_version"]

	f.rename("alpha", "alpha-two")

	tab := f.tab(api)
	if row := byName(t, tab["agents"], "alpha-two"); row["state"] != "unassigned" || row["previous_name"] != "alpha" {
		t.Fatalf("renamed row = %v", row)
	}
	for _, r := range list(tab["agents"]) {
		if r.(map[string]any)["name"] == "alpha" {
			t.Fatalf("the old name is still listed: %v", tab["agents"])
		}
	}
	if tab["list_version"] == v || tab["list_version"] == "" {
		t.Fatalf("the list version did not change on a rename that rewrote the list: %v -> %v", v, tab["list_version"])
	}
	if got := f.prompt(web, "alpha-two"); got["content"] != "Web rules." {
		t.Fatalf("prompt under the new name = %v", got)
	}
	f.get("/api/get_repository_prompt?repository_id="+web+"&agent=alpha", 404)
	if got := f.tab(web)["list_version"]; got != untouched {
		t.Fatalf("a repository the agent was not unassigned from changed its list version: %v -> %v", untouched, got)
	}
}

func TestDeletingAnAgentRemovesItsEntriesAndAPromptCountedInTheCatalog(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": ""}, 200)
	f.save(r, "beta", "Beta rules.", "", 200)
	var promptSet any
	for _, row := range list(byName(t, f.get("/api/get_agent_catalog", 200)["agents"], "beta")["repositories"]) {
		promptSet = row.(map[string]any)["prompt_set"]
	}
	if promptSet != true {
		t.Fatalf("the catalog does not say a prompt will go: %v", promptSet)
	}

	f.post("/api/delete_catalog_agent", map[string]any{"base_versions": f.versions(), "name": "beta"}, 200)

	tab := f.tab(r)
	for _, row := range list(tab["agents"]) {
		if row.(map[string]any)["name"] == "beta" {
			t.Fatalf("beta is still listed: %v", tab["agents"])
		}
	}
	// A new agent with the same name starts clean.
	f.post("/api/save_catalog_agent", map[string]any{"base_versions": f.versions(), "agent": map[string]any{"name": "beta", "description": "again"}}, 200)
	if got := f.prompt(r, "beta"); got["content"] != "" || got["unassigned"] != false {
		t.Fatalf("a new beta inherited %v", got)
	}
}

func TestDeletingARepositoryRemovesItsListAndItsPrompts(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "alpha", "list_version": ""}, 200)
	f.save(r, "alpha", "text", "", 200)

	if err := f.projects.DeleteRepository(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := f.svc.IsUnassigned(context.Background(), r, "alpha"); got {
		t.Fatal("the list outlived its repository")
	}
	if got := f.svc.RepositoryPromptSection(context.Background(), r, "alpha"); got != "" {
		t.Fatalf("the prompt outlived its repository: %q", got)
	}
	var lists, prompts int64
	f.db.Raw("SELECT COUNT(*) FROM repository_unassigned_agents").Scan(&lists)
	f.db.Raw("SELECT COUNT(*) FROM repository_agent_prompts").Scan(&prompts)
	if lists != 0 || prompts != 0 {
		t.Fatalf("rows left: %d lists, %d prompts", lists, prompts)
	}
}

func TestRenamesDeletesAndRepositoryDeletesAreAnnounced(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "alpha", "list_version": ""}, 200)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, _ := f.svc.FollowRepositoryAgents(ctx)
	f.rename("alpha", "alpha-two")
	select {
	case ev := <-events:
		if ev.RepositoryID != r || ev.ProjectID != p || !reflect.DeepEqual(ev.Agents, []string{"alpha", "alpha-two"}) {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event after a rename")
	}
	f.post("/api/delete_catalog_agent", map[string]any{"base_versions": f.versions(), "name": "alpha-two"}, 200)
	select {
	case ev := <-events:
		if ev.RepositoryID != r || !reflect.DeepEqual(ev.Agents, []string{"alpha-two"}) {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event after a delete")
	}
}

func TestTheTUIClientListsOnlyTheAgentsAssignedInARepository(t *testing.T) {
	f := newRepoAgentsFixture(t)
	p := f.project("atlas")
	r := f.repository(p, "atlas-api")
	for _, n := range []string{"alpha", "beta", "zeta"} {
		f.post("/api/create_agent", map[string]any{"name": n}, 200)
	}
	f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "selected", "agents": []string{"alpha", "beta"}}, 200)
	f.post("/api/unassign_agent", map[string]any{"repository_id": r, "agent": "beta", "list_version": ""}, 200)

	srv := httptest.NewServer(f.router)
	defer srv.Close()
	offer, err := httpclient.NewAgents(httpclient.New(srv.URL)).ListAgentsFor(context.Background(), p, r)
	if err != nil {
		t.Fatal(err)
	}
	var offered []string
	for _, a := range offer.Agents {
		offered = append(offered, a.Name)
	}
	if !reflect.DeepEqual(offered, []string{"alpha"}) {
		t.Fatalf("offered = %v", offered)
	}
	reasons := map[string]string{}
	for _, n := range offer.NotOffered {
		reasons[n.Name] = n.Reason
	}
	if !reflect.DeepEqual(reasons, map[string]string{"beta": "unassigned", "zeta": "not_enabled"}) {
		t.Fatalf("not offered = %v", reasons)
	}
}
