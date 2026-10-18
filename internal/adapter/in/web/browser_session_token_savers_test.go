package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// A session's head shows the token savers it runs with beside its usage,
// and names a saver that was on for it but not installed, so it ran
// without it. A session with neither shows neither.
func TestTheSessionHeadShowsItsTokenSavers(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	now := time.Now()
	session := func(id, task string, at time.Time, active, missing []domain.TokenSaver) *domain.Session {
		return &domain.Session{ID: id, ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: task,
			Status: domain.SessionDone, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: at,
			TokenSavers: active, TokenSaversNotInstalled: missing}
	}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Pricing page", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Usage:        fakeUsage{},
		Sessions: fakeSessions{[]*domain.Session{
			session("s1", "with rtk", now, []domain.TokenSaver{domain.TokenSaverRTK}, []domain.TokenSaver{domain.TokenSaverCodegraph}),
			session("s2", "without savers", now.Add(-time.Hour), nil, nil),
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
	text := func(sel string) string {
		return `[...` + head + `.querySelectorAll('` + sel + `')].filter(e => e.checkVisibility()).map(e => e.textContent.trim()).join(' | ')`
	}
	var savers, notice, usage, noticeTitle, otherSavers, otherNotice string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1?session=s1"),
		chromedp.Poll(head+`?.querySelector('h2')?.textContent === 'with rtk' && `+text(".usage-total")+` !== ''`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(text(".token-savers"), &savers),
		chromedp.Evaluate(text("[data-token-savers-missing]"), &notice),
		chromedp.Evaluate(``+head+`.querySelector('[data-token-savers-missing]')?.title ?? ''`, &noticeTitle),
		chromedp.Evaluate(text(".usage-total"), &usage),
		chromedp.Evaluate(`[...document.querySelectorAll('[role=option][data-session-id="s2"]')].find(r => !r.closest('[data-ssr]')).click()`, nil),
		chromedp.Poll(head+`?.querySelector('h2')?.textContent === 'without savers'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(text(".token-savers"), &otherSavers),
		chromedp.Evaluate(text("[data-token-savers-missing]"), &otherNotice),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if savers != "rtk" {
		t.Errorf("active savers = %q, want rtk", savers)
	}
	if usage == "" {
		t.Error("the usage the savers sit beside is not shown")
	}
	if !strings.Contains(notice, "codegraph") || !strings.Contains(notice, "not installed") {
		t.Errorf("not-installed notice = %q, want it to name codegraph", notice)
	}
	if !strings.Contains(noticeTitle, "Token savers page") {
		t.Errorf("notice title = %q, want it to say where to install it", noticeTitle)
	}
	if otherSavers != "" || otherNotice != "" {
		t.Errorf("a session without savers shows %q / %q", otherSavers, otherNotice)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
