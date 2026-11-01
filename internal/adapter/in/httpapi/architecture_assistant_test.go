package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeArchitectureAssistant reports events, then answers err; with block it
// waits for its context first and records that it saw it end.
type fakeArchitectureAssistant struct {
	got       ports.ArchitectureAssistantRequest
	events    []ports.ArchitectureAssistantEvent
	err       error
	block     bool
	started   chan struct{}
	cancelled chan struct{}
	ended     []string
}

func newFakeArchitectureAssistant() *fakeArchitectureAssistant {
	return &fakeArchitectureAssistant{started: make(chan struct{}, 1), cancelled: make(chan struct{})}
}

func (f *fakeArchitectureAssistant) Run(ctx context.Context, req ports.ArchitectureAssistantRequest, emit func(ports.ArchitectureAssistantEvent)) error {
	f.got = req
	for _, e := range f.events {
		emit(e)
	}
	if f.block {
		f.started <- struct{}{}
		<-ctx.Done()
		close(f.cancelled)
		return ctx.Err()
	}
	return f.err
}

func (f *fakeArchitectureAssistant) End(id string) { f.ended = append(f.ended, id) }

func (f *fakeArchitectureAssistant) Availability(context.Context, string) ports.ArchitectureAssistantAvailability {
	return ports.ArchitectureAssistantAvailability{Available: true}
}

func assistantRouter(f *fakeArchitectureAssistant) http.Handler {
	return NewRouter(NewHandler(Services{ArchitectureAssistant: f}))
}

const turnBody = `{"repository_id":"r1","message":"Set up the layers.","draft":{"version":1},"draft_base_version":7,"rule_event_id":"ev1"}`

func postTurn(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/run_architecture_assistant_turn", strings.NewReader(body)))
	return rec
}

func TestATurnAnswersSSEFramedEventsInOrder(t *testing.T) {
	f := newFakeArchitectureAssistant()
	f.events = []ports.ArchitectureAssistantEvent{
		{Name: "conversation", Data: ports.ArchitectureAssistantConversation{ConversationID: "c1"}},
		{Name: "activity", Data: ports.ArchitectureAssistantActivity{Tool: "Read", Target: "internal/domain/zone.go"}},
		{Name: "delta", Data: ports.ArchitectureAssistantDelta{Text: "Hi"}},
		{Name: "result", Data: ports.ArchitectureAssistantResult{Text: "Hi"}},
	}
	rec := postTurn(assistantRouter(f), turnBody)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	want := "event: conversation\ndata: {\"conversation_id\":\"c1\"}\n\n" +
		"event: activity\ndata: {\"tool\":\"Read\",\"target\":\"internal/domain/zone.go\"}\n\n" +
		"event: delta\ndata: {\"text\":\"Hi\"}\n\n" +
		"event: result\ndata: {\"text\":\"Hi\",\"proposal\":null}\n\n"
	if rec.Body.String() != want {
		t.Fatalf("body = %q\nwant   %q", rec.Body.String(), want)
	}
	if f.got.RepositoryID != "r1" || f.got.Message != "Set up the layers." || f.got.DraftBaseVersion != 7 || f.got.RuleEventID != "ev1" || string(f.got.Draft) != `{"version":1}` {
		t.Errorf("request = %+v", f.got)
	}
}

func TestAProposalIsStreamedInTheContractsShape(t *testing.T) {
	f := newFakeArchitectureAssistant()
	f.events = []ports.ArchitectureAssistantEvent{{Name: "result", Data: ports.ArchitectureAssistantResult{Text: "t", Proposal: &ports.ArchitectureAssistantProposal{
		ID: "p1", Summary: "s", Valid: false,
		Changes: []domain.ProposalChange{{Target: domain.ProposalTarget{Kind: "layer", Name: "domain"}, Op: "add", Value: json.RawMessage(`{"name":"domain"}`), Base: json.RawMessage("null"), Reason: "r"}},
		Errors:  []domain.ArchFieldError{{Path: "/layers/0/match/regex", Code: "INVALID_REGEX", Message: "m"}},
	}}}}
	rec := postTurn(assistantRouter(f), turnBody)
	want := `event: result` + "\n" + `data: {"text":"t","proposal":{"id":"p1","summary":"s","valid":false,` +
		`"changes":[{"target":{"kind":"layer","name":"domain"},"op":"add","value":{"name":"domain"},"base":null,"reason":"r"}],` +
		`"errors":[{"path":"/layers/0/match/regex","code":"INVALID_REGEX","message":"m"}]}}` + "\n\n"
	if rec.Body.String() != want {
		t.Fatalf("body = %q\nwant   %q", rec.Body.String(), want)
	}
}

func TestAFailedTurnEndsTheStreamWithOneErrorEvent(t *testing.T) {
	f := newFakeArchitectureAssistant()
	f.events = []ports.ArchitectureAssistantEvent{{Name: "conversation", Data: ports.ArchitectureAssistantConversation{ConversationID: "c1"}}}
	f.err = &domain.StructuredError{Code: "ARCHITECTURE_ASSISTANT_FAILED", Message: "claude exploded"}
	rec := postTurn(assistantRouter(f), turnBody)
	want := "event: conversation\ndata: {\"conversation_id\":\"c1\"}\n\nevent: error\ndata: {\"code\":\"ARCHITECTURE_ASSISTANT_FAILED\",\"message\":\"claude exploded\"}\n\n"
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestAnUnexpectedErrorAfterTheStreamOpenedIsAFailedEvent(t *testing.T) {
	f := newFakeArchitectureAssistant()
	f.events = []ports.ArchitectureAssistantEvent{{Name: "conversation", Data: ports.ArchitectureAssistantConversation{ConversationID: "c1"}}}
	f.err = context.DeadlineExceeded
	rec := postTurn(assistantRouter(f), turnBody)
	if !strings.Contains(rec.Body.String(), `"code":"ARCHITECTURE_ASSISTANT_FAILED"`) {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestAssistantRefusalsBeforeTheStreamAreJSONErrorsWithTheirStatus(t *testing.T) {
	unavailable := &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "ARCHITECTURE_ASSISTANT_UNAVAILABLE", Message: "no agent credential"},
		Details:         map[string]string{"fix_url": "/agent-isolation#credential"},
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{&domain.StructuredError{Code: "INVALID_INPUT", Message: "bad"}, http.StatusBadRequest, "INVALID_INPUT"},
		{&domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "no repository"}, http.StatusNotFound, "REPOSITORY_NOT_FOUND"},
		{&domain.StructuredError{Code: "CONVERSATION_NOT_FOUND", Message: "lost"}, http.StatusNotFound, "CONVERSATION_NOT_FOUND"},
		{&domain.StructuredError{Code: "CONVERSATION_BUSY", Message: "running"}, http.StatusConflict, "CONVERSATION_BUSY"},
		{unavailable, http.StatusServiceUnavailable, "ARCHITECTURE_ASSISTANT_UNAVAILABLE"},
	} {
		f := newFakeArchitectureAssistant()
		f.err = tc.err
		rec := postTurn(assistantRouter(f), turnBody)
		var b map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatalf("%s: body %q", tc.code, rec.Body.String())
		}
		if rec.Code != tc.status || b["code"] != tc.code || b["error"] == "" || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
			t.Errorf("%s: status %d body %v", tc.code, rec.Code, b)
		}
		if tc.code == "ARCHITECTURE_ASSISTANT_UNAVAILABLE" && b["fix_url"] != "/agent-isolation#credential" {
			t.Errorf("unavailable body = %v, want fix_url", b)
		}
	}
}

func TestAMalformedTurnBodyIsRefused(t *testing.T) {
	if rec := postTurn(assistantRouter(newFakeArchitectureAssistant()), "{"); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestWithoutAnAssistantTheEndpointsAnswer503(t *testing.T) {
	h := NewRouter(NewHandler(Services{}))
	if rec := postTurn(h, turnBody); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("turn: status %d", rec.Code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/end_architecture_assistant_conversation", strings.NewReader(`{"conversation_id":"c1"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("end: status %d", rec.Code)
	}
}

func TestAssistantEventsAreFlushedWhileTheTurnStillRuns(t *testing.T) {
	f := newFakeArchitectureAssistant()
	f.events = []ports.ArchitectureAssistantEvent{{Name: "conversation", Data: ports.ArchitectureAssistantConversation{ConversationID: "c1"}}}
	f.block = true
	srv := httptest.NewServer(assistantRouter(f))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/run_architecture_assistant_turn", "application/json", strings.NewReader(turnBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "event: conversation\n" {
		t.Fatalf("first line = %q, %v", line, err)
	}
}

func TestAnAbortedRequestCancelsTheTurnWithoutAnErrorEvent(t *testing.T) {
	f := newFakeArchitectureAssistant()
	f.block = true
	srv := httptest.NewServer(assistantRouter(f))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/run_architecture_assistant_turn", strings.NewReader(turnBody))
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	<-f.started
	cancel()
	select {
	case <-f.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn did not see its context end after the client aborted")
	}
	<-done
}

func TestEndingAConversationAnswers204ForAnyID(t *testing.T) {
	f := newFakeArchitectureAssistant()
	h := assistantRouter(f)
	for _, id := range []string{"c1", "never-issued"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/end_architecture_assistant_conversation", strings.NewReader(`{"conversation_id":"`+id+`"}`)))
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Errorf("%s: status %d body %q", id, rec.Code, rec.Body.String())
		}
	}
	if strings.Join(f.ended, ",") != "c1,never-issued" {
		t.Errorf("ended = %v", f.ended)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/end_architecture_assistant_conversation", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNoContent {
		t.Errorf("no id: status %d", rec.Code)
	}
}

func TestABeaconEndsAConversationToo(t *testing.T) {
	// navigator.sendBeacon posts text/plain.
	f := newFakeArchitectureAssistant()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/end_architecture_assistant_conversation", strings.NewReader(`{"conversation_id":"c9"}`))
	r.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	assistantRouter(f).ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent || len(f.ended) != 1 || f.ended[0] != "c9" {
		t.Fatalf("status %d, ended %v", rec.Code, f.ended)
	}
}
