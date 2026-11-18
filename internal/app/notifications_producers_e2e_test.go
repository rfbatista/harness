package app

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/mcpinterview"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/interviews"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The outcomes and the actor of a notification come from what the producers
// publish: the orchestration's approval decisions and expiry, the interviews'
// closing states, the architect channel's review request. These tests drive
// the real producers, where the ledger tests publish the events themselves.

// approvalOutcome asks for an approval through the broker as the agent's tool
// call does, settles it with settle, and answers the outcome of the
// session_waiting notification it raised.
func (l *ledger) approvalOutcome(session, reqID string, settle func(cancel context.CancelFunc)) string {
	l.t.Helper()
	ctx, cancel := context.WithCancel(l.ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.broker.Await(ctx, session, reqID, "Bash", json.RawMessage(`{"command":"go test ./..."}`))
	}()
	waiting := l.next().Changed[0]
	if waiting.Kind != domain.NotificationSessionWaiting || waiting.ResolvedAt != nil {
		l.t.Fatalf("the approval raised %+v", waiting)
	}
	settle(cancel)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		l.t.Fatal("the approval was never settled")
	}
	for _, ch := range l.next().Changed {
		if ch.ID == waiting.ID && ch.ResolvedAt != nil {
			return string(ch.Outcome)
		}
	}
	l.t.Fatal("the settled approval resolved nothing")
	return ""
}

func TestApprovalOutcomesComeFromTheOrchestration(t *testing.T) {
	l := newLedger(t)
	l.session("s1", "backend", domain.SessionModeImplement)
	decide := func(allow bool) func(context.CancelFunc) {
		return func(context.CancelFunc) {
			if err := l.orch.Resolve(l.ctx, "s1", "req", allow, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := l.approvalOutcome("s1", "req", decide(true)); got != "approved" {
		t.Errorf("an allowed approval = %q, want approved", got)
	}
	if got := l.approvalOutcome("s1", "req", decide(false)); got != "denied" {
		t.Errorf("a denied approval = %q, want denied", got)
	}
	// An approval nobody can answer any more expires, and reads as denied.
	if got := l.approvalOutcome("s1", "req", func(cancel context.CancelFunc) { cancel() }); got != "denied" {
		t.Errorf("an expired approval = %q, want denied", got)
	}
}

func TestInterviewOutcomesComeFromTheInterviews(t *testing.T) {
	l := newLedger(t)
	l.session("s1", "architect-agent", domain.SessionModeArchitect)
	svc := interviews.New(interviews.Deps{
		Repo: sqlite.NewInterviewRepository(l.db), Sessions: l.sessions, Agents: l.agentRepo, Events: l.cat.Bus,
		Turns: mcpinterview.Turns{}, Delivery: &countingTurns{},
	})
	answers := []domain.InterviewAnswer{{Chosen: []int{1}}, {Text: "Because."}}
	for _, c := range []struct {
		id, want string
		close    func(id string) error
	}{
		{"iv-sent", "answered", func(id string) error { _, err := svc.Submit(l.ctx, id, answers); return err }},
		{"iv-dismissed", "dismissed", func(id string) error { _, err := svc.Dismiss(l.ctx, id, "no"); return err }},
		{"iv-expired", "ended", func(string) error { return svc.Expire(l.ctx, "s1") }},
	} {
		if _, err := svc.Ask(l.ctx, interview(c.id, "s1")); err != nil {
			t.Fatal(err)
		}
		asked := l.next().Changed[0]
		if err := c.close(c.id); err != nil {
			t.Fatal(err)
		}
		got := l.next().Changed[0]
		if got.ID != asked.ID || got.ResolvedAt == nil || string(got.Outcome) != c.want {
			t.Errorf("interview %s: %+v, want resolved with outcome %q", c.id, got, c.want)
		}
	}
}

// A review request's actor is the architect that asked, not the session the
// review is about; an old row, which keeps only the latter, names the harness.
func TestAReviewRequestIsFromTheArchitect(t *testing.T) {
	l := newLedger(t)
	l.session("arch", "software-architect", domain.SessionModeArchitect)
	l.session("dev", "go-developer", domain.SessionModeImplement)
	if _, err := l.channel.RequestUserReview(l.ctx, "arch", ports.ReviewRequestInput{Subject: "the tickets", Body: "b", AboutSessionID: "dev"}); err != nil {
		t.Fatal(err)
	}
	n := l.next().Changed[0]
	if a := n.Actor; a.Kind != domain.ActorAgent || a.Name != "software-architect" || a.Mode != "architect" || a.SessionID != "arch" {
		t.Fatalf("review request actor = %+v, want the architect session", a)
	}

	old, err := sqlite.NewNotificationRepository(l.db).Create(&domain.Notification{
		ProjectID: l.project.ID, SessionID: "dev", Source: domain.NotificationFromSystem, Kind: domain.NotificationReviewRequested,
		Level: domain.NotificationAction, Title: "Review requested: x", Link: "/", CauseKey: "review:old", CreatedAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := l.list(url.Values{"limit": {"10"}})
	for _, row := range list {
		if str(row, "id") == old.ID {
			if actor := obj(row, "actor"); str(actor, "kind") != "system" || str(actor, "name") != "harness" {
				t.Fatalf("an old review row's actor = %v, want the harness, never the implementer", actor)
			}
			return
		}
	}
	t.Fatal("the old row is not listed")
}

// A sessions_interrupted notification ends as resumed only when a listed
// session is back; when every one was deleted it ended.
func TestInterruptedSessionsEndAsResumedOnlyWhenOneIsBack(t *testing.T) {
	l := newLedger(t)
	at := time.Now()
	mark := func(ids ...string) {
		for _, id := range ids {
			l.session(id, "agent-"+id, domain.SessionModeImplement)
			if err := l.sessions.RecordEvent(id, ports.SessionEventRecord{Seq: 1, Type: "interrupted_by_restart", Interrupted: &at, Status: domain.SessionStopped}); err != nil {
				t.Fatal(err)
			}
		}
		l.publish(domain.SessionsInterrupted{SessionIDs: ids})
		l.next()
	}
	remove := func(id string) {
		if err := l.sessions.Delete(id); err != nil {
			t.Fatal(err)
		}
		l.publish(domain.SessionDeleted{SessionID: id})
	}

	mark("a1")
	remove("a1")
	l.settled("sessions_interrupted", "ended")

	mark("b1", "b2")
	remove("b2")
	l.next() // the list again, still one marked
	if err := l.sessions.RecordEvent("b1", ports.SessionEventRecord{Seq: 2, Type: "resumed", ClearInterrupted: true, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	l.publish(domain.SessionStatusChanged{SessionID: "b1", Status: domain.SessionRunning})
	l.settled("sessions_interrupted", "resumed")
}
