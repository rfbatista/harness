package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/coder/websocket"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The session screen (direction A) in the browser: the tab row's terminal
// tools (L7), the pane note, ⋯ and its confirm, and the phone sheet.

// sessionsPageWith serves the task page with these sessions; s1's terminal is pty.
func sessionsPageWith(t *testing.T, sessions []*domain.Session, pty http.Handler) *httptest.Server {
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
		Sessions:     fakeSessions{sessions},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/terminal", pty)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := httptest.NewServer(withStream(mux))
	t.Cleanup(srv.Close)
	return srv
}

// droppingPTY sends a snapshot on every attach; the first connection closes
// (no exit: the process lives on) when drop is signalled.
type droppingPTY struct {
	drop     chan struct{}
	attaches chan int
	n        int
}

func (p *droppingPTY) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	p.n++
	n := p.n
	p.attaches <- n
	ctx := r.Context()
	b, _ := json.Marshal(ports.TerminalMessage{Type: "snapshot", Snapshot: &ports.TerminalSnapshot{
		Screen: fmt.Sprintf("attach %d\n$ ", n), CursorX: 2, CursorY: 1, Size: ports.TermSize{Cols: 80, Rows: 24}, Title: "claude",
	}})
	_ = conn.Write(ctx, websocket.MessageText, b)
	if n == 1 {
		select {
		case <-p.drop:
			_ = conn.Close(websocket.StatusGoingAway, "dropped")
			return
		case <-ctx.Done():
			return
		}
	}
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}

func liveSession() *domain.Session {
	return &domain.Session{ID: "s1", ProjectID: "p1", TicketID: "t1", Task: "implement this task", Status: domain.SessionRunning,
		Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: time.Now()}
}

// The tab row says the terminal is offline, with Reconnect; Reconnect takes
// the focus to the terminal (F2) and it is live again.
func TestTerminalOfflineThenReconnectInTheTabRow(t *testing.T) {
	pty := &droppingPTY{drop: make(chan struct{}), attaches: make(chan int, 4)}
	srv := sessionsPageWith(t, []*domain.Session{liveSession()}, pty)
	ctx, errs := browser(t)
	tools := `(document.querySelector('.pane-tabs .tools')?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	var offline, state, focusIn string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(terminalLive, nil, chromedp.WithPollingTimeout(10*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	close(pty.drop)
	err = chromedp.Run(ctx,
		chromedp.Poll(`(`+tools+`).includes('offline')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(tools, &offline),
		chromedp.Evaluate(`document.querySelector('.terminal').dataset.state`, &state),
		clickButton(`.pane-tabs .tools`, "Reconnect"),
		chromedp.Evaluate(`document.activeElement.closest('.terminal') ? 'terminal' : document.activeElement.tagName`, &focusIn),
		chromedp.Poll(terminalLive, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(offline, "offline · lost just now") || !strings.Contains(offline, "Reconnect") {
		t.Errorf("tab row while offline = %q", offline)
	}
	if state != "offline" {
		t.Errorf("terminal data-state = %q, want offline", state)
	}
	if focusIn != "terminal" {
		t.Errorf("after Reconnect the focus is on %s, want the terminal (F2)", focusIn)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Scrolled up while output arrives: Jump to latest appears with the count,
// takes the view back down, and leaves the focus where it was.
func TestJumpToLatestCountsNewLinesAndLeavesTheFocus(t *testing.T) {
	ctx, errs, pty := attachWithHistory(t)
	jump := `document.querySelector('.pane-tabs .tools .jump')`
	var label, focusBefore, focusAfter string
	err := chromedp.Run(ctx,
		chromedp.Focus(`[role=listbox][aria-label="Sessions of this task"]`, chromedp.ByQuery),
		scrollToTheTop(),
		chromedp.Poll(`!!`+jump, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for i := 0; i < 3; i++ {
		pty.print <- fmt.Sprintf("new output %d\r\n", i)
	}
	err = chromedp.Run(ctx,
		chromedp.Poll(jump+`?.getAttribute('aria-label') === 'Jump to latest, 3 new lines'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(jump+`.getAttribute('aria-label')`, &label),
		chromedp.Evaluate(`document.activeElement.getAttribute('aria-label') ?? document.activeElement.tagName`, &focusBefore),
		chromedp.Evaluate(jump+`.click()`, nil),
		chromedp.Poll(`!`+jump, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.activeElement.getAttribute('aria-label') ?? document.activeElement.tagName`, &focusAfter),
	)
	if err != nil {
		t.Fatalf("%v (jump %q)\nJS errors: %v", err, label, errs.all())
	}
	if focusBefore != focusAfter {
		t.Errorf("Jump to latest moved the focus from %q to %q", focusBefore, focusAfter)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// An ended session opened later: the note strip, an empty pane, Resume when
// the server would take it, and no terminal attached.
func TestAnEndedSessionShowsTheNoteOverAnEmptyPane(t *testing.T) {
	ended := liveSession()
	ended.Status = domain.SessionDone
	ended.Resumable = true
	srv := sessionsPageWith(t, []*domain.Session{ended}, http.NotFoundHandler())
	ctx, errs := browser(t)
	var note string
	var hasXterm bool
	var problems string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('.terminal[data-state=ended] .pane-note')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('.pane-note').textContent.replace(/\s+/g, ' ').trim()`, &note),
		chromedp.Evaluate(`!!document.querySelector('.terminal .xterm')`, &hasXterm),
		chromedp.Evaluate(`[document.scrollingElement, ...document.querySelectorAll('.session, .split-view')].filter(e => e.scrollHeight > e.clientHeight + 1).map(e => e.className || e.tagName).join(', ')`, &problems),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(note, "Resume session") || !strings.Contains(note, "ended") {
		t.Errorf("note = %q, want why and Resume session", note)
	}
	if hasXterm {
		t.Error("an ended session opened later has no terminal to attach")
	}
	if problems != "" {
		t.Errorf("these scroll: %s", problems)
	}
}

// ⋯ → Copy session id puts the id on the clipboard and says so.
func TestCopySessionIDFromTheMenu(t *testing.T) {
	srv := sessionsPageWith(t, []*domain.Session{liveSession()}, &fakePTY{exit: make(chan int, 1)})
	ctx, errs := browser(t)
	var copied, said string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(terminalLive, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`Object.defineProperty(navigator, 'clipboard', { value: { writeText: async (text) => { window.__copied = text; } } }); true`, nil),
		chromedp.Evaluate(`document.getElementById('session-more').click()`, nil),
		clickButton(`.session-head [role=menu]`, "Copy session id"),
		chromedp.Poll(`window.__copied === 's1'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`window.__copied`, &copied),
		chromedp.Poll(`[...document.querySelectorAll('main [role=status][aria-live=polite]')].some(e => e.textContent === 'Session id copied')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('main [role=status][aria-live=polite]')].map(e => e.textContent).join('|')`, &said),
	)
	if err != nil {
		t.Fatalf("%v (said %q)\nJS errors: %v", err, said, errs.all())
	}
	if copied != "s1" {
		t.Errorf("copied %q", copied)
	}
}

// On a phone ⋯ opens the session sheet (44px rows): Text size steps up with
// one resize, and Delete session… asks in a sheet that Esc closes, the focus
// back on ⋯.
func TestThePhoneSessionSheet(t *testing.T) {
	pty := newHistoryPTY()
	srv := sessionsPageWith(t, []*domain.Session{liveSession()}, pty)
	ctx, errs := browser(t)
	sheet := `document.querySelector('#session-sheet:popover-open')`
	var sheetIsSheet, confirmIsSheet bool
	var sizeWord, focusBack string
	var rowHeight float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(375, 700),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		// The live rows, not the server's first paint (which has no handlers).
		chromedp.Poll(`!document.querySelector('[data-ssr]') && !!document.querySelector('[role=option][data-session-id="s1"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=option][data-session-id="s1"]').click()`, nil),
		chromedp.Poll(terminalLive, nil, chromedp.WithPollingTimeout(10*time.Second)),
		settle(),
		chromedp.Evaluate(`document.getElementById('session-more').click()`, nil),
		chromedp.Poll(`!!`+sheet, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(sheet+`.hasAttribute('data-sheet')`, &sheetIsSheet),
		chromedp.Evaluate(`[...document.querySelectorAll('#session-sheet [role=menuitem]')].find(b => b.textContent.includes('Copy session id')).getBoundingClientRect().height`, &rowHeight),
	)
	if err != nil {
		var state string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`'detail=' + document.querySelector('.split-view')?.hasAttribute('data-detail') + ' dot=' + document.querySelector('.pane-tabs .tools [role=status]')?.dataset.state + ' popovers=' + [...document.querySelectorAll('[popover]')].map(e => (e.id || e.getAttribute('role')) + ':' + e.matches(':popover-open')).join(' ')`, &state))
		t.Fatalf("%v (%s)\nJS errors: %v", err, state, errs.all())
	}
	before := len(resizes(pty))
	err = chromedp.Run(ctx,
		clickButton(`#session-sheet`, "Text size"),
		settle(),
		chromedp.Evaluate(`[...document.querySelectorAll('#session-sheet [role=menuitem]')].find(b => b.textContent.includes('Text size')).querySelector('.meta').textContent`, &sizeWord),
		clickButton(`#session-sheet`, "Delete session"),
		chromedp.Poll(`!!document.querySelector('#session-delete:popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('#session-delete').hasAttribute('data-sheet')`, &confirmIsSheet),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Poll(`!document.querySelector('#session-delete:popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.activeElement.id`, &focusBack),
	)
	if err != nil {
		var state string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`'popovers=' + [...document.querySelectorAll('[popover]')].map(e => (e.id || e.getAttribute('role')) + ':' + e.matches(':popover-open')).join(' ') + ' active=' + document.activeElement.outerHTML.slice(0, 90)`, &state))
		t.Fatalf("%v (size %q; %s)\nJS errors: %v", err, sizeWord, state, errs.all())
	}
	if !sheetIsSheet || !confirmIsSheet {
		t.Errorf("on a phone: sheet as sheet %v, confirm as sheet %v", sheetIsSheet, confirmIsSheet)
	}
	if rowHeight < 44 {
		t.Errorf("sheet rows are %vpx, want at least 44", rowHeight)
	}
	if sizeWord != "Larger" {
		t.Errorf("Text size reads %q after one step, want Larger", sizeWord)
	}
	if n := len(resizes(pty)) - before; n != 1 {
		t.Errorf("a text size step sent %d resizes, want one", n)
	}
	if focusBack != "session-more" {
		t.Errorf("after Esc the focus is on %q, want ⋯", focusBack)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
