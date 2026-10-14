package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/usage"
	"operators-mcp/internal/application/usage/usagetest"
)

// The usage API in-process (seam H1). The tests hand usage records to the
// harness's usage sink directly, the way the LLM proxy does, and read them
// back through the API.

func usageServer(t *testing.T, opts ...usagetest.Option) (*httptest.Server, *usagetest.Env) {
	t.Helper()
	e := usagetest.NewEnv(t, opts...)
	srv := httptest.NewServer(NewRouter(NewHandler(Services{Usage: e.Service})))
	t.Cleanup(srv.Close)
	return srv, e
}

var usageT0 = time.Date(2026, 10, 14, 10, 0, 0, 0, time.Local)

func aRequest(attribution, model string, at time.Time, in, out, cacheWrite, cacheRead int64) usage.Request {
	return usage.Request{
		ID: "req-" + at.Format("150405.000") + attribution, Attribution: attribution, Method: "POST", Path: "/v1/messages",
		Model: model, Status: 200, Streamed: true, StartedAt: at, EndedAt: at.Add(1500 * time.Millisecond),
		InputTokens: in, OutputTokens: out, CacheCreationInputTokens: cacheWrite, CacheReadInputTokens: cacheRead,
	}
}

type wireRecord struct {
	ID                       string   `json:"id"`
	Attribution              string   `json:"attribution"`
	ProjectID                string   `json:"project_id"`
	TaskID                   string   `json:"task_id"`
	TaskTitle                string   `json:"task_title"`
	SessionID                string   `json:"session_id"`
	Model                    string   `json:"model"`
	Status                   int      `json:"status"`
	Streamed                 bool     `json:"streamed"`
	StartedAt                string   `json:"started_at"`
	DurationMS               int64    `json:"duration_ms"`
	InputTokens              int64    `json:"input_tokens"`
	OutputTokens             int64    `json:"output_tokens"`
	CacheCreationInputTokens int64    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64    `json:"cache_read_input_tokens"`
	Error                    string   `json:"error"`
	EstimatedCostUSD         *float64 `json:"estimated_cost_usd"`
}

func getUsageJSON(t *testing.T, url string, into any) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if resp.StatusCode == http.StatusOK && into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
	}
	return resp.StatusCode, m
}

func recentRecords(t *testing.T, srv *httptest.Server, project, query string) []wireRecord {
	t.Helper()
	var page struct {
		Records []wireRecord `json:"records"`
	}
	if code, body := getUsageJSON(t, srv.URL+"/api/projects/"+project+"/usage/records"+query, &page); code != 200 {
		t.Fatalf("recent records = %d %v", code, body)
	}
	return page.Records
}

func TestAUsageRecordIsAttributedToItsSessionTaskAndProject(t *testing.T) {
	srv, e := usageServer(t)
	e.Service.Record(context.Background(), aRequest(e.SessionA, "claude-sonnet-4-5-20250929", usageT0, 1200, 340, 5000, 80000))

	got := recentRecords(t, srv, e.ProjectA, "")
	if len(got) != 1 {
		t.Fatalf("project A has %d records, want 1", len(got))
	}
	r := got[0]
	if r.SessionID != e.SessionA || r.TaskID != e.TaskA || r.ProjectID != e.ProjectA || r.TaskTitle != "Build it" {
		t.Errorf("attributed to session %q task %q (%q) project %q, want %q %q (Build it) %q", r.SessionID, r.TaskID, r.TaskTitle, r.ProjectID, e.SessionA, e.TaskA, e.ProjectA)
	}
	if r.Model != "claude-sonnet-4-5-20250929" || r.Status != 200 || !r.Streamed || r.DurationMS != 1500 {
		t.Errorf("record = %+v, want the model, status 200, streamed, 1500 ms", r)
	}
	if r.InputTokens != 1200 || r.OutputTokens != 340 || r.CacheCreationInputTokens != 5000 || r.CacheReadInputTokens != 80000 {
		t.Errorf("tokens = %d/%d/%d/%d, want 1200/340/5000/80000", r.InputTokens, r.OutputTokens, r.CacheCreationInputTokens, r.CacheReadInputTokens)
	}
	if other := recentRecords(t, srv, e.ProjectB, ""); len(other) != 0 {
		t.Errorf("project B has %d records, want none", len(other))
	}
}

func TestHarnessSystemAndUnknownAttributionsAreKeptOutsideAnyProject(t *testing.T) {
	srv, e := usageServer(t)
	ctx := context.Background()
	e.Service.Record(ctx, aRequest(usage.SystemAttribution, "claude-haiku-4-5", usageT0, 10, 20, 0, 0))
	e.Service.Record(ctx, aRequest("no-such-session", "claude-haiku-4-5", usageT0.Add(time.Second), 1, 2, 0, 0))
	e.Service.Record(ctx, aRequest("", "claude-haiku-4-5", usageT0.Add(2*time.Second), 3, 4, 0, 0))

	for _, p := range []string{e.ProjectA, e.ProjectB} {
		if got := recentRecords(t, srv, p, ""); len(got) != 0 {
			t.Errorf("project %s has %d records, want none", p, len(got))
		}
	}
	got := recentRecords(t, srv, usage.SystemProject, "")
	if len(got) != 3 {
		t.Fatalf("the harness's own records = %d, want 3 (system, unknown, empty)", len(got))
	}
	want := []string{"", "no-such-session", usage.SystemAttribution} // newest first
	for i, r := range got {
		if r.Attribution != want[i] || r.ProjectID != "" || r.SessionID != "" || r.TaskID != "" {
			t.Errorf("record %d = attribution %q project %q session %q, want %q unresolved", i, r.Attribution, r.ProjectID, r.SessionID, want[i])
		}
	}
}

func TestRecentUsageRecordsAreNewestFirstAndLimited(t *testing.T) {
	srv, e := usageServer(t)
	for i := 0; i < 5; i++ {
		e.Service.Record(context.Background(), aRequest(e.SessionA, "claude-opus-4-1", usageT0.Add(time.Duration(i)*time.Minute), int64(i), 0, 0, 0))
	}
	got := recentRecords(t, srv, e.ProjectA, "?limit=3")
	if len(got) != 3 || got[0].InputTokens != 4 || got[1].InputTokens != 3 || got[2].InputTokens != 2 {
		t.Fatalf("limit=3 gave %+v, want the three newest, newest first", got)
	}
	if got := recentRecords(t, srv, "no-such-project", ""); got == nil || len(got) != 0 {
		t.Errorf("unknown project gave %v, want an empty list", got)
	}
	for _, q := range []string{"?limit=0", "?limit=201", "?limit=ten"} {
		if code, body := getUsageJSON(t, srv.URL+"/api/projects/"+e.ProjectA+"/usage/records"+q, nil); code != 400 || body["code"] != "INVALID_INPUT" {
			t.Errorf("%s = %d %v, want 400 INVALID_INPUT", q, code, body)
		}
	}
}

func TestAFailedUsageWriteIsLoggedAndNeverReachesTheCaller(t *testing.T) {
	_, e := usageServer(t)
	if err := e.DB.Exec("DROP TABLE usage_records").Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.Service.Record(context.Background(), aRequest(e.SessionA, "claude-sonnet-4-5", usageT0, 1, 1, 0, 0))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Record did not return after a failed write")
	}
	if !strings.Contains(e.Log.String(), "usage record") {
		t.Errorf("log = %q, want the failed usage record logged", e.Log.String())
	}
}

func TestUsageUnconfiguredAnswers503(t *testing.T) {
	srv := httptest.NewServer(NewRouter(NewHandler(Services{})))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/projects/p/usage/records")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no usage service = %d, want 503", resp.StatusCode)
	}
}

func TestEachUsageRecordCarriesAnEstimatedCostFromThePriceTable(t *testing.T) {
	srv, e := usageServer(t)
	cases := []struct {
		model                   string
		in, out, cacheW, cacheR int64
		want                    float64
	}{
		// 1200×$3 + 340×$15 + 5000×$3.75 + 80000×$0.30 per million.
		{"claude-sonnet-4-5-20250929", 1200, 340, 5000, 80000, 0.05145},
		// $4 per million in, $20 per million out.
		{"claude-opus-5-5", 1_000_000, 100_000, 0, 0, 6.0},
		// Opus 5.5 reads cache at $0.20, a twentieth of its input price.
		{"claude-opus-5-5", 0, 0, 0, 1_000_000, 0.20},
		// Fable 5.1 writes cache at $12.50 and reads it at $0.25.
		{"claude-fable-5-1", 0, 0, 1_000_000, 1_000_000, 12.75},
		// Haiku 5.5 over 100K prompt tokens: $0.50 in, $2.50 out.
		{"claude-haiku-5-5", 150_000, 1000, 0, 0, 0.0775},
		// ...and at or under it: $0.10 in, $0.50 out.
		{"claude-haiku-5-5", 100_000, 1000, 0, 0, 0.0105},
		// Model ids as the CLI may send them: a 1M-context suffix, a dated id.
		{"claude-opus-4-1-20250805", 1_000_000, 0, 0, 0, 15.0},
		{"claude-sonnet-4-6[1m]", 1_000_000, 0, 0, 0, 3.0},
	}
	for i, c := range cases {
		e.Service.Record(context.Background(), aRequest(e.SessionA, c.model, usageT0.Add(time.Duration(i)*time.Second), c.in, c.out, c.cacheW, c.cacheR))
	}
	got := recentRecords(t, srv, e.ProjectA, "")
	for i, c := range cases {
		r := got[len(got)-1-i]
		if r.EstimatedCostUSD == nil {
			t.Errorf("%s: no estimated cost, want %v", c.model, c.want)
			continue
		}
		if diff := *r.EstimatedCostUSD - c.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s %d/%d/%d/%d: estimated cost %v, want %v", c.model, c.in, c.out, c.cacheW, c.cacheR, *r.EstimatedCostUSD, c.want)
		}
	}
}

func TestAnUnknownModelHasTokensButNoEstimatedCost(t *testing.T) {
	srv, e := usageServer(t)
	for i, model := range []string{"claude-opus-9", "gpt-4o", ""} {
		e.Service.Record(context.Background(), aRequest(e.SessionA, model, usageT0.Add(time.Duration(i)*time.Second), 10, 20, 0, 0))
	}
	resp, err := http.Get(srv.URL + "/api/projects/" + e.ProjectA + "/usage/records")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var page struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	for _, r := range page.Records {
		cost, present := r["estimated_cost_usd"]
		if !present || cost != nil {
			t.Errorf("model %q: estimated_cost_usd = %v (present %v), want null", r["model"], cost, present)
		}
		if r["input_tokens"] != float64(10) || r["output_tokens"] != float64(20) {
			t.Errorf("model %q: tokens %v/%v, want 10/20", r["model"], r["input_tokens"], r["output_tokens"])
		}
	}
}
