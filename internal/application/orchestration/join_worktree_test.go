package orchestration

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// A session started on another session's worktree (Glossary: joined session):
// it runs on the worktree's branch and creates no branch and no worktree.

// Get makes the fake provisioner answer like workspaces.Service for a
// worktree it handed out.
func (f *fakeProvisioner) Get(_ context.Context, id string) *domain.Workspace {
	for _, ws := range f.created {
		if ws.ID == id {
			return ws
		}
	}
	return nil
}

type countingBranchNames struct{ calls int }

func (c *countingBranchNames) CheckBranchName(context.Context, string, string, string) (*domain.BranchNameCheck, error) {
	c.calls++
	return &domain.BranchNameCheck{Valid: true, Available: true}, nil
}

type countingBases struct{ calls int }

func (c *countingBases) PrepareBase(context.Context, string, string, string) error {
	c.calls++
	return nil
}

var _ ports.BaseBranchPreparer = (*countingBases)(nil)

func startOn(t *testing.T, svc *Service, req InteractiveRequest) (*domain.Session, error) {
	t.Helper()
	if req.ProjectID == "" {
		req.ProjectID = "p1"
	}
	if req.RepositoryID == "" {
		req.RepositoryID = "r1"
	}
	if req.TicketID == "" {
		req.TicketID = "tk1"
	}
	req.RunsOn = domain.RunnerTUI
	sess, _, err := svc.StartInteractive(context.Background(), req)
	return sess, err
}

func joinWorld(t *testing.T) (*Service, *fakeProvisioner, *domain.Session) {
	t.Helper()
	svc, prov := newInteractiveService(t)
	// The agent sandbox is on: read-only access is only enforced under it.
	svc.Sandbox = &Sandbox{Driver: &portstest.RecordingSandboxDriver{}, Home: t.TempDir()}
	creator, err := startOn(t, svc, InteractiveRequest{Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, prov, creator
}

func TestASessionStartedOnAnotherSessionsWorktreeRunsThereAndCreatesNothing(t *testing.T) {
	svc, prov, creator := joinWorld(t)
	if creator.Access != domain.AccessWrite || creator.Joined {
		t.Fatalf("a creator records access %q joined %v, want write and not joined", creator.Access, creator.Joined)
	}

	joiner, err := startOn(t, svc, InteractiveRequest{Mode: "test", WorktreeID: creator.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if len(prov.created) != 1 || len(prov.discarded) != 0 || len(prov.deleted) != 0 {
		t.Errorf("created %d, discarded %v, deleted %v; a join creates and removes nothing", len(prov.created), prov.discarded, prov.deleted)
	}
	if joiner.WorkspaceID != creator.WorkspaceID || joiner.Branch != creator.Branch || joiner.WorkingDir != creator.WorkingDir {
		t.Errorf("joiner on %s %s %s, creator on %s %s %s", joiner.WorkspaceID, joiner.Branch, joiner.WorkingDir, creator.WorkspaceID, creator.Branch, creator.WorkingDir)
	}
	if joiner.Access != domain.AccessWrite || !joiner.Joined {
		t.Errorf("joiner access %q joined %v, want write and joined", joiner.Access, joiner.Joined)
	}
	stored := svc.sessions.Get(joiner.ID)
	if stored == nil || stored.Joined != true || stored.Access != domain.AccessWrite || stored.WorkspaceID != creator.WorkspaceID {
		t.Errorf("the stored record is %+v", stored)
	}
}

func TestAStartOnAWorktreeIsRefusedBeforeAnythingIsCreated(t *testing.T) {
	svc, prov, creator := joinWorld(t)
	names, bases := &countingBranchNames{}, &countingBases{}
	svc.UseBranchNames(names)
	svc.UseBaseBranches(bases)

	// A worktree a session of another task made.
	foreign := &domain.Workspace{ID: "ws-foreign", RepositoryID: "r1", Branch: "other/work", Path: t.TempDir()}
	prov.created = append(prov.created, foreign)
	if _, err := svc.sessions.Create(&domain.Session{ID: "other", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk-other", WorkspaceID: "ws-foreign", WorkingDir: foreign.Path, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	// A worktree whose folder is gone.
	gone := &domain.Workspace{ID: "ws-gone", RepositoryID: "r1", Branch: "gone/work", Path: t.TempDir() + "/missing"}
	prov.created = append(prov.created, gone)
	if _, err := svc.sessions.Create(&domain.Session{ID: "gone-session", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", WorkspaceID: "ws-gone", WorkingDir: gone.Path, Status: domain.SessionStopped}); err != nil {
		t.Fatal(err)
	}

	before, created := len(svc.sessions.List(context.Background(), ports.SessionFilter{})), len(prov.created)
	for name, c := range map[string]struct {
		req  InteractiveRequest
		code string
	}{
		"unknown worktree":      {InteractiveRequest{WorktreeID: "nope"}, "WORKTREE_NOT_FOUND"},
		"folder gone":           {InteractiveRequest{WorktreeID: "ws-gone"}, "WORKTREE_NOT_FOUND"},
		"another task's":        {InteractiveRequest{WorktreeID: "ws-foreign"}, "WORKTREE_NOT_ON_TASK"},
		"with a base branch":    {InteractiveRequest{WorktreeID: creator.WorkspaceID, BaseBranch: "main"}, "WORKTREE_WITH_BASE_BRANCH"},
		"with a branch name":    {InteractiveRequest{WorktreeID: creator.WorkspaceID, BranchName: "mine"}, "WORKTREE_WITH_BASE_BRANCH"},
		"a bad access":          {InteractiveRequest{WorktreeID: creator.WorkspaceID, Access: "admin"}, "ACCESS_INVALID"},
		"a bad access, no join": {InteractiveRequest{Access: "admin"}, "ACCESS_INVALID"},
		"an architect":          {InteractiveRequest{Mode: "architect", WorktreeID: creator.WorkspaceID}, "INVALID_INPUT"},
		"a product session":     {InteractiveRequest{Mode: "product", TicketID: "x", WorktreeID: creator.WorkspaceID}, "INVALID_INPUT"},
	} {
		t.Run(name, func(t *testing.T) {
			if c.req.Mode == "" {
				c.req.Mode = "implement"
			}
			if c.req.Mode == "product" {
				c.req.TicketID = "tk1"
			}
			_, err := startOn(t, svc, c.req)
			if errs.Code(err) != c.code {
				t.Fatalf("error %v, want code %s", err, c.code)
			}
		})
	}
	if names.calls != 0 || bases.calls != 0 {
		t.Errorf("branch name checks %d, base fetches %d; a refused join runs neither", names.calls, bases.calls)
	}
	if after := len(svc.sessions.List(context.Background(), ports.SessionFilter{})); after != before || len(prov.created) != created || len(prov.discarded) != 0 {
		t.Errorf("sessions %d→%d, worktrees %d→%d, discarded %v; a refusal creates nothing", before, after, created, len(prov.created), prov.discarded)
	}
}

func TestAJoinThatFailsAfterTheWorktreeIsFoundLeavesTheWorktreeInPlace(t *testing.T) {
	svc, prov, creator := joinWorld(t)
	// A sandbox that is on and has no driver refuses the start late, once the
	// worktree has been resolved.
	svc.Sandbox = &Sandbox{Enabled: func() bool { return true }}
	_, err := startOn(t, svc, InteractiveRequest{Mode: "test", WorktreeID: creator.WorkspaceID})
	if err == nil {
		t.Fatal("the start was not refused")
	}
	if len(prov.discarded) != 0 || len(prov.deleted) != 0 {
		t.Errorf("discarded %v, deleted %v; the join made nothing, so it takes nothing back", prov.discarded, prov.deleted)
	}
	if _, statErr := os.Stat(creator.WorkingDir); statErr != nil {
		t.Errorf("the worktree is gone: %v", statErr)
	}
}

func TestDeletingTheCreatorKeepsTheWorktreeWhileAnotherSessionIsOnIt(t *testing.T) {
	svc, prov, creator := joinWorld(t)
	joiner, err := startOn(t, svc, InteractiveRequest{Mode: "test", WorktreeID: creator.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), creator.ID); err != nil {
		t.Fatal(err)
	}
	if len(prov.deleted) != 0 {
		t.Fatalf("deleted %v while %s is on the worktree", prov.deleted, joiner.ID)
	}
	if svc.sessions.Get(joiner.ID) == nil {
		t.Fatal("the joined session is gone")
	}
	if err := svc.Delete(context.Background(), joiner.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(prov.deleted, []string{creator.WorkspaceID}) {
		t.Errorf("deleted %v, want the worktree once its last session is deleted", prov.deleted)
	}
}

func TestAJoinedSessionIsListedAsTheTasksOwnWorktreeAndAnotherTasksIsNot(t *testing.T) {
	svc, prov, creator := joinWorld(t)
	joiner, err := startOn(t, svc, InteractiveRequest{Mode: "review", WorktreeID: creator.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.sessions.Create(&domain.Session{ID: "foreign", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk-other", WorkspaceID: "ws-x", WorkingDir: t.TempDir(), Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	_ = prov
	list, err := svc.TaskWorktrees(context.Background(), creator.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, w := range list.Worktrees {
		ids = append(ids, w.SessionID)
		if w.SessionID == joiner.ID && w.Dir != creator.WorkingDir {
			t.Errorf("the joiner is listed at %s, want the shared %s", w.Dir, creator.WorkingDir)
		}
	}
	if !slices.Contains(ids, joiner.ID) || !slices.Contains(ids, creator.ID) || slices.Contains(ids, "foreign") {
		t.Errorf("listed %v", ids)
	}
	if list.Owner != nil {
		t.Errorf("owner %+v for no path", list.Owner)
	}
	// The read hook's owner check treats the shared directory as the task's own.
	if owner := svc.worktreeOwner(context.Background(), svc.sessions.Get(creator.ID), filepathDir(creator.WorkingDir), creator.WorkingDir); owner != nil {
		t.Errorf("the task's own worktree reads as owned by %+v", owner)
	}
}

func filepathDir(p string) string {
	for i := len(p) - 1; i > 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return p
}

func TestAJoinedWriteSessionGetsTheSandboxOfASessionThatCutTheWorktree(t *testing.T) {
	w := newSandboxWorld(t)
	prov := w.svc.workspaces.(*fakeProvisioner)
	creator, creatorPolicy := w.start(t, "implement")

	joiner, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "test", RunsOn: domain.RunnerTUI, WorktreeID: creator.WorkspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := w.driver.Calls()
	if len(calls) != 2 {
		t.Fatalf("driver called %d times, want 2", len(calls))
	}
	joined := calls[1].Policy
	if len(prov.created) != 1 {
		t.Fatalf("the join created a worktree")
	}
	for _, p := range []ports.SandboxPolicy{creatorPolicy, joined} {
		if !slices.Contains(p.Writable, creator.WorkingDir) {
			t.Errorf("worktree %s not writable: %v", creator.WorkingDir, p.Writable)
		}
	}
	if joiner.WorkingDir != creator.WorkingDir {
		t.Errorf("joiner works in %s", joiner.WorkingDir)
	}
}
