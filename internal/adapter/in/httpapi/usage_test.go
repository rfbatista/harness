package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"operators-mcp/internal/application/usage/usagetest"
	"operators-mcp/internal/ports"
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

var requestSeq atomic.Int64

func aRequest(attribution, model string, at time.Time, in, out, cacheWrite, cacheRead int64) ports.UsageRequest {
	return ports.UsageRequest{
		ID: fmt.Sprintf("req-%d", requestSeq.Add(1)), Attribution: attribution, Method: "POST", Path: "/v1/messages",
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
	e.Service.Record(ctx, aRequest(ports.UsageSystemAttribution, "claude-haiku-4-5", usageT0, 10, 20, 0, 0))
	e.Service.Record(ctx, aRequest("no-such-session", "claude-haiku-4-5", usageT0.Add(time.Second), 1, 2, 0, 0))
	e.Service.Record(ctx, aRequest("", "claude-haiku-4-5", usageT0.Add(2*time.Second), 3, 4, 0, 0))

	for _, p := range []string{e.ProjectA, e.ProjectB} {
		if got := recentRecords(t, srv, p, ""); len(got) != 0 {
			t.Errorf("project %s has %d records, want none", p, len(got))
		}
	}
	got := recentRecords(t, srv, ports.UsageSystemProject, "")
	if len(got) != 3 {
		t.Fatalf("the harness's own records = %d, want 3 (system, unknown, empty)", len(got))
	}
	want := []string{"", "no-such-session", ports.UsageSystemAttribution} // newest first
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

type wireTotals struct {
	Requests                 int64    `json:"requests"`
	InputTokens              int64    `json:"input_tokens"`
	OutputTokens             int64    `json:"output_tokens"`
	CacheCreationInputTokens int64    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64    `json:"cache_read_input_tokens"`
	TotalTokens              int64    `json:"total_tokens"`
	EstimatedCostUSD         *float64 `json:"estimated_cost_usd"`
	UnpricedTokens           int64    `json:"unpriced_tokens"`
}

type wireSummary struct {
	Range  string     `json:"range"`
	Bucket string     `json:"bucket"`
	Since  string     `json:"since"`
	Totals wireTotals `json:"totals"`
	Series []struct {
		Start       string `json:"start"`
		Model       string `json:"model"`
		TotalTokens int64  `json:"total_tokens"`
	} `json:"series"`
	TopTasks []struct {
		TaskID    string     `json:"task_id"`
		TaskTitle string     `json:"task_title"`
		Totals    wireTotals `json:"totals"`
	} `json:"top_tasks"`
	TopSessions []struct {
		SessionID string     `json:"session_id"`
		TaskID    string     `json:"task_id"`
		TaskTitle string     `json:"task_title"`
		Totals    wireTotals `json:"totals"`
	} `json:"top_sessions"`
}

func usageSummary(t *testing.T, srv *httptest.Server, project, rng string) wireSummary {
	t.Helper()
	var s wireSummary
	if code, body := getUsageJSON(t, srv.URL+"/api/projects/"+project+"/usage?range="+rng, &s); code != 200 {
		t.Fatalf("usage %s = %d %v", rng, code, body)
	}
	return s
}

func near(a *float64, want float64) bool {
	return a != nil && *a-want < 1e-9 && want-*a < 1e-9
}

// The clock stands at 11:00 on 2026-10-14, local time.
var usageNow = usageT0.Add(time.Hour)

func TestUsageTotalsCountEachRecordInTheRangesItFallsIn(t *testing.T) {
	srv, e := usageServer(t, usagetest.WithClock(func() time.Time { return usageNow }))
	ctx := context.Background()
	midnight := time.Date(2026, 10, 14, 0, 0, 0, 0, time.Local)
	// Sonnet 4.5: $3 in, $15 out, $3.75 cache write, $0.30 cache read.
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0, 1_000_000, 100_000, 0, 0))              // today: $4.50
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", midnight.Add(-time.Minute), 0, 0, 1_000_000, 0)) // yesterday: $3.75
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", midnight.AddDate(0, 0, -8), 0, 0, 0, 1_000_000)) // 8 days ago: $0.30
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", midnight.AddDate(0, 0, -40), 7, 7, 7, 7))        // too old for any range
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-opus-9", usageT0.Add(time.Minute), 500, 0, 0, 0))             // today, unpriced

	cases := []struct {
		rng                     string
		requests, total         int64
		in, out, cacheW, cacheR int64
		cost                    float64
		unpriced                int64
	}{
		{"today", 2, 1_100_500, 1_000_500, 100_000, 0, 0, 4.50, 500},
		{"7d", 3, 2_100_500, 1_000_500, 100_000, 1_000_000, 0, 8.25, 500},
		{"30d", 4, 3_100_500, 1_000_500, 100_000, 1_000_000, 1_000_000, 8.55, 500},
	}
	for _, c := range cases {
		s := usageSummary(t, srv, e.ProjectA, c.rng)
		tt := s.Totals
		if tt.Requests != c.requests || tt.TotalTokens != c.total || tt.InputTokens != c.in || tt.OutputTokens != c.out ||
			tt.CacheCreationInputTokens != c.cacheW || tt.CacheReadInputTokens != c.cacheR || tt.UnpricedTokens != c.unpriced {
			t.Errorf("%s totals = %+v, want %d requests, %d tokens (%d/%d/%d/%d), %d unpriced", c.rng, tt, c.requests, c.total, c.in, c.out, c.cacheW, c.cacheR, c.unpriced)
		}
		if !near(tt.EstimatedCostUSD, c.cost) {
			t.Errorf("%s estimated cost = %v, want %v", c.rng, tt.EstimatedCostUSD, c.cost)
		}
	}
	if s := usageSummary(t, srv, e.ProjectA, "today"); s.Range != "today" || s.Bucket != "hour" || s.Since == "" {
		t.Errorf("today = range %q bucket %q since %q, want today, hour, local midnight", s.Range, s.Bucket, s.Since)
	}
	if s := usageSummary(t, srv, e.ProjectB, "30d"); s.Totals.Requests != 0 || s.Totals.TotalTokens != 0 || !near(s.Totals.EstimatedCostUSD, 0) {
		t.Errorf("project B totals = %+v, want nothing and an estimated cost of 0", s.Totals)
	}
}

func TestOnlyUnpricedUsageHasNoEstimatedCostTotal(t *testing.T) {
	srv, e := usageServer(t, usagetest.WithClock(func() time.Time { return usageNow }))
	e.Service.Record(context.Background(), aRequest(e.SessionA, "claude-opus-9", usageT0, 10, 0, 0, 0))
	if s := usageSummary(t, srv, e.ProjectA, "today"); s.Totals.EstimatedCostUSD != nil || s.Totals.UnpricedTokens != 10 {
		t.Errorf("totals = %+v, want no estimated cost and 10 unpriced tokens", s.Totals)
	}
}

func TestTheUsageSeriesIsSplitByModelPerBucket(t *testing.T) {
	srv, e := usageServer(t, usagetest.WithClock(func() time.Time { return usageNow }))
	ctx := context.Background()
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0.Add(-50*time.Minute), 100, 0, 0, 0)) // 09:10
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0.Add(-10*time.Minute), 20, 0, 0, 0))  // 09:50
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-haiku-4-5", usageT0.Add(-5*time.Minute), 3, 0, 0, 0))     // 09:55
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0.Add(5*time.Minute), 4000, 0, 0, 0))  // 10:05
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0.AddDate(0, 0, -2), 50000, 0, 0, 0))  // two days ago

	type point struct {
		hour  int
		model string
	}
	got := map[point]int64{}
	for _, p := range usageSummary(t, srv, e.ProjectA, "today").Series {
		at, err := time.Parse(time.RFC3339, p.Start)
		if err != nil {
			t.Fatalf("series start %q: %v", p.Start, err)
		}
		got[point{at.In(time.Local).Hour(), p.Model}] += p.TotalTokens
	}
	want := map[point]int64{{9, "claude-sonnet-4-5"}: 120, {9, "claude-haiku-4-5"}: 3, {10, "claude-sonnet-4-5"}: 4000}
	if len(got) != len(want) {
		t.Errorf("today's series = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("today's series at %d:00 for %s = %d, want %d", k.hour, k.model, got[k], v)
		}
	}
	week := usageSummary(t, srv, e.ProjectA, "7d")
	if week.Bucket != "day" || len(week.Series) != 3 {
		t.Errorf("7d series = bucket %q, %d points, want day buckets: today ×2 models, two days ago", week.Bucket, len(week.Series))
	}
}

func TestTopTasksAndSessionsAreRankedByTokens(t *testing.T) {
	srv, e := usageServer(t, usagetest.WithClock(func() time.Time { return usageNow }))
	ctx := context.Background()
	second := e.AddSession(t, e.ProjectA, "Ship it")
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0, 100, 0, 0, 0))
	e.Service.Record(ctx, aRequest(second.SessionID, "claude-sonnet-4-5", usageT0.Add(time.Second), 300, 0, 0, 0))
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-haiku-4-5", usageT0.Add(2*time.Second), 50, 0, 0, 0))

	s := usageSummary(t, srv, e.ProjectA, "today")
	if len(s.TopTasks) != 2 || s.TopTasks[0].TaskID != second.TaskID || s.TopTasks[0].TaskTitle != "Ship it" || s.TopTasks[0].Totals.TotalTokens != 300 ||
		s.TopTasks[1].TaskID != e.TaskA || s.TopTasks[1].TaskTitle != "Build it" || s.TopTasks[1].Totals.TotalTokens != 150 {
		t.Errorf("top tasks = %+v, want Ship it (300) then Build it (150)", s.TopTasks)
	}
	if len(s.TopSessions) != 2 || s.TopSessions[0].SessionID != second.SessionID || s.TopSessions[0].TaskTitle != "Ship it" ||
		s.TopSessions[1].SessionID != e.SessionA || s.TopSessions[1].Totals.TotalTokens != 150 {
		t.Errorf("top sessions = %+v, want the second session (300) then session A (150)", s.TopSessions)
	}
	// 100×$3 + 50×$1 per million.
	if !near(s.TopTasks[1].Totals.EstimatedCostUSD, 0.00035) {
		t.Errorf("Build it estimated cost = %v, want 0.00035", s.TopTasks[1].Totals.EstimatedCostUSD)
	}
}

func TestTaskAndSessionUsageTotalsAgreeWithTheirRecords(t *testing.T) {
	srv, e := usageServer(t, usagetest.WithClock(func() time.Time { return usageNow }))
	ctx := context.Background()
	second := e.AddSession(t, e.ProjectA, "Ship it")
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0, 1_000_000, 0, 0, 0))
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0.AddDate(0, 0, -90), 0, 0, 0, 1_000_000))
	e.Service.Record(ctx, aRequest(second.SessionID, "claude-sonnet-4-5", usageT0, 7, 0, 0, 0))

	var task, session, other wireTotals
	for _, c := range []struct {
		query string
		into  *wireTotals
	}{
		{"task_id=" + e.TaskA, &task}, {"session_id=" + e.SessionA, &session}, {"session_id=" + second.SessionID, &other},
	} {
		if code, body := getUsageJSON(t, srv.URL+"/api/usage/totals?"+c.query, c.into); code != 200 {
			t.Fatalf("totals %s = %d %v", c.query, code, body)
		}
	}
	// Totals cover every record, whatever its age: $3 + $0.30.
	if task.Requests != 2 || task.TotalTokens != 2_000_000 || !near(task.EstimatedCostUSD, 3.30) {
		t.Errorf("task A totals = %+v, want 2 requests, 2M tokens, $3.30", task)
	}
	if session.Requests != task.Requests || session.TotalTokens != task.TotalTokens || !near(session.EstimatedCostUSD, 3.30) {
		t.Errorf("session A totals = %+v, want the same as its task's %+v", session, task)
	}
	if other.Requests != 1 || other.TotalTokens != 7 {
		t.Errorf("second session totals = %+v, want 1 request, 7 tokens", other)
	}

	// The task page reads the task's and its sessions' totals at once: the
	// same totals, and zero for a session with no records.
	taskAt, sessionsAt, err := e.Service.TaskUsageTotals(ctx, e.TaskA, []string{e.SessionA, second.SessionID, "no-records"})
	if err != nil {
		t.Fatal(err)
	}
	if taskAt.Requests != task.Requests || taskAt.TotalTokens != task.TotalTokens || !near(taskAt.EstimatedCostUSD, 3.30) {
		t.Errorf("task A totals at once = %+v, want %+v", taskAt, task)
	}
	if a := sessionsAt[e.SessionA]; a == nil || a.TotalTokens != session.TotalTokens || !near(a.EstimatedCostUSD, 3.30) {
		t.Errorf("session A totals at once = %+v, want %+v", a, session)
	}
	if b := sessionsAt[second.SessionID]; b == nil || b.Requests != 1 || b.TotalTokens != 7 {
		t.Errorf("second session totals at once = %+v, want 1 request, 7 tokens", b)
	}
	if none := sessionsAt["no-records"]; none == nil || none.Requests != 0 || !near(none.EstimatedCostUSD, 0) {
		t.Errorf("a session with no records at once = %+v, want zero totals", none)
	}
}

func TestUsageQueriesRefuseInvalidInput(t *testing.T) {
	srv, e := usageServer(t)
	for _, path := range []string{
		"/api/projects/" + e.ProjectA + "/usage?range=year",
		"/api/usage/totals",
		"/api/usage/totals?task_id=a&session_id=b",
	} {
		if code, body := getUsageJSON(t, srv.URL+path, nil); code != 400 || body["code"] != "INVALID_INPUT" {
			t.Errorf("GET %s = %d %v, want 400 INVALID_INPUT", path, code, body)
		}
	}
	if code, _ := getUsageJSON(t, srv.URL+"/api/projects/"+e.ProjectA+"/usage", nil); code != 200 {
		t.Errorf("no range = %d, want 200 (today)", code)
	}
}

// followUsage opens a project's usage stream and answers its events, one
// decoded data: line each.
func followUsage(t *testing.T, ctx context.Context, srv *httptest.Server, project string) <-chan wireRecord {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/projects/"+project+"/usage/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	out := make(chan wireRecord, 16)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event:") {
				t.Errorf("usage stream sent %q, want bare data: lines", line)
			}
			data, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue
			}
			var r wireRecord
			if err := json.Unmarshal([]byte(data), &r); err != nil {
				t.Errorf("usage event %q: %v", data, err)
				return
			}
			out <- r
		}
	}()
	return out
}

func TestEachStoredUsageRecordReachesItsProjectsStream(t *testing.T) {
	srv, e := usageServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := followUsage(t, ctx, srv, e.ProjectA)
	b := followUsage(t, ctx, srv, e.ProjectB)

	e.Service.Record(ctx, aRequest(e.SessionB, "claude-sonnet-4-5", usageT0, 1, 1, 0, 0))
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0, 1_000_000, 0, 0, 0))

	select {
	case r := <-a:
		if r.SessionID != e.SessionA || r.TaskTitle != "Build it" || r.InputTokens != 1_000_000 || !near(r.EstimatedCostUSD, 3.0) {
			t.Errorf("project A got %+v, want session A's record with its task title and $3 estimated", r)
		}
	case <-ctx.Done():
		t.Fatal("project A's stream got nothing")
	}
	select {
	case r := <-b:
		if r.SessionID != e.SessionB {
			t.Errorf("project B got %+v, want only session B's record", r)
		}
	case <-ctx.Done():
		t.Fatal("project B's stream got nothing")
	}
	select {
	case r := <-a:
		t.Errorf("project A got a second record %+v, want only its own", r)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAUsageRecordThatWasNotStoredIsNotStreamed(t *testing.T) {
	srv, e := usageServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := followUsage(t, ctx, srv, e.ProjectA)
	if err := e.DB.Exec("DROP TABLE usage_records").Error; err != nil {
		t.Fatal(err)
	}
	e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0, 1, 1, 0, 0))
	select {
	case r := <-a:
		t.Errorf("stream got %+v for a record that failed to store", r)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAStalledUsageFollowerNeverDelaysTheSink(t *testing.T) {
	srv, e := usageServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Opened and never read.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/projects/"+e.ProjectA+"/usage/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			e.Service.Record(ctx, aRequest(e.SessionA, "claude-sonnet-4-5", usageT0.Add(time.Duration(i)*time.Millisecond), 4096, 4096, 4096, 4096))
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the sink stalled behind a follower that never reads")
	}
	if got := recentRecords(t, srv, e.ProjectA, "?limit=1"); len(got) != 1 {
		t.Fatalf("records = %d, want them stored", len(got))
	}
}
