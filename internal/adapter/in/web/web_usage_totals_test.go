package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// UsageTotals answers a task's or a session's all-time totals: the task has
// a price, session s2 has none.
func (f fakeUsage) UsageTotals(_ context.Context, q ports.UsageTotalsQuery) (*ports.UsageTotals, error) {
	if f.asked != nil {
		*f.asked = append(*f.asked, "totals task="+q.TaskID+" session="+q.SessionID)
	}
	if f.failTotals {
		return nil, errors.New("database is locked")
	}
	cost := 0.42
	switch {
	case q.TaskID != "":
		return &ports.UsageTotals{Requests: 3, InputTokens: 9000, OutputTokens: 1000, TotalTokens: 10000, EstimatedCostUSD: &cost}, nil
	case q.SessionID == "s2":
		return &ports.UsageTotals{Requests: 1, InputTokens: 50, TotalTokens: 50, UnpricedTokens: 50}, nil
	default:
		return &ports.UsageTotals{Requests: 2, InputTokens: 4000, TotalTokens: 4000, EstimatedCostUSD: &cost}, nil
	}
}

// TaskUsageTotals answers the task's and its sessions' totals as UsageTotals
// does, one by one.
func (f fakeUsage) TaskUsageTotals(ctx context.Context, taskID string, sessionIDs []string) (*ports.UsageTotals, map[string]*ports.UsageTotals, error) {
	task, err := f.UsageTotals(ctx, ports.UsageTotalsQuery{TaskID: taskID})
	if err != nil {
		return nil, nil, err
	}
	sessions := make(map[string]*ports.UsageTotals, len(sessionIDs))
	for _, id := range sessionIDs {
		if sessions[id], err = f.UsageTotals(ctx, ports.UsageTotalsQuery{SessionID: id}); err != nil {
			return nil, nil, err
		}
	}
	return task, sessions, nil
}

func TestTaskAndSessionPagesShowTheirUsageTotals(t *testing.T) {
	var asked []string
	w := board()
	w.usage = fakeUsage{asked: &asked}
	rec := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<script id="usage-totals-seed" type="application/json">`,
		`"project_id":"p1"`,
		`"task_id":"t-feed"`,
		`"task":{"requests":3,"input_tokens":9000`,
		`"s1":{"requests":2`,
		`"s2":{"requests":1,"input_tokens":50,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"total_tokens":50,"estimated_cost_usd":null`,
		`x-data="usageTotalsWatch" data-seed="usage-totals-seed"`,
		// the task's totals in its toolbar, the selected session's in its head
		`x-text="taskText"`,
		`x-text="sessionText(selected.id)"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task page is missing %q", want)
		}
	}
	got := strings.Join(asked, "; ")
	for _, want := range []string{"totals task=t-feed session=", "totals task= session=s1", "totals task= session=s2"} {
		if !strings.Contains(got, want) {
			t.Errorf("the page read %q, want it to read %q", got, want)
		}
	}
	if strings.Contains(got, "session=s3") || strings.Contains(got, "session=s9") {
		t.Errorf("the page read another task's sessions: %q", got)
	}
}

func TestTaskPageWithoutAUsageReaderHasNoTotals(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed").Body.String()
	for _, never := range []string{"usage-totals-seed", "usageTotalsWatch", "usageTotalsChip"} {
		if strings.Contains(body, never) {
			t.Errorf("task page without a usage reader contains %q", never)
		}
	}
}

func TestTaskPageWhoseTotalsFailStillRenders(t *testing.T) {
	w := board()
	w.usage = fakeUsage{failTotals: true}
	rec := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "usageTotalsWatch") {
		t.Error("totals that could not be read are not shown")
	}
}
