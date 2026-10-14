package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeUsage answers the Usage page's reads: today's summary and the recent
// records of the project it is asked about.
type fakeUsage struct {
	failSummary bool
	asked       *[]string
}

func (f fakeUsage) UsageSummary(_ context.Context, projectID string, r ports.UsageRange) (*ports.UsageSummary, error) {
	if f.asked != nil {
		*f.asked = append(*f.asked, "summary "+projectID+" "+string(r))
	}
	if f.failSummary {
		return nil, errors.New("database is locked")
	}
	cost := 0.5
	totals := ports.UsageTotals{Requests: 2, InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200, EstimatedCostUSD: &cost}
	return &ports.UsageSummary{
		Range: r, Since: now.Truncate(24 * 60 * 60 * 1e9), Bucket: ports.UsageBucketHour, Totals: totals,
		Series:      []ports.UsageSeriesTokens{{Start: now.Truncate(60 * 60 * 1e9), Model: "claude-sonnet-4-5", InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200}},
		TopTasks:    []ports.UsageByTask{{TaskID: "t-feed", TaskTitle: "Add SSE feed", Totals: totals}},
		TopSessions: []ports.UsageBySession{{SessionID: "s1", TaskID: "t-feed", TaskTitle: "Add SSE feed", Totals: totals}},
	}, nil
}

func (f fakeUsage) ListRecentUsage(_ context.Context, q ports.UsageQuery) (*ports.UsageRecords, error) {
	if f.asked != nil {
		*f.asked = append(*f.asked, "records "+q.ProjectID)
	}
	return &ports.UsageRecords{Records: []*domain.UsageRecord{{ID: "req-7", ProjectID: q.ProjectID, TaskID: "t-feed", SessionID: "s1", Model: "claude-sonnet-4-5", StartedAt: now, InputTokens: 10}}}, nil
}

func TestUsagePageRendersWithItsSeedAndTheRailLinksIt(t *testing.T) {
	var asked []string
	w := board()
	w.usage = fakeUsage{asked: &asked}
	h := newTestHandler(t, w)

	rec := get(t, h, "/projects/p1/usage")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<h1>Usage</h1>`,
		`x-data="usagePage" data-seed="usage-seed"`,
		`<script id="usage-seed" type="application/json">`,
		`"project_id":"p1"`,
		`"range":"today"`,
		`"top_tasks":[{"task_id":"t-feed"`,
		`"records":[{"id":"req-7"`,
		`x-effect="draw($el)"`,
		`Estimated cost`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("usage page is missing %q", want)
		}
	}
	if got := currentProjectPage(body); got != "/projects/p1/usage" {
		t.Errorf("the rail marks %q as the open page, want the Usage link", got)
	}
	if strings.Join(asked, "; ") != "summary p1 today; records p1" {
		t.Errorf("the page read %q", asked)
	}

	board := get(t, h, "/projects/p1").Body.String()
	if !strings.Contains(board, `href="/projects/p1/usage"`) {
		t.Error("the project's rail does not link to its Usage page")
	}
}

func TestUsagePageWithoutASummaryLeavesItToTheBrowser(t *testing.T) {
	w := board()
	w.usage = fakeUsage{failSummary: true}
	rec := get(t, newTestHandler(t, w), "/projects/p1/usage")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	if !strings.Contains(body, `"summary":null`) || !strings.Contains(body, `"records":[{"id":"req-7"`) {
		t.Errorf("seed should carry no summary and the records:\n%s", body)
	}
}

func TestUsageIsNotLinkedWhenItIsNotServed(t *testing.T) {
	h := newTestHandler(t, board())
	if strings.Contains(get(t, h, "/projects/p1").Body.String(), `/projects/p1/usage`) {
		t.Error("the rail links to a Usage page that is not served")
	}
	if rec := get(t, h, "/projects/p1/usage"); rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "usagePage") {
		t.Error("the Usage page is served without a usage reader")
	}
}

func TestUsagePageOfAMissingProjectIs404(t *testing.T) {
	w := board()
	w.usage = fakeUsage{}
	if rec := get(t, newTestHandler(t, w), "/projects/nope/usage"); rec.Code != http.StatusNotFound {
		t.Errorf("got %d", rec.Code)
	}
}
