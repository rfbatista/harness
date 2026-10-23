package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// A session's head shows the sandbox it started under as two quiet badges,
// amber only for a session that started with the sandbox off; a session from
// before the sandbox shows none. The list marks only the unsandboxed row.
func TestTheSessionHeadShowsItsSandboxFacts(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	now := time.Now()
	session := func(id, task string, at time.Time, mode domain.SessionMode, facts *domain.SandboxFacts) *domain.Session {
		return &domain.Session{ID: id, ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: task,
			Status: domain.SessionDone, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: at,
			Mode: mode, Sandbox: facts}
	}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Pricing page", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Usage:        fakeUsage{},
		Sessions: fakeSessions{[]*domain.Session{
			session("s1", "sandboxed one", now, domain.SessionModeImplement, &domain.SandboxFacts{Sandboxed: true, Visibility: domain.SandboxOwnRepo, Driver: "seatbelt"}),
			session("s2", "architect one", now.Add(-time.Minute), domain.SessionModeArchitect, &domain.SandboxFacts{Sandboxed: true, Visibility: domain.SandboxAllReposReadOnly, Driver: "seatbelt"}),
			session("s3", "sandbox off", now.Add(-2*time.Minute), domain.SessionModeImplement, &domain.SandboxFacts{Visibility: domain.SandboxUnrestricted}),
			session("s4", "older session", now.Add(-3*time.Minute), "", nil),
		}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	head := `document.querySelector('.session-head')`
	chips := func(sel string) string {
		return `[...` + head + `.querySelectorAll('` + sel + `')].filter(e => e.checkVisibility()).map(e => e.textContent.trim() + (e.dataset.tone ? '[' + e.dataset.tone + ']' : '')).join(' | ')`
	}
	row := func(id string) string {
		return `[...document.querySelectorAll('[role=option][data-session-id="` + id + `"]')].find(r => !r.closest('[data-ssr]'))`
	}
	open := func(id, title string) chromedp.Tasks {
		return chromedp.Tasks{
			chromedp.Evaluate(row(id)+`.click()`, nil),
			chromedp.Poll(head+`?.querySelector('h2')?.textContent === '`+title+`'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		}
	}
	var onChips, archChips, offChips, oldChips string
	var rowMeta = map[string]string{}
	var unsandboxedRows string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1?session=s1"),
		chromedp.Poll(head+`?.querySelector('h2')?.textContent === 'sandboxed one' && `+chips("[data-sandbox]")+` !== ''`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(chips("[data-sandbox], [data-sandbox-visibility]"), &onChips),
		chromedp.Evaluate(`[...document.querySelectorAll('[role=option][data-unsandboxed]')].filter(r => !r.closest('[data-ssr]')).map(r => r.dataset.sessionId).join(',')`, &unsandboxedRows),
		chromedp.Evaluate(`Object.fromEntries(['s1','s3','s4'].map(id => [id, [...document.querySelectorAll('[role=option][data-session-id="' + id + '"]')].find(r => !r.closest('[data-ssr]'))?.querySelector('.meta')?.textContent ?? '']))`, &rowMeta),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if onChips != "Sandboxed | Own worktree only" {
		t.Errorf("sandboxed chips = %q, want neutral Sandboxed and Own worktree only", onChips)
	}
	if unsandboxedRows != "s3" {
		t.Errorf("unsandboxed rows = %q, want only s3", unsandboxedRows)
	}
	for id, wantMarked := range map[string]bool{"s1": false, "s3": true, "s4": false} {
		if got := strings.Contains(rowMeta[id], "unsandboxed"); got != wantMarked {
			t.Errorf("row %s meta %q: unsandboxed = %v, want %v", id, rowMeta[id], got, wantMarked)
		}
	}

	err = chromedp.Run(ctx,
		open("s2", "architect one"),
		chromedp.Evaluate(chips("[data-sandbox], [data-sandbox-visibility]"), &archChips),
		open("s3", "sandbox off"),
		chromedp.Evaluate(chips("[data-sandbox], [data-sandbox-visibility]"), &offChips),
		open("s4", "older session"),
		chromedp.Evaluate(chips("[data-sandbox], [data-sandbox-visibility]"), &oldChips),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if archChips != "Sandboxed | Own worktree + all repos read-only" {
		t.Errorf("architect chips = %q", archChips)
	}
	if offChips != "Not sandboxed[attention] | No repository limits[attention]" {
		t.Errorf("sandbox-off chips = %q, want both amber", offChips)
	}
	if oldChips != "" {
		t.Errorf("a session from before the sandbox shows %q", oldChips)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
