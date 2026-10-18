package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
)

// The task rail in the browser: the cookie's first paint and the handover,
// folding with the peek rule, Done's cap, the filter, Needs you, the keys,
// live changes and the phone drawer. Screenshots land in RAIL_SHOTS when it
// is set.

// railWorld is a project with tasks in every busy group: the open task runs
// two sessions, one task has a review waiting, one in Backlog has a session
// waiting, and Done has eight tasks.
func railWorld() Deps {
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tasks := []*domain.Ticket{
		{ID: "t-cur", Title: "Redesign sidebar", Status: domain.TicketStatusInProgress},
		{ID: "t-rev", Title: "Improve tasks dashboard ui", Status: domain.TicketStatusInProgress, PendingReviews: 1},
		{ID: "t-run", Title: "Persist who moved a task's status between sessions", Status: domain.TicketStatusInProgress},
		{ID: "t-viz", Title: "Visualize code in the web ui", Status: domain.TicketStatusTodo},
		{ID: "t-todo", Title: "Integrate tasks with todoist", Status: domain.TicketStatusTodo},
		{ID: "t-flaky", Title: "Fix flaky project documents test", Status: domain.TicketStatusBacklog},
	}
	for i, title := range []string{"Review the UX for the web ui", "Clone repository", "Improve agent section", "Notification"} {
		tasks = append(tasks, &domain.Ticket{ID: fmt.Sprintf("t-b%d", i), Title: title, Status: domain.TicketStatusBacklog})
	}
	for i, title := range []string{"Design View", "Terminal Session Design", "Project level documentation", "Project design assets", "Architect highlights", "Resume agent sessions", "Design animated buttons", "Add assets and comments"} {
		tasks = append(tasks, &domain.Ticket{ID: fmt.Sprintf("t-d%d", i), Title: title, Status: domain.TicketStatusDone, UpdatedAt: base.Add(time.Duration(i) * time.Hour)})
	}
	for _, t := range tasks {
		t.ProjectID = "p1"
	}
	now := time.Now()
	return Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "harness"}}},
		Tasks:    fakeTickets{tasks},
		Sessions: fakeSessions{[]*domain.Session{
			{ID: "s1", ProjectID: "p1", TicketID: "t-cur", Status: domain.SessionRunning, UpdatedAt: now},
			{ID: "s2", ProjectID: "p1", TicketID: "t-cur", Status: domain.SessionThinking, UpdatedAt: now},
			{ID: "s3", ProjectID: "p1", TicketID: "t-run", Status: domain.SessionRunning, UpdatedAt: now},
			{ID: "s4", ProjectID: "p1", TicketID: "t-flaky", Status: domain.SessionIdle, UpdatedAt: now},
		}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{},
	}
}

type railHarness struct {
	ctx  context.Context
	srv  *httptest.Server
	feed *sseFeed
	errs *errorLog
}

func newRailHarness(t *testing.T) *railHarness {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(railWorld(), assets, nil))
	srv := httptest.NewServer(withStream(mux))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return &railHarness{ctx: ctx, srv: srv, feed: feed, errs: errs}
}

func (h *railHarness) run(t *testing.T, what string, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(h.ctx, actions...); err != nil {
		t.Fatalf("%s: %v\nJS errors: %v", what, err, h.errs.all())
	}
}

func (h *railHarness) eval(t *testing.T, js string, out any) {
	t.Helper()
	h.run(t, js, chromedp.Evaluate(js, out))
}

func (h *railHarness) noErrors(t *testing.T) {
	t.Helper()
	if e := h.errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// openLive loads a page and waits for the live rail to replace the server's.
func (h *railHarness) openLive(path string) chromedp.Action {
	return chromedp.Tasks{
		chromedp.Navigate(h.srv.URL + path),
		chromedp.Poll(`document.querySelector('[data-ssr]') === null && !!document.querySelector('nav[aria-label="Tasks"] .filter input')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	}
}

func railPoll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(5*time.Second))
}

// shownRows lists the task ids a group shows, in order.
func shownRows(key string) string {
	return fmt.Sprintf(`[...document.querySelectorAll('nav[aria-label="Tasks"] section[data-key=%q] a[data-task-id]')].filter(a => a.checkVisibility()).map(a => a.dataset.taskId).join(' ')`, key)
}

const railFocus = `(() => { const a = document.activeElement; return a ? (a.dataset.taskId || a.closest('section')?.dataset.key && a.tagName + ':' + a.closest('section').dataset.key || a.getAttribute('aria-label') || a.className || a.tagName) : ''; })()`

func railShot(name string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		dir := os.Getenv("RAIL_SHOTS")
		if dir == "" {
			return nil
		}
		var buf []byte
		if err := chromedp.CaptureScreenshot(&buf).Do(ctx); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name+".png"), buf, 0o644)
	})
}

// slash presses "/" as a browser does: one keyDown carrying its text, so a
// handler that prevents it also prevents the typing (chromedp's KeyEvent
// sends the character separately).
func slash() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchKeyEvent(input.KeyDown).WithKey("/").WithCode("Slash").WithText("/").WithWindowsVirtualKeyCode(191).Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey("/").WithCode("Slash").WithWindowsVirtualKeyCode(191).Do(ctx)
	})
}

func railTheme(scheme string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`document.documentElement.dataset.theme = %q`, scheme), nil)
}

// The server paints folded what the cookie folds, peeking the open task and
// what waits on the person, and the live rail takes over without moving a
// row.
func TestRailFirstPaintFollowsTheCookieAndTheHandoverDoesNotMove(t *testing.T) {
	h := newRailHarness(t)
	var noScriptRows, liveRows string
	var noScriptTop, liveTop float64
	var noScriptExpanded bool
	top := `document.querySelector('nav[aria-label="Tasks"] a[aria-current="page"]').getBoundingClientRect().top`
	h.run(t, "the first paint without script",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(h.srv.URL+"/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(`document.cookie = "rail_collapsed=backlog,todo; Path=/"`, nil),
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(shownRows("backlog")+` + ' / ' + `+shownRows("todo"), &noScriptRows),
		chromedp.Evaluate(top, &noScriptTop),
		chromedp.Evaluate(`!!document.querySelector('nav[aria-label="Tasks"] h3 > button')`, &noScriptExpanded),
		railShot("rail-no-script"),
		emulation.SetScriptExecutionDisabled(false),
	)
	if noScriptRows != "t-flaky / " {
		t.Errorf("folded groups without script show %q, want only Backlog's waiting task", noScriptRows)
	}
	if noScriptExpanded {
		t.Error("the server copy's headings are buttons")
	}
	var collapsedLive, expanded string
	h.run(t, "the live rail",
		h.openLive("/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(shownRows("backlog")+` + ' / ' + `+shownRows("todo"), &liveRows),
		chromedp.Evaluate(top, &liveTop),
		chromedp.Evaluate(`[...document.querySelectorAll('nav[aria-label="Tasks"] section[data-collapsed]')].map(s => s.dataset.key).join(',')`, &collapsedLive),
		chromedp.Evaluate(`document.querySelector('section[data-key=backlog] h3 > button').getAttribute('aria-expanded')`, &expanded),
	)
	if liveRows != noScriptRows || collapsedLive != "todo,backlog" || expanded != "false" {
		t.Errorf("live: rows %q, collapsed %q, backlog expanded %q", liveRows, collapsedLive, expanded)
	}
	if liveTop != noScriptTop {
		t.Errorf("the open task moved at the handover: %v → %v", noScriptTop, liveTop)
	}
	h.noErrors(t)
}

// A heading folds its group and the choice is remembered; a folded group
// still shows the open task and what needs the person.
func TestRailFoldsAGroupAndRemembersIt(t *testing.T) {
	h := newRailHarness(t)
	var before, after, cookie, reloaded string
	h.run(t, "fold In progress and Backlog",
		chromedp.EmulateViewport(1280, 900),
		h.openLive("/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(shownRows("in_progress"), &before),
		chromedp.Click(`section[data-key=in_progress] h3 > button`, chromedp.ByQuery),
		chromedp.Click(`section[data-key=backlog] h3 > button`, chromedp.ByQuery),
		railPoll(`document.querySelector('section[data-key=backlog] h3 > button').getAttribute('aria-expanded') === 'false'`),
		chromedp.Evaluate(shownRows("in_progress")+` + ' / ' + `+shownRows("backlog"), &after),
		chromedp.Evaluate(`document.cookie`, &cookie),
		h.openLive("/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(`[...document.querySelectorAll('section[data-collapsed]')].map(s => s.dataset.key).join(',')`, &reloaded),
	)
	if before != "t-cur t-rev t-run" {
		t.Errorf("In progress before = %q", before)
	}
	if after != "t-cur t-rev / t-flaky" {
		t.Errorf("folded: %q, want the open task and what needs you peeking", after)
	}
	if !strings.Contains(cookie, "rail_collapsed=in_progress,backlog") {
		t.Errorf("cookie = %q", cookie)
	}
	if reloaded != "in_progress,backlog" {
		t.Errorf("after a reload, folded = %q", reloaded)
	}
	h.noErrors(t)
}

// Done shows its five latest, then "Show 3 more".
func TestRailCapsDoneAndShowsTheRestOnDemand(t *testing.T) {
	h := newRailHarness(t)
	var capped, summary, all string
	h.run(t, "Done",
		chromedp.EmulateViewport(1280, 900),
		h.openLive("/projects/p1"),
		chromedp.Evaluate(shownRows("done"), &capped),
		chromedp.Evaluate(`document.querySelector('section[data-key=done] .more summary').textContent`, &summary),
		chromedp.Click(`section[data-key=done] .more summary`, chromedp.ByQuery),
		railPoll(`document.querySelector('section[data-key=done] .more details').open`),
		chromedp.Evaluate(shownRows("done"), &all),
	)
	if capped != "t-d7 t-d6 t-d5 t-d4 t-d3" || summary != "Show 3 more done tasksShow fewer" {
		t.Errorf("capped %q, summary %q", capped, summary)
	}
	if all != "t-d7 t-d6 t-d5 t-d4 t-d3 t-d2 t-d1 t-d0" {
		t.Errorf("opened: %q", all)
	}
	h.noErrors(t)
}

// The filter keeps matching titles, lifts the cap, marks the match, says
// when nothing matches, and Esc restores the rail.
func TestRailFilterFindsTasksAndEscRestores(t *testing.T) {
	h := newRailHarness(t)
	var counts, done, mark, cleared, notice string
	h.run(t, "filter",
		chromedp.EmulateViewport(1280, 900),
		h.openLive("/projects/p1"),
		chromedp.Click(`.filter input`, chromedp.ByQuery),
		chromedp.SendKeys(`.filter input`, "design", chromedp.ByQuery),
		railPoll(`document.querySelector('section[data-key=todo] h3 .count').textContent.startsWith('0 of 2')`),
		chromedp.Evaluate(`[...document.querySelectorAll('nav[aria-label="Tasks"] section')].map(s => s.querySelector('h3 .count > span').textContent).join(' | ')`, &counts),
		chromedp.Evaluate(shownRows("done"), &done),
		chromedp.Evaluate(`document.querySelector('a[data-task-id=t-d0] mark').textContent`, &mark),
		chromedp.KeyEvent(kb.Escape),
		railPoll(`document.querySelector('.filter input').value === ''`),
		chromedp.Evaluate(shownRows("done"), &cleared),
		chromedp.SendKeys(`.filter input`, "zzz", chromedp.ByQuery),
		railPoll(`!!document.querySelector('nav[aria-label="Tasks"] .notice')`),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] .notice span').textContent`, &notice),
		railShot("rail-filter-no-match"),
	)
	if counts != "1 of 3 | 0 | 0 of 2 | 0 of 5 | 4 of 8" {
		t.Errorf("counts = %q", counts)
	}
	if done != "t-d6 t-d3 t-d1 t-d0" || mark != "Design" {
		t.Errorf("filtered Done %q (the cap lifted), mark %q", done, mark)
	}
	if cleared != "t-d7 t-d6 t-d5 t-d4 t-d3" {
		t.Errorf("after Esc, Done = %q", cleared)
	}
	if notice != "No task matches “zzz”." {
		t.Errorf("notice = %q", notice)
	}
	h.noErrors(t)
}

// Needs you shows only what waits on the person; answered, it says nothing
// is left and stays on.
func TestRailNeedsYouShowsWhatWaitsAndStaysOn(t *testing.T) {
	h := newRailHarness(t)
	var count, rows, pressed, notice string
	h.run(t, "Needs you",
		chromedp.EmulateViewport(1280, 900),
		h.openLive("/projects/p1/tasks/t-cur"),
		railPoll(`!!document.querySelector('nav[aria-label="Tasks"] .head .button')?.checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] .head .badge').textContent`, &count),
		chromedp.Click(`nav[aria-label="Tasks"] .head .button`, chromedp.ByQuery),
		railPoll(`document.querySelector('nav[aria-label="Tasks"] .head .button').getAttribute('aria-pressed') === 'true'`),
		chromedp.Evaluate(`[...document.querySelectorAll('nav[aria-label="Tasks"] a[data-task-id]')].filter(a => a.checkVisibility()).map(a => a.dataset.taskId).join(' ')`, &rows),
	)
	if count != "2" || rows != "t-rev t-flaky" {
		t.Errorf("Needs you %q, rows %q", count, rows)
	}
	waitFor(t, "the rail's feed", func() bool { return h.feed.followers() >= 1 })
	h.feed.pushRaw(t, map[string]any{"session": &domain.Session{ID: "s4", ProjectID: "p1", TicketID: "t-flaky", Status: domain.SessionRunning, UpdatedAt: time.Now()}})
	h.feed.pushRaw(t, map[string]any{"ticket": &domain.Ticket{ID: "t-rev", ProjectID: "p1", Title: "Improve tasks dashboard ui", Status: domain.TicketStatusInProgress}})
	h.run(t, "nothing left",
		railPoll(`!!document.querySelector('nav[aria-label="Tasks"] p.notice')`),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] p.notice').textContent`, &notice),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] .head .button').getAttribute('aria-pressed')`, &pressed),
	)
	if notice != "Nothing needs you right now." || pressed != "true" {
		t.Errorf("notice %q, pressed %q", notice, pressed)
	}
	h.noErrors(t)
}

// The skip link is the first Tab stop; "/" focuses the filter (not from a
// text field); the arrow keys move through the Tasks nav.
func TestRailKeys(t *testing.T) {
	h := newRailHarness(t)
	var first, skipped, slashed, down, left, collapsed, right, end, home string
	h.run(t, "keys",
		chromedp.EmulateViewport(1280, 900),
		h.openLive("/projects/p1/tasks/t-cur"),
		chromedp.KeyEvent(kb.Tab),
		chromedp.Evaluate(`document.activeElement.textContent`, &first),
		chromedp.KeyEvent(kb.Enter),
		chromedp.Evaluate(`location.hash`, &skipped),
		slash(),
		chromedp.Evaluate(`document.activeElement.getAttribute('aria-label') + ':' + document.activeElement.value`, &slashed),
		chromedp.KeyEvent(kb.ArrowDown),
		chromedp.Evaluate(railFocus, &down),
		chromedp.KeyEvent(kb.ArrowLeft),
		chromedp.Evaluate(railFocus, &left),
		chromedp.KeyEvent(kb.ArrowLeft),
		railPoll(`document.querySelector('section[data-key=in_progress] h3 > button').getAttribute('aria-expanded') === 'false'`),
		chromedp.Evaluate(shownRows("in_progress"), &collapsed),
		chromedp.KeyEvent(kb.ArrowRight),
		railPoll(`document.querySelector('section[data-key=in_progress] h3 > button').getAttribute('aria-expanded') === 'true'`),
		chromedp.KeyEvent(kb.ArrowRight),
		chromedp.Evaluate(railFocus, &right),
		chromedp.KeyEvent(kb.End),
		chromedp.Evaluate(railFocus, &end),
		chromedp.KeyEvent(kb.Home),
		chromedp.Evaluate(railFocus, &home),
	)
	if first != "Skip to content" || skipped != "#main" {
		t.Errorf("first Tab %q, then %q", first, skipped)
	}
	if slashed != "Filter tasks:" {
		t.Errorf("/ focused %q", slashed)
	}
	if down != "t-cur" || left != "BUTTON:in_progress" || collapsed != "t-cur t-rev" || right != "t-cur" {
		t.Errorf("↓ %q, ← %q, folded %q, → %q", down, left, collapsed, right)
	}
	if end != "SUMMARY:done" || home != "BUTTON:in_progress" {
		t.Errorf("End %q, Home %q", end, home)
	}
	// "/" typed in a text field stays there.
	var typed string
	h.run(t, "/ in a field",
		chromedp.Evaluate(`(() => { const t = document.createElement('textarea'); t.id = 'probe'; document.querySelector('main').append(t); t.focus(); })()`, nil),
		slash(),
		chromedp.Evaluate(`document.activeElement.id + ':' + document.activeElement.value`, &typed),
	)
	if typed != "probe:/" {
		t.Errorf("/ in a textarea: %q", typed)
	}
	h.noErrors(t)
}

// A task that starts waiting rings its dot and is announced in the stream
// bar; a move into a folded group only changes its count.
func TestRailFollowsLiveChanges(t *testing.T) {
	h := newRailHarness(t)
	h.run(t, "load",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(h.srv.URL+"/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(`document.cookie = "rail_collapsed=todo; Path=/"`, nil),
		h.openLive("/projects/p1/tasks/t-cur"),
	)
	waitFor(t, "the rail's feed", func() bool { return h.feed.followers() >= 1 })
	h.feed.pushRaw(t, map[string]any{"session": &domain.Session{ID: "s9", ProjectID: "p1", TicketID: "t-viz", Status: domain.SessionWaitingApproval, PendingApprovals: 1, UpdatedAt: time.Now()}})
	var signal, washed bool
	var announced, todo string
	h.run(t, "waiting",
		railPoll(`!!document.querySelector('a[data-task-id=t-viz] .status[data-signal]')`),
		chromedp.Evaluate(`document.querySelector('a[data-task-id=t-viz]').hasAttribute('data-attention')`, &washed),
		chromedp.Evaluate(`!!document.querySelector('a[data-task-id=t-viz] .status[data-signal]')`, &signal),
		railPoll(`!!document.querySelector('[x-data=streamAnnouncer]').textContent`),
		chromedp.Evaluate(`document.querySelector('[x-data=streamAnnouncer]').textContent`, &announced),
		chromedp.Evaluate(shownRows("todo"), &todo),
	)
	if !signal || !washed || announced != "“Visualize code in the web ui” is waiting on you." {
		t.Errorf("signal %v, wash %v, announced %q", signal, washed, announced)
	}
	if todo != "t-viz" {
		t.Errorf("folded Todo shows %q, want the task that now waits peeking", todo)
	}
	h.feed.pushRaw(t, map[string]any{"ticket": &domain.Ticket{ID: "t-b0", ProjectID: "p1", Title: "Review the UX for the web ui", Status: domain.TicketStatusTodo}})
	var count string
	h.run(t, "moved into a folded group",
		railPoll(`document.querySelector('section[data-key=todo] h3 .count > span').textContent === '3'`),
		chromedp.Evaluate(shownRows("todo"), &todo),
		chromedp.Evaluate(`document.querySelector('section[data-key=backlog] h3 .count > span').textContent`, &count),
	)
	if todo != "t-viz" || count != "4" {
		t.Errorf("after the move: folded Todo shows %q, Backlog counts %q", todo, count)
	}
	h.noErrors(t)
}

// Below 48rem the rail is a drawer opened from the top bar.
func TestRailIsADrawerOnAPhone(t *testing.T) {
	h := newRailHarness(t)
	var railShown, toggleShown bool
	var badge, focused, expanded, afterEsc, focusBack string
	var rowHeight, scrollWidth, innerWidth float64
	h.run(t, "phone",
		chromedp.EmulateViewport(375, 740),
		h.openLive("/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(`document.querySelector('.rail').checkVisibility({ visibilityProperty: true })`, &railShown),
		chromedp.Evaluate(`document.querySelector('.tasks-toggle').checkVisibility()`, &toggleShown),
		chromedp.Evaluate(`document.querySelector('.tasks-toggle .badge').textContent.trim()`, &badge),
		railShot("rail-phone-closed"),
		chromedp.Click(`.tasks-toggle`, chromedp.ByQuery),
		railPoll(`document.querySelector('.rail').hasAttribute('data-open') && document.activeElement?.dataset.taskId === 't-cur'`),
		chromedp.Sleep(900*time.Millisecond), // the slide
		chromedp.Evaluate(railFocus, &focused),
		chromedp.Evaluate(`document.querySelector('.tasks-toggle').getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(`Math.min(...['a[data-task-id=t-viz]', '.filter input', 'section[data-key=todo] h3 > button', '.rail > .button'].map(s => document.querySelector(s).getBoundingClientRect().height))`, &rowHeight),
		chromedp.Evaluate(`document.documentElement.scrollWidth`, &scrollWidth),
		chromedp.Evaluate(`window.innerWidth`, &innerWidth),
		railShot("rail-phone-drawer"),
	)
	if railShown || !toggleShown || badge != "2 need you" {
		t.Errorf("closed: rail shown %v, toggle shown %v, badge %q", railShown, toggleShown, badge)
	}
	if focused != "t-cur" || expanded != "true" {
		t.Errorf("open: focus %q, expanded %q", focused, expanded)
	}
	if rowHeight < 44 {
		t.Errorf("the smallest of a row, the filter, a heading and New task is %vpx on a phone, want 44", rowHeight)
	}
	if scrollWidth > innerWidth {
		t.Errorf("the page scrolls sideways: %v > %v", scrollWidth, innerWidth)
	}
	// Tab stays in the drawer: Shift+Tab from its first stop wraps to its last.
	var wrapped bool
	h.run(t, "trap",
		chromedp.Evaluate(`document.querySelector('.rail .drawer-head button').focus()`, nil),
		chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(input.ModifierShift)),
		chromedp.Evaluate(`document.querySelector('.rail').contains(document.activeElement)`, &wrapped),
		chromedp.KeyEvent(kb.Escape),
		railPoll(`!document.querySelector('.rail').hasAttribute('data-open')`),
		chromedp.Evaluate(`document.querySelector('.tasks-toggle').getAttribute('aria-expanded')`, &afterEsc),
		chromedp.Evaluate(`document.activeElement.classList.contains('tasks-toggle') ? 'toggle' : document.activeElement.tagName`, &focusBack),
	)
	if !wrapped || afterEsc != "false" || focusBack != "toggle" {
		t.Errorf("trap %v, after Esc expanded %q, focus %q", wrapped, afterEsc, focusBack)
	}
	h.noErrors(t)
}

// Screenshots of the desktop rail in both themes, for the review.
func TestRailScreenshots(t *testing.T) {
	if os.Getenv("RAIL_SHOTS") == "" {
		t.Skip("set RAIL_SHOTS to a directory to take the rail's screenshots")
	}
	h := newRailHarness(t)
	h.run(t, "shots",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(h.srv.URL+"/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(`document.cookie = "rail_collapsed=backlog; Path=/"`, nil),
		h.openLive("/projects/p1/tasks/t-cur"),
		railTheme("light"),
		chromedp.Sleep(800*time.Millisecond),
		railShot("rail-desktop-light"),
		railTheme("dark"),
		chromedp.Sleep(800*time.Millisecond),
		railShot("rail-desktop-dark"),
		h.openLive("/projects/p1"),
		railTheme("dark"),
		chromedp.SendKeys(`.filter input`, "design", chromedp.ByQuery),
		chromedp.Sleep(800*time.Millisecond),
		railShot("rail-filter-dark"),
		chromedp.EmulateViewport(375, 740),
		h.openLive("/projects/p1/tasks/t-cur"),
		railTheme("light"),
		chromedp.Click(`.tasks-toggle`, chromedp.ByQuery),
		chromedp.Sleep(900*time.Millisecond),
		railShot("rail-phone-drawer-light"),
	)
}
