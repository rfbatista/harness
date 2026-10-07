package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/projects"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeProjects struct{ list []*domain.Project }

func (f fakeProjects) ListProjects(context.Context) ([]*domain.Project, error) { return f.list, nil }

func (f fakeProjects) GetProject(_ context.Context, id string) (*domain.Project, error) {
	for _, p := range f.list {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, errs.Newf("PROJECT_NOT_FOUND", "project %s not found", id)
}

type fakeTickets struct{ list []*domain.Ticket }

func (f fakeTickets) GetTicket(_ context.Context, id string) (*domain.Ticket, error) {
	for _, t := range f.list {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, errs.Newf("TICKET_NOT_FOUND", "ticket %s not found", id)
}

func (f fakeTickets) ListTickets(_ context.Context, projectID string) ([]*domain.Ticket, error) {
	var out []*domain.Ticket
	for _, t := range f.list {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}

type fakeAgents struct{ list []*domain.Agent }

func (f fakeAgents) ListAgents(context.Context) ([]*domain.Agent, error) { return f.list, nil }

func (f fakeAgents) GetAgent(_ context.Context, id string) (*domain.Agent, error) {
	for _, a := range f.list {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, errs.Newf("AGENT_NOT_FOUND", "agent %s not found", id)
}

type fakeRepos struct{ list []*domain.Repository }

func (f fakeRepos) ListRepositories(_ context.Context, projectID string) ([]*domain.Repository, error) {
	var out []*domain.Repository
	for _, r := range f.list {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}

type fakeEnv map[string][]*domain.EnvFile

func (f fakeEnv) ListEnvFiles(_ context.Context, repositoryID string) ([]*domain.EnvFile, error) {
	return f[repositoryID], nil
}

type fakeSessions struct{ list []*domain.Session }

func (f fakeSessions) Get(_ context.Context, id string) (*domain.Session, error) {
	for _, s := range f.list {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, errs.Newf("SESSION_NOT_FOUND", "session %s not found", id)
}

func (f fakeSessions) List(_ context.Context, filter ports.SessionFilter) ([]*domain.Session, error) {
	var out []*domain.Session
	for _, s := range f.list {
		if s.ProjectID == filter.ProjectID && (filter.TicketID == "" || s.TicketID == filter.TicketID) {
			out = append(out, s)
		}
	}
	return out, nil
}

var now = time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)

func builtAssets(t *testing.T) *Assets {
	t.Helper()
	a, err := newAssets(fstest.MapFS{
		"app.js":       {Data: []byte("console.log('app')")},
		"app.css":      {Data: []byte("body{}")},
		"document.css": {Data: []byte(".prose{}")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type world struct {
	projects  []*domain.Project
	tickets   []*domain.Ticket
	sessions  []*domain.Session
	agents    []*domain.Agent
	repos     []*domain.Repository
	env       fakeEnv
	docs      fakeDocs
	history   ports.RepositoryHistory
	artifacts []*domain.Artifact
	summaries fakeSummaries
}

// fakeSummaries counts like the projects context: nil leaves /projects unserved.
type fakeSummaries []projects.Summary

func (f fakeSummaries) ListProjectSummaries(context.Context) ([]projects.Summary, error) {
	return f, nil
}

func newTestHandler(t *testing.T, w world) http.Handler {
	t.Helper()
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "legacy designer") })
	deps := Deps{
		Projects:     fakeProjects{w.projects},
		Tasks:        fakeTickets{w.tickets},
		Sessions:     fakeSessions{w.sessions},
		Agents:       fakeAgents{w.agents},
		Repositories: fakeRepos{w.repos},
		EnvFiles:     w.env,
		Now:          func() time.Time { return now },
	}
	if w.docs != nil {
		deps.Documents = docReader{w.docs, w.tickets}
	}
	if w.history != nil {
		deps.History = w.history
	}
	if w.artifacts != nil {
		deps.Artifacts = fakeArtifacts{w.artifacts}
	}
	if w.summaries != nil {
		deps.ProjectSummaries = w.summaries
	}
	return NewHandler(deps, builtAssets(t), fallback)
}

// board is one project with three tasks; the in-progress task runs several
// sessions at once.
func board() world {
	return world{
		projects: []*domain.Project{{ID: "p1", Name: "coding_pool"}, {ID: "p2", Name: "other"}},
		tickets: []*domain.Ticket{
			{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
			{ID: "t-docs", ProjectID: "p1", Title: "Write docs", Status: domain.TicketStatusTodo},
			{ID: "t-ship", ProjectID: "p1", Title: "Ship it", Status: domain.TicketStatusDone},
			{ID: "t-else", ProjectID: "p2", Title: "Elsewhere", Status: domain.TicketStatusTodo},
		},
		sessions: []*domain.Session{
			{ID: "s1", ProjectID: "p1", TicketID: "t-feed", Task: "implement this task", AgentID: "reviewer", Status: domain.SessionIdle, UpdatedAt: now.Add(-2 * time.Minute)},
			{ID: "s2", ProjectID: "p1", TicketID: "t-feed", Task: "review <the> plan", Status: domain.SessionRunning, UpdatedAt: now.Add(-time.Hour)},
			{ID: "s3", ProjectID: "p1", TicketID: "t-docs", Task: "draft the docs", Status: domain.SessionDone, UpdatedAt: now.Add(-3 * time.Hour)},
			{ID: "s9", ProjectID: "p2", TicketID: "t-else", Task: "not this project", Status: domain.SessionRunning, UpdatedAt: now},
		},
		agents: []*domain.Agent{{ID: "reviewer", Name: "Reviewer"}, {ID: "backend", Name: "Backend dev"}},
		repos: []*domain.Repository{
			{ID: "r-harness", ProjectID: "p1", Name: "harness"},
			{ID: "r-kit", ProjectID: "p1", Name: "harnesskit"},
			{ID: "r-other", ProjectID: "p2", Name: "not ours"},
		},
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHomeOpensTheFirstProjectByName(t *testing.T) {
	h := newTestHandler(t, world{projects: []*domain.Project{{ID: "z1", Name: "zebra"}, {ID: "a1", Name: "Alpha"}}})
	rec := get(t, h, "/")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/projects/a1" {
		t.Fatalf("got %d → %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestHomeWithoutProjectsExplainsHowToStart(t *testing.T) {
	rec := get(t, newTestHandler(t, world{}), "/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No projects yet") {
		t.Fatalf("got %d:\n%s", rec.Code, rec.Body)
	}
}

func TestProjectPagePutsItsTasksOnTheRailAndTheProjectInThePicker(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<option value="p1" selected>coding_pool</option>`,
		`<option value="p2">other</option>`,
		`action="/switch-project"`,
		`<h2>in progress</h2>`,
		`href="/projects/p1/tasks/t-feed"`,
		`<span class="[ label ]">Add SSE feed</span>`,
		`data-state="waiting"`, // the feed task waits on the developer
		// The board: its first paint and its live template.
		`x-data="tasksBoard" data-project-id="p1"`,
		`x-on:task-changed.window="taskChanged"`,
		// The rail: its first paint and the live groups it regroups from the store.
		`data-current-task="" data-reports-feed="" data-seed="rail-seed" x-data="tasksRail"`, // templ renders attribute maps sorted by key
		`x-for="group in groups"`,
		`x-for="link in group.links"`,
		`x-data="streamStatus"`, // the board follows the project feed…
		`data-reports-feed`,     // …through the rail, which reports it to the stream bar
		`<script id="rail-seed" type="application/json">`,
		`"tasks":[{"id":"t-feed"`,
		`data-ssr`,
		`x-for="column in columns"`,
		`x-for="card in column.cards"`,
		`class="[ card ]" data-task-id="t-feed"`,
		`class="[ title ]" href="/projects/p1/tasks/t-feed">Add SSE feed</a>`,
		// Moving a card: the task page's status select, on every card.
		`<select class="[ status-picker ]" data-task-id="t-feed" aria-label="Move Add SSE feed to" x-on:change="moveTo">`,
		`<option value="in_progress" selected>In progress</option>`,
		`x-bind:value="card.status" x-on:change="moveTo"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("project page is missing %q", want)
		}
	}
	if strings.Contains(body, "Elsewhere") {
		t.Error("another project's task is on the board or the rail")
	}
	if strings.Contains(body, "Pick a task") {
		t.Error("the board replaced the pick-a-task empty state")
	}
	// The five columns, in board order, each present even when empty.
	heads := []string{"Backlog", "Todo", "In progress", "Review", "Done"}
	last := -1
	for _, h := range heads {
		i := strings.Index(body, `<span class="[ label ]">`+h+`</span><span class="[ badge ]">`)
		if i < 0 || i < last {
			t.Errorf("column %q missing or out of order (index %d after %d)", h, i, last)
		}
		last = i
	}
	order := []int{strings.Index(body, "<h2>in progress</h2>"), strings.Index(body, "<h2>todo</h2>"), strings.Index(body, "<h2>done</h2>")}
	if !(order[0] < order[1] && order[1] < order[2]) {
		t.Errorf("rail groups are not in kanban order: %v", order)
	}
}

func TestProjectWithoutTasksOffersToCreateOneAndStaysLive(t *testing.T) {
	w := board()
	w.tickets = nil
	body := get(t, newTestHandler(t, w), "/projects/p1").Body.String()
	for _, want := range []string{"No tasks in this project yet", "Create a task", `x-data="tasksBoard"`, `"tasks":[]`, `x-data="streamStatus"`} {
		if !strings.Contains(body, want) {
			t.Errorf("empty project page is missing %q", want)
		}
	}
	if strings.Contains(body, `class="[ card ]" data-task-id=`) {
		t.Error("no cards without tasks") // the live template's card is a template, not a card
	}
}

// The Design tab's half of the Artifacts contract: pages are embedded only in
// a sandboxed frame without allow-same-origin, titles and notes as text.
func TestDesignTabEmbedsArtifactsSandboxed(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed").Body.String()
	for _, want := range []string{`sessionsDesignPanel(panel)`, `sandbox="allow-scripts" referrerpolicy="no-referrer"></iframe>`, `x-init="loadFrame($el, frame)"`, `role="tab"`, `x-on:click="showDesign"`, `x-on:artifact-published="artifactPublished"`} {
		if !strings.Contains(body, want) {
			t.Errorf("task page lacks %s", want)
		}
	}
	for _, never := range []string{"allow-same-origin", "x-html", "autoplay", `<iframe x-`} {
		if strings.Contains(body, never) {
			t.Errorf("task page must not contain %q", never)
		}
	}
	if n := strings.Count(body, `<iframe`); n != 2 {
		t.Errorf("expected exactly two iframe templates (page, url), found %d", n)
	}
}

func TestTaskPageListsItsSessionsBesideTheDetail(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<h1 x-text="title">Add SSE feed</h1>`,
		`x-data="tasksTaskEditor" data-seed="task-seed"`,
		`<script id="task-seed" type="application/json">`,
		`<option value="in_progress" selected>In progress</option>`,
		`x-on:click="askDeleteTask"`,
		`href="/projects/p1/tasks/t-feed" aria-current="page"`,
		`data-current-task="t-feed"`,
		`x-data="sessionsPage"`,
		`x-data="streamStatus"`,
		`<script id="sessions-seed" type="application/json">`,
		`"ticket_id":"t-feed"`,
		`data-ssr`,
		`x-for="group in groups"`,
		`implement this task`,
		`review &lt;the&gt; plan`, // escaped in markup
		`your turn`,
		`Reviewer · 2m`,          // the agent's name, not its id
		`2 sessions · 1 waiting`, // one task, several sessions
		`x-on:click="startCreating"`,
		`x-data="sessionsNewSession(projectId, ticketId)"`,
		`data-default-repository="r-harness"`,
		`<option value="">Plain claude</option>`,
		`<option value="backend">Backend dev</option>`,
		`<option value="r-kit">harnesskit</option>`,
		`<option value="edits">Accept file edits</option>`,
		`x-on:click="askDelete"`,
		`x-on:click="deleteSelected"`,
		`"agent_names":{`,
		`src="/static/app.js?v=`,
		`href="/static/app.css?v=`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task page is missing %q", want)
		}
	}
	for _, leak := range []string{"draft the docs", "not this project", "not ours"} {
		if strings.Contains(body, leak) {
			t.Errorf("a session of another task leaked into the page: %q", leak)
		}
	}
	if strings.Contains(body, "data-reports-feed") {
		t.Error("the task page's own feed reports to the stream bar, not the rail")
	}
	if regexp.MustCompile(`"sessions":\[\{[^]]*"id":"s1"`).FindString(body) == "" {
		t.Error("the seed does not carry the task's sessions in the API's shape")
	}
}

func TestNewSessionNeedsARepository(t *testing.T) {
	w := board()
	w.repos = nil
	body := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed").Body.String()
	if strings.Contains(body, `x-on:click="startCreating"`) || !strings.Contains(body, "Add a repository to this project first") {
		t.Fatal("without a repository the page must not offer to start a session, and must say why")
	}
}

func TestNewProjectPageOffersTheForm(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/new")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="projectsNewProject"`,
		`id="project-root"`,
		`x-on:click="search"`, // finds the repositories inside the directory
		`x-for="f in found"`,
		`<option value="" selected disabled>Choose a project…</option>`, // no project selected on this page
	} {
		if !strings.Contains(body, want) {
			t.Errorf("new project page is missing %q", want)
		}
	}
}

func TestRepositoriesPageListsAndSeedsThem(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1/repositories")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="projectsRepositoriesPage"`,
		`<script id="repositories-seed" type="application/json">`,
		`"project_id":"p1"`,
		`"project_root":`,
		`"id":"r-harness"`,
		`x-for="s in suggestions"`,
		`>harnesskit<`,
		`x-on:submit.prevent="add"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("repositories page is missing %q", want)
		}
	}
	if strings.Contains(body, "not ours") {
		t.Error("another project's repository is listed")
	}
	if rec := get(t, newTestHandler(t, board()), "/projects/nope/repositories"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: %d", rec.Code)
	}
}

func TestPagesLinkToProjectCreationAndRepositories(t *testing.T) {
	h := newTestHandler(t, board())
	if body := get(t, h, "/projects/p1").Body.String(); !strings.Contains(body, `href="/projects/new"`) || !strings.Contains(body, `href="/projects/p1/repositories"`) {
		t.Error("the project page links to New project and Repositories")
	}
	if body := get(t, newTestHandler(t, world{}), "/").Body.String(); !strings.Contains(body, `href="/projects/new"`) {
		t.Error("the first-run page links to New project")
	}
	w := board()
	w.repos = nil
	if body := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed").Body.String(); !strings.Contains(body, `href="/projects/p1/repositories"`) {
		t.Error("a task page without repositories links to add one")
	}
}

func TestProjectsPageListsEveryProjectWithItsCounts(t *testing.T) {
	w := board()
	at := now.Add(-2 * time.Minute)
	w.summaries = fakeSummaries{
		{Project: w.projects[0], RepositoryCount: 4, OpenTaskCount: 7, RunningSessionCount: 2, LastActivityAt: &at},
		{Project: w.projects[1]},
	}
	rec := get(t, newTestHandler(t, w), "/projects")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<script id="projects-seed" type="application/json">`,
		`"running_session_count":2`,
		`href="/projects/p1"`,
		`href="/projects/p1/settings"`,
		`data-state="running">2 running<`,
		`4 repos · 7 open tasks · active 2m ago`,
		`2 projects · 2 sessions running`,
		`href="/projects/new"`,                                          // New project, from the toolbar
		`href="/projects" aria-current="page">Projects</a>`,             // the top bar marks the page
		`<option value="" selected disabled>Choose a project…</option>`, // no project selected here
	} {
		if !strings.Contains(body, want) {
			t.Errorf("projects page is missing %q", want)
		}
	}
	empty := world{summaries: fakeSummaries{}}
	if body := get(t, newTestHandler(t, empty), "/projects").Body.String(); !strings.Contains(body, "No projects yet") || !strings.Contains(body, `{"summaries":[]}`) {
		t.Error("without projects the page teaches what one is and seeds []")
	}
}

func TestTopBarLeadsToTheProjectsListOnceItIsServed(t *testing.T) {
	w := board()
	w.summaries = fakeSummaries{}
	body := get(t, newTestHandler(t, w), "/projects/p1").Body.String()
	if !strings.Contains(body, `href="/projects">Projects</a>`) {
		t.Error("the top bar links to the projects list")
	}
	if !strings.Contains(body, `href="/projects/p1/settings"`) {
		t.Error("the project page links to its settings")
	}
	if rec := get(t, newTestHandler(t, board()), "/projects"); rec.Code == http.StatusOK && !strings.Contains(rec.Body.String(), "legacy designer") {
		t.Error("without summaries /projects is not served")
	}
}

func TestSettingsPageSeedsTheProject(t *testing.T) {
	w := board()
	w.projects[0].IgnoredPaths = []string{"node_modules"}
	rec := get(t, newTestHandler(t, w), "/projects/p1/settings")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<script id="settings-seed" type="application/json">`,
		`"ignored_paths":["node_modules"]`,
		`value="coding_pool"`,
		`>node_modules<`,
		`href="/projects/p1/repositories"`,
		`/projects/p1/repositories/r-harness/env`,
		`data-tone="danger"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page is missing %q", want)
		}
	}
	if strings.Contains(body, "not ours") {
		t.Error("another project's repository is linked")
	}
	if rec := get(t, newTestHandler(t, board()), "/projects/nope/settings"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: %d", rec.Code)
	}
}

func TestNewTaskPage(t *testing.T) {
	h := newTestHandler(t, board())
	rec := get(t, h, "/projects/p1/tasks/new")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="tasksNewTask" data-project-id="p1"`,
		`<option value="backlog">Backlog</option>`,
		`<option value="done">Done</option>`,
		`<h2>in progress</h2>`, // the rail is there
	} {
		if !strings.Contains(body, want) {
			t.Errorf("new task page is missing %q", want)
		}
	}
	if rec := get(t, h, "/projects/nope/tasks/new"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: %d", rec.Code)
	}
	if body := get(t, h, "/projects/p1").Body.String(); strings.Count(body, `href="/projects/p1/tasks/new"`) < 2 {
		t.Error("the rail and the project page both offer New task")
	}
}

func TestEnvFilesPage(t *testing.T) {
	w := board()
	w.env = fakeEnv{"r-harness": {{RepositoryID: "r-harness", Path: ".env", Content: "DATABASE_URL=postgres://x"}}}
	h := newTestHandler(t, w)
	rec := get(t, h, "/projects/p1/repositories/r-harness/env")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="projectsEnvFilesPage" data-seed="env-files-seed"`,
		`"repository_id":"r-harness"`,
		`"path":".env"`,
		`DATABASE_URL=postgres://x`,
		`x-on:click="addEmpty"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("env files page is missing %q", want)
		}
	}
	if body := get(t, h, "/projects/p1/repositories").Body.String(); !strings.Contains(body, `href="/projects/p1/repositories/r-harness/env"`) {
		t.Error("the repositories page links each repository's env files")
	}
	if rec := get(t, h, "/projects/p1/repositories/r-other/env"); rec.Code != http.StatusNotFound {
		t.Errorf("another project's repository: %d", rec.Code)
	}
}

func TestMissingThingsAreCoded404s(t *testing.T) {
	h := newTestHandler(t, board())
	cases := map[string]string{
		"/projects/nope":              "PROJECT_NOT_FOUND",
		"/projects/p1/tasks/nope":     "TICKET_NOT_FOUND",
		"/projects/p1/tasks/t-else":   "TICKET_NOT_FOUND", // a task of another project
		"/projects/nope/tasks/t-feed": "PROJECT_NOT_FOUND",
	}
	for path, code := range cases {
		rec := get(t, h, path)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: got %d, want 404 %s", path, rec.Code, code)
		}
	}
}

func TestRedirects(t *testing.T) {
	h := newTestHandler(t, board())
	cases := map[string]string{
		"/switch-project?project=p2": "/projects/p2",
		"/switch-project":            "/",
		"/projects/p1/":              "/projects/p1",
		"/projects/p1/sessions":      "/projects/p1", // the old page
	}
	for path, want := range cases {
		rec := get(t, h, path)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%s: got %d → %q, want → %q", path, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

func TestOtherPathsReachTheFallback(t *testing.T) {
	rec := get(t, newTestHandler(t, world{}), "/designer")
	if rec.Body.String() != "legacy designer" {
		t.Fatalf("got %q", rec.Body)
	}
}

func TestVersionedAssetsAreImmutable(t *testing.T) {
	h := newTestHandler(t, world{})
	versioned := get(t, h, "/static/app.js?v=abc")
	if versioned.Code != http.StatusOK || !strings.Contains(versioned.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("versioned: %d %q", versioned.Code, versioned.Header().Get("Cache-Control"))
	}
	if plain := get(t, h, "/static/app.css"); plain.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("unversioned: %q", plain.Header().Get("Cache-Control"))
	}
	if hidden := get(t, h, "/static/.gitkeep"); hidden.Code != http.StatusNotFound {
		t.Errorf("dotfiles are not served: %d", hidden.Code)
	}
}

func TestUnbuiltAssetsSayHowToBuild(t *testing.T) {
	a, _ := newAssets(fstest.MapFS{})
	h := NewHandler(Deps{Projects: fakeProjects{}, Tasks: fakeTickets{}, Sessions: fakeSessions{}}, a, nil)
	if body := get(t, h, "/").Body.String(); !strings.Contains(body, "make web") {
		t.Fatalf("missing build hint:\n%s", body)
	}
}

// The app bundles are what "built" means; the document stylesheet is
// optional and its absence must not hide the pages.
func TestBuiltMeansTheAppBundles(t *testing.T) {
	a, err := newAssets(fstest.MapFS{
		"app.js":  {Data: []byte("console.log('app')")},
		"app.css": {Data: []byte("body{}")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Built() {
		t.Fatal("app.js and app.css present, but Built() is false")
	}
	if a.URL("document.css") != "/static/document.css" {
		t.Fatalf("an unbuilt stylesheet has a versioned url: %q", a.URL("document.css"))
	}
}
