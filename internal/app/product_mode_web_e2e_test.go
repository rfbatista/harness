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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
)

// TestProductModeWeb_EndToEnd exercises the backend's product-mode contract
// (ADR 0016) through the web client's own HTTP boundary, on the real server
// wiring: the orchestration service starts an interactive session with no
// ticket, exactly as the board's "Brainstorm a feature" dialog does over
// start_interactive_session; the planning service creates the feature task,
// its child and its PRD document, as the task-product skill would. No fakes
// stand in for ports.TicketReader, ports.SessionReader or ports.DocumentReader.
func TestProductModeWeb_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("web client not built; run `make web`")
	}
	ctx := context.Background()
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var (
		cat  catalog.Catalog
		plan *planning.Service
		orch *orchestration.Service
	)
	fxApp := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &plan, &orch),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })
	if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}

	p, err := cat.Projects.CreateProject(ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := cat.Projects.CreateRepository(ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := cat.Agents.CreateAgent(ctx, "product-manager", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, ProjectFeed: cat.Projects, Sessions: orch, Planning: plan, Agents: cat.Agents,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, Documents: plan,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// The board offers the dialog, with the real agent and repository choices.
	board := getBody(t, srv.URL+"/projects/"+p.ID)
	for _, want := range []string{`x-data="sessionsBrainstorm"`, `data-agents=`, `&#34;id&#34;:&#34;` + agent.ID + `&#34;,&#34;name&#34;:&#34;product-manager&#34;`, `<option value="` + repo.ID + `">harness</option>`} {
		if !strings.Contains(board, want) {
			t.Errorf("board lacks %q", want)
		}
	}

	// Start brainstorm does, over the wire, exactly as the dialog's
	// sessionsBrainstorm component builds it (toStartBody): mode=product,
	// no ticket_id at all (an empty string would not do; the server accepts
	// a missing one only in product mode), runs_on=server.
	sess := postStartSession(t, srv.URL, map[string]any{
		"project_id": p.ID, "repository_id": repo.ID, "agent_id": agent.ID,
		"mode": "product", "prompt": "Export to CSV.", "auto_accept": "off", "runs_on": "server",
	})
	if sess.TicketID != "" || sess.Mode != domain.SessionModeProduct {
		t.Fatalf("session = %+v, want a ticket-less product-mode session", sess)
	}

	// The same route refuses every other mode without a ticket, as the
	// locked-mode form relies on.
	if code := postStartSessionRejected(t, srv.URL, map[string]any{"project_id": p.ID, "repository_id": repo.ID, "mode": "architect", "runs_on": "server"}); code != "INVALID_INPUT" {
		t.Errorf("architect without a ticket = %q, want INVALID_INPUT", code)
	}

	// The Feature brainstorms page lists it: the project's sessions page,
	// not the board, not any task page.
	brainstorms := getBody(t, srv.URL+"/projects/"+p.ID+"/sessions")
	for _, want := range []string{"<h1>Feature brainstorms</h1>", sess.Task, `"ticket_id":""`, `data-locked-mode="product"`} {
		if !strings.Contains(brainstorms, want) {
			t.Errorf("feature brainstorms page lacks %q:\n%s", want, brainstorms)
		}
	}

	// The agent's work: a feature task, its PRD, and a child with a brief,
	// created the way the task-product skill creates them (create_task,
	// a document titled "PRD: …" linked to the feature).
	feature, err := plan.CreateTicket(ctx, p.ID, "Exports", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	prd, err := plan.CreateDocument(p.ID, "PRD: Exports", "# Exports\n\nGoal, users, scope.", domain.DocumentFormatMarkdown, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.LinkDocument(feature.ID, prd.ID); err != nil {
		t.Fatal(err)
	}
	child, err := plan.CreateTicket(ctx, p.ID, "CSV export", "", domain.TicketStatusTodo, feature.ID)
	if err != nil {
		t.Fatal(err)
	}
	brief, err := plan.CreateDocument(p.ID, "Brief: CSV export", "# Brief\n\nWhat to build.", domain.DocumentFormatMarkdown, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.LinkDocument(child.ID, brief.ID); err != nil {
		t.Fatal(err)
	}

	// The board shows the real feature cues, over the real ParentID and the
	// real ticket list (no fake TicketReader in the way).
	boardAfter := getBody(t, srv.URL+"/projects/"+p.ID)
	for _, want := range []string{`data-parent-title`, ">Exports<", "1 task"} {
		if !strings.Contains(boardAfter, want) {
			t.Errorf("board after feature creation lacks %q", want)
		}
	}

	// The feature task's page: its PRD, its child, the child's brief.
	featurePage := getBody(t, srv.URL+"/projects/"+p.ID+"/tasks/"+feature.ID)
	for _, want := range []string{
		`x-data="tasksFeature"`, `data-prd`, "PRD: Exports",
		`data-child-id="` + child.ID + `"`, "CSV export", "Brief: CSV export",
	} {
		if !strings.Contains(featurePage, want) {
			t.Errorf("feature task page lacks %q", want)
		}
	}

	// The child task's page: its crumb to the feature.
	childPage := getBody(t, srv.URL+"/projects/"+p.ID+"/tasks/"+child.ID)
	if !strings.Contains(childPage, `data-parent-task="`+feature.ID+`"`) || !strings.Contains(childPage, "Exports /") {
		t.Errorf("child task page lacks the parent crumb:\n%s", childPage)
	}
}

// postStartSession posts body to /api/start_interactive_session, exactly as
// the browser's sessionsBrainstorm or sessionsNewSession would, and returns
// the created session; it fails the test on a non-201 response.
func postStartSession(t *testing.T, base string, body map[string]any) *domain.Session {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(base+"/api/start_interactive_session", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start_interactive_session: %d\n%s", res.StatusCode, raw)
	}
	var out struct {
		Session *domain.Session `json:"session"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode session: %v\n%s", err, raw)
	}
	return out.Session
}

// postStartSessionRejected posts a request expected to fail and returns the
// coded error, as the web client's errors.js reads it.
func postStartSessionRejected(t *testing.T, base string, body map[string]any) string {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(base+"/api/start_interactive_session", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode == http.StatusCreated {
		t.Fatalf("start_interactive_session: expected a refusal, got 201\n%s", raw)
	}
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(raw, &out)
	return out.Code
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d\n%s", url, res.StatusCode, b)
	}
	return string(b)
}
