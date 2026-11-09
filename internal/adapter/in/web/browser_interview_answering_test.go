package web

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// Answering an open interview in the panel (UX study "Interview panel", the
// anatomy of a question, the footer and the keyboard table), in the browser:
// the footer counts what the person has answered, "Accept all recommended"
// fills the blank questions and sends nothing, a reload keeps what was typed
// and drops what is older than 7 days, number keys, Enter and Esc, and the
// terminal's focus rules while the panel has the focus and after it blurs.

// answeringInterview is five questions: a single choice recommending its
// second option, a multiple choice recommending its first, a single choice
// with no recommendation, a free text, and a free text with a recommended text.
func answeringInterview(id string) *domain.Interview {
	one, zero := 1, 0
	use := "use SSE"
	opts := func(labels ...string) []domain.InterviewOption {
		out := make([]domain.InterviewOption, len(labels))
		for i, l := range labels {
			out[i] = domain.InterviewOption{Label: l}
		}
		return out
	}
	return &domain.Interview{
		ID: id, SessionID: "s1", AgentName: "architect", State: domain.InterviewOpen,
		Questions: []domain.InterviewQuestion{
			{Title: "Which store?", Kind: domain.QuestionSingle, Options: opts("A file", "The database"), Recommended: &domain.Recommended{Option: &one}},
			{Title: "Which parts may we cut?", Kind: domain.QuestionMultiple, Options: opts("History", "Notes", "Sheet"), Recommended: &domain.Recommended{Option: &zero}},
			{Title: "Which colour?", Kind: domain.QuestionSingle, Options: opts("Blue", "Green")},
			{Title: "Where is it read?", Kind: domain.QuestionText, Options: []domain.InterviewOption{}},
			{Title: "How is it pushed?", Kind: domain.QuestionText, Options: []domain.InterviewOption{}, Recommended: &domain.Recommended{Text: &use}},
		},
	}
}

const (
	progressEl = `document.querySelector('[data-interview-progress]').textContent`
	sendEl     = `document.querySelector('[data-interview-send]').textContent`
	// noWrites counts what the page sends other than reads, from the moment it is installed.
	watchWrites = `(() => { window.__writes = []; const f = window.fetch; window.fetch = (u, o) => { if (String(o?.method ?? 'GET').toUpperCase() !== 'GET') window.__writes.push(String(u)); return f(u, o); }; })()`
)

// qSel is a selector inside the n-th question (1-based) of the panel's first interview.
func qSel(n int, rest string) string {
	return fmt.Sprintf(`[data-interview-panel] fieldset.interview-question:nth-of-type(%d) %s`, n, rest)
}

// optSel is the row of the option with this value in the n-th question.
func optSel(n int, value string) string {
	return qSel(n, fmt.Sprintf(`label:has(> input[value="%s"])`, value))
}

func panelWithAnswers(t *testing.T, width int64) (*liveInterviews, context.Context, *errorLog, *historyPTY) {
	t.Helper()
	live := newLiveInterviews()
	live.ask(answeringInterview("iv-1"))
	ctx, errs, pty, _ := openInterviewPage(t, live, width)
	ivRun(t, ctx, errs, ivPoll(docked), chromedp.Evaluate(watchWrites, nil))
	return live, ctx, errs, pty
}

// openNote opens question n's note by its summary and waits for its box.
func openNote(n int) chromedp.Action {
	return chromedp.Tasks{
		chromedp.Click(qSel(n, `summary`), chromedp.ByQuery),
		ivPoll(`document.querySelector('` + qSel(n, `textarea[data-interview-note]`) + `').checkVisibility()`),
	}
}

func footerIs(t *testing.T, ctx context.Context, progress, send string) {
	t.Helper()
	got := ivEval[string](t, ctx, `[`+progressEl+`, `+sendEl+`].join(' | ')`)
	if want := progress + " | " + send; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
}

func isAnswered(t *testing.T, ctx context.Context, n int) bool {
	t.Helper()
	return ivEval[bool](t, ctx, fmt.Sprintf(`document.querySelectorAll('[data-interview-panel] fieldset.interview-question')[%d].hasAttribute('data-answered')`, n-1))
}

func writes(t *testing.T, ctx context.Context) []string {
	t.Helper()
	return ivEval[[]string](t, ctx, `window.__writes`)
}

// Choosing an option, accepting the recommendation, Write my own and a note
// each move the footer's counts, the filled number and the Send button; a note
// alone is not an answer; clicking an option never moves the focus into a box
// except Write my own, which focuses its own.
func TestAnsweringMovesTheFooterCountsAndTheSendButton(t *testing.T) {
	_, ctx, errs, _ := panelWithAnswers(t, 1700)
	footerIs(t, ctx, "0 of 5 answered · 5 will be skipped", "Send, all skipped")
	if isAnswered(t, ctx, 1) {
		t.Error("question 1 reads answered with nothing chosen")
	}

	// Choosing an option.
	ivRun(t, ctx, errs, chromedp.Click(optSel(1, "0"), chromedp.ByQuery))
	footerIs(t, ctx, "1 of 5 answered · 4 will be skipped", "Send · 4 skipped")
	if !isAnswered(t, ctx, 1) {
		t.Error("question 1's number did not fill once answered")
	}
	if ivEval[bool](t, ctx, `document.activeElement.tagName === 'TEXTAREA'`) {
		t.Error("clicking an option moved the focus into a text box")
	}

	// Accepting the recommendation (the first row of a multiple choice): one click.
	ivRun(t, ctx, errs, chromedp.Click(optSel(2, "0"), chromedp.ByQuery))
	footerIs(t, ctx, "2 of 5 answered · 3 will be skipped", "Send · 3 skipped")
	if !ivEval[bool](t, ctx, `document.querySelector('`+optSel(2, "0")+` input').checked`) {
		t.Error("the recommended option is not ticked after a click")
	}

	// Write my own: its box appears and has the focus; it counts once it holds text.
	ivRun(t, ctx, errs, chromedp.Click(optSel(3, "own"), chromedp.ByQuery), ivPoll(`document.activeElement.matches('`+qSel(3, `textarea[data-interview-own]`)+`')`))
	footerIs(t, ctx, "2 of 5 answered · 3 will be skipped", "Send · 3 skipped")
	ivRun(t, ctx, errs, chromedp.SendKeys(qSel(3, `textarea[data-interview-own]`), "Teal", chromedp.ByQuery))
	footerIs(t, ctx, "3 of 5 answered · 2 will be skipped", "Send · 2 skipped")

	// A note alone is not an answer; it opens by itself once it holds text.
	if ivEval[bool](t, ctx, `document.querySelector('`+qSel(4, `details`)+`').open`) {
		t.Error("the note is open before anything is written in it")
	}
	ivRun(t, ctx, errs, openNote(4), chromedp.SendKeys(qSel(4, `textarea[data-interview-note]`), "careful", chromedp.ByQuery))
	footerIs(t, ctx, "3 of 5 answered · 2 will be skipped", "Send · 2 skipped")
	if isAnswered(t, ctx, 4) {
		t.Error("a note alone made question 4 read answered")
	}
	ivRun(t, ctx, errs, chromedp.SendKeys(qSel(4, `textarea[data-interview-text]`), "in the web UI", chromedp.ByQuery))
	footerIs(t, ctx, "4 of 5 answered · 1 will be skipped", "Send · 1 skipped")

	// A text recommendation is used with one click; everything answered says so.
	ivRun(t, ctx, errs, chromedp.Click(qSel(5, `.recommendation .button`), chromedp.ByQuery))
	footerIs(t, ctx, "5 of 5 answered", "Send answers")
	if got := ivEval[string](t, ctx, `document.querySelector('`+qSel(5, `textarea[data-interview-text]`)+`').value`); got != "use SSE" {
		t.Errorf("Use this text put %q in the box", got)
	}

	// Sending is a later change: the buttons are there, and nothing was sent.
	ivRun(t, ctx, errs, chromedp.Click(`[data-interview-send]`, chromedp.ByQuery), chromedp.Click(`[data-interview-dismiss]`, chromedp.ByQuery), settle())
	if w := writes(t, ctx); len(w) != 0 {
		t.Errorf("the page sent %v", w)
	}
	if n := ivEval[float64](t, ctx, `document.querySelectorAll('[data-interview-panel]').length`); n != 1 {
		t.Error("Send or Dismiss closed the panel")
	}
	shoot(t, ctx, "interview-answered")
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// "Accept all recommended" fills the blank questions that have a recommendation
// (an option, a ticked option, a text), leaves an answer the person gave and a
// question without one alone, and sends nothing.
func TestAcceptAllRecommendedFillsOnlyTheBlankQuestionsAndSendsNothing(t *testing.T) {
	_, ctx, errs, _ := panelWithAnswers(t, 1700)
	if got := ivEval[string](t, ctx, `document.querySelector('[data-interview-panel] .bulk').textContent.replace(/\s+/g, ' ').trim()`); got != "3 recommended Accept all recommended" {
		t.Errorf("bulk line = %q", got)
	}
	ivRun(t, ctx, errs, chromedp.Click(optSel(1, "0"), chromedp.ByQuery), chromedp.Click(`[data-interview-panel] .bulk .button`, chromedp.ByQuery), settle())

	got := ivEval[map[string]any](t, ctx, `(() => {
		const q = (n) => document.querySelectorAll('[data-interview-panel] fieldset.interview-question')[n];
		return {
			one: [...q(0).querySelectorAll('input:checked')].map(i => i.value).join(','),
			two: [...q(1).querySelectorAll('input:checked')].map(i => i.value).join(','),
			three: q(2).querySelectorAll('input:checked').length,
			four: q(3).querySelector('textarea').value,
			five: q(4).querySelector('textarea').value,
		};
	})()`)
	want := map[string]any{"one": "0", "two": "0", "three": float64(0), "four": "", "five": "use SSE"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("question %s = %v, want %v", k, got[k], v)
		}
	}
	footerIs(t, ctx, "3 of 5 answered · 2 will be skipped", "Send · 2 skipped")
	if w := writes(t, ctx); len(w) != 0 {
		t.Errorf("Accept all recommended sent %v", w)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// What was typed is kept in this browser per interview: a reload brings it
// back, an entry older than 7 days is dropped (and swept), a newer one stays.
func TestTypedAnswersSurviveAReloadAndDraftsOlderThanSevenDaysAreDropped(t *testing.T) {
	_, ctx, errs, _ := panelWithAnswers(t, 1700)
	ivRun(t, ctx, errs,
		chromedp.Click(optSel(1, "0"), chromedp.ByQuery),
		chromedp.Click(optSel(3, "own"), chromedp.ByQuery),
		ivPoll(`document.activeElement.matches('`+qSel(3, `textarea[data-interview-own]`)+`')`),
		chromedp.SendKeys(qSel(3, `textarea[data-interview-own]`), "Teal", chromedp.ByQuery),
		chromedp.SendKeys(qSel(5, `textarea[data-interview-text]`), "by push", chromedp.ByQuery),
		openNote(5),
		chromedp.SendKeys(qSel(5, `textarea[data-interview-note]`), "soon", chromedp.ByQuery),
	)
	footerIs(t, ctx, "3 of 5 answered · 2 will be skipped", "Send · 2 skipped")

	reload := func() {
		ivRun(t, ctx, errs, chromedp.Reload(), ivPoll(terminalLive), ivPoll(`document.querySelector('[data-ssr]') === null`), ivPoll(docked), settle())
	}
	reload()
	footerIs(t, ctx, "3 of 5 answered · 2 will be skipped", "Send · 2 skipped")
	got := ivEval[map[string]any](t, ctx, `(() => {
		const q = (n) => document.querySelectorAll('[data-interview-panel] fieldset.interview-question')[n];
		return {
			one: q(0).querySelector('input:checked')?.value,
			own: q(2).querySelector('textarea[data-interview-own]')?.value,
			ownChecked: q(2).querySelector('input:checked')?.value,
			text: q(4).querySelector('textarea[data-interview-text]').value,
			note: q(4).querySelector('textarea[data-interview-note]').value,
			noteOpen: q(4).querySelector('details').open,
			answered: q(0).hasAttribute('data-answered') && q(2).hasAttribute('data-answered'),
		};
	})()`)
	want := map[string]any{"one": "0", "own": "Teal", "ownChecked": "own", "text": "by push", "note": "soon", "noteOpen": true, "answered": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("after the reload %s = %v, want %v", k, got[k], v)
		}
	}

	// Entries older than 7 days: this interview's draft is dropped on read, a stale one for
	// another interview is swept, a fresh one for another stays.
	ivRun(t, ctx, errs, chromedp.Evaluate(`(() => {
		const key = 'harness:interview-draft:iv-1';
		const answers = JSON.parse(localStorage.getItem(key)).answers;
		const day = 86400000;
		localStorage.setItem(key, JSON.stringify({ at: Date.now() - 8 * day, answers }));
		localStorage.setItem('harness:interview-draft:gone', JSON.stringify({ at: Date.now() - 9 * day, answers }));
		localStorage.setItem('harness:interview-draft:other', JSON.stringify({ at: Date.now() - 6 * day, answers }));
	})()`, nil))
	reload()
	footerIs(t, ctx, "0 of 5 answered · 5 will be skipped", "Send, all skipped")
	keys := ivEval[string](t, ctx, `Object.keys(localStorage).filter(k => k.startsWith('harness:interview-draft:')).sort().join(',')`)
	if keys != "harness:interview-draft:other" {
		t.Errorf("drafts left after the reload = %q, want only the fresh one for another interview", keys)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Number keys select the row in the order drawn (the recommendation is 1), Enter
// on the recommendation selects it and moves to the next question, a digit in a
// text box is typed, and Esc in a text box returns to the option group and never
// closes the panel.
func TestNumberKeysEnterAndEscFollowTheStudy(t *testing.T) {
	_, ctx, errs, _ := panelWithAnswers(t, 1700)
	checked := func(n int) string {
		return ivEval[string](t, ctx, fmt.Sprintf(`[...document.querySelectorAll('[data-interview-panel] fieldset.interview-question')[%d].querySelectorAll('input:checked')].map(i => i.value).join(',')`, n-1))
	}
	focusRow := func(n int, value string) chromedp.Action {
		return chromedp.Focus(qSel(n, fmt.Sprintf(`input[value="%s"]`, value)), chromedp.ByQuery)
	}

	// Drawn order of question 1: the database (value 1) is "1", the file (value 0) is "2".
	ivRun(t, ctx, errs, focusRow(1, "1"), chromedp.KeyEvent("2"))
	if got := checked(1); got != "0" {
		t.Errorf("key 2 selected %q, want the second row drawn (value 0)", got)
	}
	if !ivEval[bool](t, ctx, `document.activeElement.value === '0'`) {
		t.Error("key 2 did not move the focus to the row it selected")
	}
	ivRun(t, ctx, errs, chromedp.KeyEvent("1"))
	if got := checked(1); got != "1" {
		t.Errorf("key 1 selected %q, want the recommendation (value 1)", got)
	}
	ivRun(t, ctx, errs, chromedp.KeyEvent("9"), chromedp.KeyEvent("3"))
	if got := checked(1); got != "1" {
		t.Errorf("keys with no row changed the choice to %q", got)
	}
	footerIs(t, ctx, "1 of 5 answered · 4 will be skipped", "Send · 4 skipped")

	// Enter on the recommendation selects it and moves to the next question's group.
	ivRun(t, ctx, errs, focusRow(1, "0"), chromedp.KeyEvent("2"), focusRow(1, "1"), chromedp.KeyEvent("\r"))
	if got := checked(1); got != "1" {
		t.Errorf("Enter on the recommendation left %q selected", got)
	}
	if !ivEval[bool](t, ctx, `document.activeElement.matches('`+qSel(2, `input`)+`')`) {
		t.Error("Enter on the recommendation did not move the focus to the next question's options")
	}

	// A digit in a text box is text; Esc returns to the option group, the panel stays.
	ivRun(t, ctx, errs, chromedp.Click(optSel(3, "own"), chromedp.ByQuery), ivPoll(`document.activeElement.matches('`+qSel(3, `textarea[data-interview-own]`)+`')`), chromedp.KeyEvent("1"))
	if got := ivEval[string](t, ctx, `document.activeElement.value`); got != "1" {
		t.Errorf("a digit in the text box made %q, want it typed", got)
	}
	if got := checked(3); got != "own" {
		t.Errorf("a digit in the text box selected %q", got)
	}
	ivRun(t, ctx, errs, chromedp.KeyEvent("\x1b"), settle())
	if !ivEval[bool](t, ctx, `document.activeElement.matches('`+qSel(3, `input`)+`') && document.activeElement.value === 'own'`) {
		t.Error("Esc in the text box did not return the focus to the question's option group")
	}
	if !ivEval[bool](t, ctx, docked) {
		t.Error("Esc closed the panel")
	}
	if ivEval[bool](t, ctx, `document.querySelector('[data-interview-toggle]').getAttribute('aria-pressed') !== 'true'`) {
		t.Error("Esc hid the panel")
	}
	if got := ivEval[string](t, ctx, `document.querySelector('`+qSel(3, `textarea[data-interview-own]`)+`').value`); got != "1" {
		t.Errorf("Esc lost what was typed: %q", got)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// In the sheet, Esc in a text box only goes back to the options; Esc there
// closes the sheet.
func TestEscInATextBoxInTheSheetReturnsToTheOptionsAndASecondEscClosesTheSheet(t *testing.T) {
	live := newLiveInterviews()
	live.ask(answeringInterview("iv-1"))
	ctx, errs, _, _ := openInterviewPage(t, live, 1280)
	ivRun(t, ctx, errs, chromedp.Evaluate(peekEl+`.querySelector('button').click()`, nil), ivPoll(`!!`+sheetEl))
	// Clicks in the modal sheet are the page's own: a selector wait hangs under a modal dialog.
	ivRun(t, ctx, errs, chromedp.Evaluate(`document.querySelector('`+optSel(3, "own")+` input').click()`, nil), ivPoll(`document.activeElement.matches('`+qSel(3, `textarea[data-interview-own]`)+`')`), chromedp.KeyEvent("\x1b"), settle())
	if !ivEval[bool](t, ctx, `!!`+sheetEl+` && document.activeElement.matches('`+qSel(3, `input`)+`')`) {
		t.Error("Esc in a text box did not stay in the sheet on the question's options")
	}
	ivRun(t, ctx, errs, chromedp.KeyEvent("\x1b"), ivPoll(`!`+sheetEl))
}

// Alt+Shift+Q goes to the first unanswered question, the first when all are answered.
func TestAltShiftQFocusesTheFirstUnansweredQuestion(t *testing.T) {
	_, ctx, errs, _ := panelWithAnswers(t, 1700)
	ivRun(t, ctx, errs, chromedp.Click(optSel(1, "0"), chromedp.ByQuery), chromedp.Click(optSel(2, "1"), chromedp.ByQuery),
		chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery), pressAltShiftQ(), ivPoll(`document.activeElement.matches('`+qSel(3, `input`)+`')`))
	if !ivEval[bool](t, ctx, `document.activeElement.value === '0'`) {
		t.Error("the focus is not on the first option of question 3")
	}
	ivRun(t, ctx, errs, pressAltShiftQ(), ivPoll(`document.activeElement.classList.contains('xterm-helper-textarea')`),
		chromedp.Click(optSel(3, "0"), chromedp.ByQuery), chromedp.SendKeys(qSel(4, `textarea[data-interview-text]`), "x", chromedp.ByQuery), chromedp.SendKeys(qSel(5, `textarea[data-interview-text]`), "y", chromedp.ByQuery),
		chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery), pressAltShiftQ(), ivPoll(`document.activeElement.matches('`+qSel(1, `input`)+`')`))
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The terminal's focus rules hold with the panel: output and a resize never
// move the focus off a panel control (F1), typing in the panel does not reach
// the agent, a blurred panel does not hand the focus to the terminal (F2), and
// a focused terminal stays focused through what the panel does (F3).
func TestTerminalFocusRulesHoldWhileThePanelHasFocusAndAfterItBlurs(t *testing.T) {
	_, ctx, errs, pty := panelWithAnswers(t, 1700)
	onTerminal := `document.activeElement.classList.contains('xterm-helper-textarea')`

	// F1, focus in the panel.
	ivRun(t, ctx, errs, chromedp.Click(optSel(3, "own"), chromedp.ByQuery), ivPoll(`document.activeElement.matches('`+qSel(3, `textarea[data-interview-own]`)+`')`), chromedp.Evaluate(`window.__kept = document.activeElement`, nil))
	pty.print <- "output while the panel has focus\r\n"
	ivRun(t, ctx, errs, chromedp.EmulateViewport(1640, 800), settle())
	pty.print <- "more output\r\n"
	ivRun(t, ctx, errs, chromedp.EmulateViewport(1700, 800), settle())
	if !ivEval[bool](t, ctx, `document.activeElement === window.__kept`) {
		t.Error("output or a resize moved the focus off the panel's text box (F1)")
	}
	ivRun(t, ctx, errs, chromedp.SendKeys(qSel(3, `textarea[data-interview-own]`), "typed", chromedp.ByQuery), settle())
	if n := keysTo(pty); n != 0 {
		t.Errorf("typing in the panel sent %d keys to the agent", n)
	}

	// F2, after it blurs: nothing hands the focus to the terminal.
	ivRun(t, ctx, errs, chromedp.Evaluate(`document.activeElement.blur()`, nil))
	pty.print <- "output after the blur\r\n"
	ivRun(t, ctx, errs, chromedp.EmulateViewport(1640, 800), settle(), chromedp.EmulateViewport(1700, 800), settle())
	if ivEval[bool](t, ctx, onTerminal) || ivEval[bool](t, ctx, `!!document.activeElement.closest('.terminal')`) {
		t.Error("the terminal took the focus after the panel blurred (F2)")
	}
	ivRun(t, ctx, errs, chromedp.Click(`.terminal .xterm-screen`, chromedp.ByQuery), ivPoll(onTerminal))

	// F3: a focused terminal stays focused through output, a resize and what the panel does.
	ivRun(t, ctx, errs, chromedp.Evaluate(`document.querySelector('[data-interview-panel] .bulk .button').click()`, nil))
	pty.print <- "output while the terminal has focus\r\n"
	ivRun(t, ctx, errs, chromedp.EmulateViewport(1640, 800), settle())
	if !ivEval[bool](t, ctx, onTerminal) {
		t.Error("the terminal lost the focus to what the panel did (F3)")
	}
	ivRun(t, ctx, errs, chromedp.KeyEvent("k"), settle())
	if n := keysTo(pty); n != 1 {
		t.Errorf("a key typed in the focused terminal reached the agent %d times, want once", n)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
