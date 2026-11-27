package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/mcpbench"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// benchSessionStub stands in for the CLI: each run waits for release (or its
// context) and answers a fixed result. No process, no model.
type benchSessionStub struct{ release chan struct{} }

func (s benchSessionStub) Setup(context.Context, ports.BenchSetupRequest) (ports.BenchSetupInfo, error) {
	return ports.BenchSetupInfo{Model: "sonnet", CLIVersion: "2.1.0"}, nil
}

func (s benchSessionStub) Run(ctx context.Context, _ ports.BenchSessionRequest) (ports.BenchSessionResult, error) {
	select {
	case <-s.release:
		return ports.BenchSessionResult{FinalText: "done", CostUSD: 0.01}, nil
	case <-ctx.Done():
		return ports.BenchSessionResult{}, ctx.Err()
	}
}

type benchJudgeStub struct{}

func (benchJudgeStub) Ask(context.Context, string, string, string) (string, error) {
	return `{"similarity": 1, "score": 1, "steps": []}`, nil
}

func benchRunsHandler(t *testing.T) (*Handler, chan struct{}) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	svc := bench.New(bench.Deps{
		Cases:      sqlite.NewBenchCaseRepository(db),
		Scorecards: sqlite.NewBenchScorecardRepository(db),
		Projects:   agentSystemProjects{"p1": {ID: "p1"}},
		Sessions:   benchSessionStub{release},
		ServeMock:  mcpbench.Serve,
		MockName:   mcpbench.Name,
		Judge:      benchJudgeStub{},
	})
	return NewHandler(Services{Bench: svc, BenchRuns: svc}), release
}

func benchRunBody(t *testing.T, h *Handler, method, path string) (int, map[string]any) {
	t.Helper()
	return doJSON(t, h, method, "/api"+path, "")
}

func waitRunEnds(t *testing.T, h *Handler, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, run := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/runs/"+id)
		if run["status"] != domain.BenchRunRunning {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run did not end")
	return nil
}

func TestStartingARunOnAProjectWithNoCasesIs422(t *testing.T) {
	h, _ := benchRunsHandler(t)
	code, body := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs")
	if code != http.StatusUnprocessableEntity || body["code"] != "BENCH_NO_CASES" {
		t.Fatalf("status %d body %v", code, body)
	}
	if code, _ := benchRunBody(t, h, http.MethodPost, "/projects/nope/bench/runs"); code != http.StatusNotFound {
		t.Fatalf("unknown project = %d, want 404", code)
	}
}

func TestARunIsStartedFollowedAndItsScorecardReadAndComparedOverHTTP(t *testing.T) {
	h, release := benchRunsHandler(t)
	if rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody)); rec.Code != http.StatusCreated {
		t.Fatalf("create case = %d: %s", rec.Code, rec.Body.String())
	}
	close(release)

	code, run := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs")
	if code != http.StatusAccepted || run["id"] == "" || run["total"] != float64(1) {
		t.Fatalf("start = %d %v", code, run)
	}
	id := run["id"].(string)
	ended := waitRunEnds(t, h, id)
	if ended["status"] != domain.BenchRunFinished || ended["scorecard_id"] != id || ended["done"] != float64(1) {
		t.Fatalf("ended = %v", ended)
	}

	code, card := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+id)
	if code != http.StatusOK || len(card["cases"].([]any)) != 1 || card["setup"].(map[string]any)["fingerprint"] == "" {
		t.Fatalf("scorecard = %d %v", code, card)
	}
	if code, list := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards"); code != http.StatusOK || len(list["scorecards"].([]any)) != 1 {
		t.Fatalf("list = %d %v", code, list)
	}

	// The first scorecard has nothing before it; a second compares with it.
	if code, body := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+id+"/compare"); code != http.StatusNotFound || body["code"] != "BENCH_NO_PREVIOUS_SCORECARD" {
		t.Fatalf("compare with none = %d %v", code, body)
	}
	_, second := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs")
	waitRunEnds(t, h, second["id"].(string))
	code, cmp := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+second["id"].(string)+"/compare")
	if code != http.StatusOK || cmp["previous_id"] != id || cmp["fingerprint_changed"] != false {
		t.Fatalf("compare = %d %v", code, cmp)
	}

	if code, _ := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/nope"); code != http.StatusNotFound {
		t.Fatalf("unknown scorecard = %d", code)
	}
	if code, _ := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/runs/nope"); code != http.StatusNotFound {
		t.Fatalf("unknown run = %d", code)
	}
}

func TestASecondConcurrentRunIs409AndACancelledRunKeepsAScorecard(t *testing.T) {
	h, _ := benchRunsHandler(t) // release is never closed: the run waits for the cancel
	postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))

	_, run := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs")
	id := run["id"].(string)
	if code, body := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs"); code != http.StatusConflict || body["code"] != "BENCH_RUN_IN_PROGRESS" {
		t.Fatalf("second run = %d %v", code, body)
	}

	if code, _ := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs/"+id+"/cancel"); code != http.StatusAccepted {
		t.Fatalf("cancel = %d", code)
	}
	ended := waitRunEnds(t, h, id)
	if ended["status"] != domain.BenchRunCancelled || ended["scorecard_id"] != id {
		t.Fatalf("ended = %v", ended)
	}
	if code, body := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs/"+id+"/cancel"); code != http.StatusConflict || body["code"] != "BENCH_RUN_NOT_RUNNING" {
		t.Fatalf("cancel again = %d %v", code, body)
	}
	_, card := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+id)
	if card["status"] != domain.ScorecardCancelled {
		t.Fatalf("scorecard = %v", card)
	}
}
