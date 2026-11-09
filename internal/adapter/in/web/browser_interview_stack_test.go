package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// Several interviews at once, what is left of the ones that ended, and the
// signals outside the panel (UX study "Interview panel", sections 2, 5, 6 and
// 7), in the browser: open interviews stack and fold each on its own, a
// second arrival never moves the scroll, the caret or the focus, an interview
// changed through the feed moves to Earlier here with its words, a gap in the
// feed is read again, and the "questions" chip, the Agent tab's badge and the
// document title say that questions wait.

// put swaps an interview into the list by id (or adds it), moves the feed's
// number on, and tells the followers unless quiet: a quiet change is one a
// follower missed, which the page learns of from the next event's gap.
func (l *liveInterviews) put(iv *domain.Interview, kind domain.InterviewEventKind, quiet bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	swapped := false
	for i, x := range l.list {
		if x.ID == iv.ID {
			l.list[i], swapped = iv, true
		}
	}
	if !swapped {
		l.list = append(l.list, iv)
	}
	if quiet {
		return
	}
	for ch := range l.subs {
		ch <- domain.InterviewEvent{Seq: l.seq, Kind: kind, Interview: iv}
	}
}

// change tells the page an interview changed, as another tab's send or the
// session's end would.
func (l *liveInterviews) change(iv *domain.Interview) { l.put(iv, domain.InterviewChanged, false) }

// closedInterview is askedInterview after it ended in the given state.
func closedInterview(id string, state domain.InterviewState, ended time.Time) *domain.Interview {
	iv := askedInterview(id, "architect")
	iv.State, iv.EndedAt = state, &ended
	if state == domain.InterviewSent {
		delivered := domain.InterviewDelivered
		iv.Delivery, iv.DeliveredAt = &delivered, &ended
		iv.Answers = []domain.InterviewAnswer{{Chosen: []int{1}, Note: "survives restarts"}, {Skipped: true}}
	}
	return iv
}

const (
	cards   = `[...document.querySelectorAll('[data-interview-panel] [data-interview-card]')]`
	rowsEl  = `[...document.querySelectorAll('[data-interview-panel] [data-interview-row]')]`
	polite  = `document.querySelector('main p[aria-live=polite]').textContent`
	termFoc = `document.activeElement.classList.contains('xterm-helper-textarea')`
)

// cardStates lists the cards top to bottom as "id:open" or "id:folded".
const cardStates = `(` + cards + `.map(c => c.dataset.interviewCard + ':' + (c.hasAttribute('data-collapsed') ? 'folded' : 'open')).join(' '))`

// rowStates lists the Earlier rows top to bottom as "id:state:opened|folded".
const rowStates = `(` + rowsEl + `.map(r => r.dataset.state + ':' + (r.hasAttribute('data-collapsed') ? 'folded' : 'opened')).join(' '))`

func clickIn(sel string) chromedp.Action {
	return chromedp.Evaluate(`document.querySelector(`+"`"+sel+"`"+`).click()`, nil)
}

// Two interviews stack: the newest is expanded on top and the older is folded
// to a row that says who asked, its first question and how far the person got;
// each folds and expands on its own; the arrival is announced.
func TestStackedInterviewsFoldAndExpandEachOnItsOwn(t *testing.T) {
	live := newLiveInterviews()
	live.ask(askedInterview("iv-1", "architect"))
	ctx, errs, _, _ := openInterviewPage(t, live, 1700)
	ivRun(t, ctx, errs, ivPoll(docked))
	if got := ivEval[string](t, ctx, cardStates); got != "iv-1:open" {
		t.Fatalf("a lone interview reads %q", got)
	}
	if ivEval[bool](t, ctx, `!!document.querySelector('[data-interview-card] header > .fold')`) {
		t.Error("a lone interview offers to fold")
	}

	live.ask(askedInterview("iv-2", "architect"))
	ivRun(t, ctx, errs, ivPoll(cards+`.length === 2`), settle())
	if got := ivEval[string](t, ctx, cardStates); got != "iv-2:open iv-1:folded" {
		t.Fatalf("stack = %q, want the new one on top and expanded, the first folded", got)
	}
	if got := ivEval[string](t, ctx, polite); got != "architect asked 2 more questions" {
		t.Errorf("the polite region says %q on the second arrival", got)
	}
	folded := ivEval[map[string]any](t, ctx, `(() => {
		const c = document.querySelector('[data-interview-card="iv-1"]');
		const head = c.querySelector(':scope > .head');
		return {
			label: c.getAttribute('aria-label'),
			head: head.innerText.replace(/\s+/g, ' ').trim(),
			expanded: head.getAttribute('aria-expanded'),
			shown: head.checkVisibility(),
			questions: c.querySelector('fieldset').checkVisibility(),
			footer: c.querySelector('footer').checkVisibility(),
		};
	})()`)
	want := map[string]any{
		"label": "Open interview from architect, folded", "head": "architect asks 2 questions Which store holds interviews? 0 of 2",
		"expanded": "false", "shown": true, "questions": false, "footer": false,
	}
	for k, v := range want {
		if folded[k] != v {
			t.Errorf("folded card %s = %v, want %v", k, folded[k], v)
		}
	}
	shoot(t, ctx, "interview-stack")

	// Each on its own: expanding the first leaves the second expanded.
	ivRun(t, ctx, errs, clickIn(`[data-interview-card="iv-1"] > .head`), ivPoll(cardStates+` === 'iv-2:open iv-1:open'`))
	if !ivEval[bool](t, ctx, `document.querySelector('[data-interview-card="iv-1"] fieldset').checkVisibility()`) {
		t.Error("the expanded card shows no questions")
	}
	// Folding the newest leaves the other as it is.
	ivRun(t, ctx, errs, clickIn(`[data-interview-card="iv-2"] header > .fold`), ivPoll(cardStates+` === 'iv-2:folded iv-1:open'`))
	ivRun(t, ctx, errs, clickIn(`[data-interview-card="iv-2"] > .head`), ivPoll(cardStates+` === 'iv-2:open iv-1:open'`))
	ivRun(t, ctx, errs, clickIn(`[data-interview-card="iv-1"] header > .fold`), ivPoll(cardStates+` === 'iv-2:open iv-1:folded'`))
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A second interview arrives while the person is typing in the first: it is
// added above as a new card, and the first stays as it is (it had been
// touched), with its scroll position, its focus and its caret where they were.
func TestASecondArrivalMovesNeitherScrollNorCaretNorFocus(t *testing.T) {
	live := newLiveInterviews()
	live.ask(answeringInterview("iv-1"))
	ctx, errs, _, _ := openInterviewPage(t, live, 1700)
	ivRun(t, ctx, errs, ivPoll(docked), chromedp.Evaluate(`window.scrollTo(0, 0)`, nil))
	box := qSel(4, `textarea[data-interview-text]`)
	ivRun(t, ctx, errs,
		chromedp.SendKeys(box, "the page seed", chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const ta = document.querySelector(`+"`"+box+"`"+`);
			ta.scrollIntoView({ block: 'center' });
			ta.focus();
			ta.setSelectionRange(4, 8);
			window.__ta = ta;
		})()`, nil),
		settle(),
	)
	measure := `(() => {
		const ta = window.__ta, s = document.querySelector('[data-interview-scroll]');
		return {
			same: document.activeElement === ta, start: ta.selectionStart, end: ta.selectionEnd, value: ta.value,
			top: Math.round(ta.getBoundingClientRect().top), scrolled: s.scrollTop > 0,
			cards: ` + cardStates + `,
		};
	})()`
	before := ivEval[map[string]any](t, ctx, measure)
	if before["same"] != true || before["scrolled"] != true {
		t.Fatalf("the premise failed: %v", before)
	}

	live.ask(askedInterview("iv-2", "architect"))
	ivRun(t, ctx, errs, ivPoll(cards+`.length === 2`), settle())
	after := ivEval[map[string]any](t, ctx, measure)
	for _, k := range []string{"same", "start", "end", "value", "top"} {
		if after[k] != before[k] {
			t.Errorf("%s was %v and is %v after a second interview arrived", k, before[k], after[k])
		}
	}
	if after["cards"] != "iv-2:open iv-1:open" {
		t.Errorf("cards = %v, want the new one above and the touched one still open", after["cards"])
	}

	// An untouched first interview folds when the next arrives.
	live2 := newLiveInterviews()
	live2.ask(askedInterview("a", "architect"))
	ctx2, errs2, _, _ := openInterviewPage(t, live2, 1700)
	ivRun(t, ctx2, errs2, ivPoll(docked))
	live2.ask(askedInterview("b", "architect"))
	ivRun(t, ctx2, errs2, ivPoll(cardStates+` === 'b:open a:folded'`))
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// An interview changed through the feed (another tab sent it, the person
// dismissed it there, the session ended) moves to Earlier here with its words,
// loses the draft kept in this browser, and says so politely; the newest
// ended is on top; each row opens on its own; an expired one reads back what
// was typed and copies it.
func TestAnInterviewChangedThroughTheFeedMovesToEarlierWithItsWords(t *testing.T) {
	live := newLiveInterviews()
	ctx, errs, _, _ := openInterviewPage(t, live, 1700)
	draft := func(id string) bool {
		return ivEval[bool](t, ctx, fmt.Sprintf(`localStorage.getItem('harness:interview-draft:%s') !== null`, id))
	}
	now := time.Now()

	// Sent from another tab.
	live.ask(askedInterview("iv-1", "architect"))
	ivRun(t, ctx, errs, ivPoll(docked), chromedp.Click(optSel(1, "0"), chromedp.ByQuery))
	if !draft("iv-1") {
		t.Fatal("answering kept no draft: the premise failed")
	}
	live.change(closedInterview("iv-1", domain.InterviewSent, now.Add(-10*time.Minute)))
	ivRun(t, ctx, errs, ivPoll(rowStates+` === 'sent:folded'`), settle())
	if got := ivEval[string](t, ctx, polite); got != "Interview answered in another tab" {
		t.Errorf("the polite region says %q", got)
	}
	if draft("iv-1") {
		t.Error("the unsent draft of an interview sent elsewhere is still kept")
	}
	if n := ivEval[float64](t, ctx, cards+`.length`); n != 0 {
		t.Errorf("%v open cards after the only interview was sent", n)
	}
	if !ivEval[bool](t, ctx, docked) {
		t.Error("the panel that was showing left with its last open interview: Earlier is not reachable")
	}
	if got := ivEval[string](t, ctx, `document.querySelector('[data-interview-empty]')?.innerText.replace(/\s+/g, ' ').trim() ?? ''`); got != "No questions right now When an agent asks you questions, they appear here, and your answers go back to it automatically." {
		t.Errorf("empty line = %q", got)
	}
	if got := ivEval[string](t, ctx, toggleEl+`.getAttribute('aria-label') + '|' + `+toggleEl+`.getAttribute('aria-pressed') + '|' + !!`+toggleEl+`.querySelector('.badge')`); got != "Questions|true|false" {
		t.Errorf("toggle = %q, want it bare (no badge) and pressed", got)
	}

	// Dismissed in another tab.
	live.ask(askedInterview("iv-2", "architect"))
	ivRun(t, ctx, errs, ivPoll(cards+`.length === 1`), chromedp.Click(optSel(1, "1"), chromedp.ByQuery))
	live.change(closedInterview("iv-2", domain.InterviewDismissed, now.Add(-5*time.Minute)))
	ivRun(t, ctx, errs, ivPoll(rowStates+` === 'dismissed:folded sent:folded'`), settle())
	if got := ivEval[string](t, ctx, polite); got != "Interview dismissed in another tab" {
		t.Errorf("the polite region says %q", got)
	}
	if draft("iv-2") {
		t.Error("the draft of a dismissed interview is still kept")
	}

	// Expired: what was typed is kept to be read back.
	live.ask(askedInterview("iv-3", "architect"))
	ivRun(t, ctx, errs, ivPoll(cards+`.length === 1`),
		chromedp.Click(optSel(1, "0"), chromedp.ByQuery),
		chromedp.SendKeys(qSel(2, `textarea[data-interview-text]`), "typed before the end", chromedp.ByQuery),
	)
	live.change(closedInterview("iv-3", domain.InterviewExpired, now))
	ivRun(t, ctx, errs, ivPoll(rowStates+` === 'expired:folded dismissed:folded sent:folded'`), settle())
	if got := ivEval[string](t, ctx, polite); got != "Interview expired: the session ended" {
		t.Errorf("the polite region says %q", got)
	}
	if !draft("iv-3") {
		t.Error("what was typed in an expired interview was dropped: nothing is left to read back")
	}
	shoot(t, ctx, "interview-earlier")

	words := ivEval[[]string](t, ctx, rowsEl+`.map(r => r.getAttribute('aria-label') + ' | ' + r.querySelector('.head strong').textContent + ' | ' + r.querySelector('.head .text span').textContent + ' | ' + r.querySelector('.head .badge').textContent)`)
	patterns := []string{
		`^Expired interview from architect at \d\d:\d\d \| Expired · architect · \d\d:\d\d \| Session ended \d\d:\d\d · the agent never read it \| 2 questions$`,
		`^Dismissed interview from architect at \d\d:\d\d \| Dismissed · architect · \d\d:\d\d \| Dismissed \d\d:\d\d · the agent was told \| 2 questions$`,
		`^Sent interview from architect at \d\d:\d\d \| Sent · architect · \d\d:\d\d \| 1 answered, 1 skipped · delivered \d\d:\d\d \| 2 questions$`,
	}
	if len(words) != 3 {
		t.Fatalf("rows = %v", words)
	}
	for i, p := range patterns {
		if !ivEval[bool](t, ctx, fmt.Sprintf(`/%s/.test(%q)`, p, words[i])) {
			t.Errorf("row %d = %q, want it to match %s", i+1, words[i], p)
		}
	}
	if got := ivEval[string](t, ctx, `document.querySelector('[data-interview-earlier]').textContent`); got != "Earlier · 3" {
		t.Errorf("the group says %q", got)
	}

	// Each row opens on its own and reads each question back.
	ivRun(t, ctx, errs, clickIn(`[data-interview-row][data-state="sent"] > .head`), ivPoll(rowStates+` === 'expired:folded dismissed:folded sent:opened'`))
	sent := ivEval[string](t, ctx, `document.querySelector('[data-interview-row][data-state="sent"] .interview-read').innerText.replace(/\n+/g, ' | ')`)
	if sent != "1 · Which store holds interviews? | The harness database | survives restarts | 2 · Where is it read? | Skipped" {
		t.Errorf("the sent interview reads back %q", sent)
	}
	skipped := ivEval[map[string]string](t, ctx, `(() => { const s = document.querySelector('[data-interview-row][data-state="sent"] .skipped'); const c = getComputedStyle(s); return { style: c.fontStyle, color: c.color, faint: getComputedStyle(document.documentElement).getPropertyValue('--color-ink-faint') }; })()`)
	if skipped["style"] != "italic" {
		t.Errorf("Skipped is %s, want italic", skipped["style"])
	}
	ivRun(t, ctx, errs, clickIn(`[data-interview-row][data-state="expired"] > .head`), ivPoll(rowStates+` === 'expired:opened dismissed:folded sent:opened'`))
	expired := ivEval[string](t, ctx, `document.querySelector('[data-interview-row][data-state="expired"] .interview-read').innerText.replace(/\n+/g, ' | ')`)
	if expired != "1 · Which store holds interviews? | A file per interview | 2 · Where is it read? | typed before the end" {
		t.Errorf("the expired interview reads back %q", expired)
	}
	if n := ivEval[float64](t, ctx, rowsEl+`.filter(r => r.querySelector('[data-interview-copy]')).length`); n != 1 {
		t.Errorf("%v rows offer Copy answers, want only the expired one", n)
	}

	// Copy answers puts what was typed on the clipboard as plain text.
	ivRun(t, ctx, errs, chromedp.Evaluate(`Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: (t) => { window.__copied = t; return Promise.resolve(); } } })`, nil),
		clickIn(`[data-interview-copy]`), ivPoll(`window.__copied !== undefined`))
	if got := ivEval[string](t, ctx, `window.__copied`); got != "1. Which store holds interviews?\nA file per interview\n\n2. Where is it read?\ntyped before the end" {
		t.Errorf("copied %q", got)
	}
	ivRun(t, ctx, errs, ivPoll(polite+` === 'Answers copied'`))

	// Reloaded, the expired interview still reads back what was typed.
	ivRun(t, ctx, errs, chromedp.Reload(), ivPoll(terminalLive), ivPoll(`document.querySelector('[data-ssr]') === null`), chromedp.Evaluate(toggleEl+`.click()`, nil), ivPoll(docked))
	ivRun(t, ctx, errs, clickIn(`[data-interview-row][data-state="expired"] > .head`), ivPoll(`!!document.querySelector('[data-interview-row][data-state="expired"] .interview-read')?.checkVisibility()`))
	if got := ivEval[string](t, ctx, `document.querySelector('[data-interview-row][data-state="expired"] .interview-read').innerText.replace(/\n+/g, ' | ')`); got != expired {
		t.Errorf("after a reload the expired interview reads %q, want %q", got, expired)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// With only history, the toggle is there without a badge and opens the panel;
// nothing opens it by itself.
func TestOnlyHistoryIsReachableFromTheToggleAndNeverOpensByItself(t *testing.T) {
	live := newLiveInterviews()
	live.put(closedInterview("iv-1", domain.InterviewSent, time.Now().Add(-time.Hour)), domain.InterviewAsked, true)
	ctx, errs, _, _ := openInterviewPage(t, live, 1700)
	if ivEval[bool](t, ctx, `!!`+panelEl) {
		t.Error("the panel opened by itself on history alone")
	}
	if got := ivEval[string](t, ctx, toggleEl+`.getAttribute('aria-label') + '|' + `+toggleEl+`.getAttribute('aria-pressed') + '|' + !!`+toggleEl+`.querySelector('.badge')`); got != "Questions|false|false" {
		t.Fatalf("toggle = %q, want it there, bare and not pressed", got)
	}
	ivRun(t, ctx, errs, clickIn(`[data-interview-toggle]`), ivPoll(docked))
	if got := ivEval[string](t, ctx, rowStates); got != "sent:folded" {
		t.Errorf("rows = %q", got)
	}
	if got := ivEval[string](t, ctx, `document.querySelector('[data-interview-panel] header .who').textContent`); got != "nothing open" {
		t.Errorf("the head says %q", got)
	}
	ivRun(t, ctx, errs, clickIn(`[data-interview-toggle]`), ivPoll(`!`+panelEl))
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A gap in the feed's numbers is read again: the panel then matches the
// server, what was sent and what arrived in what was missed included.
func TestAGapInTheFeedReadsTheInterviewsAgainAndThePanelMatchesTheServer(t *testing.T) {
	live := newLiveInterviews()
	live.ask(askedInterview("iv-1", "architect"))
	ctx, errs, _, wire := openInterviewPage(t, live, 1700)
	ivRun(t, ctx, errs, ivPoll(docked))
	if n := wire.apiReads(); n != 0 {
		t.Fatalf("%d reads before anything was missed", n)
	}

	// The page misses two changes: iv-1 was sent elsewhere and iv-2 was asked.
	live.put(closedInterview("iv-1", domain.InterviewSent, time.Now()), domain.InterviewChanged, true)
	live.put(askedInterview("iv-2", "architect"), domain.InterviewAsked, true)
	live.ask(askedInterview("iv-3", "architect"))
	ivRun(t, ctx, errs, ivPoll(cardStates+` === 'iv-3:open iv-2:folded'`), ivPoll(rowStates+` === 'sent:folded'`), settle())
	if n := wire.apiReads(); n < 1 {
		t.Errorf("%d reads after a gap, want the list read again", n)
	}
	if got := ivEval[string](t, ctx, toggleEl+`.querySelector('.badge').textContent`); got != "4" {
		t.Errorf("the toggle counts %q open questions, want the server's 4", got)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// No arrival, change, fold or copy moves the terminal's focus, and the keys
// the person types keep reaching the agent.
func TestNothingTheInterviewsDoMovesTheTerminalsFocus(t *testing.T) {
	live := newLiveInterviews()
	ctx, errs, pty, _ := openInterviewPage(t, live, 1700)
	ivRun(t, ctx, errs, chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery))
	still := func(what string) {
		t.Helper()
		if !ivEval[bool](t, ctx, termFoc) {
			t.Fatalf("%s moved the focus out of the terminal to %s", what, ivEval[string](t, ctx, `document.activeElement.outerHTML.slice(0, 120)`))
		}
	}
	now := time.Now()
	live.ask(askedInterview("iv-1", "architect"))
	ivRun(t, ctx, errs, ivPoll(docked), settle())
	still("an interview arriving")
	live.ask(askedInterview("iv-2", "architect"))
	ivRun(t, ctx, errs, ivPoll(cards+`.length === 2`), settle())
	still("a second interview arriving")
	live.change(closedInterview("iv-1", domain.InterviewSent, now.Add(-time.Minute)))
	ivRun(t, ctx, errs, ivPoll(rowStates+` === 'sent:folded'`), settle())
	still("an interview moving to Earlier")
	live.change(closedInterview("iv-2", domain.InterviewExpired, now))
	ivRun(t, ctx, errs, ivPoll(rowStates+` === 'expired:folded sent:folded'`), settle())
	still("an interview expiring")
	ivRun(t, ctx, errs, chromedp.KeyEvent("x"), settle())
	if keysTo(pty) != 1 {
		t.Errorf("a key typed after all that reached the agent %d times, want once", keysTo(pty))
	}
	live.ask(askedInterview("iv-3", "architect"))
	ivRun(t, ctx, errs, ivPoll(cards+`.length === 1`), settle())
	still("an interview arriving after others closed")
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The note's summary reads Note once it holds text and Add a note while it is
// empty, as the mockup draws it.
func TestTheNoteSummaryReadsNoteOnceItHoldsText(t *testing.T) {
	_, ctx, errs, _ := panelWithAnswers(t, 1700)
	summary := `document.querySelector('` + qSel(1, `summary`) + `').textContent.trim()`
	if got := ivEval[string](t, ctx, summary); got != "Add a note" {
		t.Fatalf("an empty note's summary = %q", got)
	}
	ivRun(t, ctx, errs, openNote(1))
	if got := ivEval[string](t, ctx, summary); got != "Add a note" {
		t.Errorf("an opened but empty note's summary = %q, want Add a note", got)
	}
	ivRun(t, ctx, errs, chromedp.SendKeys(qSel(1, `textarea[data-interview-note]`), "because", chromedp.ByQuery), ivPoll(summary+` === 'Note'`))
	ivRun(t, ctx, errs, chromedp.SendKeys(qSel(1, `textarea[data-interview-note]`), "\b\b\b\b\b\b\b", chromedp.ByQuery), ivPoll(summary+` === 'Add a note'`))
}

// A running session with an open interview reads "questions" on its row and in
// its head, and is in the Needs you group; one without does not.
func TestTheQuestionsChipShowsOnTheSessionRowAndHead(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	now := time.Now()
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions: fakeSessions{[]*domain.Session{
			{ID: "s-ask", ProjectID: "p1", TicketID: "t1", Task: "Port tickets", Status: domain.SessionRunning, OpenInterviewCount: 2, OpenQuestionCount: 4, UpdatedAt: now},
			{ID: "s-run", ProjectID: "p1", TicketID: "t1", Task: "Bump harnesskit", Status: domain.SessionRunning, UpdatedAt: now.Add(-time.Minute)},
		}},
	}, assets, nil)
	srv := newBrowserServer(pages)
	defer srv.Close()

	ivRun(t, ctx, errs,
		chromedp.EmulateViewport(1700, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1?session=s-ask"),
		ivPoll(`document.querySelector('[data-ssr]') === null`),
	)
	row := func(id string) string {
		return ivEval[string](t, ctx, fmt.Sprintf(`(document.querySelector('.list [role=option][data-session-id=%q] > * .badge, .list [role=option][data-session-id=%q] .badge')?.textContent ?? '').trim()`, id, id))
	}
	if got := row("s-ask"); got != "questions" {
		t.Errorf("the asking session's row chip = %q, want questions", got)
	}
	if got := row("s-run"); got != "" {
		t.Errorf("a session with nothing open has the chip %q", got)
	}
	if got := ivEval[string](t, ctx, `document.querySelector('.session-head .status').textContent.trim()`); got != "questions" {
		t.Errorf("the session head says %q, want questions", got)
	}
	if got := ivEval[string](t, ctx, `document.querySelector('.session-head .status').dataset.state`); got != "running" {
		t.Errorf("the session head's state = %q, want running (the dot stays the session's own)", got)
	}
	groups := ivEval[[]string](t, ctx, `[...document.querySelectorAll('.list [role=group]')].map(g => g.querySelector('.group > span').textContent + ': ' + [...g.querySelectorAll('[role=option]')].map(r => r.dataset.sessionId).join(','))`)
	if fmt.Sprint(groups) != "[Needs you: s-ask Running: s-run]" {
		t.Errorf("groups = %v, want the asking session under Needs you", groups)
	}
	shoot(t, ctx, "interview-chip")
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The Agent board card: a session with an open interview reads "questions"
// after the approval and before your turn, in the browser as on first paint.
func TestTheAgentBoardCardReadsQuestionsAfterApprovalAndBeforeYourTurn(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	since := time.Now().Add(-3 * time.Minute)
	seed := &domain.BoardSeed{
		Seq: 7, GeneratedAt: time.Now(), ProjectCount: 1,
		Projects: []domain.BoardProject{{ProjectID: "p1", ProjectName: "coding_pool", Cards: []domain.BoardCard{{
			TaskID: "t1", ProjectID: "p1", TaskTitle: "Interview UI", TaskStatus: "in_progress", NeedsYou: true, NeedsYouSince: &since, LastStatusChangeAt: since,
			Sessions: []domain.BoardSessionLine{
				{SessionID: "s-run", AgentName: "web", Role: "delegate", Status: domain.SessionRunning, StatusSince: since},
				{SessionID: "s-turn", AgentName: "go", Role: "delegate", Status: domain.SessionIdle, StatusSince: since},
				{SessionID: "s-ask", AgentName: "architect", Role: "architect", Status: domain.SessionRunning, StatusSince: since, OpenInterviewCount: 1, OpenQuestionCount: 3},
			},
		}}}},
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}}, AgentBoard: fakeBoard{seed}}, assets, nil))
	srv := newBrowserServer(mux)
	defer srv.Close()

	lines := `[...document.querySelectorAll('.session-line')].map(l => l.dataset.session + ':' + (l.querySelector('.badge')?.textContent ?? l.querySelector('.status .visually-hidden').textContent)).join(' ')`
	ivRun(t, ctx, errs, chromedp.EmulateViewport(1400, 900), chromedp.Navigate(srv.URL+"/"), ivPoll(`document.querySelectorAll('.session-line').length === 3`))
	const want = "s-ask:questions s-turn:your turn s-run:running"
	if got := ivEval[string](t, ctx, lines); got != want {
		t.Errorf("first paint lines = %q, want %q", got, want)
	}
	ivRun(t, ctx, errs, ivPoll(`document.querySelector('[data-ssr]') === null`))
	if got := ivEval[string](t, ctx, lines); got != want {
		t.Errorf("live lines = %q, want %q", got, want)
	}
	if got := ivEval[string](t, ctx, `document.querySelector('.session-line[data-session="s-ask"] .badge').dataset.tone`); got != "attention" {
		t.Errorf("the questions chip's tone = %q, want attention", got)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// With another tab in front the Agent tab carries the open question count in a
// badge (named "5 open questions"), which follows the feed; the document title
// leads with the count while questions are open and goes when they are not.
func TestTheAgentTabBadgeAndTheTitleCountTheOpenQuestions(t *testing.T) {
	live := newLiveInterviews()
	live.ask(askedInterview("iv-1", "architect"))
	live.ask(askedInterview("iv-2", "architect"))
	ctx, errs, _, _ := openInterviewPage(t, live, 1700)
	title := ivEval[string](t, ctx, `document.title.replace(/^\(\d+\) /, '')`)
	ivRun(t, ctx, errs, ivPoll(docked), ivPoll(`document.title === '(4) `+title+`'`))
	badge := `(document.querySelector('[role=tab] [data-questions-badge]')?.textContent.replace(/\s+/g, ' ').trim() ?? '')`
	if got := ivEval[string](t, ctx, badge); got != "" {
		t.Errorf("the Agent tab has a badge %q while it is in front: the toggle takes over", got)
	}
	if got := ivEval[string](t, ctx, `document.title`); got != "(4) "+title {
		t.Errorf("title = %q, want (4) %s", got, title)
	}

	// Another tab in front.
	ivRun(t, ctx, errs, clickIn(`[role=tab]:nth-child(2)`), ivPoll(`document.querySelector('[role=tab][aria-selected=true]')?.textContent.trim() !== 'Agent'`), settle())
	ivRun(t, ctx, errs, ivPoll(badge+` === '4 open questions 4'`))
	if got := ivEval[string](t, ctx, `document.querySelector('[role=tab] [data-questions-badge]').title`); got != "4 open questions" {
		t.Errorf("badge title = %q", got)
	}
	if n := ivEval[float64](t, ctx, `document.querySelectorAll('[role=tab] > .status[data-dot-only]').length`); n != 0 {
		t.Errorf("a plain dot beside the badge, which already holds one")
	}
	if !ivEval[bool](t, ctx, `!!document.querySelector('[role=tab] [data-questions-badge] .status[data-state=waiting]')`) {
		t.Error("the badge holds no waiting dot")
	}
	shoot(t, ctx, "interview-tab-badge")

	// What arrives meanwhile is counted: the panel is not mounted on this tab.
	live.ask(askedInterview("iv-3", "architect"))
	ivRun(t, ctx, errs, ivPoll(badge+` === '6 open questions 6'`))
	ivRun(t, ctx, errs, ivPoll(`document.title === '(6) `+title+`'`))
	live.change(closedInterview("iv-1", domain.InterviewSent, time.Now()))
	ivRun(t, ctx, errs, ivPoll(badge+` === '4 open questions 4'`), ivPoll(`document.title === '(4) `+title+`'`))

	// Back on the Agent tab the toggle takes over.
	ivRun(t, ctx, errs, clickIn(`[role=tab]:nth-child(1)`), ivPoll(`!document.querySelector('[role=tab] [data-questions-badge]')`), ivPoll(`!!`+toggleEl))
	if got := ivEval[string](t, ctx, toggleEl+`.getAttribute('aria-label')`); got != "Questions, 4 open" {
		t.Errorf("toggle = %q", got)
	}

	// Nothing open: the title is the page's again.
	live.change(closedInterview("iv-2", domain.InterviewDismissed, time.Now()))
	live.change(closedInterview("iv-3", domain.InterviewSent, time.Now()))
	ivRun(t, ctx, errs, ivPoll(`document.title === `+fmt.Sprintf("%q", title)))
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The rail counts a running session with an open interview among what needs
// the person, live: Needs you grows by one when the session starts asking.
func TestTheRailCountsARunningSessionThatAsksAmongWhatNeedsThePerson(t *testing.T) {
	h := newRailHarness(t)
	var count string
	h.run(t, "the rail",
		chromedp.EmulateViewport(1280, 900),
		h.openLive("/projects/p1/tasks/t-cur"),
		railPoll(`!!document.querySelector('nav[aria-label="Tasks"] .head .button')?.checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] .head .badge').textContent`, &count),
	)
	if count != "2" {
		t.Fatalf("Needs you counts %q before anything asks, want 2", count)
	}
	waitFor(t, "the rail's feed", func() bool { return h.feed.followers() >= 1 })
	h.feed.pushRaw(t, map[string]any{"session": &domain.Session{ID: "s3", ProjectID: "p1", TicketID: "t-run", Status: domain.SessionRunning, OpenInterviewCount: 1, OpenQuestionCount: 3, UpdatedAt: time.Now()}})
	h.run(t, "a session asks", railPoll(`document.querySelector('nav[aria-label="Tasks"] .head .badge').textContent === '3'`))
	h.feed.pushRaw(t, map[string]any{"session": &domain.Session{ID: "s3", ProjectID: "p1", TicketID: "t-run", Status: domain.SessionRunning, UpdatedAt: time.Now()}})
	h.run(t, "answered", railPoll(`document.querySelector('nav[aria-label="Tasks"] .head .badge').textContent === '2'`))
	h.noErrors(t)
}
