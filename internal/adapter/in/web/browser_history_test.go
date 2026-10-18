package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/ports"
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
	srv := httptest.NewServer(withStream(mux))
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
	srv := httptest.NewServer(withStream(mux))
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

// TestHistoryCommitSwapInTheBrowser: on a repository's history, opening a
// commit swaps its detail in place (the page is not reloaded): the address
// bar and the list's highlight follow, the swapped diff goes live from the
// seed it carries, Back returns to the previous commit, and a swap that
// fails loads the page.
func TestHistoryCommitSwapInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, func(w *codeWorld, d *Deps) {
		d.History = gitHistory{dirs: map[string]string{w.at.RepositoryID: w.f.RepoDir}}
	})
	pages, fail := failingSwaps(w.srv.Config.Handler)
	w.srv.Config.Handler = pages
	ctx, errs := browser(t)
	base := "/projects/" + w.f.ProjectID + "/repositories/" + w.repoID + "/history"
	row := func(hash string) string {
		return `document.querySelector('nav[aria-label=Commits] a.commit[href*="/history/` + hash + `"]')`
	}
	liveDiff := `document.querySelectorAll('section[aria-label=Diff] [data-ssr]').length === 0 && document.querySelectorAll('section[aria-label=Diff] .code-lines .line').length > 0`

	var first, opened, current, back, files string
	var swapped, reloaded bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.srv.URL+base),
		chromedp.Poll(liveDiff, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('article[aria-label=Commit] h2').textContent`, &first),
		markWindow(),
		chromedp.Evaluate(row(w.f.Rename)+`.click()`, nil),
		chromedp.Poll(`location.pathname.endsWith('/history/`+w.f.Rename+`')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`document.querySelector('article[aria-label=Commit] .code')?.textContent === '`+w.f.Rename+`' && `+liveDiff, nil, chromedp.WithPollingTimeout(10*time.Second)),
		windowMarked(&swapped),
		chromedp.Evaluate(`document.querySelector('article[aria-label=Commit] h2').textContent`, &opened),
		chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Diff] section.file h3.path')].map(h => h.title).join(',')`, &files),
		chromedp.Poll(`document.querySelector('nav[aria-label=Commits] [aria-current=page]')?.getAttribute('href').includes('/history/`+w.f.Rename+`')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('nav[aria-label=Commits] [aria-current=page]').length + ' ' + document.querySelector('nav[aria-label=Commits] [aria-current=page]').getAttribute('href')`, &current),

		chromedp.Evaluate(`history.back()`, nil),
		pollAcrossLoads(`document.readyState === 'complete' && location.pathname === '`+base+`' && !!document.querySelector('article[aria-label=Commit] h2')`),
		chromedp.Evaluate(`document.querySelector('article[aria-label=Commit] h2').textContent`, &back),

		markWindow(),
		chromedp.ActionFunc(func(context.Context) error { fail.Store(true); return nil }),
		chromedp.Evaluate(row(w.f.Rename)+`.click()`, nil),
		pollAcrossLoads(`document.readyState === 'complete' && location.pathname.endsWith('/history/`+w.f.Rename+`') && !!document.querySelector('article[aria-label=Commit]')`),
		windowMarked(&reloaded),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !swapped {
		t.Error("opening a commit reloaded the page")
	}
	if opened == first || files != "docs/new.md,internal/app/app.go" {
		t.Errorf("the swapped detail is %q with files %q, want the renaming commit's", opened, files)
	}
	if !strings.HasPrefix(current, "1 ") || !strings.Contains(current, "/history/"+w.f.Rename) {
		t.Errorf("the highlighted row is %q, want the opened commit", current)
	}
	if back != first {
		t.Errorf("Back shows %q, want %q", back, first)
	}
	if reloaded {
		t.Error("a failing swap stayed in place instead of loading the page")
	}
}

// TestSessionHistorySwapInTheBrowser: on a session's history, opening a
// commit and then the uncommitted changes swaps the detail in place while
// the pull request panel keeps its state (its form stays open); the address
// bar follows, Back returns to the commit, and a swap that fails loads the
// page.
func TestSessionHistorySwapInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	w := historyWorld(t)
	w.prs = fakePullRequests{views: map[string]*ports.PullRequestView{"ws-agent": {Draft: agentDraft()}}}
	agent := agentCommit(t, w)
	mux := http.NewServeMux()
	mux.Handle("/api/", &fakePullRequestAPI{})
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, History: w.history,
		PullRequests: w.prs, Now: func() time.Time { return now },
	}, assets, nil))
	pages, fail := failingSwaps(mux)
	srv := httptest.NewServer(withStream(pages))
	defer func() { srv.CloseClientConnections(); srv.Close() }() // the feed's stream stays open

	formOpen := `(() => { const el = document.getElementById('pr-form'); return !!el && getComputedStyle(el).display !== 'none' })()`
	heading := `document.querySelector('article[id=commit-detail] h2')?.textContent`
	var commit, changes, back string
	var swapped, keptForm, reloaded bool
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+sessionHistory),
		chromedp.Poll(`!!document.querySelector('main header [x-on\\:click="openForm"]') && getComputedStyle(document.querySelector('main header [x-on\\:click="openForm"]')).display !== 'none'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('main header [x-on\\:click="openForm"]').click()`, nil),
		chromedp.Poll(formOpen, nil, chromedp.WithPollingTimeout(5*time.Second)),
		markWindow(),
		chromedp.Evaluate(`document.querySelector('nav[aria-label=Commits] a.commit[href*="/history/`+agent+`"]').click()`, nil),
		chromedp.Poll(`location.pathname.endsWith('/history/`+agent+`') && `+heading+` === 'Add the SSE feed'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(heading, &commit),
		chromedp.Evaluate(`document.querySelector('nav[aria-label=Commits] a[data-working]').click()`, nil),
		chromedp.Poll(`location.pathname.endsWith('/history/working') && `+heading+` === 'Uncommitted changes'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(heading, &changes),
		windowMarked(&swapped),
		chromedp.Evaluate(formOpen, &keptForm),

		chromedp.Evaluate(`history.back()`, nil),
		pollAcrossLoads(`document.readyState === 'complete' && location.pathname.endsWith('/history/`+agent+`') && !!document.querySelector('article[id=commit-detail] h2')`),
		chromedp.Evaluate(heading, &back),

		markWindow(),
		chromedp.ActionFunc(func(context.Context) error { fail.Store(true); return nil }),
		chromedp.Evaluate(`document.querySelector('nav[aria-label=Commits] a[data-working]').click()`, nil),
		pollAcrossLoads(`document.readyState === 'complete' && location.pathname.endsWith('/history/working') && !!document.querySelector('article[aria-label="Uncommitted changes"]')`),
		windowMarked(&reloaded),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if commit != "Add the SSE feed" || changes != "Uncommitted changes" {
		t.Errorf("swapped in %q, then %q", commit, changes)
	}
	if !swapped || !keptForm {
		t.Errorf("the page was kept %v, the pull request form stayed open %v", swapped, keptForm)
	}
	if back != "Add the SSE feed" {
		t.Errorf("Back shows %q, want the commit", back)
	}
	if reloaded {
		t.Error("a failing swap stayed in place instead of loading the page")
	}
}

// TestHistoryShowMoreInTheBrowser: on both history pages, "Show more"
// appends the next commits below the ones shown without reloading: the
// list keeps its scroll and the open commit stays open, the address bar
// carries the new limit, Back returns to the shorter list, and a request
// that fails loads the page.
func TestHistoryShowMoreInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	w := historyWorld(t)
	pages, fail := failingSwaps(NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, History: w.history,
		Now: func() time.Time { return now },
	}, assets, nil))
	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := httptest.NewServer(withStream(mux))
	defer srv.Close()

	rows := `document.querySelectorAll('nav[aria-label=Commits] a.commit:not([data-working])').length`
	list := `document.querySelector('nav[aria-label=Commits]')`
	heading := `document.querySelector('#commit-detail h2').textContent`
	for _, base := range []string{"/projects/p1/repositories/r1/history", sessionHistory} {
		fail.Store(false)
		var before, after, shorter, failed int
		var scrolled, kept float64
		var openBefore, openAfter, search string
		var swapped, reloaded bool
		err := chromedp.Run(ctx,
			chromedp.EmulateViewport(1280, 200), // short, so the list scrolls
			chromedp.Navigate(srv.URL+base+"?limit=2"),
			pageSettled(),
			chromedp.Evaluate(rows, &before),
			chromedp.Evaluate(heading, &openBefore),
			chromedp.Evaluate(`(l => { l.scrollTop = l.scrollHeight; return l.scrollTop })(`+list+`)`, &scrolled),
			markWindow(),
			chromedp.Evaluate(`document.querySelector('#history-more a').click()`, nil),
			chromedp.Poll(rows+` === 5 && location.search === '?limit=202'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(rows, &after),
			chromedp.Evaluate(list+`.scrollTop`, &kept),
			chromedp.Evaluate(heading, &openAfter),
			chromedp.Evaluate(`location.search`, &search),
			windowMarked(&swapped),

			chromedp.Evaluate(`history.back()`, nil),
			pollAcrossLoads(`document.readyState === 'complete' && location.search === '?limit=2' && !!document.querySelector('#history-more')`),
			chromedp.Evaluate(rows, &shorter),

			markWindow(),
			chromedp.ActionFunc(func(context.Context) error { fail.Store(true); return nil }),
			chromedp.Evaluate(`document.querySelector('#history-more a').click()`, nil),
			pollAcrossLoads(`document.readyState === 'complete' && location.search === '?limit=202' && `+rows+` === 5`),
			chromedp.Evaluate(rows, &failed),
			windowMarked(&reloaded),
		)
		if err != nil {
			t.Fatalf("%s: %v\nJS errors: %v", base, err, errs.all())
		}
		if before != 2 || after != 5 || !swapped {
			t.Errorf("%s: %d rows, then %d; the page was kept %v", base, before, after, swapped)
		}
		if scrolled == 0 || kept != scrolled {
			t.Errorf("%s: the list was scrolled to %v, and is at %v after Show more", base, scrolled, kept)
		}
		if openAfter != openBefore || search != "?limit=202" {
			t.Errorf("%s: open %q then %q; address %q", base, openBefore, openAfter, search)
		}
		if shorter != 2 {
			t.Errorf("%s: Back shows %d rows, want the shorter list", base, shorter)
		}
		if reloaded || failed != 5 {
			t.Errorf("%s: a failing Show more stayed in place (%v) or loaded %d rows", base, reloaded, failed)
		}
	}
}
