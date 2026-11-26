package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeGateRuns struct {
	runs []domain.GateRun
	last ports.GateRunRequest
	// output, when set, writes the first part, waits for release, then the rest.
	first, rest string
	release     chan struct{}
}

func (f *fakeGateRuns) Output(_ context.Context, _, run string, kind domain.GateKind, w io.Writer) error {
	if run == "nope" {
		return &domain.StructuredError{Code: "GATE_RUN_NOT_FOUND", Message: "no such run"}
	}
	_, _ = io.WriteString(w, f.first)
	if f.release != nil {
		<-f.release
	}
	_, _ = io.WriteString(w, f.rest)
	return nil
}

func (f *fakeGateRuns) Run(_ context.Context, req ports.GateRunRequest) (*domain.GateRun, error) {
	f.last = req
	switch req.SessionID {
	case "nope":
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	case "plain":
		return nil, &domain.StructuredError{Code: "SESSION_NOT_GATED", Message: "the session is not gated"}
	}
	run := domain.GateRun{ID: "r1", SessionID: req.SessionID, Trigger: req.Trigger, StartedAt: time.Unix(1, 0),
		Gates: []domain.GateResult{{Kind: domain.GateTest, Command: "go test", ExitCode: 1, DurationMs: 12, Output: "FAIL"}}}
	run.Finish(time.Unix(2, 0))
	f.runs = append(f.runs, run)
	return &run, nil
}

func (f *fakeGateRuns) List(_ context.Context, id string) ([]domain.GateRun, error) {
	if id == "nope" {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return f.runs, nil
}

func gateRunsRouter(f *fakeGateRuns) http.Handler {
	return NewRouter(NewHandler(Services{GateRuns: f}))
}

func TestThePersonsTriggerRunsAndRecordsAGateRunWithItsPerGateResults(t *testing.T) {
	f := &fakeGateRuns{}
	h := gateRunsRouter(f)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/s1/gate-runs", nil))
	if rec.Code != http.StatusOK || f.last.Trigger != domain.GateTriggerPerson || f.last.SessionID != "s1" {
		t.Fatalf("trigger: %d %s, request %+v", rec.Code, rec.Body, f.last)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/s1/gate-runs", nil))
	var body struct {
		Runs []struct {
			Result  string `json:"result"`
			Trigger string `json:"trigger"`
			Counted bool   `json:"counted"`
			Gates   []struct {
				Command    string `json:"command"`
				ExitCode   int    `json:"exit_code"`
				DurationMs int64  `json:"duration_ms"`
				Output     string `json:"output"`
			} `json:"gates"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Runs) != 1 {
		t.Fatalf("list: %v %s", err, rec.Body)
	}
	r := body.Runs[0]
	if r.Result != "red" || r.Trigger != "person" || r.Counted || len(r.Gates) != 1 || r.Gates[0].ExitCode != 1 || r.Gates[0].Output != "FAIL" || r.Gates[0].DurationMs != 12 {
		t.Errorf("run = %+v", r)
	}
}

func TestAGateRunOfAnUnknownSessionIs404AndOfASessionThatIsNotGatedIsACodedConflict(t *testing.T) {
	h := gateRunsRouter(&fakeGateRuns{})
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/sessions/nope/gate-runs", http.StatusNotFound},
		{http.MethodGet, "/api/sessions/nope/gate-runs", http.StatusNotFound},
		{http.MethodPost, "/api/sessions/plain/gate-runs", http.StatusConflict},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d: %s", c.method, c.path, rec.Code, c.want, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/plain/gate-runs", nil))
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "SESSION_NOT_GATED" {
		t.Errorf("code = %q: %s", body.Code, rec.Body)
	}
}

func TestAGatesOutputStreamsItsFirstPartBeforeTheGateEndsAndTheWholeByItsEnd(t *testing.T) {
	f := &fakeGateRuns{first: "building\n", rest: "done\n", release: make(chan struct{})}
	srv := httptest.NewServer(gateRunsRouter(f))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/sessions/s1/gate-runs/r1/gates/build/output")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	r := bufio.NewReader(res.Body)
	line, err := r.ReadString('\n')
	if err != nil || line != "building\n" {
		t.Fatalf("first part = %q, %v", line, err)
	}
	close(f.release)
	rest, _ := io.ReadAll(r)
	if string(rest) != "done\n" {
		t.Errorf("rest = %q", rest)
	}
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("status %d type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestStreamingAFinishedRunsGateAnswersItsRecordedOutputAndAnUnknownRunIs404(t *testing.T) {
	h := gateRunsRouter(&fakeGateRuns{first: "recorded"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/s1/gate-runs/r1/gates/test/output", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "recorded" {
		t.Errorf("%d %q", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/s1/gate-runs/nope/gates/test/output", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown run: %d", rec.Code)
	}
}
