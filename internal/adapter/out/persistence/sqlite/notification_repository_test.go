package sqlite_test

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func newNotificationRepo(t *testing.T) *sqlite.NotificationRepository {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return sqlite.NewNotificationRepository(db)
}

// seed stores n, a system info notification unless it says otherwise.
func seed(t *testing.T, r *sqlite.NotificationRepository, n domain.Notification) *domain.Notification {
	t.Helper()
	if n.Source == "" {
		n.Source = domain.NotificationFromSystem
	}
	if n.Level == "" {
		n.Level = domain.NotificationInfo
	}
	got, err := r.Create(&n)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func ids(list []*domain.Notification) []string {
	out := make([]string, len(list))
	for i, n := range list {
		out[i] = n.ID
	}
	return out
}

func TestNotificationRepositoryPagesNewestFirst(t *testing.T) {
	r := newNotificationRepo(t)
	t0 := time.UnixMilli(1_700_000_000_000)
	for i := range 5 {
		// two share a millisecond, so the id breaks the tie
		at := t0.Add(time.Duration(i/2) * time.Second)
		seed(t, r, domain.Notification{ProjectID: "p1", Title: "n", CreatedAt: at})
	}
	var all []*domain.Notification
	f := ports.NotificationFilter{Limit: 2}
	for {
		page, err := r.List(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page...)
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		f.BeforeAt, f.BeforeID = last.CreatedAt, last.ID
	}
	if len(all) != 5 {
		t.Fatalf("paged %d, want 5: %v", len(all), ids(all))
	}
	seen := map[string]bool{}
	for i, n := range all {
		if seen[n.ID] {
			t.Fatalf("%s twice", n.ID)
		}
		seen[n.ID] = true
		if i > 0 && n.CreatedAt.After(all[i-1].CreatedAt) {
			t.Fatalf("not newest first: %v", ids(all))
		}
	}
}

func TestNotificationRepositoryFilters(t *testing.T) {
	r := newNotificationRepo(t)
	now := time.Now()
	a := seed(t, r, domain.Notification{ProjectID: "p1", Title: "a", CreatedAt: now.Add(-3 * time.Second)})
	b := seed(t, r, domain.Notification{ProjectID: "p2", Title: "b", CreatedAt: now.Add(-2 * time.Second), ReadAt: &now})
	c := seed(t, r, domain.Notification{ProjectID: "p1", Title: "c", CreatedAt: now.Add(-1 * time.Second), ResolvedAt: &now})

	if got, _ := r.List(context.Background(), ports.NotificationFilter{ProjectID: "p1"}); len(got) != 2 || got[0].ID != c.ID || got[1].ID != a.ID {
		t.Fatalf("project p1 = %v", ids(got))
	}
	if got, _ := r.List(context.Background(), ports.NotificationFilter{UnreadOnly: true}); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("unread = %v", ids(got))
	}
	if got, _ := r.List(context.Background(), ports.NotificationFilter{Since: a.CreatedAt}); len(got) != 2 || got[1].ID != b.ID {
		t.Fatalf("since a = %v", ids(got))
	}
	if n, _ := r.CountUnread(context.Background()); n != 1 {
		t.Fatalf("CountUnread = %d", n)
	}
}

func TestNotificationRepositoryMarkReadAnswersOnlyWhatChanged(t *testing.T) {
	r := newNotificationRepo(t)
	at := time.Now()
	a := seed(t, r, domain.Notification{ProjectID: "p1", Title: "a"})
	b := seed(t, r, domain.Notification{ProjectID: "p2", Title: "b"})

	got, err := r.MarkRead([]string{a.ID}, at)
	if err != nil || len(got) != 1 || got[0].ID != a.ID || got[0].ReadAt == nil {
		t.Fatalf("MarkRead = %+v, %v", got, err)
	}
	if got, _ := r.MarkRead([]string{a.ID}, at); len(got) != 0 {
		t.Fatalf("again = %v", ids(got))
	}
	if got, _ := r.MarkAllRead("p1", at); len(got) != 0 {
		t.Fatalf("all of p1 = %v", ids(got))
	}
	if got, _ := r.MarkAllRead("", at); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("all = %v", ids(got))
	}
}

func TestNotificationRepositoryResolvesByCauseAndTask(t *testing.T) {
	r := newNotificationRepo(t)
	at := time.Now()
	w := seed(t, r, domain.Notification{ProjectID: "p1", TaskID: "t1", Title: "w", CauseKey: "session_waiting:s1"})
	rv := seed(t, r, domain.Notification{ProjectID: "p1", TaskID: "t1", Title: "r", CauseKey: "task_review:t1"})
	ag := seed(t, r, domain.Notification{ProjectID: "p1", TaskID: "t1", Title: "agent", Source: domain.NotificationFromAgent})

	if open := r.OpenByCause("session_waiting:s1"); open == nil || open.ID != w.ID {
		t.Fatalf("OpenByCause = %+v", open)
	}
	got, _ := r.Resolve("session_waiting:s1", at)
	if len(got) != 1 || got[0].ResolvedAt == nil {
		t.Fatalf("Resolve = %+v", got)
	}
	if r.OpenByCause("session_waiting:s1") != nil {
		t.Fatal("still open after Resolve")
	}
	if !r.AnyByCause("session_waiting:s1") || r.AnyByCause("session_waiting:nope") {
		t.Fatal("AnyByCause")
	}
	got, _ = r.ResolveTask("t1", at)
	if len(got) != 1 || got[0].ID != rv.ID {
		t.Fatalf("ResolveTask = %v (agent %s never resolves)", ids(got), ag.ID)
	}
}

func TestNotificationRepositoryCountsAgentPostsAndPrunes(t *testing.T) {
	r := newNotificationRepo(t)
	now := time.Now()
	old := now.Add(-31 * 24 * time.Hour)
	agent := domain.NotificationFromAgent
	seed(t, r, domain.Notification{ProjectID: "p1", SessionID: "s1", Source: agent, Title: "x"})
	seed(t, r, domain.Notification{ProjectID: "p1", SessionID: "s1", Source: agent, Title: "x", CreatedAt: now.Add(-2 * time.Hour)})
	seed(t, r, domain.Notification{ProjectID: "p1", Source: agent, Title: "x"})
	seed(t, r, domain.Notification{ProjectID: "p1", Title: "system"})

	if n, _ := r.CountAgentSince("s1", "p1", now.Add(-time.Hour)); n != 1 {
		t.Fatalf("session count = %d", n)
	}
	if n, _ := r.CountAgentSince("", "p1", now.Add(-time.Hour)); n != 1 {
		t.Fatalf("session-less count = %d", n)
	}

	keepUnread := seed(t, r, domain.Notification{ProjectID: "p2", Title: "old unread", CreatedAt: old})
	seed(t, r, domain.Notification{ProjectID: "p2", Title: "old read", CreatedAt: old, ReadAt: &now})
	seed(t, r, domain.Notification{ProjectID: "p2", Title: "old resolved", CreatedAt: old, ResolvedAt: &now})
	if n, err := r.Prune(now.Add(-30 * 24 * time.Hour)); err != nil || n != 2 {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	if r.Get(keepUnread.ID) == nil {
		t.Fatal("an unread one was pruned")
	}
	if err := r.DeleteByProject("p1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.List(context.Background(), ports.NotificationFilter{ProjectID: "p1"}); len(got) != 0 {
		t.Fatalf("after DeleteByProject = %v", ids(got))
	}
}
