//go:build !windows

package app

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"go.uber.org/fx"

	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// TestSandboxSettings_ASessionStartedAfterAChangeSeesIt_EndToEnd runs the real
// wiring with a recording sandbox driver: the switch and the extra paths the
// person saves reach the policy of the next session started, and a session
// already started keeps the policy it had.
func TestSandboxSettings_ASessionStartedAfterAChangeSeesIt_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	ctx := context.Background()
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)

	var (
		cat   catalog.Catalog
		orch  *orchestration.Service
		store *sandboxsettings.Store
	)
	fxApp := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &orch, &store),
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

	driver := &portstest.RecordingSandboxDriver{}
	// The platform's driver is another wiring's business: a recording one
	// stands in, and the settings are bound to it as the app binds them.
	orch.Sandbox = &orchestration.Sandbox{Driver: driver}
	bindSandboxSettings(orch, store)
	start := func() {
		t.Helper()
		if _, _, err := orch.StartInteractive(ctx, ports.InteractiveRequest{ProjectID: p.ID, RepositoryID: repo.ID, Mode: "product", RunsOn: domain.RunnerTUI}); err != nil {
			t.Fatalf("start: %v", err)
		}
	}

	start()
	first := driver.Calls()
	if len(first) != 1 || slices.Contains(first[0].Policy.Writable, "/opt/extra") {
		t.Fatalf("first session: %+v", first)
	}

	if _, err := store.AddPath(ports.SandboxPath{Path: "/opt/extra", Write: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddPath(ports.SandboxPath{Path: "/opt/notes"}); err != nil {
		t.Fatal(err)
	}
	start()
	calls := driver.Calls()
	if len(calls) != 2 {
		t.Fatalf("driver calls = %d, want 2", len(calls))
	}
	if p := calls[1].Policy; !slices.Contains(p.Writable, "/opt/extra") || !slices.Contains(p.Readable, "/opt/notes") {
		t.Errorf("second session policy lacks the new paths: %+v", p)
	}
	if slices.Contains(calls[0].Policy.Writable, "/opt/extra") {
		t.Errorf("the running session's policy changed: %+v", calls[0].Policy)
	}

	if err := store.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	start()
	if n := len(driver.Calls()); n != 2 {
		t.Fatalf("a session started with the sandbox off was wrapped (%d calls)", n)
	}
	if err := store.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	start()
	if n := len(driver.Calls()); n != 3 {
		t.Fatalf("driver calls = %d after switching back on, want 3", n)
	}
}
