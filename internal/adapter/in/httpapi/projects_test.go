package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/projects"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// projectActivity stands in for planning and orchestration behind the
// projects context's read ports.
type projectActivity struct {
	tasks    map[string]ports.TaskActivity
	sessions map[string]ports.SessionActivity
	live     map[string][]domain.ProjectSessionRef
}

func (a *projectActivity) TaskActivityByProject(context.Context) (map[string]ports.TaskActivity, error) {
	return a.tasks, nil
}

func (a *projectActivity) SessionActivityByProject(context.Context) (map[string]ports.SessionActivity, error) {
	return a.sessions, nil
}

func (a *projectActivity) LiveProjectSessions(_ context.Context, projectID string) ([]domain.ProjectSessionRef, error) {
	return a.live[projectID], nil
}

func newProjectsServer(t *testing.T) (*httptest.Server, *projects.Service, *projectActivity) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := projects.NewService(sqlite.NewProjectRepository(db), sqlite.NewRepositoryRepository(db), nil)
	act := &projectActivity{tasks: map[string]ports.TaskActivity{}, sessions: map[string]ports.SessionActivity{}, live: map[string][]domain.ProjectSessionRef{}}
	svc.UseActivity(act, act)
	srv := httptest.NewServer(NewRouter(NewHandler(Services{Projects: svc, ProjectFeed: svc})))
	t.Cleanup(srv.Close)
	return srv, svc, act
}

func postProjectJSON(t *testing.T, url string, body any) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, strings.TrimSpace(buf.String())
}

func TestHTTP_ListProjectSummaries_WireShape(t *testing.T) {
	srv, svc, act := newProjectsServer(t)
	ctx := context.Background()

	resp, err := http.Get(srv.URL + "/api/list_project_summaries")
	if err != nil {
		t.Fatal(err)
	}
	var empty bytes.Buffer
	empty.ReadFrom(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(empty.String()) != `{"summaries":[]}` {
		t.Fatalf("no projects = %d %s, want 200 {\"summaries\":[]}", resp.StatusCode, empty.String())
	}

	busy, _ := svc.CreateProject(ctx, "busy", t.TempDir())
	idle, _ := svc.CreateProject(ctx, "Idle", t.TempDir())
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	act.tasks[busy.ID] = ports.TaskActivity{OpenCount: 2, LastUpdatedAt: at}
	act.sessions[busy.ID] = ports.SessionActivity{LiveCount: 1, LastActivityAt: at.Add(-time.Hour)}

	resp, err = http.Get(srv.URL + "/api/list_project_summaries")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Summaries []map[string]json.RawMessage `json:"summaries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Summaries) != 2 {
		t.Fatalf("%d summaries, want 2", len(out.Summaries))
	}
	want := []map[string]string{
		{
			"project":               `{"id":"` + busy.ID + `","name":"busy","root_dir":"` + busy.RootDir + `"}`,
			"repository_count":      `0`,
			"open_task_count":       `2`,
			"running_session_count": `1`,
			"last_activity_at":      `"2026-10-07T12:00:00Z"`,
		},
		{
			"project":               `{"id":"` + idle.ID + `","name":"Idle","root_dir":"` + idle.RootDir + `"}`,
			"repository_count":      `0`,
			"open_task_count":       `0`,
			"running_session_count": `0`,
			"last_activity_at":      `null`,
		},
	}
	for i, w := range want {
		got := out.Summaries[i]
		if len(got) != len(w) {
			t.Errorf("summary %d keys = %v, want %v", i, keysOf(got), w)
		}
		for k, v := range w {
			if string(got[k]) != v {
				t.Errorf("summary %d %s = %s, want %s", i, k, got[k], v)
			}
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestHTTP_ProjectWriteRefusals(t *testing.T) {
	srv, svc, _ := newProjectsServer(t)
	taken, _ := svc.CreateProject(context.Background(), "Taken", t.TempDir())
	for _, c := range []struct {
		path   string
		body   map[string]any
		status int
		code   string
	}{
		{"/api/create_project", map[string]any{"name": "", "root_dir": t.TempDir()}, 400, "INVALID_INPUT"},
		{"/api/create_project", map[string]any{"name": "taken", "root_dir": t.TempDir()}, 409, "PROJECT_NAME_TAKEN"},
		{"/api/create_project", map[string]any{"name": "new", "root_dir": "relative"}, 400, "PROJECT_ROOT_INVALID"},
		{"/api/update_project", map[string]any{"project_id": "missing", "name": "x"}, 404, "PROJECT_NOT_FOUND"},
		{"/api/update_project", map[string]any{"name": "x"}, 400, "INVALID_INPUT"},
		{"/api/update_project", map[string]any{"project_id": taken.ID, "root_dir": "/no/such/dir/anywhere"}, 400, "PROJECT_ROOT_INVALID"},
		{"/api/delete_project", map[string]any{}, 400, "INVALID_INPUT"},
		{"/api/delete_project", map[string]any{"project_id": "missing"}, 404, "PROJECT_NOT_FOUND"},
	} {
		status, body := postProjectJSON(t, srv.URL+c.path, c.body)
		var got map[string]any
		json.Unmarshal([]byte(body), &got)
		if status != c.status || got["code"] != c.code || got["error"] == "" || len(got) != 2 {
			t.Errorf("%s %v = %d %s, want %d {error, code: %s}", c.path, c.body, status, body, c.status, c.code)
		}
	}
}

func TestHTTP_DeleteProject_RefusedWhileSessionsRun(t *testing.T) {
	srv, svc, act := newProjectsServer(t)
	p, _ := svc.CreateProject(context.Background(), "Busy", t.TempDir())
	act.live[p.ID] = []domain.ProjectSessionRef{{ID: "s1", TicketID: "t1", Agent: "go-developer"}, {ID: "s2"}}

	status, body := postProjectJSON(t, srv.URL+"/api/delete_project", map[string]any{"project_id": p.ID})
	if status != http.StatusConflict {
		t.Fatalf("status = %d %s, want 409", status, body)
	}
	var got struct {
		Error   string `json:"error"`
		Code    string `json:"code"`
		Details struct {
			Sessions []map[string]string `json:"sessions"`
		} `json:"details"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "PROJECT_HAS_RUNNING_SESSIONS" || !strings.Contains(got.Error, "s1") {
		t.Fatalf("body = %s", body)
	}
	want := []map[string]string{{"id": "s1", "ticket_id": "t1", "agent": "go-developer"}, {"id": "s2", "ticket_id": "", "agent": ""}}
	if len(got.Details.Sessions) != 2 {
		t.Fatalf("details.sessions = %v, want %v", got.Details.Sessions, want)
	}
	for i, w := range want {
		for k, v := range w {
			if got.Details.Sessions[i][k] != v || len(got.Details.Sessions[i]) != 3 {
				t.Fatalf("details.sessions[%d] = %v, want %v", i, got.Details.Sessions[i], w)
			}
		}
	}

	delete(act.live, p.ID)
	if status, body := postProjectJSON(t, srv.URL+"/api/delete_project", map[string]any{"project_id": p.ID}); status != http.StatusNoContent {
		t.Fatalf("delete once sessions ended = %d %s", status, body)
	}
}

func TestHTTP_ProjectEvents_StreamsEachChange(t *testing.T) {
	srv, _, _ := newProjectsServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/project_events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	dir := t.TempDir()
	_, body := postProjectJSON(t, srv.URL+"/api/create_project", map[string]any{"name": "Live", "root_dir": dir})
	var created struct {
		Project struct{ ID string } `json:"project"`
	}
	json.Unmarshal([]byte(body), &created)
	id := created.Project.ID
	postProjectJSON(t, srv.URL+"/api/update_project", map[string]any{"project_id": id, "name": "Renamed"})
	postProjectJSON(t, srv.URL+"/api/add_ignored_path", map[string]any{"project_id": id, "path": "dist"})
	postProjectJSON(t, srv.URL+"/api/remove_ignored_path", map[string]any{"project_id": id, "path": "dist"})
	postProjectJSON(t, srv.URL+"/api/delete_project", map[string]any{"project_id": id})

	want := []string{
		`{"project":{"id":"` + id + `","name":"Live","root_dir":"` + dir + `"}}`,
		`{"project":{"id":"` + id + `","name":"Renamed","root_dir":"` + dir + `"}}`,
		`{"project":{"id":"` + id + `","name":"Renamed","root_dir":"` + dir + `","ignored_paths":["dist"]}}`,
		`{"project":{"id":"` + id + `","name":"Renamed","root_dir":"` + dir + `"}}`,
		`{"project":{"id":"` + id + `"},"deleted":true}`,
	}
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
	for i, w := range want {
		select {
		case got := <-lines:
			if got != w {
				t.Fatalf("event %d = %s, want %s", i, got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("event %d never came; want %s", i, w)
		}
	}
}
