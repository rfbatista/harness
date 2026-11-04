//go:build !windows

package app

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var (
	benchRun     = flag.Bool("pagebench", false, "run the page benchmark: every project page, its median and worst page server time")
	benchURL     = flag.String("pagebench.url", "", "the server to benchmark, such as http://127.0.0.1:8080; empty runs the seeded four-times project")
	benchProject = flag.String("pagebench.project", "harness", "the project to benchmark on a server, by name or id")
	benchBudgets = flag.String("pagebench.budgets", "", "a budgets file: the benchmark fails when a page is over its budget")
	benchRuns    = flag.Int("pagebench.runs", 7, "how many times each page is requested")
)

// The page benchmark: go test ./internal/app -run TestPageBenchmark -pagebench
// [-pagebench.url=http://127.0.0.1:8080 -pagebench.project=harness]
// It prints a table, one row per project page. Without -pagebench.url it runs
// on the seeded four-times project, in process. See "Page time" in the README.
func TestPageBenchmark(t *testing.T) {
	if !*benchRun {
		t.Skip("the page benchmark runs only with -pagebench")
	}
	rows := benchmarkRows(t)
	t.Logf("\n%s", table(rows))
	if *benchBudgets == "" {
		return
	}
	budgets, err := readPageBudgets(*benchBudgets)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range budgets.overBudget(rows) {
		t.Error(line)
	}
}

// benchmarkRows runs the benchmark where the flags say.
func benchmarkRows(t *testing.T) []benchRow {
	t.Helper()
	if *benchURL != "" {
		bench := newPageBench(*benchURL, "", *benchRuns)
		id, err := bench.projectID(*benchProject)
		if err != nil {
			t.Fatal(err)
		}
		bench.project = id
		rows, err := bench.run()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	b := newFourTimesProject(t)
	srv := httptest.NewServer(b.handler)
	t.Cleanup(srv.Close)
	rows, err := newPageBench(srv.URL, b.project, *benchRuns).run()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}


// The benchmark prints a row per project page, each with its breakdown.
func TestThePageBenchmarkMeasuresEveryProjectPage(t *testing.T) {
	b := newBudgetProject(t)
	b.addSessions(6)
	srv := httptest.NewServer(b.handler)
	defer srv.Close()

	rows, err := newPageBench(srv.URL, b.project, 3).run()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.page)
		if !r.breakdown || r.median <= 0 || r.worst < r.median {
			t.Errorf("%s: %+v", r.page, r)
		}
	}
	if got := strings.Join(names, ","); got != "board,task page,settings,documents,code,design,design system,usage,reviews" {
		t.Errorf("pages measured: %s", got)
	}
	if out := table(rows); !strings.Contains(out, "git (calls)") || !strings.Contains(out, "task page") {
		t.Errorf("table:\n%s", out)
	}
}

// A server older than this feature answers without Server-Timing: the page
// is timed by the client and shown with no breakdown.
func TestThePageBenchmarkFallsBackToTheTimeToFirstByte(t *testing.T) {
	b := newBudgetProject(t)
	b.addSessions(6)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.handler.ServeHTTP(&noTiming{ResponseWriter: w}, r)
	}))
	defer srv.Close()

	rows, err := newPageBench(srv.URL, b.project, 3).run()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.breakdown || r.median <= 0 {
			t.Errorf("%s: %+v", r.page, r)
		}
	}
	if out := table(rows); !strings.Contains(out, "no breakdown") {
		t.Errorf("table:\n%s", out)
	}
}

// noTiming drops Server-Timing, as an older server would not send it.
type noTiming struct{ http.ResponseWriter }

func (n *noTiming) WriteHeader(code int) {
	n.Header().Del("Server-Timing")
	n.ResponseWriter.WriteHeader(code)
}

// A page that does not answer 200 stops the benchmark and names the page.
func TestThePageBenchmarkFailsWhenAPageDoesNotAnswer(t *testing.T) {
	b := newBudgetProject(t)
	b.addSessions(6)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/usage") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		b.handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	_, err := newPageBench(srv.URL, b.project, 3).run()
	if err == nil || !strings.Contains(err.Error(), "usage") || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v", err)
	}
}
