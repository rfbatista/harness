package orchestration

import (
	"context"
	"slices"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The list the New session form's picker reads: the worktrees the harness
// made for sessions of one task in one repository.

func TestTheWorktreePickerListsOnlyTheTasksLiveWorktreesWithTheirSessions(t *testing.T) {
	svc, prov, creator := joinWorld(t)
	joiner, err := startOn(t, svc, InteractiveRequest{Mode: "review", WorktreeID: creator.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.sessions.UpdateStatus(joiner.ID, domain.SessionStopped); err != nil {
		t.Fatal(err)
	}

	// An ended session whose folder is gone, a cleared one, a session of
	// another task, and one before the feature (no access recorded).
	gone := &domain.Workspace{ID: "ws-gone", RepositoryID: "r1", Branch: "gone", Path: t.TempDir() + "/missing"}
	foreign := &domain.Workspace{ID: "ws-foreign", RepositoryID: "r1", Branch: "foreign", Path: t.TempDir()}
	old := &domain.Workspace{ID: "ws-old", RepositoryID: "r1", Branch: "old/work", Path: t.TempDir()}
	prov.created = append(prov.created, gone, foreign, old)
	cleared := time.Now()
	for _, s := range []*domain.Session{
		{ID: "s-gone", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", WorkspaceID: "ws-gone", WorkingDir: gone.Path, Status: domain.SessionStopped},
		{ID: "s-cleared", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Branch: "cleared", Status: domain.SessionStopped, WorktreeClearedAt: &cleared},
		{ID: "s-foreign", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk-other", WorkspaceID: "ws-foreign", WorkingDir: foreign.Path, Status: domain.SessionRunning},
		{ID: "s-old", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", WorkspaceID: "ws-old", WorkingDir: old.Path, Branch: "old/work", Status: domain.SessionStopped},
	} {
		if _, err := svc.sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}

	view, err := svc.TaskWorktreeChoices(context.Background(), "tk1", "r1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, w := range view.Worktrees {
		ids = append(ids, w.WorktreeID)
		if !w.OnDisk {
			t.Errorf("%s is listed but not on disk", w.WorktreeID)
		}
	}
	if !slices.Equal(ids, []string{"ws-old", creator.WorkspaceID}) && !slices.Equal(ids, []string{creator.WorkspaceID, "ws-old"}) {
		t.Fatalf("listed %v, want the creator's worktree and the old one only", ids)
	}

	var shared, legacy = view.Worktrees[0], view.Worktrees[1]
	if shared.WorktreeID != creator.WorkspaceID {
		shared, legacy = legacy, shared
	}
	if shared.Branch != creator.Branch || shared.CreatedBySessionID != creator.ID || len(shared.Sessions) != 2 {
		t.Errorf("shared worktree = %+v", shared)
	}
	bySession := map[string]string{}
	running := map[string]bool{}
	for _, s := range shared.Sessions {
		bySession[s.SessionID] = s.Access + "/" + s.Mode
		running[s.SessionID] = s.Running
	}
	if bySession[creator.ID] != "write/implement" || bySession[joiner.ID] != "read_only/review" {
		t.Errorf("sessions = %v", bySession)
	}
	for _, s := range shared.Sessions {
		switch {
		case s.Running && s.EndedAt != nil:
			t.Errorf("%s runs and has an end time %v", s.SessionID, s.EndedAt)
		case !s.Running && (s.EndedAt == nil || s.EndedAt.IsZero()):
			t.Errorf("%s ended and has no end time", s.SessionID)
		}
	}
	if !running[creator.ID] && running[joiner.ID] {
		t.Errorf("running = %v, want the ended joiner marked ended", running)
	}
	if len(legacy.Sessions) != 1 || legacy.Sessions[0].Access != "write" || legacy.CreatedBySessionID != "s-old" {
		t.Errorf("a session from before the feature is listed as %+v, want write on the worktree it cut", legacy)
	}
	if view.Policy.Effective || view.Policy.Project || view.Policy.TaskOverride != "project" || view.TaskWorktreeID != nil || view.ChoiceNeeded {
		t.Errorf("policy block = %+v %v %v, want the no-policy values", view.Policy, view.TaskWorktreeID, view.ChoiceNeeded)
	}
}

func TestTheWorktreePickerIsNewestFirstAndEmptyWhenTheRepositoryHasNone(t *testing.T) {
	svc, prov, creator := joinWorld(t)
	newer := &domain.Workspace{ID: "ws-newer", RepositoryID: "r1", Branch: "newer", Path: t.TempDir()}
	prov.created = append(prov.created, newer)
	time.Sleep(5 * time.Millisecond) // the store stamps creation in milliseconds
	if _, err := svc.sessions.Create(&domain.Session{ID: "s-newer", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", WorkspaceID: "ws-newer", WorkingDir: newer.Path, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.TaskWorktreeChoices(context.Background(), "tk1", "r1")
	if err != nil || len(view.Worktrees) != 2 || view.Worktrees[0].WorktreeID != "ws-newer" || view.Worktrees[1].WorktreeID != creator.WorkspaceID {
		t.Fatalf("view %+v, %v", view, err)
	}

	view, err = svc.TaskWorktreeChoices(context.Background(), "tk1", "r-other")
	if err != nil || view.Worktrees == nil || len(view.Worktrees) != 0 {
		t.Fatalf("no worktrees must be an empty list, got %+v, %v", view, err)
	}
}
