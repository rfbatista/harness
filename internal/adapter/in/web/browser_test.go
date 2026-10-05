package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// These tests run real JavaScript in headless Chrome: the browser unit suite
// (web/test) and the Sessions page booting on the embedded bundle. They skip
// when Chrome is missing or under -short.

const repoRoot = "../../../.."

func browser(t *testing.T) (context.Context, *errorLog) {
	t.Helper()
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	ctx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-extensions", true))...)
	ctx, cancelBrowser := chromedp.NewContext(ctx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(func() { cancelTimeout(); cancelBrowser(); cancelAlloc() })

	log := &errorLog{}
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventExceptionThrown:
			log.add(e.ExceptionDetails.Error())
		case *runtime.EventConsoleAPICalled:
			if e.Type == runtime.APITypeError {
				var parts []string
				for _, a := range e.Args {
					parts = append(parts, string(a.Value))
				}
				log.add("console.error: " + strings.Join(parts, " "))
			}
		}
	})
	if err := chromedp.Run(ctx); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) || strings.Contains(err.Error(), "executable file not found") {
			t.Skipf("browser test: Chrome not found (%v)", err)
		}
		t.Fatal(err)
	}
	return ctx, log
}

type errorLog struct {
	mu   sync.Mutex
	errs []string
}

func (l *errorLog) add(s string) { l.mu.Lock(); l.errs = append(l.errs, s); l.mu.Unlock() }

func (l *errorLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.errs...)
}

// clickButton clicks, through the DOM, the first button under scope whose
// text starts with label, waiting for it to exist and be enabled.
func clickButton(scope, label string) chromedp.Action {
	js := fmt.Sprintf(`(() => {
		const b = [...document.querySelectorAll(%q + ' button')].find(b => b.textContent.trim().startsWith(%q) && !b.disabled);
		if (!b) return false;
		b.click();
		return true;
	})()`, scope, label)
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(5*time.Second))
}

// noStore serves files fresh, so Chrome never runs a stale module.
func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func TestBrowserSuite(t *testing.T) {
	ctx, _ := browser(t)
	srv := httptest.NewServer(noStore(http.FileServer(http.Dir(repoRoot))))
	defer srv.Close()

	var results struct {
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
		Failures []struct {
			Test  string `json:"test"`
			Error string `json:"error"`
		} `json:"failures"`
	}
	err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/web/test/index.html"),
		chromedp.Poll(`window.__testResults`, &results, chromedp.WithPollingTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range results.Failures {
		t.Errorf("%s\n%s", f.Test, f.Error)
	}
	if results.Passed == 0 && results.Failed == 0 {
		t.Fatal("the browser suite ran no tests")
	}
	t.Logf("browser suite: %d passed, %d failed", results.Passed, results.Failed)
}

func TestSessionsPageComesAliveInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	recent := time.Now()
	h := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{[]*domain.Agent{{ID: "a1", Name: "Reviewer"}}},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions: fakeSessions{[]*domain.Session{
			// One task, three sessions at once.
			{ID: "s1", ProjectID: "p1", TicketID: "t1", Task: "Add SSE feed", Status: domain.SessionIdle, UpdatedAt: recent},
			{ID: "s2", ProjectID: "p1", TicketID: "t1", Task: "Port tickets", Status: domain.SessionRunning, UpdatedAt: recent.Add(-time.Minute)},
			{ID: "s3", ProjectID: "p1", TicketID: "t1", Task: "Bump harnesskit", Status: domain.SessionFailed, UpdatedAt: recent.Add(-time.Hour)},
		}},
	}, assets, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	var rows, groups, agentOptions []string
	var detail, ssrLeft, formRepo, confirmText string
	var formOpen, formClosed, confirmGone bool
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		// Alpine has taken over once the server-rendered copy is gone.
		chromedp.Poll(`document.querySelector('[data-ssr]') === null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.list [role=group] .row .title')].map(e => e.textContent)`, &rows),
		chromedp.Evaluate(`[...document.querySelectorAll('.list [role=group] > .group > span:first-child')].map(e => e.textContent)`, &groups),
		chromedp.Evaluate(`String(document.querySelectorAll('[data-ssr]').length)`, &ssrLeft),
		chromedp.Click(`//div[contains(@class,'row')][.//span[text()='Port tickets']]`, chromedp.BySearch),
		chromedp.Poll(`document.querySelector('main section h2')?.textContent === 'Port tickets'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('main section h2')?.textContent ?? ''`, &detail),

		// New session: the form opens in the detail pane with the server's choices.
		clickButton(`main header`, "New session"),
		chromedp.Poll(`!!document.querySelector('form[x-data^=sessionsNewSession] textarea')`, &formOpen, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('#new-session-agent option')].map(o => o.textContent)`, &agentOptions),
		chromedp.Evaluate(`document.querySelector('#new-session-repository').value`, &formRepo),
		clickButton(`main form`, "Cancel"),
		chromedp.Poll(`!document.querySelector('form[x-data^=sessionsNewSession]')`, &formClosed, chromedp.WithPollingTimeout(5*time.Second)),

		// Delete asks first, inline.
		clickButton(`main section`, "Delete"),
		chromedp.Poll(`document.querySelector('[role=alertdialog]')?.textContent ?? ''`, &confirmText, chromedp.WithPollingTimeout(5*time.Second)),
		clickButton(`[role=alertdialog]`, "Keep it"),
		chromedp.Poll(`!document.querySelector('[role=alertdialog]')`, &confirmGone, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}

	if got := fmt.Sprint(rows); got != "[Add SSE feed Port tickets Bump harnesskit]" {
		t.Errorf("live rows = %s", got)
	}
	if got := fmt.Sprint(groups); got != "[Needs you Running Earlier]" {
		t.Errorf("groups = %s", got)
	}
	if ssrLeft != "0" || detail != "Port tickets" {
		t.Errorf("ssr left = %s, detail = %q", ssrLeft, detail)
	}
	if got := fmt.Sprint(agentOptions); got != "[Plain claude Reviewer]" || formRepo != "r1" {
		t.Errorf("new-session form: agents %s, repository %q", got, formRepo)
	}
	if !strings.Contains(confirmText, "Delete this session?") {
		t.Errorf("delete confirmation = %q", confirmText)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors on the page:\n%s", strings.Join(e, "\n"))
	}
}
