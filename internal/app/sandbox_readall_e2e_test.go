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
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// TestSandboxReadAll_ASessionStartedAfterTheProjectSettingChangesFollowsIt_EndToEnd
// runs the real wiring (the project reader behind Sandbox.ReadAllRepos) with a
// recording driver: turning "agents can read every repository" off through the
// catalog gives the next implement session the old policy (no other repository
// readable), turning it on gives every repository readable, and a session
// already started keeps the policy it had.
func TestSandboxReadAll_ASessionStartedAfterTheProjectSettingChangesFollowsIt_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	ctx := context.Background()
	dir := t.TempDir()
	repoDir, otherDir := filepath.Join(dir, "repo"), filepath.Join(dir, "other")
	gitInit(t, repoDir)
	gitInit(t, otherDir)

	var (
		cat  catalog.Catalog
		orch *orchestration.Service
		plan *planning.Service
	)
	fxApp := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &orch, &plan),
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
	if _, err := cat.Projects.CreateRepository(ctx, p.ID, "other", "", "https://example.com/other.git", otherDir); err != nil {
		t.Fatal(err)
	}

	driver := &portstest.RecordingSandboxDriver{}
	if orch.Sandbox == nil || orch.Sandbox.ReadAllRepos == nil {
		t.Fatal("the app wires no project reader behind Sandbox.ReadAllRepos")
	}
	orch.Sandbox.Driver = driver // the platform's driver is another wiring's business
	start := func() ports.SandboxPolicy {
		t.Helper()
		tk, err := plan.CreateTicket(ctx, p.ID, "Ticket", "", domain.TicketStatusTodo, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := orch.StartInteractive(ctx, ports.InteractiveRequest{ProjectID: p.ID, RepositoryID: repo.ID, TicketID: tk.ID, Mode: "implement", RunsOn: domain.RunnerTUI}); err != nil {
			t.Fatalf("start: %v", err)
		}
		calls := driver.Calls()
		return calls[len(calls)-1].Policy
	}
	reads := func(p ports.SandboxPolicy) bool { return slices.Contains(p.Readable, otherDir) }

	if on := start(); !reads(on) {
		t.Fatalf("default: the project's other repository is not readable: %v", on.Readable)
	}
	if _, err := cat.Projects.SetProjectAgentsReadAllRepos(ctx, p.ID, false); err != nil {
		t.Fatal(err)
	}
	off := start()
	if reads(off) || !slices.Contains(off.Hidden, otherDir) {
		t.Errorf("off: other repository readable %v or not hidden %v", off.Readable, off.Hidden)
	}
	if _, err := cat.Projects.SetProjectAgentsReadAllRepos(ctx, p.ID, true); err != nil {
		t.Fatal(err)
	}
	if on := start(); !reads(on) {
		t.Errorf("on again: not readable: %v", on.Readable)
	}
	if reads(driver.Calls()[1].Policy) {
		t.Error("the session started with the setting off changed its policy")
	}
}
