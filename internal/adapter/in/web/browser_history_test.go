package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestHistoryInTheBrowser opens a repository's history from the
// repositories page, checks the graph's row slices join, and opens a commit.
func TestHistoryInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	w := historyWorld(t)
	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, History: w.history,
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var path, opened, mergeHref string
	var rowHeights []float64
	var shot []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/repositories"),
		chromedp.Poll(`!!document.querySelector('a[href$="/history"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('a[href$="/history"]').click()`, nil),
		waitForPath(srv.URL+"/projects/p1/repositories/r1/history", &path),
		chromedp.Evaluate(`[...document.querySelectorAll('.history .commit')].map(r => [r.getBoundingClientRect().height, r.querySelector('svg').getBoundingClientRect().height]).flat()`, &rowHeights),
		chromedp.Evaluate(`[...document.querySelectorAll('.history .commit')].find(r => r.textContent.includes('Merge the docs')).href`, &mergeHref),
		chromedp.ActionFunc(func(ctx context.Context) error { return chromedp.Navigate(mergeHref).Do(ctx) }),
		chromedp.Poll(`document.querySelector('article[aria-label=Commit] h2')?.textContent === 'Merge the docs'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.history .commit[aria-current=page] .text').textContent`, &opened),
		chromedp.FullScreenshot(&shot, 85),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if len(rowHeights) != 10 {
		t.Fatalf("rows = %v", rowHeights)
	}
	for i := 0; i < len(rowHeights); i += 2 {
		if rowHeights[i] != rowHeights[i+1] {
			t.Errorf("row %d is %vpx high, its graph %vpx: the slices would not join", i/2, rowHeights[i], rowHeights[i+1])
		}
	}
	if opened != "Merge the docs" {
		t.Errorf("highlighted row = %q", opened)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/history.jpg", shot, 0o644)
	}
}

// TestSessionHistoryInTheBrowser follows a session's "Git history" link
// from its task page.
func TestSessionHistoryInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	w := historyWorld(t)
	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, History: w.history,
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var href, path, heading string
	var shot []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(`[...document.querySelectorAll('[role=listbox] .row')].some(r => r.textContent.includes('build the feed'))`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('[role=listbox] .row')].find(r => r.textContent.includes('build the feed')).click()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('a.button')].some(a => a.textContent.trim() === 'Git history')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('a.button')].find(a => a.textContent.trim() === 'Git history').href`, &href),
		chromedp.ActionFunc(func(ctx context.Context) error { return chromedp.Navigate(href).Do(ctx) }),
		waitForPath(srv.URL+"/projects/p1/tasks/t-feed/sessions/s-agent/history", &path),
		chromedp.Evaluate(`document.querySelector('article h2').textContent`, &heading),
		chromedp.FullScreenshot(&shot, 85),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if heading != "Uncommitted changes" {
		t.Errorf("open = %q, want the uncommitted changes", heading)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/session-history.jpg", shot, 0o644)
	}
}
