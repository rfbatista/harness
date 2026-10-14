package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/fx"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
)

// Load of the freeze test: concurrentSessions sessions each stream events as
// fast as the fake claude writes them while the HTTP reads below run for
// loadDuration. A read slower than readBound is a freeze.
const (
	concurrentSessions = 10
	loadDuration       = 6 * time.Second
	readBound          = time.Second
)

// streamingClaude is a stand-in claude that, once started, writes a steady
// stream of agent events (text, tool use and usage-bearing turn results)
// until it is stopped.
const streamingClaude = `#!/bin/sh
echo '{"type":"system","subtype":"init","session_id":"fake"}'
i=0
while :; do
  echo '{"type":"assistant","session_id":"fake","message":{"role":"assistant","content":[{"type":"text","text":"working '$i'"}]}}'
  echo '{"type":"assistant","session_id":"fake","message":{"role":"assistant","content":[{"type":"tool_use","id":"t'$i'","name":"Read","input":{"file_path":"x"}}]}}'
  echo '{"type":"result","subtype":"success","session_id":"fake","result":"ok","total_cost_usd":0.001,"usage":{"input_tokens":5,"output_tokens":9}}'
  i=$((i+1))
done
`

// freezeE2E is the server wired as it runs, on a database file, with the
// HTTP API on a test server.
type freezeE2E struct {
	t       *testing.T
	ctx     context.Context
	app     *fx.App
	orch    *orchestration.Service
	art     *artifacts.Service
	srv     *httptest.Server
	db      *gorm.DB
	project string
	repo    string
	ticket  string

	mu      sync.Mutex
	slowest time.Duration
	reads   int
}

func startFreezeE2E(t *testing.T, dir, dbPath string) *freezeE2E {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	repoDir := filepath.Join(dir, "repo")
	if _, err := os.Stat(repoDir); err != nil {
		gitInit(t, repoDir)
	}
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte(streamingClaude), 0o755); err != nil {
		t.Fatal(err)
	}

	e := &freezeE2E{t: t, ctx: context.Background()}
	var (
		cat  catalog.Catalog
		plan *planning.Service
	)
	e.app = fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: dbPath, Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		fx.Populate(&e.orch, &e.art, &plan, &cat, &e.db),
		fx.NopLogger,
	)
	if err := e.app.Start(e.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.app.Stop(context.Background()) })

	e.srv = httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Sessions: e.orch, Planning: plan, Artifacts: e.art})))
	t.Cleanup(e.srv.Close)

	if ps, err := cat.Projects.ListProjects(e.ctx); err == nil && len(ps) > 0 {
		e.project = ps[0].ID
		return e // a database that already holds the project
	}
	if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := cat.Projects.CreateProject(e.ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cat.Projects.CreateRepository(e.ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(e.ctx, p.ID, "Freeze", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	e.project, e.repo, e.ticket = p.ID, r.ID, tk.ID
	return e
}

// get reads path over HTTP and fails the test when it errors, answers other
// than 200, reports a locked database, or takes longer than readBound.
func (e *freezeE2E) get(path string) []byte {
	e.t.Helper()
	start := time.Now()
	res, err := http.Get(e.srv.URL + path)
	if err != nil {
		e.t.Errorf("GET %s: %v", path, err)
		return nil
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	took := time.Since(start)
	e.mu.Lock()
	if took > e.slowest {
		e.slowest = took
	}
	e.reads++
	e.mu.Unlock()
	if res.StatusCode != http.StatusOK {
		e.t.Errorf("GET %s: %d %s", path, res.StatusCode, body)
	}
	if strings.Contains(string(body), "database is locked") {
		e.t.Errorf("GET %s: %s", path, body)
	}
	if took > readBound {
		e.t.Errorf("GET %s took %v, over %v", path, took, readBound)
	}
	return body
}

// With several sessions streaming events into a database file, the web UI's
// reads of tasks, sessions and session history keep answering promptly and
// never come back empty or failed for a locked database.
func TestConcurrentSessionsOnAFileDatabaseAnswerPromptly(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler)) // a line per event
	t.Cleanup(func() { slog.SetDefault(prev) })
	dir := t.TempDir()
	e := startFreezeE2E(t, dir, filepath.Join(dir, "data.db"))

	ids := e.startSessions(concurrentSessions)

	deadline := time.Now().Add(loadDuration)
	var wg sync.WaitGroup
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seen := map[string]int{}
			for time.Now().Before(deadline) && !t.Failed() {
				e.get("/api/list_tickets?project_id=" + e.project)
				var list struct {
					Sessions []json.RawMessage `json:"sessions"`
				}
				if err := json.Unmarshal(e.get("/api/sessions?project_id="+e.project), &list); err == nil && len(list.Sessions) < concurrentSessions {
					t.Errorf("listed %d sessions while %d run", len(list.Sessions), concurrentSessions)
				}
				for _, id := range ids {
					var h struct {
						Events []json.RawMessage `json:"events"`
					}
					_ = json.Unmarshal(e.get("/api/sessions/"+id+"/history"), &h)
					if len(h.Events) < seen[id] {
						t.Errorf("session %s history shrank from %d to %d events", id, seen[id], len(h.Events))
					}
					seen[id] = len(h.Events)
				}
			}
		}()
	}
	wg.Wait()
	t.Logf("%d reads, slowest %v", e.reads, e.slowest)
}

// Starting the server on an existing rollback-journal database switches it to
// WAL, with its data intact.
func TestServerStartSwitchesARollbackJournalDatabaseToWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	e := startFreezeE2E(t, dir, dbPath)
	project := e.project
	e.srv.Close()
	if err := e.app.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := journalMode(t, dbPath, "PRAGMA journal_mode=DELETE"); got != "delete" {
		t.Fatalf("back to rollback journal: %q", got)
	}

	e = startFreezeE2E(t, dir, dbPath)
	if got := journalMode(t, dbPath, "PRAGMA journal_mode"); got != "wal" {
		t.Errorf("journal_mode after start = %q, want wal", got)
	}
	if e.project != project {
		t.Errorf("project after the switch = %q, want %q", e.project, project)
	}
	if body := e.get("/api/list_tickets?project_id=" + project); !strings.Contains(string(body), "Freeze") {
		t.Errorf("the task is gone after the switch: %s", body)
	}
}

// journalMode runs a journal_mode pragma on dbPath over its own connection.
func journalMode(t *testing.T, dbPath, pragma string) string {
	t.Helper()
	db, err := sql.Open(gormsqlite.DriverName, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow(pragma).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	return mode
}
