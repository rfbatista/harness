package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Seam C edge cases: the bench routes at their limits.

func TestEveryBenchRouteAnswers503WhenTheBenchIsNotConfigured(t *testing.T) {
	h := NewHandler(Services{})
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/projects/p1/bench/cases"},
		{http.MethodPost, "/projects/p1/bench/cases"},
		{http.MethodDelete, "/projects/p1/bench/cases/c"},
		{http.MethodPost, "/projects/p1/bench/cases/from-trajectory"},
		{http.MethodGet, "/projects/p1/bench/trajectories"},
		{http.MethodGet, "/projects/p1/bench/trajectories/t"},
		{http.MethodPost, "/projects/p1/bench/runs"},
		{http.MethodGet, "/projects/p1/bench/runs/r"},
		{http.MethodPost, "/projects/p1/bench/runs/r/cancel"},
		{http.MethodGet, "/projects/p1/bench/scorecards"},
		{http.MethodGet, "/projects/p1/bench/scorecards/s"},
		{http.MethodGet, "/projects/p1/bench/scorecards/s/compare"},
		{http.MethodGet, "/projects/p1/tasks/t/trajectory-recording"},
		{http.MethodPut, "/projects/p1/tasks/t/trajectory-recording"},
	} {
		rec := httptest.NewRecorder()
		NewRouter(h).ServeHTTP(rec, httptest.NewRequest(r.method, "/api"+r.path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d, want 503", r.method, r.path, rec.Code)
		}
	}
}

func TestMalformedAndEmptyBenchCaseBodiesAre400AndStoreNothing(t *testing.T) {
	h := benchHandler(t, ":memory:")
	for name, body := range map[string]string{"not json": `{nope`, "empty": ``, "wrong shape": `[1,2]`, "tools of the wrong type": `{"prompt":"p","tools":"x"}`} {
		if code, _ := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/cases", body); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, code)
		}
	}
	if _, list := doJSON(t, h, http.MethodGet, "/api/projects/p1/bench/cases", ""); len(list["cases"].([]any)) != 0 {
		t.Fatalf("a refused body stored a case: %v", list)
	}
}

func TestACaseExpectingAnUnmockedToolIs422WithTheFieldPath(t *testing.T) {
	h := benchHandler(t, ":memory:")
	code, body := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/cases",
		`{"prompt":"p","tools":[{"name":"read_file","result":"x"}],"expect":{"tools":[{"tool":"write_file"}]}}`)
	errs, _ := body["errors"].([]any)
	if code != http.StatusUnprocessableEntity || len(errs) != 1 {
		t.Fatalf("= %d %v", code, body)
	}
	e := errs[0].(map[string]any)
	if e["field"] != "expect.tools[0].tool" || e["code"] != "UNKNOWN_TOOL" {
		t.Fatalf("error = %v", e)
	}
}

func TestACaseWithNullToolsAndAReferenceIsStoredWithAnEmptyToolList(t *testing.T) {
	h := benchHandler(t, ":memory:")
	code, c := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/cases", `{"prompt":"p","tools":null,"expect":{"reference":"r"}}`)
	if code != http.StatusCreated {
		t.Fatalf("= %d %v", code, c)
	}
	if tools, ok := c["tools"].([]any); !ok || len(tools) != 0 {
		t.Fatalf("tools = %#v, want []", c["tools"])
	}
}

func TestDeletingAnUnknownCaseOrAnotherProjectsCaseIs404AndLeavesTheCase(t *testing.T) {
	h := benchHandler(t, ":memory:")
	rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	var created domain.BenchCase
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	if got := benchDelete(t, h, "/projects/p1/bench/cases/nope"); got.Code != http.StatusNotFound {
		t.Fatalf("unknown = %d", got.Code)
	}
	if got := benchDelete(t, h, "/projects/p2/bench/cases/"+created.ID); got.Code != http.StatusNotFound {
		t.Fatalf("cross-project delete = %d", got.Code)
	}
	if _, list := doJSON(t, h, http.MethodGet, "/api/projects/p1/bench/cases", ""); len(list["cases"].([]any)) != 1 {
		t.Fatalf("the case was deleted through another project: %v", list)
	}
	if _, list := doJSON(t, h, http.MethodGet, "/api/projects/p2/bench/cases", ""); len(list["cases"].([]any)) != 0 {
		t.Fatalf("p2 sees p1's case: %v", list)
	}
}

func TestAMalformedRunBodyIs400AndStartsNoRun(t *testing.T) {
	h, release := benchRunsHandler(t)
	postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	close(release)
	if code, _ := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/runs", `{"model":`); code != http.StatusBadRequest {
		t.Fatalf("malformed = %d, want 400", code)
	}
	if code, list := doJSON(t, h, http.MethodGet, "/api/projects/p1/bench/scorecards", ""); code != http.StatusOK || len(list["scorecards"].([]any)) != 0 {
		t.Fatalf("a refused request ran: %v", list)
	}
	// An empty body and an explicit empty object both run on the defaults.
	for _, body := range []string{``, `{}`} {
		code, run := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/runs", body)
		if code != http.StatusAccepted {
			t.Fatalf("body %q = %d %v", body, code, run)
		}
		waitRunEnds(t, h, run["id"].(string))
	}
}

func TestConcurrentRunRequestsOverHTTPStartExactlyOneRun(t *testing.T) {
	h, release := benchRunsHandler(t)
	postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))

	const n = 10
	codes := make(chan int, n)
	ids := make(chan string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec := httptest.NewRecorder()
			NewRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/projects/p1/bench/runs", nil))
			codes <- rec.Code
			var run map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &run)
			if id, ok := run["id"].(string); ok {
				ids <- id
			}
		}()
	}
	close(start)
	wg.Wait()
	close(release)
	close(codes)
	accepted, conflict := 0, 0
	for c := range codes {
		switch c {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflict++
		default:
			t.Errorf("status %d", c)
		}
	}
	if accepted != 1 || conflict != n-1 {
		t.Fatalf("accepted %d conflict %d", accepted, conflict)
	}
	waitRunEnds(t, h, <-ids)
}

func TestAfterACancelANewRunCanStartAndComparingSkipsTheCancelledOne(t *testing.T) {
	h, release := benchRunsHandler(t)
	postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))

	// Only run so far is cancelled: the next one has no baseline to compare with.
	_, first := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs")
	fid := first["id"].(string)
	benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs/"+fid+"/cancel")
	waitRunEnds(t, h, fid)

	close(release)
	code, second := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs")
	if code != http.StatusAccepted {
		t.Fatalf("a new run after a cancel = %d %v", code, second)
	}
	sid := second["id"].(string)
	waitRunEnds(t, h, sid)
	if code, body := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+sid+"/compare"); code != http.StatusNotFound || body["code"] != "BENCH_NO_PREVIOUS_SCORECARD" {
		t.Fatalf("compare with only a cancelled previous = %d %v", code, body)
	}
}

func TestARunAndAScorecardOfAnotherProjectAreNotFound(t *testing.T) {
	h, release := benchRunsHandler(t)
	close(release)
	postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	_, run := benchRunBody(t, h, http.MethodPost, "/projects/p1/bench/runs")
	id := run["id"].(string)
	waitRunEnds(t, h, id)
	// p2 does not exist in the run handler: an unknown project is 404 on every read.
	for _, p := range []string{"/projects/p2/bench/runs/" + id, "/projects/p2/bench/scorecards/" + id, "/projects/p2/bench/scorecards/" + id + "/compare", "/projects/p2/bench/scorecards"} {
		if code, _ := benchRunBody(t, h, http.MethodGet, p); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, code)
		}
	}
}

// ---- trajectories ----

// failingToolSession is an ended session whose only tool call failed.
type failingToolSession struct{ recordedSession }

func (failingToolSession) ListEvents(string, int64) ([]ports.StoredEvent, error) {
	ev := func(seq int64, typ string, raw string) ports.StoredEvent {
		return ports.StoredEvent{Seq: seq, Type: typ, Payload: json.RawMessage(raw)}
	}
	return []ports.StoredEvent{
		ev(1, "tool_use", `{"type":"tool_use","tool_name":"read_file","tool_input":{"path":"gone.md"}}`),
		ev(2, "tool_result", `{"type":"tool_result","text":"Error: no such file","is_error":true}`),
		ev(3, "usage", `{"type":"usage","text":"It does not exist."}`),
	}, nil
}

func trajectoryHandlerWith(t *testing.T, log interface {
	Find(ctx context.Context, id string) (*domain.Session, error)
	ListEvents(sessionID string, fromSeq int64) ([]ports.StoredEvent, error)
}) (*Handler, *bench.Service) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := bench.New(bench.Deps{
		Cases: sqlite.NewBenchCaseRepository(db), Trajectories: sqlite.NewBenchTrajectoryRepository(db),
		Tasks: recordingTasks{"t1": {ID: "t1", ProjectID: "p1"}}, SessionLog: log, Settings: recordingDefault(true),
		Projects: agentSystemProjects{"p1": {ID: "p1"}, "p2": {ID: "p2"}},
	})
	return NewHandler(Services{Bench: svc, BenchTrajectories: svc}), svc
}

// A trajectory recorded with a failing tool call must replay that call as a
// failure; a case that answers it with success tests a different run.
func TestARecordedFailingToolCallBecomesAMockThatFailsOverHTTP(t *testing.T) {
	h, svc := trajectoryHandlerWith(t, failingToolSession{})
	if err := svc.RecordSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	code, c := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/cases/from-trajectory", `{"trajectory_id":"s1"}`)
	if code != http.StatusCreated {
		t.Fatalf("= %d %v", code, c)
	}
	tool := c["tools"].([]any)[0].(map[string]any)
	if tool["is_error"] != true {
		t.Fatalf("the failing call replays as a success: %v", tool)
	}
}

func TestATrajectoryOfAnotherProjectIsNotFoundAndNotMadeACase(t *testing.T) {
	h, svc := trajectoryHandler(t, true)
	if err := svc.RecordSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"p2", "nope"} {
		if code, _ := doJSON(t, h, http.MethodGet, "/api/projects/"+p+"/bench/trajectories/s1", ""); code != http.StatusNotFound {
			t.Errorf("GET trajectory via %s = %d, want 404", p, code)
		}
		if code, _ := doJSON(t, h, http.MethodPost, "/api/projects/"+p+"/bench/cases/from-trajectory", `{"trajectory_id":"s1"}`); code != http.StatusNotFound {
			t.Errorf("case from trajectory via %s = %d, want 404", p, code)
		}
	}
}

func TestACaseFromTrajectoryNeedsAnIDAndAReadableBody(t *testing.T) {
	h, _ := trajectoryHandler(t, true)
	if code, _ := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/cases/from-trajectory", `{`); code != http.StatusBadRequest {
		t.Fatalf("malformed = %d, want 400", code)
	}
	if code, _ := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/cases/from-trajectory", `{}`); code != http.StatusNotFound {
		t.Fatalf("no id = %d, want 404", code)
	}
}

func TestATrajectoryRecordingFlagRefusesMalformedBodiesAndUnknownTasks(t *testing.T) {
	h, _ := trajectoryHandler(t, true)
	if code, _ := putJSON(t, h, "/projects/p1/tasks/t1/trajectory-recording", `{"enabled":"yes"}`); code != http.StatusBadRequest {
		t.Fatalf("string flag = %d, want 400", code)
	}
	if code, _ := putJSON(t, h, "/projects/p1/tasks/nope/trajectory-recording", `{"enabled":true}`); code != http.StatusNotFound {
		t.Fatalf("unknown task = %d, want 404", code)
	}
	// The flag did not change on a refused request.
	if _, rec := doJSON(t, h, http.MethodGet, "/api/projects/p1/tasks/t1/trajectory-recording", ""); rec["source"] != "project" {
		t.Fatalf("flag = %v", rec)
	}
}

// {} or a key the flag does not have is a mistake, not a request to clear the
// flag: only an explicit {"enabled": null} follows the default again.
func TestATrajectoryRecordingFlagRefusesABodyWithoutEnabledOrWithAnUnknownKey(t *testing.T) {
	h, _ := trajectoryHandler(t, true)
	const path = "/projects/p1/tasks/t1/trajectory-recording"
	if code, _ := putJSON(t, h, path, `{"enabled": false}`); code != http.StatusOK {
		t.Fatalf("set = %d", code)
	}
	for _, body := range []string{`{}`, `{"enable": true}`, `{"enabled": true, "extra": 1}`, `{"extra": null}`} {
		if code, _ := putJSON(t, h, path, body); code != http.StatusUnprocessableEntity {
			t.Errorf("PUT %s = %d, want 422", body, code)
		}
	}
	if _, rec := doJSON(t, h, http.MethodGet, "/api"+path, ""); rec["enabled"] != false || rec["source"] != "task" {
		t.Fatalf("a refused body changed the flag: %v", rec)
	}
}
