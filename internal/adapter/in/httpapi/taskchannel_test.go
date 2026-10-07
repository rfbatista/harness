package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeChannel records what the routes asked for and answers from fixed data.
type fakeChannel struct {
	filter   ports.TaskMessageFilter
	task     string
	project  string
	state    domain.ReviewState
	respond  error
	minutes  int
	delegate string
}

func (f *fakeChannel) ListTaskMessages(_ context.Context, taskID string, flt ports.TaskMessageFilter) ([]*domain.TaskMessage, error) {
	f.task, f.filter = taskID, flt
	return []*domain.TaskMessage{{ID: "m1", Kind: domain.MessageQuestion, Body: "?", DocumentIDs: []string{}, ArtifactIDs: []string{}}}, nil
}

func (f *fakeChannel) ListReviewRequests(_ context.Context, taskID string, st domain.ReviewState) ([]*domain.ReviewRequest, error) {
	f.task, f.state = taskID, st
	return []*domain.ReviewRequest{{ID: "r1"}}, nil
}

func (f *fakeChannel) ListProjectReviewRequests(_ context.Context, projectID string, st domain.ReviewState) ([]*domain.ReviewRequest, error) {
	f.project, f.state = projectID, st
	return nil, nil
}

func (f *fakeChannel) RespondReview(_ context.Context, id string, d domain.ReviewState, note string) (*domain.ReviewRequest, bool, error) {
	if f.respond != nil {
		return nil, false, f.respond
	}
	return &domain.ReviewRequest{ID: id, State: d, ResponseNote: note}, true, nil
}

func (f *fakeChannel) SetStatusCheckByPerson(_ context.Context, id string, n int) (*domain.StatusCheck, error) {
	f.delegate, f.minutes = id, n
	return &domain.StatusCheck{DelegateSessionID: id, EveryMinutes: n, State: domain.StatusCheckPaused}, nil
}

func callJSON(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func TestHTTP_ArchitectChannel(t *testing.T) {
	ch := &fakeChannel{}
	srv := httptest.NewServer(NewRouter(NewHandler(Services{TaskChannel: ch})))
	defer srv.Close()

	status, out := callJSON(t, "GET", srv.URL+"/api/task_messages?ticket_id=tk1&session_id=s1&since=2026-10-07T09:00:00Z", "")
	if status != 200 || ch.task != "tk1" || ch.filter.SessionID != "s1" || !ch.filter.Since.Equal(time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("task_messages: %d %+v %+v", status, out, ch)
	}
	if msgs := out["messages"].([]any); len(msgs) != 1 || msgs[0].(map[string]any)["document_ids"] == nil {
		t.Fatalf("messages: %+v", out)
	}
	if status, _ := callJSON(t, "GET", srv.URL+"/api/task_messages?ticket_id=tk1&since=yesterday", ""); status != 400 {
		t.Fatalf("bad since: %d", status)
	}

	if status, out := callJSON(t, "GET", srv.URL+"/api/review_requests?ticket_id=tk1&state=pending", ""); status != 200 || ch.state != domain.ReviewPending || out["review_requests"] == nil {
		t.Fatalf("review_requests by task: %d %+v", status, out)
	}
	if status, out := callJSON(t, "GET", srv.URL+"/api/review_requests?project_id=p1&state=pending", ""); status != 200 || ch.project != "p1" {
		t.Fatalf("review_requests by project: %d %+v", status, out)
	}
	if status, _ := callJSON(t, "GET", srv.URL+"/api/review_requests", ""); status != 400 {
		t.Fatalf("one of ticket_id or project_id: %d", status)
	}
	if status, _ := callJSON(t, "GET", srv.URL+"/api/review_requests?ticket_id=tk1&state=maybe", ""); status != 400 {
		t.Fatalf("bad state: %d", status)
	}

	status, out = callJSON(t, "POST", srv.URL+"/api/respond_review_request", `{"review_id":"r1","decision":"changes_requested","note":"split it"}`)
	rv, _ := out["review_request"].(map[string]any)
	if status != 200 || out["delivered"] != true || rv["state"] != "changes_requested" {
		t.Fatalf("respond: %d %+v", status, out)
	}
	ch.respond = &domain.StructuredError{Code: "REVIEW_NOT_PENDING", Message: "already approved"}
	if status, out := callJSON(t, "POST", srv.URL+"/api/respond_review_request", `{"review_id":"r1","decision":"approved"}`); status != 409 || out["code"] != "REVIEW_NOT_PENDING" {
		t.Fatalf("settled review: %d %+v", status, out)
	}

	status, out = callJSON(t, "POST", srv.URL+"/api/set_status_check", `{"delegate_session_id":"d1","every_minutes":0}`)
	if status != 200 || ch.delegate != "d1" || ch.minutes != 0 || out["status_check"] == nil {
		t.Fatalf("set_status_check: %d %+v", status, out)
	}
	if status, _ := callJSON(t, "POST", srv.URL+"/api/set_status_check", `{"delegate_session_id":"d1"}`); status != 400 {
		t.Fatalf("every_minutes is required: %d", status)
	}
}

func TestHTTP_ArchitectChannelErrorCodes(t *testing.T) {
	want := map[string]int{
		"REVIEW_NOT_FOUND": 404, "STATUS_CHECK_NOT_FOUND": 404, "MESSAGE_NOT_FOUND": 404, "NO_ARCHITECT": 404,
		"REVIEW_NOT_PENDING": 409, "ARCHITECT_ONLY": 403, "TASK_STATUS_OWNED_BY_ARCHITECT": 403, "SESSION_NOT_ON_TASK": 403,
	}
	for code, status := range want {
		ch := &fakeChannel{respond: &domain.StructuredError{Code: code, Message: "x"}}
		srv := httptest.NewServer(NewRouter(NewHandler(Services{TaskChannel: ch})))
		got, _ := callJSON(t, "POST", srv.URL+"/api/respond_review_request", `{"review_id":"r1","decision":"approved"}`)
		srv.Close()
		if got != status {
			t.Errorf("%s: %d, want %d", code, got, status)
		}
	}
}

func TestHTTP_ArchitectChannelUnconfigured(t *testing.T) {
	srv := httptest.NewServer(NewRouter(NewHandler(Services{})))
	defer srv.Close()
	if status, _ := callJSON(t, "GET", srv.URL+"/api/task_messages?ticket_id=tk1", ""); status != 503 {
		t.Fatalf("no channel: %d", status)
	}
}
