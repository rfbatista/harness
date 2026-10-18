//go:build !windows

package orchestration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/codegraph"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// mcpServerEntry is one server of a --mcp-config payload.
type mcpServerEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// mcpServersOf reads the servers of the --mcp-config claude was launched
// with: a file for a headless session, inline JSON for an interactive one.
func mcpServersOf(t *testing.T, args []string) map[string]mcpServerEntry {
	t.Helper()
	raw, ok := flag(args, "--mcp-config")
	if !ok {
		t.Fatalf("no --mcp-config in %q", args)
	}
	if !strings.HasPrefix(raw, "{") {
		b, err := os.ReadFile(raw)
		if err != nil {
			t.Fatalf("read --mcp-config: %v", err)
		}
		raw = string(b)
	}
	var cfg struct {
		MCPServers map[string]mcpServerEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("--mcp-config is not JSON: %v\n%s", err, raw)
	}
	return cfg.MCPServers
}

// allowedToolsOf are the tools of --allowedTools: one comma-joined value
// for an interactive session, one value each for a headless one.
func allowedToolsOf(args []string) []string {
	var out []string
	for i, a := range args {
		if a != "--allowedTools" {
			continue
		}
		for _, v := range args[i+1:] {
			if strings.HasPrefix(v, "--") {
				break
			}
			out = append(out, strings.Split(v, ",")...)
		}
	}
	return out
}

// putIndex writes a repository index into checkout, as a build leaves it.
func putIndex(t *testing.T, checkout string) map[string]string {
	t.Helper()
	files := map[string]string{
		"codegraph.db":     "db",
		"codegraph.db-wal": "wal",
		"codegraph.db-shm": "shm",
		"meta/config.json": "{}",
	}
	for name, body := range files {
		p := filepath.Join(checkout, domain.RepositoryIndexDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// checkCodegraphWired checks the launch carries codegraph's MCP server,
// beside the task's, with its tools allowed and its telemetry off.
func checkCodegraphWired(t *testing.T, args []string, codegraph string) {
	t.Helper()
	servers := mcpServersOf(t, args)
	cg, ok := servers["codegraph"]
	if !ok {
		t.Fatalf("no codegraph MCP server in %+v", servers)
	}
	if cg.Command != codegraph || !slices.Equal(cg.Args, []string{"serve", "--mcp"}) || cg.Type != "" {
		t.Errorf("codegraph server = %+v, want stdio %s serve --mcp", cg, codegraph)
	}
	if cg.Env["CODEGRAPH_TELEMETRY"] != "0" || cg.Env["DO_NOT_TRACK"] != "1" {
		t.Errorf("codegraph server env = %v, want its telemetry off", cg.Env)
	}
	if task := servers["task"]; task.Type != "http" || task.URL == "" {
		t.Errorf("task server = %+v, want it beside codegraph", task)
	}
	tools := allowedToolsOf(args)
	if !slices.Contains(tools, "mcp__codegraph") {
		t.Errorf("--allowedTools = %q, want codegraph's tools allowed", tools)
	}
	if !slices.Contains(tools, "mcp__task__get_task") {
		t.Errorf("--allowedTools = %q, want the task tools still allowed", tools)
	}
}

// codegraph on and installed, an index in the repository's checkout: the
// session runs with codegraph's MCP server in its worktree and starts from
// a copy of the index.
func TestAHeadlessSessionRunsWithCodegraphAndACopyOfTheRepositoryIndex(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	savers := withTokenSavers(t, svc)
	codegraph := savers.install(t, domain.TokenSaverCodegraph, "codegraph")
	savers.set(t, domain.ActivationLevelRepository, "r1", domain.TokenSaverCodegraph, domain.ActivationOn)
	checkout := svc.catalog.Repositories.(*fakeResolver).repo.RootDir
	files := putIndex(t, checkout)

	sess, launch := startHeadless(t, svc, w.task.ID)
	checkCodegraphWired(t, launch.Args, codegraph)
	if launch.Env["DO_NOT_TRACK"] != "1" {
		t.Errorf("the session's env = %v, want DO_NOT_TRACK=1", launch.Env)
	}
	// startHeadless found the launch in the worktree: claude runs there, and
	// so does the codegraph server it spawns.
	for name, body := range files {
		got, err := os.ReadFile(filepath.Join(sess.WorkingDir, domain.RepositoryIndexDir, name))
		if err != nil || string(got) != body {
			t.Errorf("worktree index %s = %q (%v), want %q", name, got, err, body)
		}
	}
	if !slices.Equal(sess.TokenSavers, []domain.TokenSaver{domain.TokenSaverCodegraph}) {
		t.Errorf("session records %v active, want [codegraph]", sess.TokenSavers)
	}
}

// No index built yet: the session starts anyway, with the MCP server and
// no index in its worktree.
func TestASessionStartsWithCodegraphBeforeItsRepositoryIndexIsBuilt(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	savers := withTokenSavers(t, svc)
	codegraph := savers.install(t, domain.TokenSaverCodegraph, "codegraph")
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverCodegraph, domain.ActivationOn)

	sess, launch := startHeadless(t, svc, w.task.ID)
	checkCodegraphWired(t, launch.Args, codegraph)
	if _, err := os.Stat(filepath.Join(sess.WorkingDir, domain.RepositoryIndexDir)); !os.IsNotExist(err) {
		t.Errorf("worktree has %s (%v), want none", domain.RepositoryIndexDir, err)
	}
	if !slices.Equal(sess.TokenSavers, []domain.TokenSaver{domain.TokenSaverCodegraph}) {
		t.Errorf("session records %v active, want [codegraph]", sess.TokenSavers)
	}
}

// An interactive session gets codegraph the same way, telemetry env
// included, and keeps its server when resumed.
func TestAnInteractiveSessionRunsWithCodegraph(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	w := withShells(t, svc)
	savers := withTokenSavers(t, svc)
	codegraph := savers.install(t, domain.TokenSaverCodegraph, "codegraph")
	savers.set(t, domain.ActivationLevelTask, w.task.ID, domain.TokenSaverCodegraph, domain.ActivationOn)
	putIndex(t, svc.catalog.Repositories.(*fakeResolver).repo.RootDir)

	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	checkCodegraphWired(t, launch.Args, codegraph)
	env := specEnv(launch.Spec)
	if env["CODEGRAPH_TELEMETRY"] != "0" || env["DO_NOT_TRACK"] != "1" {
		t.Errorf("the session's env = %v, want codegraph's telemetry off", env)
	}
	if launch.Dir != sess.WorkingDir {
		t.Errorf("claude runs in %q, want the worktree %q", launch.Dir, sess.WorkingDir)
	}
	if _, err := os.Stat(filepath.Join(sess.WorkingDir, domain.RepositoryIndexDir, "codegraph.db")); err != nil {
		t.Errorf("worktree index: %v", err)
	}

	ctx := context.Background()
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.EndInteractive(ctx, sess.ID, 0, false) })
	checkCodegraphWired(t, launchOf(t, spec).Args, codegraph)
}

// While a build runs on the repository's checkout its index may be half
// written: the session starts without a copy, as before a first build.
func TestASessionStartsWithoutTheIndexWhileItsBuildRuns(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	savers := withTokenSavers(t, svc)
	cg := savers.install(t, domain.TokenSaverCodegraph, "codegraph")
	savers.set(t, domain.ActivationLevelRepository, "r1", domain.TokenSaverCodegraph, domain.ActivationOn)
	repo := svc.catalog.Repositories.(*fakeResolver).repo
	putIndex(t, repo.RootDir)
	stub := newStubCodegraph(t)
	indexes := workspaces.NewRepositoryIndexes(workspaces.RepositoryIndexDeps{
		Projects:      indexProjects{&domain.Repository{ID: "r1", ProjectID: "p1", Name: "api", RootDir: repo.RootDir}},
		Sessions:      svc,
		Installations: installedCodegraph(stub.path),
		Activations:   codegraphOnNowhere{},
		Indexer:       codegraph.NewIndexer(),
		Excludes:      gitcli.NewExcludes(),
	})
	t.Cleanup(indexes.Close)
	svc.IndexBuilds = indexes

	release := stub.hold(t)
	if _, err := indexes.RebuildRepositoryIndex(context.Background(), "r1"); err != nil {
		t.Fatal(err)
	}
	stub.waitCalls(t, 1)
	sess, launch := startHeadless(t, svc, w.task.ID)
	release()
	indexes.Wait()

	checkCodegraphWired(t, launch.Args, cg)
	if _, err := os.Stat(filepath.Join(sess.WorkingDir, domain.RepositoryIndexDir)); !os.IsNotExist(err) {
		t.Errorf("worktree has %s (%v) though a build was running, want none", domain.RepositoryIndexDir, err)
	}
}
