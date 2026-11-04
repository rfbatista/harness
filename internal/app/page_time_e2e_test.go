//go:build !windows

package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every project page says how long it took to build: its page server time
// and where it went, in the Server-Timing header and in one log line.
func TestEveryProjectPageReportsItsPageServerTime(t *testing.T) {
	b := newBudgetProject(t)
	b.addSessions(6)
	base := "/projects/" + b.project
	pages := map[string]string{
		"board":     base,
		"task page": base + "/tasks/" + b.tickets[0],
		"settings":  base + "/settings",
		"documents": base + "/documents",
		"code":      base + "/repositories/" + b.repo + "/code",
		"design":    base + "/design",
		"usage":     base + "/usage",
		"reviews":   base + "/reviews",
	}
	for name, path := range pages {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(prev)

			rec := httptest.NewRecorder()
			b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil).WithContext(context.Background()))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: %d", path, rec.Code)
			}
			m := parseServerTiming(t, rec.Header().Get("Server-Timing"))
			for _, k := range []string{"total", "db", "git", "render", "frame", "catalog"} {
				if _, ok := m[k]; !ok {
					t.Fatalf("Server-Timing %q has no %s", rec.Header().Get("Server-Timing"), k)
				}
			}
			if m["render"] > m["total"] || m["frame"] > m["total"] {
				t.Errorf("render %v and frame %v must each be within total %v", m["render"], m["frame"], m["total"])
			}
			line := logs.String()
			for _, want := range []string{"page time", "route=", "project=" + b.project, "total=", "db=", "git=", "git_calls=", "render=", "frame=", "catalog=", "catalog_reads="} {
				if !strings.Contains(line, want) {
					t.Errorf("log line lacks %q:\n%s", want, line)
				}
			}
		})
	}
}

// parseServerTiming reads "name;dur=1.5, name;desc=..;dur=2" into durations.
func parseServerTiming(t *testing.T, header string) map[string]time.Duration {
	t.Helper()
	out := map[string]time.Duration{}
	for _, metric := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(metric), ";")
		name := parts[0]
		for _, p := range parts[1:] {
			if v, ok := strings.CutPrefix(p, "dur="); ok {
				ms, err := strconv.ParseFloat(v, 64)
				if err != nil {
					t.Fatalf("Server-Timing %q: %v", header, err)
				}
				out[name] = time.Duration(ms * float64(time.Millisecond))
			}
		}
	}
	return out
}

// The task page reads a worktree's git state and the database: its db time is
// above zero, and its git call count is the number of git processes it ran.
func TestAPageCountsTheDatabaseTimeAndTheGitProcessesItRan(t *testing.T) {
	real, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "runs")
	script := "#!/bin/sh\necho run >> '" + log + "'\nexec '" + filepath.Join(strings.TrimSpace(string(real)), "git") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := newBudgetProject(t, filepath.Join(dir, "git"))
	b.addSessions(6)
	path := "/projects/" + b.project + "/tasks/" + b.tickets[0]

	runsSoFar := func() int {
		data, _ := os.ReadFile(log)
		return strings.Count(string(data), "run\n")
	}
	before := runsSoFar()
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	runs := runsSoFar() - before
	header := rec.Header().Get("Server-Timing")
	m := parseServerTiming(t, header)
	if m["db"] <= 0 {
		t.Errorf("db is %v: %s", m["db"], header)
	}
	if runs == 0 {
		t.Fatal("the task page ran no git: the wrapper git was not used")
	}
	if !strings.Contains(header, fmt.Sprintf("git;desc=\"%d calls\"", runs)) {
		t.Errorf("the page ran git %d times: %s", runs, header)
	}
	if m["git"] <= 0 {
		t.Errorf("git is %v: %s", m["git"], header)
	}
}

// A project page that fails still reports its time.
func TestAFailedProjectPageStillReportsItsPageServerTime(t *testing.T) {
	b := newBudgetProject(t)
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/"+b.project+"/tasks/no-such-task", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	if m := parseServerTiming(t, rec.Header().Get("Server-Timing")); m["total"] <= 0 {
		t.Errorf("Server-Timing %q", rec.Header().Get("Server-Timing"))
	}
}

// The agent catalog is read from disk on every request that resolves an
// agent: a page says how long that took and how many reads it made.
func TestAPageCountsTheAgentCatalogReadsItMade(t *testing.T) {
	b := newBudgetProject(t)
	b.agents = seedAgentCatalog(t, b.dir)
	b.addSessions(6)
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/"+b.project+"/settings", nil))
	header := rec.Header().Get("Server-Timing")
	m := parseServerTiming(t, header)
	if m["catalog"] <= 0 || !strings.Contains(header, "catalog;desc=\"") || strings.Contains(header, "catalog;desc=\"0 reads\"") {
		t.Errorf("Server-Timing %q", header)
	}
	if m["catalog"] > m["total"] {
		t.Errorf("catalog %v is over total %v: %q", m["catalog"], m["total"], header)
	}
	// A page of a project with no agent reads none.
	plain := newBudgetProject(t)
	plain.addSessions(6)
	rec = httptest.NewRecorder()
	plain.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/"+plain.project+"/settings", nil))
	if h := rec.Header().Get("Server-Timing"); !strings.Contains(h, "catalog;desc=\"0 reads\"") {
		t.Errorf("Server-Timing %q", h)
	}
}
