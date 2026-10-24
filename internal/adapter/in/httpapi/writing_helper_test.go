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

// fakeWritingHelper reports events, then answers err; with block it waits for
// its context first and records that it saw it end.
type fakeWritingHelper struct {
	got       ports.WritingHelperRequest
	events    []ports.WritingHelperEvent
	err       error
	block     bool
	started   chan struct{}
	cancelled chan struct{}
}

func newFakeWritingHelper() *fakeWritingHelper {
	return &fakeWritingHelper{started: make(chan struct{}, 1), cancelled: make(chan struct{})}
}

func (f *fakeWritingHelper) Run(ctx context.Context, req ports.WritingHelperRequest, emit func(ports.WritingHelperEvent)) error {
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

func (f *fakeWritingHelper) Availability(context.Context) ports.WritingHelperAvailability {
	return ports.WritingHelperAvailability{Available: true}
}

func writingHelperRouter(f *fakeWritingHelper) http.Handler {
	return NewRouter(NewHandler(Services{WritingHelper: f}))
}

const stepBody = `{"project_id":"p1","step":"draft","field":{"kind":"task_description","text":"x"}}`

func TestADraftStepAnswersSSEFramedEventsInOrder(t *testing.T) {
	f := newFakeWritingHelper()
	f.events = []ports.WritingHelperEvent{
		{Name: "delta", Data: ports.WritingHelperDelta{Text: "Hel"}},
		{Name: "delta", Data: ports.WritingHelperDelta{Text: "lo"}},
		{Name: "result", Data: ports.WritingHelperDraftResult{Text: "Hello"}},
	}
	rec := httptest.NewRecorder()
	writingHelperRouter(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/run_writing_helper_step", strings.NewReader(stepBody)))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	want := "event: delta\ndata: {\"text\":\"Hel\"}\n\n" +
		"event: delta\ndata: {\"text\":\"lo\"}\n\n" +
		"event: result\ndata: {\"text\":\"Hello\"}\n\n"
	if rec.Body.String() != want {
		t.Fatalf("body = %q\nwant   %q", rec.Body.String(), want)
	}
	if f.got.ProjectID != "p1" || f.got.Field.Kind != "task_description" {
		t.Errorf("request = %+v", f.got)
	}
}

func TestAFailedStepEndsTheStreamWithOneErrorEvent(t *testing.T) {
	f := newFakeWritingHelper()
	f.events = []ports.WritingHelperEvent{{Name: "delta", Data: ports.WritingHelperDelta{Text: "He"}}}
	f.err = &domain.StructuredError{Code: "WRITING_HELPER_FAILED", Message: "claude exploded"}
	rec := httptest.NewRecorder()
	writingHelperRouter(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/run_writing_helper_step", strings.NewReader(stepBody)))
	want := "event: delta\ndata: {\"text\":\"He\"}\n\nevent: error\ndata: {\"code\":\"WRITING_HELPER_FAILED\",\"message\":\"claude exploded\"}\n\n"
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestAFailureBeforeAnyEventIsStillAnErrorEvent(t *testing.T) {
	f := newFakeWritingHelper()
	f.err = &domain.StructuredError{Code: "WRITING_HELPER_FAILED", Message: "bad json"}
	rec := httptest.NewRecorder()
	writingHelperRouter(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/run_writing_helper_step", strings.NewReader(stepBody)))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "event: error\n") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestRefusalsBeforeTheStreamAreJSONErrorsWithTheirStatus(t *testing.T) {
	unavailable := &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "WRITING_HELPER_UNAVAILABLE", Message: "no agent credential"},
		Details:         map[string]string{"fix_url": "/agent-isolation#credential"},
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{&domain.StructuredError{Code: "INVALID_INPUT", Message: "bad step"}, http.StatusBadRequest, "INVALID_INPUT"},
		{&domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "no project"}, http.StatusNotFound, "PROJECT_NOT_FOUND"},
		{unavailable, http.StatusConflict, "WRITING_HELPER_UNAVAILABLE"},
	} {
		f := newFakeWritingHelper()
		f.err = tc.err
		rec := httptest.NewRecorder()
		writingHelperRouter(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/run_writing_helper_step", strings.NewReader(stepBody)))
		var b map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatalf("%s: body %q", tc.code, rec.Body.String())
		}
		if rec.Code != tc.status || b["code"] != tc.code || b["error"] == "" || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
			t.Errorf("%s: status %d body %v", tc.code, rec.Code, b)
		}
		if tc.code == "WRITING_HELPER_UNAVAILABLE" && b["fix_url"] != "/agent-isolation#credential" {
			t.Errorf("unavailable body = %v, want fix_url", b)
		}
	}
}

func TestAMalformedBodyIsRefused(t *testing.T) {
	rec := httptest.NewRecorder()
	writingHelperRouter(newFakeWritingHelper()).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/run_writing_helper_step", strings.NewReader("{")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestWithoutAWritingHelperTheEndpointAnswers503(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(NewHandler(Services{})).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/run_writing_helper_step", strings.NewReader(stepBody)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestEventsAreFlushedAsTheyAreWritten(t *testing.T) {
	f := newFakeWritingHelper()
	f.events = []ports.WritingHelperEvent{{Name: "delta", Data: ports.WritingHelperDelta{Text: "first"}}}
	f.block = true
	srv := httptest.NewServer(writingHelperRouter(f))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/run_writing_helper_step", "application/json", strings.NewReader(stepBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	line, err := r.ReadString('\n')
	if err != nil || line != "event: delta\n" {
		t.Fatalf("first line = %q, %v; the delta should arrive while the step still runs", line, err)
	}
}

func TestAnAbortedRequestCancelsTheStep(t *testing.T) {
	f := newFakeWritingHelper()
	f.block = true
	srv := httptest.NewServer(writingHelperRouter(f))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/run_writing_helper_step", strings.NewReader(stepBody))
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
		t.Fatal("the step did not see its context end after the client aborted")
	}
	<-done
}
