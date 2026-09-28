package sqlite

import (
	"testing"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

func newTestDB(t *testing.T) *SessionRepository {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return NewSessionRepository(db)
}

// One UI filter spans several backend statuses — "running" in the pool means
// running, idle or thinking underneath — so the filter matches any of them.
func TestSessionRepo_ListMatchesAnyStatus(t *testing.T) {
	r := newTestDB(t)
	for _, s := range []struct {
		id     string
		status domain.SessionStatus
	}{
		{"a", domain.SessionIdle},
		{"b", domain.SessionThinking},
		{"c", domain.SessionDone},
	} {
		if _, err := r.Create(&domain.Session{ID: s.id, ProjectID: "p1", Task: "t", Status: s.status}); err != nil {
			t.Fatal(err)
		}
	}

	alive := r.List(ports.SessionFilter{
		Statuses: []domain.SessionStatus{domain.SessionRunning, domain.SessionIdle, domain.SessionThinking},
	})
	if len(alive) != 2 {
		t.Fatalf("want 2 alive sessions, got %d", len(alive))
	}
	if all := r.List(ports.SessionFilter{}); len(all) != 3 {
		t.Fatalf("empty filter must match all, got %d", len(all))
	}
}

func TestSessionRepository_PersistsWorkspaceAndBranch(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSessionRepository(db)

	created, err := repo.Create(&domain.Session{
		ProjectID:    "p1",
		RepositoryID: "r1",
		WorkspaceID:  "ws1",
		Branch:       "agent/ship-it",
		Task:         "ship it",
		WorkingDir:   "/tmp/ws1",
		Status:       domain.SessionStarting,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.WorkspaceID != "ws1" || created.Branch != "agent/ship-it" {
		t.Fatalf("create dropped worktree fields: %+v", created)
	}
	got := repo.Get(created.ID)
	if got == nil || got.WorkspaceID != "ws1" || got.Branch != "agent/ship-it" {
		t.Fatalf("get dropped worktree fields: %+v", got)
	}
}

func TestSessionRepo_UpdateAutoRun(t *testing.T) {
	r := newTestDB(t)
	created, err := r.Create(&domain.Session{ProjectID: "p1", Task: "do", Status: domain.SessionStarting})
	if err != nil {
		t.Fatal(err)
	}
	if created.AutoRun {
		t.Fatal("a new session must start with auto-run off")
	}

	if err := r.UpdateAutoRun(created.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(created.ID); got == nil || !got.AutoRun {
		t.Fatalf("auto-run not persisted: %+v", got)
	}
	// Switching back off must actually clear the column — a false in a GORM
	// struct update is the zero value and would be silently skipped, which is
	// why this goes through a map update.
	if err := r.UpdateAutoRun(created.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(created.ID); got == nil || got.AutoRun {
		t.Fatalf("auto-run not cleared: %+v", got)
	}

	if err := r.UpdateAutoRun("nope", true); err == nil {
		t.Fatal("expected an error for an unknown session")
	}
}

// A session can be started already gated — the flag has to survive Create, not
// only the later update.
func TestSessionRepo_CreatePersistsAutoRun(t *testing.T) {
	r := newTestDB(t)
	created, err := r.Create(&domain.Session{ProjectID: "p1", Task: "do", Status: domain.SessionStarting, AutoRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !created.AutoRun {
		t.Fatalf("create dropped auto_run: %+v", created)
	}
	if got := r.Get(created.ID); got == nil || !got.AutoRun {
		t.Fatalf("get dropped auto_run: %+v", got)
	}
}

func TestSessionRepo_InteractiveAndClaudeSessionID(t *testing.T) {
	r := newTestDB(t)
	created, err := r.Create(&domain.Session{
		ID: "s1", ProjectID: "p1", Task: "do", Status: domain.SessionRunning,
		Interactive: true, ClaudeSessionID: "s1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Interactive || created.ClaudeSessionID != "s1" {
		t.Fatalf("create dropped interactive fields: %+v", created)
	}

	if err := r.UpdateClaudeSessionID("s1", "after-clear"); err != nil {
		t.Fatal(err)
	}
	if got := r.Get("s1"); got == nil || !got.Interactive || got.ClaudeSessionID != "after-clear" {
		t.Fatalf("claude session id not persisted: %+v", got)
	}
	if err := r.UpdateClaudeSessionID("nope", "x"); err == nil {
		t.Fatal("expected an error for an unknown session")
	}
}

func TestSessionRepo_CRUDAndEvents(t *testing.T) {
	r := newTestDB(t)
	created, err := r.Create(&domain.Session{ID: "s1", ProjectID: "p1", Task: "do", Status: domain.SessionStarting})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "s1" {
		t.Fatalf("want id s1, got %q", created.ID)
	}
	if got := r.Get("s1"); got == nil || got.Status != domain.SessionStarting {
		t.Fatalf("Get failed: %+v", got)
	}
	if err := r.UpdateStatus("s1", domain.SessionRunning); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateMetrics("s1", 1.5, 10, 20, "tool: Bash", 1); err != nil {
		t.Fatal(err)
	}
	got := r.Get("s1")
	if got.Status != domain.SessionRunning || got.CostUSD != 1.5 || got.OutputTokens != 20 || got.PendingApprovals != 1 {
		t.Fatalf("metrics not persisted: %+v", got)
	}

	if list := r.List(ports.SessionFilter{ProjectID: "p1"}); len(list) != 1 {
		t.Fatalf("List want 1 got %d", len(list))
	}
	if list := r.List(ports.SessionFilter{Statuses: []domain.SessionStatus{domain.SessionDone}}); len(list) != 0 {
		t.Fatalf("List by status want 0 got %d", len(list))
	}

	if err := r.AppendEvent("s1", 1, "output", []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendEvent("s1", 2, "status", []byte(`{"b":2}`)); err != nil {
		t.Fatal(err)
	}
	evs := r.ListEvents("s1", 0)
	if len(evs) != 2 || evs[0].Seq != 1 || evs[1].Type != "status" {
		t.Fatalf("ListEvents bad: %+v", evs)
	}
	if from := r.ListEvents("s1", 1); len(from) != 1 || from[0].Seq != 2 {
		t.Fatalf("ListEvents fromSeq bad: %+v", from)
	}
}
