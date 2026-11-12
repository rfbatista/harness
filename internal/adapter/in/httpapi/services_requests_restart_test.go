package httpapi

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/application/servicerequests"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Requests survive a restart of the harness and expire with their session
// (ADR 0083, ADR 0078).

// restartWaiter records the recount a boot asks for.
type restartWaiter struct {
	fakeRequestWaiter
	restored map[string]int
}

func (w *restartWaiter) ServiceRequestsRestored(id string, n int) {
	if w.restored == nil {
		w.restored = map[string]int{}
	}
	w.restored[id] += n
}

type recordedTurn struct{ session, key, text string }

// fakeTurns records the turns a session is sent.
type fakeTurns struct {
	mu    sync.Mutex
	turns []recordedTurn
}

func (t *fakeTurns) Deliver(session, key, text string, _ func(time.Time)) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns = append(t.turns, recordedTurn{session, key, text})
	return true, nil
}

func (t *fakeTurns) all() []recordedTurn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]recordedTurn{}, t.turns...)
}

// afterRestart is a second requester over the stores the first one filled, as a
// restarted harness has, and what it was told.
func (f *mcpFixture) afterRestart() (*servicerequests.Service, *restartWaiter, *fakeTurns) {
	f.t.Helper()
	w, turns := &restartWaiter{}, &fakeTurns{}
	r := servicerequests.NewService(servicerequests.Deps{
		Services: f.svc, Store: f.requestStore, Sessions: f.sessions, Waiter: w, Turns: turns, Events: f.cat.Bus,
	})
	r.Subscribe(f.cat.Bus)
	return r, w, turns
}

// fileAndLoseTheCall files a start through the first requester and drops the
// call that waited, as a restart does.
func (f *mcpFixture) fileAndLoseTheCall(session string) map[string]any {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = f.requests.Submit(ctx, ports.ServiceRequestInput{
			SessionID: session, Action: domain.ServiceRequestStart, ServiceID: f.createdID("web"), ServiceName: "web",
			Env: map[string]string{"D": "after-restart"}, EnvGiven: true,
		})
	}()
	r := f.awaitPending(1)[0]
	cancel()
	<-done
	return r
}

func TestServicesRequestsRestart_APendingRequestStaysAnswerableAndItsResultReachesTheSessionAsATurn(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	r := f.fileAndLoseTheCall("sess-1")
	t.Cleanup(func() { f.operate("stop_service", id) })

	restarted, waiter, turns := f.afterRestart()
	if err := restarted.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := waiter.restored["sess-1"]; got != 1 {
		t.Fatalf("the restore adds the stored pending requests to the session's count: %d", got)
	}
	if err := restarted.Restore(context.Background()); err != nil || waiter.restored["sess-1"] != 1 {
		t.Fatalf("a second restore adds nothing: %v %d", err, waiter.restored["sess-1"])
	}
	if list := f.pendingRequests(); len(list) != 1 || list[0]["id"] != r["id"] {
		t.Fatalf("the request is still pending: %v", list)
	}

	got, err := restarted.Answer(context.Background(), r["id"].(string), "approve", "go ahead")
	if err != nil || got.State != domain.ServiceRequestDone {
		t.Fatalf("approve after a restart = %+v, %v", got, err)
	}
	f.awaitStatus(id, "running")
	f.awaitLog(id, "got D=after-restart")
	sent := turns.all()
	if len(sent) != 1 || sent[0].session != "sess-1" || !strings.Contains(sent[0].text, "[service request "+got.ID+" · done]") {
		t.Fatalf("turns = %+v", sent)
	}
	if strings.Contains(sent[0].text, "after-restart") {
		t.Fatalf("a turn holds a value: %q", sent[0].text)
	}
	run := f.service(id)["run"].(map[string]any)["id"]

	if _, err := restarted.Answer(context.Background(), r["id"].(string), "approve", ""); err == nil {
		t.Fatal("an answered request is answered once")
	}
	if now := f.service(id)["run"].(map[string]any)["id"]; now != run {
		t.Fatal("the action ran twice")
	}
	// A restart that replays nothing: the next boot leaves a done request alone.
	again, _, _ := f.afterRestart()
	if err := again.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if now := f.service(id)["run"].(map[string]any)["id"]; now != run {
		t.Fatal("a restart replayed a done request")
	}
}

func TestServicesRequestsRestart_ARequestOfASessionThatEndedWhileTheServerWasDownExpiresAtBoot(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	r := f.fileAndLoseTheCall("sess-1")
	if err := f.sessions.UpdateStatus("sess-1", domain.SessionStopped); err != nil {
		t.Fatal(err)
	}

	restarted, waiter, _ := f.afterRestart()
	if err := restarted.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, out := f.call("GET", "/api/service_requests?project_id="+f.projectID, nil)
	if got := out["requests"].([]any)[0].(map[string]any); got["id"] != r["id"] || got["state"] != "expired" {
		t.Fatalf("requests = %v", out)
	}
	if waiter.restored["sess-1"] != 0 {
		t.Fatalf("an ended session is not recounted: %v", waiter.restored)
	}
	if st := f.status(id); st != "stopped" {
		t.Fatalf("an expired request starts nothing: %s", st)
	}
}

func TestServicesRequestsRestart_AnApprovedRequestThatNeverRanIsFailedOnceNotReplayed(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	r := f.fileAndLoseTheCall("sess-1")
	// The server died between the approval and the action.
	stored, err := f.requestStore.Get(context.Background(), r["id"].(string))
	if err != nil || stored == nil {
		t.Fatal(err)
	}
	stored.State, stored.AnsweredAt = domain.ServiceRequestApproved, time.Now().UTC().Format(time.RFC3339)
	if err := f.requestStore.Save(context.Background(), stored); err != nil {
		t.Fatal(err)
	}

	restarted, _, turns := f.afterRestart()
	for i := 0; i < 2; i++ {
		if err := restarted.Restore(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := f.requestStore.Get(context.Background(), r["id"].(string))
	if got.State != domain.ServiceRequestFailed || !strings.Contains(got.ResultError, "restart") {
		t.Fatalf("request = %+v", got)
	}
	if st := f.status(id); st != "stopped" {
		t.Fatalf("the repair replays nothing: %s", st)
	}
	if n := len(turns.all()); n != 1 {
		t.Fatalf("the session is told once: %d turns", n)
	}
}
