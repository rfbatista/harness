//go:build !windows

package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/in/web/serversettings"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

type topBarProjects []*domain.Project

func (p topBarProjects) ListProjects(context.Context) ([]*domain.Project, error) { return p, nil }
func (p topBarProjects) GetProject(_ context.Context, id string) (*domain.Project, error) {
	for _, pr := range p {
		if pr.ID == id {
			return pr, nil
		}
	}
	return nil, errors.New("project not found")
}

type topBarInbox struct {
	mu     sync.Mutex
	unread int
	fail   bool
}

func (i *topBarInbox) set(n int) { i.mu.Lock(); i.unread = n; i.mu.Unlock() }
func (i *topBarInbox) ListNotifications(context.Context, ports.NotificationQuery) (*ports.NotificationPage, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.fail {
		return nil, context.DeadlineExceeded
	}
	return &ports.NotificationPage{Notifications: []*domain.Notification{}, UnreadCount: i.unread}, nil
}

type topBarBoard struct{ ports.AgentBoard }

type topBarWorld struct {
	srv      *httptest.Server
	projects *topBarProjects
	inbox    *topBarInbox
}

// newTopBarWorld serves the API over the web adapter's top-bar model. blocked
// puts the sandbox on without a driver, which blocks agents from starting.
func newTopBarWorld(t *testing.T, deps func(*web.Deps), blocked bool) *topBarWorld {
	t.Helper()
	w := &topBarWorld{
		projects: &topBarProjects{{ID: "p2", Name: "zeta"}, {ID: "p1", Name: "Harness"}, {ID: "p3", Name: "alpha"}},
		inbox:    &topBarInbox{unread: 3},
	}
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	d := web.Deps{
		Projects: w.projects, AgentBoard: topBarBoard{}, Notifications: w.inbox, ProjectSummaries: topBarSummaries{},
		Settings: cat.Settings, DataPaths: &serversettings.Paths{},
		SandboxSettings: &sandboxsettings.Store{Settings: cat.Settings, Home: t.TempDir()},
		SandboxDriver:   func() ports.SandboxDriver { return nil },
	}
	if !blocked {
		d.SandboxDriver = func() ports.SandboxDriver { return &portstest.RecordingSandboxDriver{} }
	}
	if deps != nil {
		deps(&d)
	}
	w.srv = httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{TopBars: web.NewTopBars(d)})))
	t.Cleanup(w.srv.Close)
	return w
}

type topBarSummaries struct{}

func (topBarSummaries) ListProjectSummaries(context.Context) ([]ports.ProjectSummary, error) {
	return nil, nil
}

func (w *topBarWorld) get(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	res, err := http.Get(w.srv.URL + "/api/top_bar?path=" + url.QueryEscape(path))
	return decodeTopBar(t, res, err)
}

func decodeTopBar(t *testing.T, res *http.Response, err error) (int, map[string]any) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}

func linkKeys(body map[string]any) []string {
	var keys []string
	for _, l := range body["links"].([]any) {
		keys = append(keys, l.(map[string]any)["key"].(string))
	}
	return keys
}

func link(t *testing.T, body map[string]any, key string) map[string]any {
	t.Helper()
	for _, l := range body["links"].([]any) {
		if m := l.(map[string]any); m["key"] == key {
			return m
		}
	}
	t.Fatalf("no %s link in %v", key, linkKeys(body))
	return nil
}

func currentKeys(body map[string]any) []string {
	var keys []string
	for _, l := range body["links"].([]any) {
		if m := l.(map[string]any); m["current"] == true {
			keys = append(keys, m["key"].(string))
		}
	}
	return keys
}

func TestTheTopBarModelOfAProjectTaskPath(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	status, body := w.get(t, "/projects/p1/tasks/t1")
	if status != 200 {
		t.Fatalf("status %d: %v", status, body)
	}
	if body["path"] != "/projects/p1/tasks/t1" || body["current_project_id"] != "p1" || body["switch_project_href"] != "/switch-project" {
		t.Fatalf("model = %v", body)
	}
	var names []string
	for _, p := range body["projects"].([]any) {
		names = append(names, p.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, []string{"alpha", "Harness", "zeta"}) {
		t.Fatalf("projects = %v, want by name, case-insensitive", names)
	}
	if got := currentKeys(body); len(got) != 0 {
		t.Fatalf("a task page marks no link as current, got %v", got)
	}
	nt := body["notifications"].(map[string]any)
	page := nt["page"].(map[string]any)
	if nt["inbox_href"] != "/notifications" || page["unread_count"] != float64(3) {
		t.Fatalf("notifications = %v", nt)
	}
}

func TestTheServedLinksComeInTheBarsOrderAndTheCurrentOneFollowsThePath(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	cases := map[string]string{
		"/":                "board",
		"/projects":        "projects",
		"/agents":          "agents",
		"/agents/skills/x": "agents",
		"/models":          "models",
		"/agent-isolation": "agent_isolation",
		"/server":          "server",
	}
	for path, key := range cases {
		_, body := w.get(t, path)
		if got := currentKeys(body); !slices.Equal(got, []string{key}) {
			t.Errorf("%s: current = %v, want [%s]", path, got, key)
		}
		if body["current_project_id"] != "" {
			t.Errorf("%s: current project = %v", path, body["current_project_id"])
		}
	}
	_, body := w.get(t, "/")
	if got := linkKeys(body); !slices.Equal(got, []string{"board", "projects", "agents", "models", "agent_isolation", "server"}) {
		t.Fatalf("links = %v", got)
	}
}

func TestAPathThatMatchesNoPageIsNotAnError(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	for _, path := range []string{"/nowhere", "/projects/nope", "/projects/new"} {
		status, body := w.get(t, path)
		if status != 200 || body["current_project_id"] != "" || len(currentKeys(body)) != 0 {
			t.Errorf("%s = %d %v, want no current project and no current link", path, status, body)
		}
	}
}

func TestTheProjectsLinkIsReplacedByNewProjectWhenTheListIsNotServed(t *testing.T) {
	w := newTopBarWorld(t, func(d *web.Deps) { d.ProjectSummaries = nil }, false)
	_, body := w.get(t, "/")
	keys := linkKeys(body)
	if !slices.Contains(keys, "new_project") || slices.Contains(keys, "projects") {
		t.Fatalf("links = %v, want new_project and not projects", keys)
	}
	if link(t, body, "new_project")["href"] != "/projects/new" {
		t.Fatalf("new_project = %v", link(t, body, "new_project"))
	}
}

func TestOnlyTheServedPagesAreLinked(t *testing.T) {
	w := newTopBarWorld(t, func(d *web.Deps) { d.AgentBoard, d.DataPaths, d.Settings, d.SandboxSettings = nil, nil, nil, nil }, false)
	_, body := w.get(t, "/")
	if got := linkKeys(body); !slices.Equal(got, []string{"projects", "agents"}) {
		t.Fatalf("links = %v", got)
	}
}

func TestAgentIsolationCarriesTheAttentionMarkWhenSomethingBlocksAgents(t *testing.T) {
	blocked := newTopBarWorld(t, nil, true)
	_, body := blocked.get(t, "/")
	if link(t, body, "agent_isolation")["attention"] != true {
		t.Fatalf("agent_isolation = %v, want attention", link(t, body, "agent_isolation"))
	}
	if link(t, body, "board")["attention"] != false {
		t.Fatal("only the isolation link carries the mark")
	}
	free := newTopBarWorld(t, nil, false)
	_, body = free.get(t, "/")
	if link(t, body, "agent_isolation")["attention"] != false {
		t.Fatalf("agent_isolation = %v, want no attention", link(t, body, "agent_isolation"))
	}
}

func TestTheBellIsNullWhenTheInboxIsNotServedAndItsPageNullWhenUnread(t *testing.T) {
	off := newTopBarWorld(t, func(d *web.Deps) { d.Notifications = nil }, false)
	_, body := off.get(t, "/")
	if body["notifications"] != nil {
		t.Fatalf("notifications = %v, want null", body["notifications"])
	}
	w := newTopBarWorld(t, nil, false)
	w.inbox.fail = true
	_, body = w.get(t, "/")
	nt := body["notifications"].(map[string]any)
	if nt["inbox_href"] != "/notifications" || nt["page"] != nil {
		t.Fatalf("notifications = %v, want the bell with a null page", nt)
	}
}

func TestNoProjectsGivesAnEmptyListNotNull(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	*w.projects = nil
	_, body := w.get(t, "/")
	if list, ok := body["projects"].([]any); !ok || len(list) != 0 {
		t.Fatalf("projects = %v, want []", body["projects"])
	}
}

func TestEachCallReadsFresh(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	unread := func() float64 {
		_, body := w.get(t, "/")
		return body["notifications"].(map[string]any)["page"].(map[string]any)["unread_count"].(float64)
	}
	if unread() != 3 {
		t.Fatal("first read")
	}
	w.inbox.set(7)
	*w.projects = append(*w.projects, &domain.Project{ID: "p4", Name: "new"})
	if unread() != 7 {
		t.Fatal("the unread count must not be held between calls")
	}
	_, body := w.get(t, "/")
	if len(body["projects"].([]any)) != 4 {
		t.Fatalf("a new project must show at once: %v", body["projects"])
	}
}

func TestAMissingOrMalformedPathIs400InvalidInput(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	for _, raw := range []string{"", "?path=", "?path=projects", "?path=http://evil.test/", "?path=//evil.test/x", "?path=%00", "?path=/a%zz"} {
		res, err := http.Get(w.srv.URL + "/api/top_bar" + raw)
		status, body := decodeTopBar(t, res, err)
		if status != 400 || body["code"] != "INVALID_INPUT" {
			t.Errorf("%q = %d %v, want 400 INVALID_INPUT", raw, status, body)
		}
	}
}
