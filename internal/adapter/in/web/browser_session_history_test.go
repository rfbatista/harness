package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
)

// A session's History tab in the browser, against a fake of
// GET /api/sessions/{id}/transcript that pages as the contract
// "Harness server ↔ Web UI — agent session history" says (T1–T6).

type transcriptEntry struct {
	Cursor    string `json:"cursor"`
	At        string `json:"at"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Tool      string `json:"tool"`
	IsError   bool   `json:"is_error"`
	Truncated bool   `json:"truncated"`
}

// fakeTranscript is one session's saved conversation; nil entries means
// there is none yet (SESSION_TRANSCRIPT_MISSING).
type fakeTranscript struct {
	mu       sync.Mutex
	entries  []transcriptEntry
	requests []transcriptRequest
}

type transcriptRequest struct {
	before, after string
	at            time.Time
}

// numberedEntries are "entry 1".."entry n", alternating a person and the agent.
func numberedEntries(from, n int) []transcriptEntry {
	out := make([]transcriptEntry, n)
	for i := range out {
		k := from + i
		kind := "assistant"
		if k%2 == 1 {
			kind = "user"
		}
		out[i] = transcriptEntry{Cursor: fmt.Sprintf("c%06d", k), At: time.Date(2026, 10, 13, 9, k%60, 0, 0, time.UTC).Format(time.RFC3339), Kind: kind, Text: fmt.Sprintf("entry %d", k)}
	}
	return out
}

func (f *fakeTranscript) append(e ...transcriptEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e...)
}

func (f *fakeTranscript) seen() []transcriptRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

func (f *fakeTranscript) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	before, after := q.Get("before"), q.Get("after")
	f.requests = append(f.requests, transcriptRequest{before: before, after: after, at: time.Now()})
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, code string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "code": code})
	}
	if f.entries == nil {
		fail(http.StatusNotFound, "SESSION_TRANSCRIPT_MISSING")
		return
	}
	limit := 200
	if s := q.Get("limit"); s != "" {
		limit, _ = strconv.Atoi(s)
	}
	if (before != "" && after != "") || limit < 1 || limit > 500 {
		fail(http.StatusBadRequest, "INVALID_INPUT")
		return
	}
	at := func(c string) int {
		return slices.IndexFunc(f.entries, func(e transcriptEntry) bool { return e.Cursor == c })
	}
	start, end := max(0, len(f.entries)-limit), len(f.entries)
	switch {
	case before != "":
		end = at(before)
		start = max(0, end-limit)
	case after != "":
		start = at(after) + 1
		end = min(len(f.entries), start+limit)
	}
	if start < 0 || end < 0 {
		fail(http.StatusBadRequest, "INVALID_INPUT")
		return
	}
	page := slices.Clone(f.entries[start:end])
	first, last := "", ""
	if len(page) > 0 {
		first, last = page[0].Cursor, page[len(page)-1].Cursor
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"entries": page, "first_cursor": first, "last_cursor": last,
		"has_older": start > 0, "has_newer": end < len(f.entries),
	})
}

// historyPage serves the task page with s1, whose terminal is a live PTY and
// whose saved conversation is tr.
func historyPage(t *testing.T, s1 *domain.Session, tr *fakeTranscript) *httptest.Server {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions:     fakeSessions{[]*domain.Session{s1}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/terminal", newHistoryPTY())
	mux.Handle("/api/sessions/s1/transcript", tr)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const (
	historyLog   = `document.querySelector('.session .transcript[role=log]')`
	historyLines = `[...document.querySelectorAll('.session .transcript > .line')]`
	// The log shows its bottom.
	historyAtBottom = `(l => !!l && l.scrollHeight - l.scrollTop - l.clientHeight < 4)(` + historyLog + `)`
	historyJump     = `document.querySelector('.session .transcript > .jump')`
)

// lineTop is where the line whose text is exactly text sits below the log's
// top, in px; null while it is not rendered.
func lineTop(text string) string {
	return fmt.Sprintf(`(() => { const el = %s.find(l => l.querySelector('.text')?.textContent === %q); return el ? el.getBoundingClientRect().top - %s.getBoundingClientRect().top : null })()`, historyLines, text, historyLog)
}

// hasLine: the line whose text is exactly text is rendered.
func hasLine(text string) string {
	return fmt.Sprintf(`%s.some(l => l.querySelector('.text')?.textContent === %q)`, historyLines, text)
}

// openHistory opens the task page at width×height on s1's History tab.
func openHistory(t *testing.T, srv *httptest.Server, width, height int64, scheme string) (context.Context, *errorLog) {
	t.Helper()
	ctx, errs := browser(t)
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		emulateColorScheme(scheme),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1?session=s1"),
		chromedp.Poll(`!document.querySelector('[data-ssr]') && !!document.querySelector('.pane-tabs [role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		clickButton(`.pane-tabs [role=tablist]`, "History"),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	return ctx, errs
}

func noJSErrors(t *testing.T, errs *errorLog) {
	t.Helper()
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// History opens on the newest entries, at the bottom. Scrolling to the top
// loads the page before without moving the line the reader was looking at,
// back to the start of the conversation.
func TestHistoryShowsTheNewestAndLoadsOlderWithoutAJump(t *testing.T) {
	tr := &fakeTranscript{entries: numberedEntries(1, 450)}
	srv := historyPage(t, liveSession(), tr)
	ctx, errs := openHistory(t, srv, 1280, 800, "light")
	var atBottom bool
	var before, after float64
	err := chromedp.Run(ctx,
		chromedp.Poll(hasLine("entry 450"), nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(historyAtBottom, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(historyAtBottom, &atBottom),
		// The oldest line loaded, entry 251, scrolled into the top of the log:
		// where it is, measured before the scroll event can load anything.
		chromedp.Evaluate(`(() => { `+historyLog+`.scrollTop = 0; return `+lineTop("entry 251")+` })()`, &before),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	err = chromedp.Run(ctx,
		chromedp.Poll(hasLine("entry 51"), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(lineTop("entry 251"), &after),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if d := after - before; d > 2 || d < -2 {
		t.Errorf("loading older lines moved the line in view by %.0fpx (from %.0f to %.0f)", d, before, after)
	}
	err = chromedp.Run(ctx,
		chromedp.Evaluate(historyLog+`.scrollTop = 0`, nil),
		chromedp.Poll(hasLine("entry 1"), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(historyLog+`.scrollTop = 0`, nil),
		chromedp.Poll(`document.querySelector('.session .transcript > .older').textContent.includes('Start of the conversation')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	var befores []string
	for _, r := range tr.seen() {
		if r.before != "" {
			befores = append(befores, r.before)
		}
	}
	if want := []string{"c000251", "c000051"}; !slices.Equal(befores, want) {
		t.Errorf("before cursors = %v, want %v", befores, want)
	}
	if !atBottom {
		t.Error("History did not open at the bottom")
	}
	noJSErrors(t, errs)
}

// While the session runs, new entries arrive within a poll or two. A reader
// at the bottom follows them; one who scrolled up stays where they are, with
// the focus where it was, and Jump to latest takes them down.
func TestHistoryCatchesUpWithoutMovingAReaderWhoScrolledUp(t *testing.T) {
	tr := &fakeTranscript{entries: numberedEntries(1, 120)}
	srv := historyPage(t, liveSession(), tr)
	ctx, errs := openHistory(t, srv, 1280, 800, "dark")
	err := chromedp.Run(ctx,
		chromedp.Poll(hasLine("entry 120"), nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(historyAtBottom, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	tr.append(numberedEntries(121, 1)...)
	err = chromedp.Run(ctx,
		chromedp.Poll(hasLine("entry 121"), nil, chromedp.WithPollingTimeout(6*time.Second)),
		chromedp.Poll(historyAtBottom, nil, chromedp.WithPollingTimeout(2*time.Second)),
	)
	if err != nil {
		t.Fatalf("a reader at the bottom does not follow: %v\nJS errors: %v", err, errs.all())
	}

	var scrolled, stayed float64
	var focusBefore, focusAfter, label string
	var atBottom bool
	err = chromedp.Run(ctx,
		chromedp.Focus(`.pane-tabs [role=tab][aria-selected=true]`, chromedp.ByQuery),
		chromedp.Evaluate(`(l => (l.scrollTop = Math.floor((l.scrollHeight - l.clientHeight) / 2)))(`+historyLog+`)`, &scrolled),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.activeElement.textContent.trim()`, &focusBefore),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	tr.append(numberedEntries(122, 3)...)
	err = chromedp.Run(ctx,
		chromedp.Poll(historyJump+`?.textContent.trim() === '3 new · Jump to latest'`, nil, chromedp.WithPollingTimeout(6*time.Second)),
		chromedp.Evaluate(historyLog+`.scrollTop`, &stayed),
		chromedp.Evaluate(historyJump+`.textContent.trim()`, &label),
		chromedp.Evaluate(historyJump+`.click()`, nil),
		chromedp.Poll(`!`+historyJump, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(historyAtBottom, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(historyAtBottom, &atBottom),
		chromedp.Evaluate(`document.activeElement.textContent.trim()`, &focusAfter),
	)
	if err != nil {
		var state string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(l => JSON.stringify({top: l.scrollTop, h: l.scrollHeight, c: l.clientHeight, lines: `+historyLines+`.length, last: `+historyLines+`.at(-1)?.textContent, jump: `+historyJump+`?.textContent}))(`+historyLog+`)`, &state))
		t.Fatalf("%v (jump %q; %s)\nJS errors: %v", err, label, state, errs.all())
	}
	shoot(t, ctx, "history-desktop-dark")
	if d := stayed - scrolled; d > 1 || d < -1 {
		t.Errorf("new entries moved a reader who scrolled up from %.0f to %.0f", scrolled, stayed)
	}
	if focusBefore != focusAfter {
		t.Errorf("the focus moved from %q to %q", focusBefore, focusAfter)
	}
	if !atBottom {
		t.Error("Jump to latest did not take the reader to the newest line")
	}
	// T5: catching up with after=, never more often than every 2 s.
	var polls []time.Time
	for _, r := range tr.seen() {
		if r.after != "" {
			polls = append(polls, r.at)
		}
	}
	if len(polls) < 2 {
		t.Fatalf("after= requests = %d, want the catch-up polls", len(polls))
	}
	for i := 1; i < len(polls); i++ {
		if gap := polls[i].Sub(polls[i-1]); gap < 1900*time.Millisecond {
			t.Errorf("two catch-ups %v apart, want at least 2 s", gap)
		}
	}
	noJSErrors(t, errs)
}

// An ended session still has its History, read once; one with no saved
// conversation shows a plain empty state, not an error.
func TestHistoryOfAnEndedSessionAndOfOneWithoutATranscript(t *testing.T) {
	ended := liveSession()
	ended.Status = domain.SessionDone
	tr := &fakeTranscript{entries: numberedEntries(1, 3)}
	srv := historyPage(t, ended, tr)
	ctx, errs := openHistory(t, srv, 1280, 800, "light")
	err := chromedp.Run(ctx,
		chromedp.Poll(hasLine("entry 3"), nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(2500*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if n := len(tr.seen()); n != 1 {
		t.Errorf("an ended session's History asked %d times, want once", n)
	}
	noJSErrors(t, errs)

	missing := &fakeTranscript{}
	srv = historyPage(t, liveSession(), missing)
	ctx, errs = openHistory(t, srv, 1280, 800, "dark")
	var text string
	var alarms int
	err = chromedp.Run(ctx,
		chromedp.Poll(`!!document.querySelector('.session .frame[aria-label=History] .empty h2')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('.session .frame[aria-label=History]').textContent.replace(/\s+/g, ' ').trim()`, &text),
		chromedp.Evaluate(`[...document.querySelectorAll('.toast, [role=alert], .session .frame[aria-label=History] .pane-note')].filter(e => e.checkVisibility()).length`, &alarms),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(text, "No saved conversation yet") {
		t.Errorf("History without a transcript reads %q", text)
	}
	if alarms != 0 {
		t.Errorf("a missing transcript raised %d alarms; it is an empty state", alarms)
	}
	// The CLI writes its first turn: the running session's History shows it.
	missing.append(numberedEntries(1, 1)...)
	if err := chromedp.Run(ctx, chromedp.Poll(hasLine("entry 1"), nil, chromedp.WithPollingTimeout(6*time.Second))); err != nil {
		t.Fatalf("the transcript that appeared never showed: %v", err)
	}
	noJSErrors(t, errs)
}

// Agent text is text: markup in it is shown, never run. The tab, the log and
// a tool line are reached with the keyboard, and Enter opens the tool line.
func TestHistoryIsTextAndWorksFromTheKeyboard(t *testing.T) {
	tr := &fakeTranscript{entries: []transcriptEntry{
		{Cursor: "a", Kind: "user", Text: `<img src=x onerror="window.__pwned=1">`},
		{Cursor: "b", Kind: "tool_use", Tool: "Bash", Text: "git status --short"},
		{Cursor: "c", Kind: "tool_result", Tool: "Bash", Text: " M a.js\n M b.js", IsError: true},
	}}
	srv := historyPage(t, liveSession(), tr)
	ctx, errs := openHistory(t, srv, 1280, 800, "light")
	var pwned, imgs int
	var shown, expanded, detail string
	var reached []string
	err := chromedp.Run(ctx,
		chromedp.Poll(historyLines+`.length === 3`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`window.__pwned ? 1 : 0`, &pwned),
		chromedp.Evaluate(historyLog+`.querySelectorAll('img').length`, &imgs),
		chromedp.Evaluate(historyLines+`[0].querySelector('.text').textContent`, &shown),
		chromedp.Focus(`.pane-tabs [role=tab][aria-selected=true]`, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			for i := 0; i < 20; i++ {
				if err := chromedp.KeyEvent(kb.Tab).Do(ctx); err != nil {
					return err
				}
				var at string
				if err := chromedp.Evaluate(`(e => e.getAttribute('role') === 'log' ? 'log' : e.classList.contains('summary') ? 'summary' : e.tagName)(document.activeElement)`, &at).Do(ctx); err != nil {
					return err
				}
				reached = append(reached, at)
				if at == "summary" {
					return nil
				}
			}
			return fmt.Errorf("Tab never reached a tool line: %v", reached)
		}),
		chromedp.KeyEvent(kb.Enter),
		chromedp.Poll(`document.activeElement.getAttribute('aria-expanded') === 'true'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(`document.activeElement.getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(`(d => d && getComputedStyle(d).display !== 'none' ? d.textContent : '')(document.activeElement.parentElement.querySelector('.detail'))`, &detail),
	)
	if err != nil {
		t.Fatalf("%v (reached %v)\nJS errors: %v", err, reached, errs.all())
	}
	shoot(t, ctx, "history-desktop-light")
	if pwned != 0 || imgs != 0 {
		t.Errorf("agent text became markup: %d images, ran %d", imgs, pwned)
	}
	if shown != `<img src=x onerror="window.__pwned=1">` {
		t.Errorf("the user line reads %q", shown)
	}
	if !slices.Contains(reached, "log") {
		t.Errorf("Tab went %v, never through the log", reached)
	}
	if detail != "git status --short" {
		t.Errorf("Enter on the tool line shows %q", detail)
	}
	var errorKind string
	_ = chromedp.Run(ctx, chromedp.Evaluate(historyLines+`[2].dataset.kind`, &errorKind))
	if errorKind != "error" {
		t.Errorf("a failed tool result is %q, want marked as an error", errorKind)
	}
	noJSErrors(t, errs)
}

// On a 320px phone in the dark: the History tab fits, nothing scrolls
// sideways, the page does not scroll (L1), and the log does.
func TestHistoryOnAPhone(t *testing.T) {
	tr := &fakeTranscript{entries: numberedEntries(1, 80)}
	tr.entries = append(tr.entries, transcriptEntry{Cursor: "long", Kind: "assistant", Text: strings.Repeat("a_long_unbroken_identifier_", 12)})
	srv := historyPage(t, liveSession(), tr)
	ctx, errs := browser(t)
	var problems string
	var timeShown bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(320, 640),
		emulateColorScheme("dark"),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!document.querySelector('[data-ssr]') && !!document.querySelector('[role=option][data-session-id="s1"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=option][data-session-id="s1"]').click()`, nil),
		clickButton(`.pane-tabs [role=tablist]`, "History"),
		chromedp.Poll(historyLines+`.length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`[document.scrollingElement.scrollHeight > document.scrollingElement.clientHeight + 1 ? 'the page scrolls' : '',
			document.scrollingElement.scrollWidth > document.scrollingElement.clientWidth + 1 ? 'the page scrolls sideways' : '',
			`+historyLog+`.scrollWidth > `+historyLog+`.clientWidth + 1 ? 'the log scrolls sideways' : '',
			`+historyLog+`.scrollHeight > `+historyLog+`.clientHeight ? '' : 'the log does not scroll',
			(r => r.right <= 320 && r.left >= 0 ? '' : 'the log is off screen')(`+historyLog+`.getBoundingClientRect())].filter(Boolean).join(', ')`, &problems),
		chromedp.Evaluate(`getComputedStyle(`+historyLines+`[0].querySelector('time')).display !== 'none'`, &timeShown),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	shoot(t, ctx, "history-phone-dark")
	if problems != "" {
		t.Errorf("at 320px: %s", problems)
	}
	if timeShown {
		t.Error("at 320px the time column should fold away")
	}
	noJSErrors(t, errs)
}
