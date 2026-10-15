package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/gitcredentials"
	"operators-mcp/internal/application/orchestration"
)

// The server stores a credential through the API, seals it with a key file
// next to the database, and serves it back without its secret. The graph is
// the shipped one minus the Genkit flows, which register globally once per
// process.
func TestTheServerKeepsGitCredentials_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	var creds *gitcredentials.Service
	a := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule, GitCredentialsModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		fx.Populate(&creds),
		fx.NopLogger,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background()) })
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{GitCredentials: creds})))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/create_git_credential", "application/json",
		strings.NewReader(`{"name":"GitHub","kind":"https_token","host":"github.com","token":"ghp_e2etoken9876"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.Contains(string(body), "ghp_e2etoken9876") || !strings.Contains(string(body), `"fingerprint":"…9876"`) {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	info, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret.key next to the database: %v, %v", info, err)
	}

	resp, err = http.Get(srv.URL + "/api/list_git_credentials")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Credentials []map[string]any `json:"credentials"`
		Locked      bool             `json:"locked"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil || len(list.Credentials) != 1 || list.Credentials[0]["host"] != "github.com" || list.Locked {
		t.Fatalf("list: %+v, %v", list, err)
	}
}

// Sessions and their app runs get the stored credentials in the shipped
// graph.
func TestSessionsAndAppRunsAreBoundToTheStoredCredentials(t *testing.T) {
	dir := t.TempDir()
	var (
		orch  *orchestration.Service
		runs  *apps.Service
		creds *gitcredentials.Service
	)
	a := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule, GitCredentialsModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		fx.Populate(&orch, &runs, &creds),
		fx.NopLogger,
	)
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	if orch.GitAuth != creds || runs.GitAuth != creds {
		t.Fatalf("sessions %v and app runs %v are not bound to the credentials", orch.GitAuth, runs.GitAuth)
	}
}

// The default database path is relative ("data.db"), but sessions run git
// from their worktrees: every path their git environment names must be
// absolute, or ssh looks for its agent under the worktree.
func TestARelativeDatabaseStillGivesAbsoluteGitDirectories(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	dir, err := secretsDir(Config{DBPath: "data.db"})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(wd); !filepath.IsAbs(dir) || evalOr(dir) != want {
		t.Fatalf("data dir %q for a relative database, want %q", dir, wd)
	}
}

func evalOr(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
