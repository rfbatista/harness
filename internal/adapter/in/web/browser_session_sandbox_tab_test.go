package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// The session's Sandbox tab (UX 7): last in the row and quiet, the session's
// sandbox facts above the denied access the sandbox recorded for it.

type sandboxViolationDTO struct {
	When      string `json:"when"`
	Operation string `json:"operation"`
	Path      string `json:"path,omitempty"`
	Process   string `json:"process"`
}

// sandboxServer serves the task page with s1 (done, so no terminal) and
// answers its violations route with log.
func sandboxServer(t *testing.T, facts *domain.SandboxFacts, log http.Handler) *httptest.Server {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	s1 := &domain.Session{ID: "s1", ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: "sandboxed one",
		Status: domain.SessionDone, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: time.Now(),
		Mode: domain.SessionModeImplement, Sandbox: facts}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Pricing page", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Usage:        fakeUsage{},
		Sessions:     fakeSessions{[]*domain.Session{s1}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/sandbox/violations", log)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv
}

func violationsJSON(available bool, v ...sandboxViolationDTO) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if v == nil {
			v = []sandboxViolationDTO{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"available": available, "violations": v})
	})
}

var seatbeltFacts = &domain.SandboxFacts{Sandboxed: true, Visibility: domain.SandboxOwnRepo, Driver: "seatbelt"}

// openSandboxTab opens the task page and clicks the Sandbox tab.
func openSandboxTab(t *testing.T, srv *httptest.Server) (ctxErrs *errorLog, run func(...chromedp.Action)) {
	t.Helper()
	ctx, errs := browser(t)
	run = func(actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("%v\nJS errors: %v", err, errs.all())
		}
	}
	run(
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1?session=s1"),
		chromedp.Poll(`!document.querySelector('[data-ssr]') && !!document.querySelector('.pane-tabs [role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(`.pane-tabs [role=tab]:last-child`),
	)
	return errs, run
}

const (
	sbPanel  = `document.querySelector('.session [aria-label="Sandbox"]')`
	sbText   = `(` + sbPanel + `?.textContent ?? '').replace(/\s+/g, ' ')`
	sbRows   = `[...document.querySelectorAll('.session [aria-label="Sandbox"] [data-violation]')]`
	sbRowTxt = sbRows + `.map(r => [...r.children].map(c => c.textContent.trim()).filter(Boolean).join(' '))`
)

func poll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(8*time.Second))
}

var threeKinds = []sandboxViolationDTO{
	{When: "2026-10-23T21:41:07Z", Operation: "file-read-data", Path: "/Users/me/.ssh/id_ed25519", Process: "git"},
	{When: "2026-10-23T21:30:00Z", Operation: "file-write-create", Path: "/Users/me/.cache/pnpm/store/tmp", Process: "pnpm"},
	{When: "2026-10-23T21:25:00Z", Operation: "file-write-create", Path: "/Users/me/.cache/pnpm/store/tmp", Process: "pnpm"},
	{When: "2026-10-23T21:22:00Z", Operation: "file-write-create", Path: "/Users/me/.cache/pnpm/store/tmp", Process: "pnpm"},
	{When: "2026-10-23T21:21:00Z", Operation: "file-read-data", Path: "/Users/me/.ssh/id_ed25519", Process: "git"},
	{When: "2026-10-23T21:20:00Z", Operation: "file-read-data", Path: "/Users/me/.ssh/id_ed25519", Process: "git"},
	{When: "2026-10-23T21:19:30Z", Operation: "network-outbound", Path: "*:443", Process: "node"},
}

func TestTheSandboxTabIsLastQuietAndListsGroupedDenials(t *testing.T) {
	srv := sandboxServer(t, seatbeltFacts, violationsJSON(true, threeKinds...))
	errs, run := openSandboxTab(t, srv)

	var lastTab, tabBadges string
	var facts, summary string
	run(
		poll(sbPanel+` !== null && `+sbRows+`.length > 0`),
		chromedp.Evaluate(`(() => { const t = [...document.querySelectorAll('.pane-tabs [role=tab]')]; return t.at(-1).textContent.trim() + '|' + t.at(-1).getAttribute('aria-selected') })()`, &lastTab),
		chromedp.Evaluate(`document.querySelector('.pane-tabs [role=tab]:last-child').querySelectorAll('.badge, .status, [data-count]').length + ''`, &tabBadges),
		chromedp.Evaluate(`(document.querySelector('[data-sandbox-facts]')?.textContent ?? '').replace(/\s+/g, ' ')`, &facts),
		chromedp.Evaluate(`(document.querySelector('[data-violation-summary]')?.textContent ?? '').replace(/\s+/g, ' ').trim()`, &summary),
	)
	if lastTab != "Sandbox|true" {
		t.Errorf("last tab = %q, want the selected Sandbox tab", lastTab)
	}
	if tabBadges != "0" {
		t.Errorf("the Sandbox tab carries %s badges, want none", tabBadges)
	}
	for _, want := range []string{"Ran sandboxed", "Seatbelt (sandbox-exec)", "this worktree", "read and write"} {
		if !strings.Contains(facts, want) {
			t.Errorf("facts %q lack %q", facts, want)
		}
	}
	if summary != "· 7 denied, 3 different paths" {
		t.Errorf("summary = %q", summary)
	}

	var rows []string
	run(chromedp.Evaluate(sbRowTxt, &rows))
	if len(rows) != 3 {
		t.Fatalf("grouped rows = %q, want 3 (repeats grouped)", rows)
	}
	for i, want := range []string{"read /Users/me/.ssh/id_ed25519 git ×3", "write /Users/me/.cache/pnpm/store/tmp pnpm ×3", "other *:443 node"} {
		if !strings.Contains(rows[i], want) {
			t.Errorf("row %d = %q, want %q", i, rows[i], want)
		}
	}

	// Group repeats off: every denial has its own row.
	run(chromedp.Click(`[data-group-repeats]`), poll(sbRows+`.length === 7`))

	// Filter by operation.
	run(chromedp.Click(`[data-operation="write"]`), poll(sbRows+`.length === 3 && `+sbRows+`.every(r => r.textContent.includes('pnpm'))`))
	run(chromedp.Click(`[data-operation="all"]`), poll(sbRows+`.length === 7`))
	run(chromedp.Click(`[data-group-repeats]`), poll(sbRows+`.length === 3`))

	// Filter by path or process, case-insensitively.
	run(chromedp.SendKeys(`[aria-label="Sandbox"] input[type=search]`, "PNPM"), poll(sbRows+`.length === 1`))
	run(chromedp.SendKeys(`[aria-label="Sandbox"] input[type=search]`, "zzz"), poll(`!!document.querySelector('[data-no-match]') && `+sbRows+`.length === 0`))
	var noMatch string
	run(chromedp.Evaluate(`document.querySelector('[data-no-match]').textContent.replace(/\s+/g, ' ').trim()`, &noMatch))
	if noMatch != "No denied access matches this filter. Clear filters." {
		t.Errorf("filtered-to-nothing = %q", noMatch)
	}
	run(chromedp.Click(`[data-no-match] button`), poll(sbRows+`.length === 3`))

	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

func TestAllowPathOpensTheAgentIsolationSettingsWithThePathFilledIn(t *testing.T) {
	srv := sandboxServer(t, seatbeltFacts, violationsJSON(true, threeKinds...))
	_, run := openSandboxTab(t, srv)

	var href, sshAllow string
	var detail string
	run(
		poll(sbRows+`.length === 3`),
		// The pnpm cache is a path worth allowing.
		chromedp.Evaluate(sbRows+`.find(r => r.textContent.includes('pnpm')).click()`, nil),
		poll(`!!document.querySelector('[data-violation-detail] a[data-allow-path]')`),
		chromedp.Evaluate(`document.querySelector('[data-violation-detail] a[data-allow-path]').getAttribute('href')`, &href),
		chromedp.Evaluate(`document.querySelector('[data-violation-detail]').textContent.replace(/\s+/g, ' ')`, &detail),
		// Credentials are never offered for allowing.
		chromedp.Evaluate(sbRows+`.find(r => r.textContent.includes('.ssh')).click()`, nil),
		poll(`document.querySelector('[data-violation-detail]')?.textContent.includes('id_ed25519')`),
		chromedp.Evaluate(`String(!!document.querySelector('[data-violation-detail] a[data-allow-path]'))`, &sshAllow),
	)
	if want := "/agent?allow_path=%2FUsers%2Fme%2F.cache%2Fpnpm%2Fstore%2Ftmp#sandbox"; href != want {
		t.Errorf("Allow path href = %q, want %q", href, want)
	}
	if !strings.Contains(detail, "Applies to new sessions") {
		t.Errorf("detail %q does not say it applies to new sessions", detail)
	}
	if sshAllow != "false" {
		t.Error("an SSH key path is offered for allowing")
	}
}

func TestTheSandboxTabStates(t *testing.T) {
	var calls atomic.Int32
	failFirst := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "boom", "code": "SANDBOX_LOG_UNAVAILABLE"})
			return
		}
		violationsJSON(true, threeKinds[0]).ServeHTTP(w, r)
	})
	release := make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		violationsJSON(true).ServeHTTP(w, r)
	})

	for _, tc := range []struct {
		name  string
		facts *domain.SandboxFacts
		log   http.Handler
		want  string
		wait  string // extra JS that must hold once want is shown
	}{
		{"none denied", seatbeltFacts, violationsJSON(true), "Nothing was denied. The sandbox did not refuse a single read or write in this session.", `!document.querySelector('.session [aria-label="Sandbox"] input[type=search]')`},
		{"not available", &domain.SandboxFacts{Sandboxed: true, Visibility: domain.SandboxOwnRepo, Driver: "bubblewrap"}, violationsJSON(false), "Violation collection is not available on this platform. The sandbox still applies.", sbRows + `.length === 0`},
		{"ran without the sandbox", &domain.SandboxFacts{Visibility: domain.SandboxUnrestricted}, violationsJSON(true), "This session ran without the sandbox. Nothing was recorded.", sbRows + `.length === 0`},
		{"before the sandbox", nil, violationsJSON(true), "This session started before the sandbox existed. Nothing was recorded.", sbRows + `.length === 0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := sandboxServer(t, tc.facts, tc.log)
			_, run := openSandboxTab(t, srv)
			run(poll(sbText + `.includes(` + strconv.Quote(tc.want) + `)`))
			run(poll(tc.wait))
		})
	}

	t.Run("loading and the facts do not wait on the log", func(t *testing.T) {
		srv := sandboxServer(t, seatbeltFacts, slow)
		_, run := openSandboxTab(t, srv)
		var busy string
		run(
			poll(`!!document.querySelector('.session [aria-label="Sandbox"] [aria-busy=true]')`),
			poll(`document.querySelector('[data-sandbox-facts]')?.textContent.includes('Ran sandboxed')`),
			chromedp.Evaluate(`String(document.querySelectorAll('.session [aria-label="Sandbox"] [aria-busy=true] [data-skeleton]').length)`, &busy),
		)
		if busy != "3" {
			t.Errorf("%s skeleton rows, want 3", busy)
		}
		close(release)
		run(poll(sbText + `.includes('Nothing was denied.')`))
	})

	t.Run("a failed load says the session itself is fine and retries", func(t *testing.T) {
		srv := sandboxServer(t, seatbeltFacts, failFirst)
		_, run := openSandboxTab(t, srv)
		run(
			poll(sbText+`.includes('Could not load the sandbox log.')`),
			poll(sbText+`.includes('SANDBOX_LOG_UNAVAILABLE') && `+sbText+`.includes("The session's own data is fine")`),
			chromedp.Click(`.session [aria-label="Sandbox"] [role=alert] button`),
			poll(sbRows+`.length === 1`),
		)
	})
}

func TestTheSandboxChipsOpenTheSandboxTab(t *testing.T) {
	srv := sandboxServer(t, seatbeltFacts, violationsJSON(true))
	ctx, errs := browser(t)
	var tab string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1?session=s1"),
		poll(`!document.querySelector('[data-ssr]') && !!document.querySelector('[data-sandbox-chips] button')`),
		chromedp.Evaluate(`document.querySelector('[data-sandbox-chips] button[data-sandbox]').title`, &tab),
		chromedp.Click(`[data-sandbox-chips] button[data-sandbox-visibility]`),
		poll(`document.querySelector('.pane-tabs [role=tab]:last-child').getAttribute('aria-selected') === 'true'`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if tab != "Open the Sandbox tab" {
		t.Errorf("chip title = %q", tab)
	}
}
