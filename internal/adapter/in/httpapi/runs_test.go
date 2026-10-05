package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"operators-mcp/internal/adapter/out/agents/command"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type oneSession struct{ s *domain.Session }

func (o oneSession) Get(_ context.Context, id string) (*domain.Session, error) {
	if id == o.s.ID {
		return o.s, nil
	}
	return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
}
func (oneSession) List(context.Context, ports.SessionFilter) ([]*domain.Session, error) { return nil, nil }

func TestHTTP_RunsStartListAttachStop(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	deps := catalog.SQLiteDeps(db)
	deps.RunCommands = sqlite.NewRunCommandRepository(db)
	cat := catalog.New(deps)
	host := termhost.New(shell.Direct{}, ptyunix.New(), command.Agent{Shell: "/bin/sh"})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	services := servicesOf(cat)
	services.Apps = apps.NewService(oneSession{&domain.Session{ID: "s1", WorkingDir: t.TempDir()}}, cat.Projects, host)
	srv := httptest.NewServer(NewRouter(NewHandler(services)))
	defer srv.Close()

	post := func(path, body string) (int, map[string]any) {
		res, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}

	code, out := post("/api/start_run", `{"session_id":"s1","command":"echo app is up; sleep 30"}`)
	if code != http.StatusOK {
		t.Fatalf("start: %d %v", code, out)
	}
	runID := out["run"].(map[string]any)["id"].(string)

	res, _ := http.Get(srv.URL + "/api/list_runs?session_id=s1")
	var listed struct{ Runs []map[string]any }
	_ = json.NewDecoder(res.Body).Decode(&listed)
	res.Body.Close()
	if len(listed.Runs) != 1 || listed.Runs[0]["status"] != "running" {
		t.Fatalf("list = %+v", listed.Runs)
	}

	// The terminal socket's first snapshot replays the run's output.
	deadline := time.Now().Add(5 * time.Second)
	var screen string
	for time.Now().Before(deadline) && !strings.Contains(screen, "app is up") {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/runs/"+runID+"/terminal", nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		_, data, err := conn.Read(ctx)
		cancel()
		conn.CloseNow()
		if err != nil {
			t.Fatal(err)
		}
		var m ports.TerminalMessage
		_ = json.Unmarshal(data, &m)
		if m.Type == "snapshot" && m.Snapshot != nil {
			screen = m.Snapshot.Screen
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(screen, "app is up") {
		t.Fatalf("snapshot never replayed the output: %q", screen)
	}

	code, out = post("/api/stop_run", `{"run_id":"`+runID+`"}`)
	if code != http.StatusOK || out["run"].(map[string]any)["status"] != "stopped" {
		t.Fatalf("stop: %d %v", code, out)
	}
	if code, out := post("/api/stop_run", `{"run_id":"nope"}`); code != http.StatusNotFound || out["code"] != "RUN_NOT_FOUND" {
		t.Fatalf("stop unknown: %d %v", code, out)
	}
	if code, out := post("/api/start_run", `{"session_id":"s1","name":"server"}`); code != http.StatusNotFound || out["code"] != "RUN_COMMAND_NOT_FOUND" {
		t.Fatalf("unknown saved command: %d %v", code, out)
	}
}

func TestHTTP_RunCommands(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	repos := sqlite.NewRepositoryRepository(db)
	p, _ := projects.Create("proj", t.TempDir())
	repo, _ := repos.Create(p.ID, "api", "", "file:///x", t.TempDir())
	deps := catalog.SQLiteDeps(db)
	deps.Projects, deps.Repositories = projects, repos
	deps.RunCommands = sqlite.NewRunCommandRepository(db)
	router := NewRouter(NewHandler(servicesOf(catalog.New(deps))))
	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	if rec := call("POST", "/api/save_repository_run_command", `{"repository_id":"`+repo.ID+`","name":"server","command":"make air"}`); rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if rec := call("GET", "/api/list_repository_run_commands?repository_id="+repo.ID, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"command":"make air"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := call("POST", "/api/save_repository_run_command", `{"repository_id":"`+repo.ID+`","name":"a/b","command":"x"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "INVALID_NAME") {
		t.Fatalf("bad name: %d %s", rec.Code, rec.Body)
	}
	if rec := call("POST", "/api/delete_repository_run_command", `{"repository_id":"`+repo.ID+`","name":"server"}`); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
}
