package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/gitcredentials"
	"operators-mcp/internal/application/orchestration"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// The server as it ships stores a credential, seals it with a key file next
// to the database, and serves it back without its secret.
func TestTheServerKeepsGitCredentials_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	addr := freeAddr(t)
	app := New(Config{HTTPAddr: addr, MCPAddr: freeAddr(t), DBPath: filepath.Join(dir, "harness.db"), Root: dir,
		ClaudeBin: "/bin/true", SessionShell: "direct"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	base := "http://" + addr
	post := func(path, body string) (int, string) {
		t.Helper()
		resp, err := http.Post(base+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	status, body := post("/api/create_git_credential", `{"name":"GitHub","kind":"https_token","host":"github.com","token":"ghp_e2etoken9876"}`)
	if status != 200 || strings.Contains(body, "ghp_e2etoken9876") || !strings.Contains(body, `"fingerprint":"…9876"`) {
		t.Fatalf("create: %d %s", status, body)
	}
	info, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret.key next to the database: %v, %v", info, err)
	}

	resp, err := http.Get(base + "/api/list_git_credentials")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Credentials []map[string]any `json:"credentials"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil || len(list.Credentials) != 1 || list.Credentials[0]["host"] != "github.com" {
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
