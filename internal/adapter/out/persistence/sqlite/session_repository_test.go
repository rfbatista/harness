package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
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

	alive := r.List(context.Background(), ports.SessionFilter{
		Statuses: []domain.SessionStatus{domain.SessionRunning, domain.SessionIdle, domain.SessionThinking},
	})
	if len(alive) != 2 {
		t.Fatalf("want 2 alive sessions, got %d", len(alive))
	}
	if all := r.List(context.Background(), ports.SessionFilter{}); len(all) != 3 {
		t.Fatalf("empty filter must match all, got %d", len(all))
	}
}

// The sessions of many tasks are read in one query, newest first.
func TestSessionRepo_ListMatchesAnyOfTheTickets(t *testing.T) {
	r := newTestDB(t)
	for _, s := range []struct{ id, ticket string }{{"a", "t1"}, {"b", "t2"}, {"c", "t3"}, {"d", ""}} {
		if _, err := r.Create(&domain.Session{ID: s.id, ProjectID: "p1", TicketID: s.ticket, Task: "t", Status: domain.SessionDone}); err != nil {
			t.Fatal(err)
		}
	}
	got := r.List(context.Background(), ports.SessionFilter{TicketIDs: []string{"t1", "t3"}})
	ids := []string{}
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if len(ids) != 2 || !(ids[0] == "a" && ids[1] == "c" || ids[0] == "c" && ids[1] == "a") {
		t.Fatalf("sessions of t1 and t3 = %v, want a and c", ids)
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

func TestSessionRepo_CreatePersistsMode(t *testing.T) {
	r := newTestDB(t)
	created, err := r.Create(&domain.Session{ProjectID: "p1", Task: "do", Status: domain.SessionRunning, Mode: domain.SessionModeArchitect})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Get(created.ID); got == nil || got.Mode != domain.SessionModeArchitect {
		t.Fatalf("mode not persisted: %+v", got)
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
	if err := r.UpdateMetrics("s1", 1.5, ports.SessionTokens{Input: 10, Output: 20}, "tool: Bash", 1); err != nil {
		t.Fatal(err)
	}
	got := r.Get("s1")
	if got.Status != domain.SessionRunning || got.CostUSD != 1.5 || got.OutputTokens != 20 || got.PendingApprovals != 1 {
		t.Fatalf("metrics not persisted: %+v", got)
	}

	if list := r.List(context.Background(), ports.SessionFilter{ProjectID: "p1"}); len(list) != 1 {
		t.Fatalf("List want 1 got %d", len(list))
	}
	if list := r.List(context.Background(), ports.SessionFilter{Statuses: []domain.SessionStatus{domain.SessionDone}}); len(list) != 0 {
		t.Fatalf("List by status want 0 got %d", len(list))
	}

	if err := r.RecordEvent("s1", ports.SessionEventRecord{Seq: 1, Type: "output", Payload: []byte(`{"a":1}`)}); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordEvent("s1", ports.SessionEventRecord{Seq: 2, Type: "status", Payload: []byte(`{"b":2}`)}); err != nil {
		t.Fatal(err)
	}
	evs := mustListEvents(t, r, "s1", 0)
	if len(evs) != 2 || evs[0].Seq != 1 || evs[1].Type != "status" {
		t.Fatalf("ListEvents bad: %+v", evs)
	}
	if from := mustListEvents(t, r, "s1", 1); len(from) != 1 || from[0].Seq != 2 {
		t.Fatalf("ListEvents fromSeq bad: %+v", from)
	}
}

func TestSessionRepository_Runner(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSessionRepository(db)
	s, err := repo.Create(&domain.Session{ID: "s1", Interactive: true, RunsOn: domain.RunnerServer})
	if err != nil {
		t.Fatal(err)
	}
	if s.RunsOn != domain.RunnerServer {
		t.Fatalf("created RunsOn = %q, want server", s.RunsOn)
	}
	if err := repo.UpdateRunner("s1", domain.RunnerTUI, "laptop"); err != nil {
		t.Fatal(err)
	}
	if got := repo.Get("s1"); got.RunsOn != domain.RunnerTUI || got.RunnerHost != "laptop" {
		t.Fatalf("after UpdateRunner: %q on %q, want tui on laptop", got.RunsOn, got.RunnerHost)
	}
	if err := repo.UpdateRunner("missing", domain.RunnerTUI, ""); err == nil {
		t.Fatal("UpdateRunner on a missing session succeeded")
	}
}

// Interactive sessions from before RunsOn existed were all run by the client.
func TestMigrateInteractiveRunner(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSessionRepository(db)
	for _, s := range []*domain.Session{
		{ID: "legacy", Interactive: true},
		{ID: "hosted", Interactive: true, RunsOn: domain.RunnerServer},
		{ID: "headless"},
	} {
		if _, err := repo.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // idempotent
		if err := migrateInteractiveRunner(db); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[string]domain.Runner{"legacy": domain.RunnerTUI, "hosted": domain.RunnerServer, "headless": ""} {
		if got := repo.Get(id).RunsOn; got != want {
			t.Errorf("%s RunsOn = %q, want %q", id, got, want)
		}
	}
}

// An event is stored with the metrics and status it brings, or not at all.
func TestSessionRepository_RecordEventIsAllOrNothing(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewSessionRepository(db)
	if _, err := r.Create(&domain.Session{ID: "s1", Status: domain.SessionStarting}); err != nil {
		t.Fatal(err)
	}
	err = r.RecordEvent("s1", ports.SessionEventRecord{
		Seq: 1, Type: "usage", Payload: []byte(`{}`), Status: domain.SessionIdle,
		Metrics: &ports.SessionMetrics{CostUSD: 0.5, Tokens: ports.SessionTokens{Input: 3}, LastAction: "tool: Read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Get("s1")
	if got.Status != domain.SessionIdle || got.CostUSD != 0.5 || got.InputTokens != 3 || got.LastAction != "tool: Read" {
		t.Fatalf("session after the event: %+v", got)
	}

	if err := r.RecordEvent("gone", ports.SessionEventRecord{Seq: 1, Type: "done", Payload: []byte(`{}`), Status: domain.SessionStopped}); err == nil {
		t.Fatal("recording a status on a missing session succeeded")
	}
	if evs := mustListEvents(t, r, "gone", 0); len(evs) != 0 {
		t.Fatalf("the event of a failed record was kept: %+v", evs)
	}
}

// A session or event read that fails reaches its caller as an error, never as
// "not found" or an empty list; a session that is not there still reads as
// not found.
func TestSessionRepository_FailedReadsReportErrors(t *testing.T) {
	db := openFile(t, filepath.Join(t.TempDir(), "data.db"))
	repo := NewSessionRepository(db)
	created, err := repo.Create(&domain.Session{ProjectID: "p1", Task: "t", Status: domain.SessionRunning})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordEvent(created.ID, ports.SessionEventRecord{Seq: 1, Type: "output", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

	if s, err := repo.Find(context.Background(), "missing"); s != nil || err != nil {
		t.Fatalf("Find(missing) = %v, %v; want nil, nil", s, err)
	}
	if s, err := repo.Find(context.Background(), created.ID); s == nil || err != nil {
		t.Fatalf("Find = %v, %v", s, err)
	}

	if err := Close(db); err != nil {
		t.Fatal(err)
	}
	if s, err := repo.Find(context.Background(), created.ID); err == nil {
		t.Errorf("Find on a failed read = %v, nil; want an error", s)
	}
	if list, err := repo.Query(context.Background(), ports.SessionFilter{}); err == nil {
		t.Errorf("Query on a failed read = %v, nil; want an error", list)
	}
	if evs, err := repo.ListEvents(created.ID, 0); err == nil {
		t.Errorf("ListEvents on a failed read = %v, nil; want an error", evs)
	}
}

// mustListEvents is the session's events after fromSeq; a failed read fails t.
func mustListEvents(t *testing.T, r *SessionRepository, sessionID string, fromSeq int64) []ports.StoredEvent {
	t.Helper()
	evs, err := r.ListEvents(sessionID, fromSeq)
	if err != nil {
		t.Fatalf("events of %s: %v", sessionID, err)
	}
	return evs
}
