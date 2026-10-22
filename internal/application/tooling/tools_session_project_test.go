package tooling

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// archList is a project map that knows one project.
type archList struct {
	project  string
	contexts []*domain.BoundedContext
	zones    []*domain.Zone
}

func (a archList) ListBoundedContexts(projectID string) []*domain.BoundedContext {
	if projectID != a.project {
		return nil
	}
	return a.contexts
}
func (a archList) ListZones(_ context.Context, projectID string) []*domain.Zone {
	if projectID != a.project {
		return nil
	}
	return a.zones
}

// projectDeps are the tools' dependencies, built once the session's project
// id is known; nil fields are left unset.
type projectDeps struct {
	agents ports.AgentLister
	arch   ports.ArchitectureMap
	repos  ports.RepositoryLister
}

// scopedRepos answers only for the project the caller is in.
type scopedRepos map[string][]*domain.Repository

func (r scopedRepos) ListRepositories(_ context.Context, projectID string) ([]*domain.Repository, error) {
	return r[projectID], nil
}

// projectToolsFixture builds the session project tools for session "me" on a
// task of a fresh project, with deps built for that project, and returns a caller that runs one by name and
// round-trips its result through JSON, as an MCP client would see it.
func projectToolsFixture(t *testing.T, deps func(projectID string) projectDeps) func(name string) (map[string]any, error) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	proj, _ := projects.Create("p", t.TempDir())
	task, _ := plan.CreateTicket(context.Background(), proj.ID, "Export CSV", "", domain.TicketStatusInProgress, "")
	if _, err := sessions.Create(&domain.Session{ID: "me", ProjectID: proj.ID, TicketID: task.ID, RepositoryID: "r-api", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	d := deps(proj.ID)
	tools := map[string]domain.Tool{}
	for _, tl := range SessionTaskTools(plan, sessions, d.agents, d.arch, PeerStarter{Repositories: d.repos}, ArtifactTooling{}, nil, NotificationTooling{}) {
		tools[tl.Name] = tl
	}
	return func(name string) (map[string]any, error) {
		tl, ok := tools[name]
		if !ok {
			t.Fatalf("%s not built", name)
		}
		out, err := tl.Handler(WithSessionID(context.Background(), "me"), map[string]any{})
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(out)
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m, nil
	}
}

func TestListProjectRepositories_ListsTheSessionsProject(t *testing.T) {
	call := projectToolsFixture(t, func(p string) projectDeps {
		return projectDeps{repos: scopedRepos{
			p:         {{ID: "r-api", Name: "api", Description: "HTTP API"}, {ID: "r-web", Name: "web"}},
			"p-other": {{ID: "r-x", Name: "secret"}},
		}}
	})
	out, err := call("list_project_repositories")
	if err != nil {
		t.Fatal(err)
	}
	repos := out["repositories"].([]any)
	if len(repos) != 2 {
		t.Fatalf("repositories = %v, want the two of the session's project", repos)
	}
	api := repos[0].(map[string]any)
	if api["name"] != "api" || api["description"] != "HTTP API" || api["yours"] != true {
		t.Errorf("api = %v", api)
	}
	if repos[1].(map[string]any)["yours"] != false {
		t.Errorf("web marked as the caller's: %v", repos[1])
	}
}

func TestListBoundedContexts_GroupsZones(t *testing.T) {
	call := projectToolsFixture(t, func(p string) projectDeps {
		return projectDeps{arch: archList{
			project:  p,
			contexts: []*domain.BoundedContext{{ID: "bc1", Name: "Billing", Purpose: "Charge users", UbiquitousLanguage: []domain.LanguageTerm{{Term: "Invoice"}}}},
			zones: []*domain.Zone{
				{ID: "z1", Name: "billing-api", Pattern: "internal/billing/**", BoundedContextID: "bc1"},
				{ID: "z2", Name: "scripts", Pattern: "scripts/**"},
			},
		}}
	})
	out, err := call("list_bounded_contexts")
	if err != nil {
		t.Fatal(err)
	}
	bcs := out["bounded_contexts"].([]any)
	if len(bcs) != 1 {
		t.Fatalf("bounded_contexts = %v", bcs)
	}
	billing := bcs[0].(map[string]any)
	zones := billing["zones"].([]any)
	if billing["name"] != "Billing" || len(zones) != 1 || zones[0].(map[string]any)["pattern"] != "internal/billing/**" {
		t.Errorf("billing = %v", billing)
	}
	if un := out["unassigned_zones"].([]any); len(un) != 1 || un[0].(map[string]any)["name"] != "scripts" {
		t.Errorf("unassigned_zones = %v", un)
	}
}

func TestListAgents_NamesWhatEachIsFor(t *testing.T) {
	call := projectToolsFixture(t, func(string) projectDeps {
		return projectDeps{agents: agentList{{ID: "a1", Name: "backend-developer", Description: "Plans Go backend work", Skills: []domain.Skill{{Name: "golang-testing"}}}}}
	})
	out, err := call("list_agents")
	if err != nil {
		t.Fatal(err)
	}
	a := out["agents"].([]any)[0].(map[string]any)
	if a["name"] != "backend-developer" || a["description"] != "Plans Go backend work" || a["skills"].([]any)[0] != "golang-testing" {
		t.Errorf("agent = %v", a)
	}
}

func TestSessionProjectTools_Unavailable(t *testing.T) {
	call := projectToolsFixture(t, func(string) projectDeps { return projectDeps{} })
	for _, name := range []string{"list_project_repositories", "list_bounded_contexts", "list_agents"} {
		if _, err := call(name); errs.Code(err) != "UNAVAILABLE" {
			t.Errorf("%s without its dependency: err = %v, want UNAVAILABLE", name, err)
		}
	}
}

// A session with no task (a project-level product session, ADR 0016) still
// sees its project: the project tools scope by the session's project, not
// its task.
func TestProjectToolsWorkInASessionWithoutATask(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	proj, _ := projects.Create("p", t.TempDir())
	if _, err := sessions.Create(&domain.Session{ID: "free", ProjectID: proj.ID, RepositoryID: "r-api", Task: "Product: p", Status: domain.SessionRunning, Mode: domain.SessionModeProduct}); err != nil {
		t.Fatal(err)
	}
	repos := scopedRepos{proj.ID: {{ID: "r-api", Name: "api"}}}
	arch := archList{project: proj.ID, contexts: []*domain.BoundedContext{{ID: "bc1", ProjectID: proj.ID, Name: "Billing"}}}
	agents := agentList{{ID: "a1", Name: "Go Developer"}}
	tools := map[string]domain.Tool{}
	for _, tl := range SessionTaskTools(plan, sessions, agents, arch, PeerStarter{Repositories: repos}, ArtifactTooling{}, nil, NotificationTooling{}) {
		tools[tl.Name] = tl
	}
	ctx := WithSessionID(context.Background(), "free")
	for name, key := range map[string]string{"list_project_repositories": "repositories", "list_bounded_contexts": "bounded_contexts", "list_agents": "agents"} {
		out, err := tools[name].Handler(ctx, map[string]any{})
		if err != nil {
			t.Fatalf("%s without a task: %v", name, err)
		}
		b, _ := json.Marshal(out)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if list, _ := m[key].([]any); len(list) != 1 {
			t.Errorf("%s = %s, want one %s", name, b, key)
		}
	}
	for _, name := range []string{"get_task", "update_task_status", "start_task_session", "list_task_sessions"} {
		if _, err := tools[name].Handler(ctx, map[string]any{"status": "review", "prompt": "go"}); errs.Code(err) != "SESSION_HAS_NO_TASK" {
			t.Errorf("%s without a task = %v, want SESSION_HAS_NO_TASK", name, err)
		}
	}
}
