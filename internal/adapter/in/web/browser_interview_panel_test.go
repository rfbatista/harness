package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The interview panel beside the session's terminal (UX study "Interview
// panel"), in the browser: docked beside the terminal where the session column
// is 56rem wide, a peek strip and a sheet where it is not, open by itself on
// arrival unless the person hid it, one resize of the terminal per dock, open
// or close, and Alt+Shift+Q.

// liveInterviews is session s1's interviews as the server keeps them: a list,
// the number its feed has reached, and the feed. It answers the page's seed
// and, behind the real HTTP API, the resync read and the interview feed.
type liveInterviews struct {
	mu   sync.Mutex
	seq  int64
	list []*domain.Interview
	subs map[chan domain.InterviewEvent]struct{}
	// down refuses a follow: the feed is not there.
	down bool
}

func newLiveInterviews() *liveInterviews {
	return &liveInterviews{subs: map[chan domain.InterviewEvent]struct{}{}}
}

// ask opens an interview on the session and publishes it, as the service does.
func (l *liveInterviews) ask(iv *domain.Interview) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	l.list = append(l.list, iv)
	for ch := range l.subs {
		ch <- domain.InterviewEvent{Seq: l.seq, Kind: domain.InterviewAsked, Interview: iv}
	}
}

// waitFollowed waits for a page to follow the feed, so that what is asked next
// reaches it as an event rather than being in its seed.
func (l *liveInterviews) waitFollowed(t *testing.T) {
	t.Helper()
	for range 160 {
		l.mu.Lock()
		n := len(l.subs)
		l.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the page never followed the interview feed")
}

func (l *liveInterviews) ListInterviews(_ context.Context, ids []string) (map[string]*domain.InterviewList, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]*domain.InterviewList{}
	for _, id := range ids {
		if id == "s1" {
			out[id] = &domain.InterviewList{Seq: l.seq, Interviews: append([]*domain.Interview{}, l.list...)}
		}
	}
	return out, nil
}

func (l *liveInterviews) FollowInterviews(ctx context.Context, _ string) (<-chan domain.InterviewEvent, error) {
	ch := make(chan domain.InterviewEvent, 16)
	l.mu.Lock()
	if l.down {
		l.mu.Unlock()
		return nil, errors.New("the feed is down")
	}
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	go func() {
		<-ctx.Done()
		l.mu.Lock()
		delete(l.subs, ch)
		l.mu.Unlock()
	}()
	return ch, nil
}

// gatedInterviews is what the HTTP API reads and follows: it answers nothing
// until the gate opens (a nil gate is open) and counts the reads.
type gatedInterviews struct {
	*liveInterviews
	gate  chan struct{}
	mu    sync.Mutex
	reads int
	// answers is the server's side of Submit and Dismiss, for the tests that send; nil: not answered here.
	answers *answerer
	// url is where the page is served.
	url string
}

func (g *gatedInterviews) wait(ctx context.Context) error {
	if g.gate == nil {
		return nil
	}
	select {
	case <-g.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *gatedInterviews) ListInterviews(ctx context.Context, ids []string) (map[string]*domain.InterviewList, error) {
	g.mu.Lock()
	g.reads++
	g.mu.Unlock()
	if err := g.wait(ctx); err != nil {
		return nil, err
	}
	return g.liveInterviews.ListInterviews(ctx, ids)
}

func (g *gatedInterviews) FollowInterviews(ctx context.Context, id string) (<-chan domain.InterviewEvent, error) {
	if err := g.wait(ctx); err != nil {
		return nil, err
	}
	return g.liveInterviews.FollowInterviews(ctx, id)
}

func (g *gatedInterviews) apiReads() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reads
}

// The panel tests read and follow; answering is the server's (see the
// interviews end-to-end tests) unless a test gives the page an answerer.
func (g *gatedInterviews) Submit(ctx context.Context, id string, answers []domain.InterviewAnswer) (*domain.Interview, error) {
	if g.answers != nil {
		return g.answers.respond(ctx, id, domain.InterviewSent, answers, "")
	}
	return nil, errors.New("not answered here")
}

func (g *gatedInterviews) Dismiss(ctx context.Context, id, note string) (*domain.Interview, error) {
	if g.answers != nil {
		return g.answers.respond(ctx, id, domain.InterviewDismissed, nil, note)
	}
	return nil, errors.New("not answered here")
}

var _ ports.Interviews = (*gatedInterviews)(nil)

// askedInterview is an open interview of two questions: a single choice whose
// second option the agent recommends, with a markdown body, and a free text.
func askedInterview(id, agent string) *domain.Interview {
	two := 1
	return &domain.Interview{
		ID: id, SessionID: "s1", AgentName: agent, AskedAt: time.Now(), State: domain.InterviewOpen,
		Questions: []domain.InterviewQuestion{
			{
				Title: "Which store holds interviews?", Kind: domain.QuestionSingle,
				Body:        "Pick **one** of `two` stores.\n\n- the database\n- a file\n\n<script>window.pwned = 1</script>",
				Options:     []domain.InterviewOption{{Label: "A file per interview"}, {Label: "The harness database", Description: "It survives restarts."}},
				Recommended: &domain.Recommended{Option: &two},
			},
			{Title: "Where is it read?", Kind: domain.QuestionText, Options: []domain.InterviewOption{}},
		},
	}
}

// interviewPage serves the task page for live session s1 with its terminal on
// pty and its interviews on live, over the real HTTP API; gate holds back the
// API's answers (nil: none held).
func interviewPage(t *testing.T, live *liveInterviews, pty http.Handler, gate chan struct{}) (*httptest.Server, *gatedInterviews) {
	t.Helper()
	return interviewPageWrapped(t, live, pty, gate, nil)
}

// interviewPageWrapped is interviewPage with what the server answers passed
// through wrap (nil: as it is), for a test that watches or breaks a request.
func interviewPageWrapped(t *testing.T, live *liveInterviews, pty http.Handler, gate chan struct{}, wrap func(http.Handler) http.Handler) (*httptest.Server, *gatedInterviews) {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	wire := &gatedInterviews{liveInterviews: live, gate: gate}
	// The inbox's bell leads the title with a count, as in the app: with it the
	// page's open questions can lead the title instead.
	inbox := &fakeInbox{stream: &sseFeed{}}
	pages := NewHandler(Deps{
		Notifications: inbox,
		Projects:      fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:         fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:        fakeAgents{},
		Repositories:  fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions:      fakeSessions{[]*domain.Session{liveSession()}},
		Interviews:    live,
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/terminal", pty)
	mux.Handle("/api/notifications/events", inbox.stream)
	mux.Handle("/api/notifications", inbox)
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Interviews: wire})))
	mux.Handle("/", pages)
	var served http.Handler = mux
	if wrap != nil {
		served = wrap(mux)
	}
	srv := newBrowserServer(served)
	t.Cleanup(srv.Close)
	wire.url = srv.URL
	return srv, wire
}

const (
	panelEl  = `document.querySelector('[data-interview-panel]')`
	docked   = `(p => !!p && p.checkVisibility() && !p.closest('.interview-sheet'))(` + panelEl + `)`
	peekEl   = `document.querySelector('[data-interview-peek]')`
	toggleEl = `document.querySelector('[data-interview-toggle]')`
	sheetEl  = `document.querySelector('.interview-sheet')`
	activeIn = `((r) => !!r && r.contains(document.activeElement))`
)

func ivPoll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(8*time.Second))
}

// openInterviewPage opens the page at the given window and waits for the
// terminal and the stream to be live.
func openInterviewPage(t *testing.T, live *liveInterviews, width int64) (context.Context, *errorLog, *historyPTY, *gatedInterviews) {
	t.Helper()
	return openInterviewPageWrapped(t, live, width, nil)
}

// openInterviewPageWrapped is openInterviewPage over a server wrapped by wrap.
func openInterviewPageWrapped(t *testing.T, live *liveInterviews, width int64, wrap func(http.Handler) http.Handler) (context.Context, *errorLog, *historyPTY, *gatedInterviews) {
	t.Helper()
	pty := newHistoryPTY()
	srv, wire := interviewPageWrapped(t, live, pty, nil, wrap)
	ctx, errs := browser(t)
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		ivPoll(terminalLive),
		ivPoll(`document.querySelector('[data-ssr]') === null`),
		settle(),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	live.waitFollowed(t)
	return ctx, errs, pty, wire
}

func ivEval[T any](t *testing.T, ctx context.Context, js string) T {
	t.Helper()
	var v T
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v)); err != nil {
		t.Fatalf("%v\n%s", err, js)
	}
	return v
}

func ivRun(t *testing.T, ctx context.Context, errs *errorLog, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
}

// pressAltShiftQ presses Alt+Shift+Q wherever the focus is.
func pressAltShiftQ() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		mods := input.ModifierAlt | input.ModifierShift
		key := func(t input.KeyType) *input.DispatchKeyEventParams {
			return input.DispatchKeyEvent(t).WithModifiers(mods).WithKey("Q").WithCode("KeyQ").WithWindowsVirtualKeyCode(81)
		}
		if err := key(input.KeyRawDown).Do(ctx); err != nil {
			return err
		}
		return key(input.KeyUp).Do(ctx)
	})
}

func keysTo(pty *historyPTY) int {
	n := 0
	for _, m := range pty.messages() {
		if m.Type == "key" {
			n++
		}
	}
	return n
}

// Wide, an interview that arrives docks beside the terminal; the panel shows
// the agent's recommendation first, never checked, and the markdown body
// rendered with the raw HTML in it shown as text.
func TestAnInterviewAsksBesideTheTerminalWithItsRecommendationFirst(t *testing.T) {
	live := newLiveInterviews()
	ctx, errs, _, _ := openInterviewPage(t, live, 1700)
	if ivEval[bool](t, ctx, `!!`+panelEl) {
		t.Fatal("a panel with nothing asked")
	}
	live.ask(askedInterview("iv-1", "architect"))
	ivRun(t, ctx, errs, ivPoll(docked))

	got := ivEval[map[string]any](t, ctx, `(() => {
		const q = document.querySelector('[data-interview-panel] .interview-question');
		const opts = [...q.querySelectorAll('.interview-option')];
		return {
			title: document.querySelector('[data-interview-panel] h2').textContent,
			region: document.querySelector('[data-interview-panel]').getAttribute('aria-label'),
			section: document.querySelector('[data-interview-panel] section').getAttribute('aria-label'),
			heading: document.querySelector('[data-interview-panel] section > header strong').textContent,
			legend: q.querySelector('legend').textContent.replace(/\s+/g, ' ').trim(),
			first: opts[0].textContent.replace(/\s+/g, ' ').trim(),
			firstRec: opts[0].hasAttribute('data-recommended'),
			labels: opts.map(o => o.querySelector('.text > span:last-of-type').textContent),
			checked: document.querySelectorAll('[data-interview-panel] input:checked').length,
			bold: q.querySelector('.body strong')?.textContent,
			code: q.querySelector('.body code')?.textContent,
			items: q.querySelectorAll('.body li').length,
			script: !!q.querySelector('.body script') || window.pwned === 1,
			shown: q.querySelector('.body').textContent.includes('<script>'),
			text: document.querySelectorAll('[data-interview-panel] textarea[placeholder="Write your answer"]').length,
			radios: q.querySelectorAll('input[type=radio]').length,
		};
	})()`)
	want := map[string]any{
		"title": "Interview", "region": "Interview panel", "section": "Open interview from architect",
		"heading": "architect asks 2 questions", "legend": "1Which store holds interviews?",
		"first": "Agent recommendsThe harness databaseIt survives restarts.", "firstRec": true,
		"checked": float64(0), "bold": "one", "code": "two", "items": float64(2), "script": false, "shown": true,
		"text": float64(1), "radios": float64(3),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if labels, _ := got["labels"].([]any); len(labels) != 3 || labels[0] != "The harness database" || labels[1] != "A file per interview" || labels[2] != "Write my own" {
		t.Errorf("options drawn as %v, want the recommended one first and Write my own last", got["labels"])
	}
	shoot(t, ctx, "interview-panel-docked")
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The panel docks where the session column is at least 56rem wide and is a
// peek strip with a sheet where it is not; the sheet never opens by itself,
// opens over the terminal without resizing it, takes the focus, and gives it
// back to the peek button on Esc.
func TestTheInterviewPanelDocksWideAndIsASheetNarrow(t *testing.T) {
	live := newLiveInterviews()
	live.ask(askedInterview("iv-1", "architect"))
	ctx, errs, pty, _ := openInterviewPage(t, live, 1700)
	ivRun(t, ctx, errs, ivPoll(docked))
	if ivEval[bool](t, ctx, `!!`+peekEl) {
		t.Error("a peek strip beside a docked panel")
	}
	col := ivEval[float64](t, ctx, `document.querySelector('.session').getBoundingClientRect().width / parseFloat(getComputedStyle(document.documentElement).fontSize)`)
	if col < 56 {
		t.Fatalf("the session column is %.1frem at 1700px: the test's premise is wrong", col)
	}

	// Narrower: the panel leaves for a peek strip, and nothing opens by itself.
	ivRun(t, ctx, errs, chromedp.EmulateViewport(1280, 800), ivPoll(`!!`+peekEl), settle())
	if ivEval[bool](t, ctx, docked) || ivEval[bool](t, ctx, `!!`+sheetEl) {
		t.Fatal("a docked panel or a sheet below 56rem of session column")
	}
	shoot(t, ctx, "interview-peek")
	if got := ivEval[string](t, ctx, peekEl+`.textContent.replace(/\s+/g, ' ').trim()`); got != "2 questions from architect Answer" {
		t.Errorf("peek strip = %q", got)
	}
	if got := ivEval[string](t, ctx, toggleEl+`.getAttribute('aria-pressed') + ' ' + `+toggleEl+`.getAttribute('aria-label')`); got != "false Questions, 2 open" {
		t.Errorf("toggle = %q", got)
	}

	before := len(resizes(pty))
	ivRun(t, ctx, errs, chromedp.Evaluate(peekEl+`.querySelector('button').click()`, nil), ivPoll(`!!`+sheetEl), settle())
	state := ivEval[map[string]any](t, ctx, `(() => {
		const s = `+sheetEl+`;
		return {
			role: s.getAttribute('role'), modal: s.getAttribute('aria-modal'),
			inert: document.querySelector('.agent-stage > .terminal').inert,
			focus: s.contains(document.activeElement),
			width: s.getBoundingClientRect().width, stage: s.parentElement.parentElement.getBoundingClientRect().width,
			peek: !!`+peekEl+`,
		};
	})()`)
	if state["role"] != "dialog" || state["modal"] != "true" || state["inert"] != true || state["focus"] != true || state["peek"] != false {
		t.Errorf("sheet = %v, want a modal dialog, the terminal inert, the focus inside, no peek strip", state)
	}
	shoot(t, ctx, "interview-sheet")
	if w := state["width"].(float64); w > 40*16+1 {
		t.Errorf("sheet is %vpx wide, want at most 40rem", w)
	}
	ivRun(t, ctx, errs, chromedp.KeyEvent("\x1b"), ivPoll(`!`+sheetEl+` && !!`+peekEl), settle())
	if got := ivEval[string](t, ctx, `document.activeElement.textContent.trim()`); got != "Answer" {
		t.Errorf("after Esc the focus is on %q, want the peek strip's Answer", got)
	}
	if ivEval[bool](t, ctx, `document.querySelector('.agent-stage > .terminal').inert`) {
		t.Error("the terminal is still inert after the sheet closed")
	}
	if n := len(resizes(pty)) - before; n != 0 {
		t.Errorf("the sheet sent %d resizes, want none: %+v", n, resizes(pty)[before:])
	}

	// Wide again: it docks.
	ivRun(t, ctx, errs, chromedp.EmulateViewport(1700, 800), ivPoll(docked))
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// An interview that arrives opens the docked panel by itself, without moving
// the focus out of the terminal; once the person hides it, it stays hidden for
// later interviews (and a reload) while the toggle keeps counting.
func TestAnArrivingInterviewOpensTheDockedPanelUnlessThePersonHidIt(t *testing.T) {
	live := newLiveInterviews()
	ctx, errs, _, _ := openInterviewPage(t, live, 1700)
	ivRun(t, ctx, errs, chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery))
	if ivEval[bool](t, ctx, `!!`+toggleEl) {
		t.Error("a Questions toggle with nothing asked")
	}

	live.ask(askedInterview("iv-1", "architect"))
	ivRun(t, ctx, errs, ivPoll(docked), settle())
	if !ivEval[bool](t, ctx, `document.activeElement.classList.contains('xterm-helper-textarea')`) {
		t.Error("an arriving interview took the focus out of the terminal")
	}
	toggle := func() string {
		return ivEval[string](t, ctx, `(() => { const b = `+toggleEl+`; return b ? b.getAttribute('aria-pressed') + ' ' + b.getAttribute('aria-label') + ' ' + b.querySelector('.badge').textContent : 'none'; })()`)
	}
	if got := toggle(); got != "true Questions, 2 open 2" {
		t.Errorf("toggle = %q", got)
	}
	if got := ivEval[string](t, ctx, `document.querySelector('main p[aria-live=polite]').textContent`); got != "architect asks 2 questions" {
		t.Errorf("the polite region says %q on arrival", got)
	}

	ivRun(t, ctx, errs, chromedp.Evaluate(toggleEl+`.click()`, nil), ivPoll(`!`+panelEl), settle())
	if got := toggle(); got != "false Questions, 2 open 2" {
		t.Errorf("after hiding, toggle = %q", got)
	}
	live.ask(askedInterview("iv-2", "architect"))
	ivRun(t, ctx, errs, ivPoll(toggleEl+`.querySelector('.badge').textContent === '4'`), settle())
	if ivEval[bool](t, ctx, `!!`+panelEl) {
		t.Error("the panel opened again by itself after the person hid it")
	}
	if got := toggle(); got != "false Questions, 4 open 4" {
		t.Errorf("after the second interview, toggle = %q", got)
	}

	// Hidden in this session, hidden after a reload.
	ivRun(t, ctx, errs, chromedp.Reload(), ivPoll(terminalLive), ivPoll(`document.querySelector('[data-ssr]') === null`), settle())
	if ivEval[bool](t, ctx, `!!`+panelEl) {
		t.Error("the panel is drawn after a reload although the person hid it")
	}
	if got := toggle(); got != "false Questions, 4 open 4" {
		t.Errorf("after the reload, toggle = %q", got)
	}
	ivRun(t, ctx, errs, chromedp.Evaluate(toggleEl+`.click()`, nil), ivPoll(docked))
	if got := toggle(); got != "true Questions, 4 open 4" {
		t.Errorf("after showing it again, toggle = %q", got)
	}
	if n := ivEval[float64](t, ctx, `document.querySelectorAll('[data-interview-panel] section').length`); n != 2 {
		t.Errorf("%v interview cards, want 2", n)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The terminal refits once when the docked panel opens, hides or shows, and
// its last row and column stay inside the pane each time.
func TestTheTerminalRefitsOnceWhenTheInterviewPanelDocksOpensOrCloses(t *testing.T) {
	live := newLiveInterviews()
	ctx, errs, pty, _ := openInterviewPage(t, live, 1700)
	checkFits(t, ctx, pty, errs, "before an interview")

	step := func(when string, action func()) {
		t.Helper()
		before := len(resizes(pty))
		action()
		ivRun(t, ctx, errs, settle())
		if n := len(resizes(pty)) - before; n != 1 {
			t.Errorf("%s: %d resizes sent, want exactly one: %+v", when, n, resizes(pty)[before:])
		}
		checkFits(t, ctx, pty, errs, when)
	}
	step("the interview arrives and the panel docks", func() { live.ask(askedInterview("iv-1", "architect")); ivRun(t, ctx, errs, ivPoll(docked)) })
	step("the person hides the panel", func() { ivRun(t, ctx, errs, chromedp.Evaluate(toggleEl+`.click()`, nil), ivPoll(`!`+panelEl)) })
	step("the person shows it again", func() { ivRun(t, ctx, errs, chromedp.Evaluate(toggleEl+`.click()`, nil), ivPoll(docked)) })
	step("its close button hides it", func() {
		ivRun(t, ctx, errs, chromedp.Evaluate(`document.querySelector('[data-interview-panel] header .button').click()`, nil), ivPoll(`!`+panelEl))
	})
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Alt+Shift+Q is the page's: it never reaches the agent, focuses the first
// question's first control (opening the panel when it is collapsed), and
// pressed again from inside the panel it gives the focus back to the terminal.
func TestAltShiftQMovesTheFocusToTheInterviewPanelAndBackWithoutReachingTheAgent(t *testing.T) {
	live := newLiveInterviews()
	live.ask(askedInterview("iv-1", "architect"))
	ctx, errs, pty, _ := openInterviewPage(t, live, 1700)
	ivRun(t, ctx, errs, ivPoll(docked), chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery))

	// The channel to the agent works: a plain key arrives.
	ivRun(t, ctx, errs, chromedp.KeyEvent("x"), settle())
	if keysTo(pty) != 1 {
		t.Fatalf("a typed key reached the agent %d times, want once", keysTo(pty))
	}

	inPanel := `(` + activeIn + `)(` + panelEl + `)`
	onTerminal := `document.activeElement.classList.contains('xterm-helper-textarea')`
	first := `document.activeElement.matches('.interview-question input[type=radio]') && document.activeElement.closest('label').hasAttribute('data-recommended')`

	ivRun(t, ctx, errs, pressAltShiftQ(), ivPoll(inPanel))
	if !ivEval[bool](t, ctx, first) {
		t.Error("the focus is not on the first question's first control (the recommended option)")
	}
	ivRun(t, ctx, errs, pressAltShiftQ(), ivPoll(onTerminal))

	// Collapsed: the key opens the panel and focuses it.
	ivRun(t, ctx, errs, chromedp.Evaluate(toggleEl+`.click()`, nil), ivPoll(`!`+panelEl), chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery))
	ivRun(t, ctx, errs, pressAltShiftQ(), ivPoll(docked+` && `+inPanel))
	if !ivEval[bool](t, ctx, first) {
		t.Error("collapsed: the focus is not on the first control after the panel opened")
	}
	ivRun(t, ctx, errs, pressAltShiftQ(), ivPoll(onTerminal), settle())

	if n := keysTo(pty); n != 1 {
		t.Errorf("the agent received %d keys, want only the typed x: Alt+Shift+Q reached it: %+v", n, pty.messages())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Narrow, Alt+Shift+Q opens the sheet and focuses its first control; pressed
// again it closes the sheet and the terminal has the focus.
func TestAltShiftQOpensTheSheetBelowTheDockWidth(t *testing.T) {
	live := newLiveInterviews()
	live.ask(askedInterview("iv-1", "architect"))
	ctx, errs, _, _ := openInterviewPage(t, live, 1100)
	ivRun(t, ctx, errs, ivPoll(`!!`+peekEl), chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery))
	ivRun(t, ctx, errs, pressAltShiftQ(), ivPoll(`!!`+sheetEl+` && (`+activeIn+`)(`+sheetEl+`)`))
	if !ivEval[bool](t, ctx, `document.activeElement.matches('.interview-question input[type=radio]')`) {
		t.Error("the focus is not on the first control")
	}
	ivRun(t, ctx, errs, pressAltShiftQ(), ivPoll(`!`+sheetEl+` && document.activeElement.classList.contains('xterm-helper-textarea')`))
}

// The first paint holds the seed: the panel is in the page before the stream or
// any read of the interviews has answered.
func TestTheInterviewPanelIsInTheFirstPaintFromTheSeed(t *testing.T) {
	live := newLiveInterviews()
	live.ask(askedInterview("iv-1", "architect"))
	gate := make(chan struct{})
	pty := newHistoryPTY()
	srv, wire := interviewPage(t, live, pty, gate)
	ctx, errs := browser(t)
	ivRun(t, ctx, errs,
		chromedp.EmulateViewport(1700, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		ivPoll(docked),
	)
	if n := wire.apiReads(); n != 0 {
		t.Errorf("the panel waited on %d reads of the interviews API", n)
	}
	if got := ivEval[string](t, ctx, toggleEl+`?.getAttribute('aria-label') ?? ''`); got != "Questions, 2 open" {
		t.Errorf("toggle = %q at first paint", got)
	}
	close(gate)
	// The feed opens afterwards and brings a second interview without a reload.
	live.waitFollowed(t)
	live.ask(askedInterview("iv-2", "architect"))
	ivRun(t, ctx, errs, ivPoll(`document.querySelectorAll('[data-interview-panel] section').length === 2`))
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
