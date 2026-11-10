package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/notifications/notificationstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// resumer is the single-session resume as the app wires it: it clears the
// mark and announces the status moving, or fails for the sessions in failing.
type resumer struct {
	env     *notificationstest.Env
	bus     *eventbus.Bus
	failing map[string]string
	called  []string
}

func (r *resumer) ResumeSession(ctx context.Context, id string) error {
	r.called = append(r.called, id)
	if reason, ok := r.failing[id]; ok {
		return errors.New(reason)
	}
	err := r.env.Sessions.RecordEvent(id, ports.SessionEventRecord{Seq: 2, Type: "resumed_with_continue_turn", ClearInterrupted: true, Status: domain.SessionRunning})
	if err != nil {
		return err
	}
	return r.bus.Publish(ctx, domain.SessionStatusChanged{SessionID: id, Status: domain.SessionRunning})
}

type interruptedRig struct {
	srv *httptest.Server
	env *notificationstest.Env
	bus *eventbus.Bus
	svc *notifications.Service
	res *resumer
}

func newInterruptedRig(t *testing.T) *interruptedRig {
	t.Helper()
	e := notificationstest.NewEnv(t)
	bus := eventbus.New()
	res := &resumer{env: e, bus: bus, failing: map[string]string{}}
	svc := e.Service(t, notifications.Options{Resumer: res})
	svc.Subscribe(bus)
	srv := httptest.NewServer(NewRouter(NewHandler(Services{Notifications: svc, InterruptedResumer: svc})))
	t.Cleanup(srv.Close)
	return &interruptedRig{srv: srv, env: e, bus: bus, svc: svc, res: res}
}

// interrupted stores sessions as a restart left them and announces the boot's
// list; it answers the id of the notification that lists them.
func (r *interruptedRig) interrupted(t *testing.T, ids ...string) string {
	t.Helper()
	for _, id := range ids {
		if _, err := r.env.Sessions.Create(&domain.Session{ID: id, ProjectID: r.env.ProjectA, Status: domain.SessionRunning, Interactive: true}); err != nil {
			t.Fatal(err)
		}
		at := time.Now()
		if err := r.env.Sessions.RecordEvent(id, ports.SessionEventRecord{Seq: 1, Type: "interrupted_by_restart", Interrupted: &at, Status: domain.SessionStopped}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.bus.Publish(context.Background(), domain.SessionsInterrupted{SessionIDs: ids}); err != nil {
		t.Fatal(err)
	}
	page, err := r.svc.ListNotifications(context.Background(), ports.NotificationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range page.Notifications {
		if n.Kind == domain.NotificationSessionsInterrupted && n.ResolvedAt == nil {
			return n.ID
		}
	}
	t.Fatal("no open sessions_interrupted notification")
	return ""
}

type resumeAnswer struct {
	Notification *domain.Notification `json:"notification"`
	Results      []struct {
		SessionID string `json:"session_id"`
		Outcome   string `json:"outcome"`
		Reason    string `json:"reason"`
	} `json:"results"`
	Code string `json:"code"`
}

func (r *interruptedRig) resumeAll(t *testing.T, id string) (int, resumeAnswer) {
	t.Helper()
	resp, err := http.Post(r.srv.URL+"/api/notifications/"+id+"/resume_interrupted", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out resumeAnswer
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func outcomes(a resumeAnswer) map[string]string {
	out := map[string]string{}
	for _, r := range a.Results {
		out[r.SessionID] = r.Outcome
	}
	return out
}

func TestResumeAllInterruptedResumesEveryListedSessionAndResolvesTheNotification(t *testing.T) {
	r := newInterruptedRig(t)
	id := r.interrupted(t, "s1", "s2", "s3")

	status, got := r.resumeAll(t, id)
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	for _, s := range []string{"s1", "s2", "s3"} {
		if outcomes(got)[s] != "resumed" {
			t.Errorf("outcomes = %v, want every session resumed", outcomes(got))
		}
	}
	if got.Notification == nil || got.Notification.ResolvedAt == nil {
		t.Fatalf("notification = %+v, want it resolved", got.Notification)
	}
	for _, s := range got.Notification.InterruptedSessions {
		if s.State != domain.InterruptedSessionResumed {
			t.Errorf("session %s state = %s, want resumed", s.SessionID, s.State)
		}
	}
	if strings.Join(r.res.called, ",") != "s1,s2,s3" {
		t.Errorf("resumed in order %v, want one by one in the list's order", r.res.called)
	}
}

func TestResumeAllSkipsASessionResumedByHandOrDeleted(t *testing.T) {
	r := newInterruptedRig(t)
	id := r.interrupted(t, "s1", "s2", "s3")
	if err := r.res.ResumeSession(context.Background(), "s1"); err != nil { // by hand
		t.Fatal(err)
	}
	r.res.called = nil
	if err := r.env.Sessions.Delete("s2"); err != nil {
		t.Fatal(err)
	}

	_, got := r.resumeAll(t, id)
	if want := map[string]string{"s1": "skipped", "s2": "skipped", "s3": "resumed"}; !equal(outcomes(got), want) {
		t.Errorf("outcomes = %v, want %v", outcomes(got), want)
	}
	if strings.Join(r.res.called, ",") != "s3" {
		t.Errorf("resume called for %v, want only s3: never twice", r.res.called)
	}
}

func TestAFailedResumeLeavesThatSessionMarkedWithItsReasonAndTheOthersResume(t *testing.T) {
	r := newInterruptedRig(t)
	id := r.interrupted(t, "s1", "s2", "s3")
	r.res.failing["s2"] = "the worktree is gone"

	_, got := r.resumeAll(t, id)
	if want := map[string]string{"s1": "resumed", "s2": "failed", "s3": "resumed"}; !equal(outcomes(got), want) {
		t.Fatalf("outcomes = %v, want %v", outcomes(got), want)
	}
	for _, res := range got.Results {
		if res.SessionID == "s2" && res.Reason != "the worktree is gone" {
			t.Errorf("reason = %q", res.Reason)
		}
	}
	n := got.Notification
	if n == nil || n.ResolvedAt != nil {
		t.Fatalf("notification = %+v, want it still open", n)
	}
	states := map[string]domain.InterruptedSessionState{}
	for _, s := range n.InterruptedSessions {
		states[s.SessionID] = s.State
		if s.SessionID == "s2" && s.Reason != "the worktree is gone" {
			t.Errorf("s2 entry reason = %q", s.Reason)
		}
	}
	if states["s2"] != domain.InterruptedSessionFailed || states["s1"] != domain.InterruptedSessionResumed {
		t.Errorf("states = %v", states)
	}

	// The next click retries only the session still marked, and it resolves.
	delete(r.res.failing, "s2")
	r.res.called = nil
	_, retry := r.resumeAll(t, id)
	if want := map[string]string{"s1": "skipped", "s2": "resumed", "s3": "skipped"}; !equal(outcomes(retry), want) {
		t.Errorf("retry outcomes = %v, want %v", outcomes(retry), want)
	}
	if strings.Join(r.res.called, ",") != "s2" {
		t.Errorf("retry resumed %v, want only s2", r.res.called)
	}
	if retry.Notification == nil || retry.Notification.ResolvedAt == nil {
		t.Errorf("notification after the retry = %+v, want it resolved", retry.Notification)
	}
}

func TestResumeAllAnswersTheContractsErrors(t *testing.T) {
	r := newInterruptedRig(t)
	other, err := r.svc.Notify(context.Background(), ports.NotifyRequest{ProjectID: r.env.ProjectA, Title: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if status, got := r.resumeAll(t, "missing"); status != 404 || got.Code != "NOTIFICATION_NOT_FOUND" {
		t.Errorf("unknown id = %d %q, want 404 NOTIFICATION_NOT_FOUND", status, got.Code)
	}
	if status, got := r.resumeAll(t, other.ID); status != 400 || got.Code != "INVALID_INPUT" {
		t.Errorf("another kind = %d %q, want 400 INVALID_INPUT", status, got.Code)
	}
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
