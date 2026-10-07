package sqlite

import (
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestTaskMessageRepo_ListDeliverQueue(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTaskMessageRepository(db)
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	a, err := r.Create(&domain.TaskMessage{TaskID: "t", ProjectID: "p", FromSessionID: "d1", ToSessionID: "arch",
		Kind: domain.MessageStatusReport, Status: domain.ReportWorking, Body: "on it", CreatedAt: t0, Queued: true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := r.Create(&domain.TaskMessage{TaskID: "t", FromSessionID: "arch", ToSessionID: "d2", Kind: domain.MessageReply,
		Body: "ok", DocumentIDs: []string{"doc1"}, CreatedAt: t0.Add(time.Minute)})
	_, _ = r.Create(&domain.TaskMessage{TaskID: "other", FromSessionID: "x", ToSessionID: "y", Kind: domain.MessageQuestion, Body: "?", CreatedAt: t0})

	all := r.List("t", ports.TaskMessageFilter{})
	if len(all) != 2 || all[0].ID != a.ID || all[1].ID != b.ID {
		t.Fatalf("oldest first on the task: %+v", all)
	}
	if got := r.List("t", ports.TaskMessageFilter{SessionID: "d2"}); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("narrowed to d2: %+v", got)
	}
	if got := r.List("t", ports.TaskMessageFilter{Since: t0}); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("since: %+v", got)
	}
	if got := r.Get(a.ID); len(got.DocumentIDs) != 0 || got.DocumentIDs == nil || got.Status != domain.ReportWorking || got.ProjectID != "p" {
		t.Fatalf("round trip: %+v", got)
	}
	if got := r.Get(b.ID); len(got.DocumentIDs) != 1 || got.DocumentIDs[0] != "doc1" {
		t.Fatalf("ids: %+v", got.DocumentIDs)
	}
	if q := r.ListQueued(); len(q) != 1 || q[0].ID != a.ID {
		t.Fatalf("queued: %+v", q)
	}
	got, err := r.MarkDelivered(a.ID, t0.Add(2*time.Minute))
	if err != nil || !got.Delivered || got.DeliveredAt == nil || got.Queued {
		t.Fatalf("mark delivered: %+v %v", got, err)
	}
	if len(r.ListQueued()) != 0 {
		t.Fatal("delivered leaves the outbox")
	}
	if _, err := r.MarkDelivered("ghost", t0); err == nil {
		t.Fatal("ghost must be MESSAGE_NOT_FOUND")
	}
	if lr := r.LastReport("d1"); lr == nil || lr.ID != a.ID {
		t.Fatalf("last report: %+v", lr)
	}
	if r.LastReport("arch") != nil {
		t.Fatal("a reply is not a report")
	}
}

// Messages and reviews outlive a deleted session: no foreign key, no cascade.
func TestTaskChannelRows_OutliveTheirSessions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewSessionRepository(db)
	s, err := sessions.Create(&domain.Session{ID: "d1", ProjectID: "p", TicketID: "t", Status: domain.SessionRunning})
	if err != nil {
		t.Fatal(err)
	}
	msgs := NewTaskMessageRepository(db)
	reviews := NewReviewRequestRepository(db)
	m, _ := msgs.Create(&domain.TaskMessage{TaskID: "t", FromSessionID: s.ID, ToSessionID: "arch", Kind: domain.MessageQuestion, Body: "?"})
	rv, _ := reviews.Create(&domain.ReviewRequest{TaskID: "t", ProjectID: "p", ArchitectSessionID: "arch", AboutSessionID: s.ID, Subject: "s", Body: "b", State: domain.ReviewPending})
	if err := sessions.Delete(s.ID); err != nil {
		t.Fatal(err)
	}
	if msgs.Get(m.ID) == nil || reviews.Get(rv.ID) == nil {
		t.Fatal("deleting a session must not delete its messages or reviews")
	}
}

func TestReviewRequestRepo_ListUpdateCount(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewReviewRequestRepository(db)
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	a, _ := r.Create(&domain.ReviewRequest{TaskID: "t", ProjectID: "p", ArchitectSessionID: "arch", Subject: "a", Body: "b", State: domain.ReviewPending, CreatedAt: t0})
	b, _ := r.Create(&domain.ReviewRequest{TaskID: "t", ProjectID: "p", ArchitectSessionID: "arch", Subject: "b", Body: "b", State: domain.ReviewPending, CreatedAt: t0.Add(time.Minute)})
	_, _ = r.Create(&domain.ReviewRequest{TaskID: "t2", ProjectID: "p", ArchitectSessionID: "arch2", Subject: "c", Body: "b", State: domain.ReviewPending, CreatedAt: t0})

	if got := r.List(ports.ReviewFilter{TaskID: "t"}); len(got) != 2 || got[0].ID != b.ID {
		t.Fatalf("newest first: %+v", got)
	}
	if got := r.List(ports.ReviewFilter{ProjectID: "p", State: domain.ReviewPending}); len(got) != 3 {
		t.Fatalf("project pending: %d", len(got))
	}
	if n := r.CountPending("t"); n != 2 {
		t.Fatalf("pending = %d", n)
	}
	at := t0.Add(time.Hour)
	a.State, a.ResponseNote, a.RespondedAt = domain.ReviewChangesRequested, "fix it", &at
	if err := r.Update(a); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(a.ID); got.State != domain.ReviewChangesRequested || got.ResponseNote != "fix it" || got.RespondedAt == nil {
		t.Fatalf("update: %+v", got)
	}
	if n := r.CountPending("t"); n != 1 {
		t.Fatalf("pending after respond = %d", n)
	}
	if err := r.Update(&domain.ReviewRequest{ID: "ghost"}); err == nil {
		t.Fatal("ghost update must fail")
	}
}

func TestStatusCheckRepo_SaveListActive(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewStatusCheckRepository(db)
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	late, early := t0.Add(10*time.Minute), t0.Add(2*time.Minute)
	_ = r.Save(&domain.StatusCheck{TaskID: "t", ArchitectSessionID: "arch", DelegateSessionID: "d1", EveryMinutes: 10, NextAt: &late, State: domain.StatusCheckActive})
	_ = r.Save(&domain.StatusCheck{TaskID: "t", ArchitectSessionID: "arch", DelegateSessionID: "d2", EveryMinutes: 2, NextAt: &early, State: domain.StatusCheckActive})
	_ = r.Save(&domain.StatusCheck{TaskID: "t", ArchitectSessionID: "arch", DelegateSessionID: "d3", EveryMinutes: 0, State: domain.StatusCheckPaused})

	act := r.ListActive()
	if len(act) != 2 || act[0].DelegateSessionID != "d2" {
		t.Fatalf("active, earliest first: %+v", act)
	}
	c := r.Get("d1")
	c.FiredCount, c.State = 3, domain.StatusCheckEnded
	if err := r.Save(c); err != nil {
		t.Fatal(err)
	}
	if got := r.Get("d1"); got.FiredCount != 3 || got.State != domain.StatusCheckEnded || got.NextAt == nil || !got.NextAt.Equal(late) {
		t.Fatalf("save replaces: %+v", got)
	}
	if n := len(r.ListByTask("t")); n != 3 {
		t.Fatalf("by task = %d", n)
	}
	if n := len(r.ListByArchitect("arch")); n != 3 {
		t.Fatalf("by architect = %d", n)
	}
	if r.Get("ghost") != nil {
		t.Fatal("ghost")
	}
}

func TestTaskStatusChangeRepo_History(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTaskStatusChangeRepository(db)
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	_ = r.Append(&domain.TaskStatusChange{TaskID: "t", Status: domain.TicketStatusInProgress, By: domain.ChangedBySession, BySessionID: "arch", Reason: "go", At: t0})
	_ = r.Append(&domain.TaskStatusChange{TaskID: "t", Status: domain.TicketStatusReview, By: domain.ChangedByPerson, At: t0.Add(time.Minute)})
	h := r.ListByTask("t")
	if len(h) != 2 || h[0].Reason != "go" || h[0].BySessionID != "arch" || h[1].By != domain.ChangedByPerson {
		t.Fatalf("history: %+v", h)
	}
}
