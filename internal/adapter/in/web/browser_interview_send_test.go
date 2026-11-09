package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// Sending and dismissing an interview from the panel (UX study "Interview
// panel", sections 3, 4 (d, e, f), 5, 8 and 9), in the browser: the exact
// answers posted, the confirm only when something is blank, sending and sent
// with the delivery, a failed send that keeps the answers and cannot send
// twice, the coded refusals, dismiss with an optional note, a send in another
// tab, Send waiting for the stream, and ⌘/Ctrl+Enter.

// answerer is the server's side of Submit and Dismiss for these tests, as the
// service does it: the first answer closes the interview, publishes it on the
// feed (before it answers, as the service does, so the echo can beat the HTTP
// answer) and delivers once; a repeat returns what is stored and delivers
// nothing. gate holds an answer back; failures are returned instead of acting.
type answerer struct {
	live *liveInterviews

	mu       sync.Mutex
	gate     chan struct{}
	failures []error
	// loseAnswer makes the next success act and publish but answer with a 500: the answer was lost on the way back.
	loseAnswer bool
	// delivery is what a send reads at first: queued, or delivered with a time.
	delivery  domain.InterviewDelivery
	calls     []string
	delivered map[string]int
}

func newAnswerer(live *liveInterviews) *answerer {
	return &answerer{live: live, delivery: domain.InterviewQueued, delivered: map[string]int{}}
}

// refusal is a coded refusal carrying the interview as the server has it.
func refusal(code, message string, iv *domain.Interview) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: code, Message: message},
		Details: struct {
			Interview *domain.Interview `json:"interview"`
		}{iv},
	}
}

// failNext queues what the next calls answer instead of acting.
func (a *answerer) failNext(errs ...error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures = append(a.failures, errs...)
}

func (a *answerer) hold() chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gate = make(chan struct{})
	return a.gate
}

func (a *answerer) log() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.calls...)
}

func (a *answerer) deliveries(id string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.delivered[id]
}

func (a *answerer) respond(ctx context.Context, id string, to domain.InterviewState, answers []domain.InterviewAnswer, note string) (*domain.Interview, error) {
	a.mu.Lock()
	a.calls = append(a.calls, string(to)+" "+id)
	gate := a.gate
	a.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	a.mu.Lock()
	if len(a.failures) > 0 {
		err := a.failures[0]
		a.failures = a.failures[1:]
		a.mu.Unlock()
		return nil, err
	}
	lose := a.loseAnswer
	a.loseAnswer = false
	a.mu.Unlock()

	a.live.mu.Lock()
	var current *domain.Interview
	for _, iv := range a.live.list {
		if iv.ID == id {
			current = iv
		}
	}
	a.live.mu.Unlock()
	if current == nil {
		return nil, &domain.StructuredError{Code: "INTERVIEW_NOT_FOUND", Message: "interview " + id + " not found"}
	}
	if current.State != domain.InterviewOpen {
		if current.State == to {
			return current, nil // the same operation again: the stored result, nothing delivered
		}
		return nil, refusal("INTERVIEW_NOT_OPEN", "the interview is "+string(current.State)+", not open", current)
	}
	now := time.Now()
	closed := *current
	closed.State, closed.EndedAt = to, &now
	delivery := a.delivery
	closed.Delivery = &delivery
	if delivery == domain.InterviewDelivered {
		closed.DeliveredAt = &now
	}
	if to == domain.InterviewSent {
		closed.Answers = answers
	} else if note != "" {
		closed.DismissNote = &note
	}
	a.mu.Lock()
	a.delivered[id]++
	a.mu.Unlock()
	a.live.change(&closed)
	if lose {
		return nil, errors.New("the answer was lost on the way back")
	}
	return &closed, nil
}

// bodyTap records what the page posts to the interview answers, as the server read it.
type bodyTap struct {
	mu     sync.Mutex
	bodies []string
}

func (b *bodyTap) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/interviews/") {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			b.mu.Lock()
			b.bodies = append(b.bodies, r.URL.Path+" "+string(body))
			b.mu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

func (b *bodyTap) all() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string{}, b.bodies...)
}

// dropFeed ends the interview feeds followed now and refuses new ones, so the
// page's stream reads as reconnecting; restoreFeed lets it back.
func (l *liveInterviews) dropFeed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.down = true
	for ch := range l.subs {
		close(ch)
		delete(l.subs, ch)
	}
}

func (l *liveInterviews) restoreFeed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.down = false
}

const (
	footEl    = `document.querySelector('[data-interview-card] [data-interview-footer]')`
	statusEl  = `document.querySelector('[data-interview-card] [data-interview-status]')`
	stripEl   = `document.querySelector('[data-interview-card] [data-interview-confirm]')`
	errorEl   = `document.querySelector('[data-interview-card] [data-interview-error]')`
	sendBtn   = `[data-interview-card] [data-interview-send]`
	dismissBt = `[data-interview-card] [data-interview-dismiss]`
	// said keeps what the page's polite region says, whatever it says next.
	watchSaid = `(() => { window.__said = []; const el = document.querySelector('main p[aria-live=polite]'); new MutationObserver(() => { if (el.textContent) window.__said.push(el.textContent); }).observe(el, { childList: true, characterData: true, subtree: true }); })()`
)

// sendPage opens the answering interview iv-1 on a wide page with an answerer
// behind the real HTTP API, a tap on what is posted, and the page's polite
// region watched.
func sendPage(t *testing.T) (*liveInterviews, context.Context, *errorLog, *gatedInterviews, *bodyTap) {
	t.Helper()
	live := newLiveInterviews()
	live.ask(answeringInterview("iv-1"))
	tap := &bodyTap{}
	ctx, errs, _, wire := openInterviewPageWrapped(t, live, 1700, tap.wrap)
	wire.answers = newAnswerer(live)
	ivRun(t, ctx, errs, ivPoll(docked), chromedp.Evaluate(watchSaid, nil))
	return live, ctx, errs, wire, tap
}

// answerFirst answers the first n of the five questions the quick way: the
// recommended or first option of the choices, typed text for the free ones.
func answerFirst(t *testing.T, ctx context.Context, errs *errorLog, n int) {
	t.Helper()
	steps := []chromedp.Action{
		chromedp.Click(optSel(1, "1"), chromedp.ByQuery),
		chromedp.Click(optSel(2, "0"), chromedp.ByQuery),
		chromedp.Click(optSel(3, "0"), chromedp.ByQuery),
		chromedp.SendKeys(qSel(4, `textarea[data-interview-text]`), "in the page seed", chromedp.ByQuery),
		chromedp.Click(qSel(5, `.recommendation button`), chromedp.ByQuery),
	}
	ivRun(t, ctx, errs, steps[:n]...)
}

func clickSend(t *testing.T, ctx context.Context, errs *errorLog) {
	t.Helper()
	ivRun(t, ctx, errs, chromedp.Click(sendBtn, chromedp.ByQuery))
}

func firstRow(js string) string {
	return `(r => r ? ` + js + ` : '')(document.querySelector('[data-interview-panel] [data-interview-row]'))`
}

func rowText(sel string) string {
	return firstRow(`(r.querySelector('` + sel + `')?.textContent ?? '').replace(/\s+/g, ' ').trim()`)
}

func focusIsInPanelNotTerminal(t *testing.T, ctx context.Context, when string) {
	t.Helper()
	if ivEval[bool](t, ctx, termFoc) {
		t.Errorf("%s: the focus is in the terminal", when)
	}
}

// A fully answered interview skips the confirm: Send posts exactly the
// contract's answers, the footer says it is sending, the fields are read-only
// and Dismiss is off while it goes, the card then moves under Earlier at once,
// opened, with the banner and the delivery. The server's echo of the same send
// is not announced as another tab's.
func TestSendingAFullyAnsweredInterviewPostsTheAnswersAndShowsItSentWithItsDelivery(t *testing.T) {
	for _, c := range []struct {
		name     string
		delivery domain.InterviewDelivery
		want     *regexp.Regexp
	}{
		{"queued", domain.InterviewQueued, regexp.MustCompile(`^5 answered · queued: the agent is mid-turn$`)},
		{"delivered", domain.InterviewDelivered, regexp.MustCompile(`^5 answered · delivered \d\d:\d\d$`)},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, ctx, errs, wire, tap := sendPage(t)
			wire.answers.delivery = c.delivery
			answerFirst(t, ctx, errs, 5)
			footerIs(t, ctx, "5 of 5 answered", "Send answers")

			release := wire.answers.hold()
			clickSend(t, ctx, errs)
			ivRun(t, ctx, errs, ivPoll(`(`+statusEl+`)?.textContent === 'Sending your answers…'`))
			sending := ivEval[map[string]any](t, ctx, `(() => {
				const card = document.querySelector('[data-interview-card]');
				const status = card.querySelector('[data-interview-status]');
				return {
					live: status.getAttribute('role') + '/' + status.getAttribute('aria-live'),
					busy: card.querySelector('[data-interview-send]').getAttribute('aria-busy'),
					label: card.querySelector('[data-interview-send]').textContent.trim(),
					sendOff: card.querySelector('[data-interview-send]').disabled,
					dismissOff: card.querySelector('[data-interview-dismiss]').disabled,
					fieldsLocked: [...card.querySelectorAll('fieldset')].every(f => f.disabled),
					confirm: !!card.querySelector('[data-interview-confirm]'),
				};
			})()`)
			want := map[string]any{"live": "status/polite", "busy": "true", "label": "Sending…", "sendOff": true, "dismissOff": true, "fieldsLocked": true, "confirm": false}
			if fmt.Sprint(sending) != fmt.Sprint(want) {
				t.Errorf("while sending: %v, want %v", sending, want)
			}
			focusIsInPanelNotTerminal(t, ctx, "while sending")

			close(release)
			ivRun(t, ctx, errs, ivPoll(rowStates+` === 'sent:opened'`), ivPoll(`!document.querySelector('[data-interview-card]')`))
			if got := ivEval[string](t, ctx, rowText(`[data-interview-sent-banner]`)); got != "Answers sent. The agent reads them as its next message, after its current turn." {
				t.Errorf("banner = %q", got)
			}
			if got := ivEval[string](t, ctx, firstRow(`r.querySelector('.head .text > span').textContent`)); !c.want.MatchString(got) {
				t.Errorf("the folded line = %q, want %v", got, c.want)
			}
			if got := ivEval[string](t, ctx, firstRow(`r.querySelector('.head strong').textContent`)); !strings.HasPrefix(got, "Sent · architect") {
				t.Errorf("row title = %q", got)
			}

			wantBody := `/api/interviews/iv-1/answers {"answers":[{"chosen":[1],"text":"","note":"","skipped":false},{"chosen":[0],"text":"","note":"","skipped":false},{"chosen":[0],"text":"","note":"","skipped":false},{"chosen":[],"text":"in the page seed","note":"","skipped":false},{"chosen":[],"text":"use SSE","note":"","skipped":false}]}`
			if got := tap.all(); len(got) != 1 || strings.TrimSpace(got[0]) != wantBody {
				t.Errorf("posted %q, want one %q", got, wantBody)
			}
			if n := wire.answers.deliveries("iv-1"); n != 1 {
				t.Errorf("%d deliveries", n)
			}
			ivRun(t, ctx, errs, ivPoll(`window.__said.length > 0`))
			said := ivEval[[]string](t, ctx, `window.__said`)
			if fmt.Sprint(said) != "[Answers sent]" {
				t.Errorf("the polite region said %q: this tab's own send is announced once and is never another tab's", said)
			}
			focusIsInPanelNotTerminal(t, ctx, "after sending")
			if e := errs.all(); len(e) > 0 {
				t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
			}
		})
	}
}

// Sending with blanks asks first, in a strip in the footer; nothing is posted
// until the person agrees; Review skipped goes to the first blank question and
// focuses it; Send 5 answers sends them, the blanks as skipped.
func TestSendingWithBlanksAsksFirstAndReviewSkippedFocusesTheFirstBlank(t *testing.T) {
	_, ctx, errs, wire, tap := sendPage(t)
	answerFirst(t, ctx, errs, 2)
	footerIs(t, ctx, "2 of 5 answered · 3 will be skipped", "Send · 3 skipped")

	clickSend(t, ctx, errs)
	ivRun(t, ctx, errs, ivPoll(`!!`+stripEl), ivPoll(`document.activeElement.textContent.trim() === 'Send 5 answers'`))
	strip := ivEval[map[string]any](t, ctx, `(() => {
		const s = `+stripEl+`;
		return {
			role: s.getAttribute('role'),
			text: s.querySelector('p').textContent.replace(/\s+/g, ' ').trim(),
			buttons: [...s.querySelectorAll('button')].map(b => b.textContent.trim()),
			focus: document.activeElement.textContent.trim(),
		};
	})()`)
	if strip["role"] != "alert" || strip["text"] != "Send with 3 skipped? The agent reads “skipped” for those questions. You cannot edit after sending." ||
		fmt.Sprint(strip["buttons"]) != "[Review skipped Send 5 answers]" || strip["focus"] != "Send 5 answers" {
		t.Errorf("the strip = %v", strip)
	}
	if got := tap.all(); len(got) != 0 {
		t.Fatalf("posted before the confirm: %v", got)
	}

	ivRun(t, ctx, errs, chromedp.Click(`[data-interview-review]`, chromedp.ByQuery), ivPoll(`!`+stripEl))
	ivRun(t, ctx, errs, ivPoll(`(f => !!f && f.contains(document.activeElement) && f === document.querySelectorAll('[data-interview-panel] fieldset.interview-question')[2])(document.activeElement.closest('fieldset'))`))
	if got := tap.all(); len(got) != 0 {
		t.Fatalf("Review skipped posted: %v", got)
	}
	if got := ivEval[string](t, ctx, sendEl); got != "Send · 3 skipped" {
		t.Errorf("after reviewing, Send reads %q", got)
	}

	clickSend(t, ctx, errs)
	ivRun(t, ctx, errs, ivPoll(`!!`+stripEl), chromedp.Click(`[data-interview-confirm-send]`, chromedp.ByQuery), ivPoll(rowStates+` === 'sent:opened'`))
	got := tap.all()
	if len(got) != 1 || strings.Count(got[0], `{"chosen":[],"text":"","note":"","skipped":true}`) != 3 {
		t.Errorf("want one post with the 3 blanks as skipped, got %v", got)
	}
	if n := wire.answers.deliveries("iv-1"); n != 1 {
		t.Errorf("%d deliveries", n)
	}
}

// A send the harness never answers keeps the answers and says so with the
// code; Try again sends the same answers and delivers exactly once, even when
// the first attempt did reach the server and only its answer was lost.
func TestAFailedSendKeepsTheAnswersAndTryAgainSendsOnce(t *testing.T) {
	_, ctx, errs, wire, tap := sendPage(t)
	answerFirst(t, ctx, errs, 5)
	wire.answers.failNext(errors.New("boom"))

	clickSend(t, ctx, errs)
	ivRun(t, ctx, errs, ivPoll(`!!`+errorEl), ivPoll(`document.activeElement === document.querySelector('`+sendBtn+`')`))
	failed := ivEval[map[string]any](t, ctx, `(() => {
		const e = `+errorEl+`;
		const card = document.querySelector('[data-interview-card]');
		return {
			role: e.getAttribute('role'), tone: e.dataset.tone,
			text: e.querySelector('span').textContent.replace(/\s+/g, ' ').trim(),
			code: e.querySelector('.code').textContent.trim(),
			primary: card.querySelector('[data-interview-send]').textContent.trim(),
			primaryOn: !card.querySelector('[data-interview-send]').disabled,
			locked: [...card.querySelectorAll('fieldset')].some(f => f.disabled),
			kept: card.querySelectorAll('.interview-question[data-answered]').length,
			focus: document.activeElement === card.querySelector('[data-interview-send]'),
		};
	})()`)
	want := map[string]any{"role": "alert", "tone": "danger", "text": "Could not send your answers. The harness did not answer. Your answers are kept here.", "code": "INTERVIEW_SEND_FAILED · try again", "primary": "Try again", "primaryOn": true, "locked": false, "kept": float64(5), "focus": true}
	if fmt.Sprint(failed) != fmt.Sprint(want) {
		t.Errorf("failed footer = %v, want %v", failed, want)
	}
	if n := wire.answers.deliveries("iv-1"); n != 0 {
		t.Fatalf("a failed send delivered %d", n)
	}

	clickSend(t, ctx, errs)
	ivRun(t, ctx, errs, ivPoll(rowStates+` === 'sent:opened'`))
	if n := wire.answers.deliveries("iv-1"); n != 1 {
		t.Errorf("%d deliveries after Try again, want 1", n)
	}
	posts := tap.all()
	if len(posts) != 2 || posts[0] != posts[1] {
		t.Errorf("the retry is not the same send: %q", posts)
	}
}

// The first attempt reaches the server and only its answer is lost: the feed
// shows the interview sent, and nothing the page does sends it a second time.
func TestASendWhoseAnswerWasLostDoesNotSendTwice(t *testing.T) {
	_, ctx, errs, wire, _ := sendPage(t)
	answerFirst(t, ctx, errs, 5)
	wire.answers.mu.Lock()
	wire.answers.loseAnswer = true
	wire.answers.mu.Unlock()

	clickSend(t, ctx, errs)
	// The answer never comes, so nothing opens the row: the feed alone moved it.
	ivRun(t, ctx, errs, ivPoll(rowStates+` === 'sent:folded'`), settle())
	if n := wire.answers.deliveries("iv-1"); n != 1 {
		t.Errorf("%d deliveries", n)
	}
	if got := ivEval[bool](t, ctx, `!!document.querySelector('[data-interview-card]')`); got {
		t.Error("the sent interview is still open")
	}
	said := ivEval[[]string](t, ctx, `window.__said`)
	for _, s := range said {
		if strings.Contains(s, "another tab") {
			t.Errorf("this tab's own send was announced as another tab's: %q", said)
		}
	}
	if len(wire.answers.log()) != 1 {
		t.Errorf("calls = %v", wire.answers.log())
	}
}

// Dismiss asks first, with an optional note; Keep answering returns the focus
// to Dismiss and loses nothing; Dismiss interview tells the server, and the
// interview reads "Dismissed 12:44 · the agent was told" under Earlier with its
// draft gone. With a note or without.
func TestDismissWithAndWithoutANoteAfterTheConfirm(t *testing.T) {
	for _, c := range []struct{ name, note, body string }{
		{"with a note", "not now", `{"note":"not now"}`},
		{"without a note", "", `{"note":""}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, ctx, errs, wire, tap := sendPage(t)
			wire.answers.delivery = domain.InterviewDelivered
			answerFirst(t, ctx, errs, 2)

			ivRun(t, ctx, errs, chromedp.Click(dismissBt, chromedp.ByQuery), ivPoll(`!!document.querySelector('[data-interview-dismiss-confirm]')`))
			confirm := ivEval[map[string]any](t, ctx, `(() => {
				const s = document.querySelector('[data-interview-dismiss-confirm]');
				return {
					role: s.getAttribute('role'), label: s.getAttribute('aria-label'),
					text: s.querySelector('p').textContent.replace(/\s+/g, ' ').trim(),
					buttons: [...s.querySelectorAll('button')].map(b => b.textContent.trim()),
					danger: s.querySelector('[data-interview-dismiss-go]').dataset.variant,
					noteBox: !!s.querySelector('textarea[data-interview-dismiss-note]'),
				};
			})()`)
			want := map[string]any{"role": "alertdialog", "label": "Dismiss this interview?", "text": "Dismiss this interview? The agent is told you chose not to answer. Your typed answers are discarded. You cannot undo it.",
				"buttons": []any{"Keep answering", "Dismiss interview"}, "danger": "danger", "noteBox": true}
			if fmt.Sprint(confirm) != fmt.Sprint(want) {
				t.Errorf("the confirm = %v, want %v", confirm, want)
			}

			// Keep answering: back to the interview, the focus on Dismiss, the answers intact.
			ivRun(t, ctx, errs, chromedp.Click(`[data-interview-keep]`, chromedp.ByQuery), ivPoll(`!document.querySelector('[data-interview-dismiss-confirm]')`),
				ivPoll(`document.activeElement === document.querySelector('`+dismissBt+`')`))
			footerIs(t, ctx, "2 of 5 answered · 3 will be skipped", "Send · 3 skipped")
			if got := tap.all(); len(got) != 0 {
				t.Fatalf("Keep answering posted: %v", got)
			}

			ivRun(t, ctx, errs, chromedp.Click(dismissBt, chromedp.ByQuery), ivPoll(`!!document.querySelector('[data-interview-dismiss-confirm]')`))
			if c.note != "" {
				ivRun(t, ctx, errs, chromedp.SendKeys(`[data-interview-dismiss-note]`, c.note, chromedp.ByQuery))
			}
			ivRun(t, ctx, errs, chromedp.Click(`[data-interview-dismiss-go]`, chromedp.ByQuery), ivPoll(rowStates+` === 'dismissed:opened'`))
			if got := tap.all(); len(got) != 1 || strings.TrimSpace(got[0]) != "/api/interviews/iv-1/dismiss "+c.body {
				t.Errorf("posted %q, want %s", got, c.body)
			}
			if got := ivEval[string](t, ctx, firstRow(`r.querySelector('.head .text > span').textContent`)); !regexp.MustCompile(`^Dismissed \d\d:\d\d · the agent was told$`).MatchString(got) {
				t.Errorf("the folded line = %q", got)
			}
			if c.note != "" {
				if got := ivEval[string](t, ctx, firstRow(`r.querySelector('.interview-read').innerText.replace(/\s+/g, ' ')`)); !strings.Contains(got, "Your note to the agent "+c.note) {
					t.Errorf("the note is not read back: %q", got)
				}
			}
			if ivEval[bool](t, ctx, `!!document.querySelector('[data-interview-card]')`) {
				t.Error("the dismissed interview is still open")
			}
			if n := wire.answers.deliveries("iv-1"); n != 1 {
				t.Errorf("%d deliveries", n)
			}
			// The draft is gone from this browser.
			if n := ivEval[int](t, ctx, `Object.keys(localStorage).filter(k => k.includes('iv-1')).length`); n != 0 {
				t.Errorf("%d drafts of iv-1 are still kept in this browser", n)
			}
			focusIsInPanelNotTerminal(t, ctx, "after dismissing")
		})
	}
}

// A dismiss that fails reads like a send that failed, keeps the typed answers
// and the note, and Try again dismisses once.
func TestAFailedDismissKeepsTheAnswersAndTheNote(t *testing.T) {
	_, ctx, errs, wire, tap := sendPage(t)
	answerFirst(t, ctx, errs, 2)
	wire.answers.failNext(errors.New("boom"))
	ivRun(t, ctx, errs, chromedp.Click(dismissBt, chromedp.ByQuery), ivPoll(`!!document.querySelector('[data-interview-dismiss-confirm]')`),
		chromedp.SendKeys(`[data-interview-dismiss-note]`, "later", chromedp.ByQuery), chromedp.Click(`[data-interview-dismiss-go]`, chromedp.ByQuery), ivPoll(`!!`+errorEl))
	if got := ivEval[string](t, ctx, `(`+errorEl+`).textContent.replace(/\s+/g, ' ').trim()`); got != "Could not dismiss the interview. The harness did not answer. Your answers are kept here. INTERVIEW_SEND_FAILED · try again" {
		t.Errorf("banner = %q", got)
	}
	if got := ivEval[string](t, ctx, sendEl); got != "Try again" {
		t.Errorf("the primary button reads %q", got)
	}
	clickSend(t, ctx, errs)
	ivRun(t, ctx, errs, ivPoll(rowStates+` === 'dismissed:opened'`))
	posts := tap.all()
	if len(posts) != 2 || posts[0] != posts[1] || !strings.Contains(posts[1], `{"note":"later"}`) {
		t.Errorf("posted %q", posts)
	}
	if n := wire.answers.deliveries("iv-1"); n != 1 {
		t.Errorf("%d deliveries", n)
	}
}

// A send that finds the session ended is refused as expired: the interview
// reads Expired under Earlier with its banner, what was typed read back, and
// Copy answers. One refused as not open shows the state the server says; one
// refused as invalid shows its message and stays open with its answers.
func TestARefusedSendShowsWhatTheServerSays(t *testing.T) {
	t.Run("session ended: expired, with Copy answers", func(t *testing.T) {
		_, ctx, errs, wire, _ := sendPage(t)
		answerFirst(t, ctx, errs, 5)
		ended := time.Now()
		expired := askedExpired("iv-1", ended)
		wire.answers.failNext(refusal("INTERVIEW_EXPIRED", "the session ended before the answer, so the interview expired", expired))
		clickSend(t, ctx, errs)
		ivRun(t, ctx, errs, ivPoll(rowStates+` === 'expired:opened'`))
		got := ivEval[map[string]any](t, ctx, `(() => {
			const r = document.querySelector('[data-interview-panel] [data-interview-row]');
			return {
				banner: r.querySelector('[data-interview-sent-banner]')?.textContent.trim(),
				role: r.querySelector('[data-interview-sent-banner]')?.getAttribute('role'),
				copy: r.querySelector('[data-interview-copy]')?.textContent.trim(),
				answers: [...r.querySelectorAll('.interview-read dd')].map(d => d.textContent.trim()).join(' | '),
				line: r.querySelector('.head .text > span').textContent,
			};
		})()`)
		if got["banner"] != "The session ended before the agent could read your answers. Copy them if you need them." || got["role"] != "alert" || got["copy"] != "Copy answers" ||
			got["answers"] != "The database | History | Blue | in the page seed | use SSE" || !strings.HasSuffix(fmt.Sprint(got["line"]), "the agent never read it") {
			t.Errorf("expired = %v", got)
		}
		if ivEval[bool](t, ctx, `!!document.querySelector('[data-interview-card]')`) {
			t.Error("an expired interview is still open")
		}
	})
	t.Run("not open: the state the server says", func(t *testing.T) {
		_, ctx, errs, wire, _ := sendPage(t)
		answerFirst(t, ctx, errs, 5)
		then := time.Now()
		gone := askedExpired("iv-1", then)
		gone.State = domain.InterviewDismissed
		wire.answers.failNext(refusal("INTERVIEW_NOT_OPEN", "the interview is dismissed, not open", gone))
		clickSend(t, ctx, errs)
		ivRun(t, ctx, errs, ivPoll(rowStates+` === 'dismissed:opened'`))
	})
	t.Run("invalid: the message, the answers kept, no retry loop", func(t *testing.T) {
		_, ctx, errs, wire, _ := sendPage(t)
		answerFirst(t, ctx, errs, 5)
		wire.answers.failNext(&domain.StructuredError{Code: "INTERVIEW_ANSWERS_INVALID", Message: "answer 4 is too long"})
		clickSend(t, ctx, errs)
		ivRun(t, ctx, errs, ivPoll(`!!`+errorEl))
		if got := ivEval[string](t, ctx, `(`+errorEl+`).textContent.replace(/\s+/g, ' ').trim()`); got != "Could not send your answers. answer 4 is too long INTERVIEW_ANSWERS_INVALID" {
			t.Errorf("banner = %q", got)
		}
		if got := ivEval[string](t, ctx, sendEl); got != "Send answers" {
			t.Errorf("the primary button reads %q, want the ordinary Send", got)
		}
		if n := ivEval[int](t, ctx, `document.querySelectorAll('[data-interview-card] .interview-question[data-answered]').length`); n != 5 {
			t.Errorf("%d answers kept", n)
		}
	})
}

// askedExpired is the answering interview as the server reports it after the session ended.
func askedExpired(id string, ended time.Time) *domain.Interview {
	iv := answeringInterview(id)
	iv.State, iv.EndedAt = domain.InterviewExpired, &ended
	return iv
}

// While the page stream is not live, Send is disabled with "Waiting for the
// connection" and sends nothing, but reading and typing go on; it works again
// once the stream is back.
func TestSendWaitsForTheConnectionWhileTheStreamIsReconnecting(t *testing.T) {
	live := newLiveInterviews()
	live.ask(answeringInterview("iv-1"))
	tap := &bodyTap{}
	ctx, errs, _, wire := openInterviewPageWrapped(t, live, 1700, tap.wrap)
	wire.answers = newAnswerer(live)
	ivRun(t, ctx, errs, ivPoll(docked))
	answerFirst(t, ctx, errs, 3)
	if ivEval[bool](t, ctx, `document.querySelector('`+sendBtn+`').disabled`) {
		t.Fatal("Send is disabled with the stream live")
	}

	live.dropFeed()
	ivRun(t, ctx, errs, ivPoll(`(`+statusEl+`)?.textContent === 'Waiting for the connection'`))
	if !ivEval[bool](t, ctx, `document.querySelector('`+sendBtn+`').disabled`) {
		t.Error("Send is enabled while the stream is down")
	}
	// Reading and typing continue.
	ivRun(t, ctx, errs, chromedp.SendKeys(qSel(4, `textarea[data-interview-text]`), "typed while offline", chromedp.ByQuery))
	footerIs(t, ctx, "4 of 5 answered · 1 will be skipped", "Send · 1 skipped")
	if !ivEval[bool](t, ctx, `document.querySelector('`+sendBtn+`').disabled`) {
		t.Error("Send came back while the stream is down")
	}
	// Even a key cannot send it.
	ivRun(t, ctx, errs, pressCtrlEnter(input.ModifierCtrl))
	ivRun(t, ctx, errs, settle())
	if got := tap.all(); len(got) != 0 {
		t.Fatalf("sent into the void: %v", got)
	}

	live.restoreFeed()
	ivRun(t, ctx, errs, chromedp.Poll(`!document.querySelector('`+sendBtn+`').disabled`, nil, chromedp.WithPollingTimeout(20*time.Second)))
	if got := ivEval[string](t, ctx, `(`+statusEl+`).textContent`); got != "" {
		t.Errorf("the reason stays: %q", got)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// bgPoll is ivPoll for a page that may be in a background tab, where animation
// frames do not run: it polls on a timer.
func bgPoll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(8*time.Second), chromedp.WithPollingInterval(50*time.Millisecond))
}

// pressCtrlEnter presses Enter with a modifier (Ctrl, or Meta for ⌘) wherever the focus is.
func pressCtrlEnter(mods input.Modifier) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		key := func(t input.KeyType) *input.DispatchKeyEventParams {
			return input.DispatchKeyEvent(t).WithModifiers(mods).WithKey("Enter").WithCode("Enter").WithWindowsVirtualKeyCode(13)
		}
		if err := key(input.KeyRawDown).Do(ctx); err != nil {
			return err
		}
		return key(input.KeyUp).Do(ctx)
	})
}

// ⌘/Ctrl+Enter from a text box is the Send button: it asks the same confirm
// when something is blank, and sends a fully answered interview.
func TestCommandOrControlEnterFromATextBoxSendsThroughTheSameConfirm(t *testing.T) {
	for _, c := range []struct {
		name string
		mods input.Modifier
	}{{"Ctrl", input.ModifierCtrl}, {"Meta", input.ModifierCommand}} {
		t.Run(c.name, func(t *testing.T) {
			_, ctx, errs, wire, tap := sendPage(t)
			answerFirst(t, ctx, errs, 3)
			// In a text box, with a blank question left: the confirm.
			ivRun(t, ctx, errs, chromedp.Focus(qSel(4, `textarea[data-interview-text]`), chromedp.ByQuery), chromedp.SendKeys(qSel(4, `textarea[data-interview-text]`), "typing", chromedp.ByQuery))
			ivRun(t, ctx, errs, pressCtrlEnter(c.mods), ivPoll(`!!`+stripEl))
			if got := ivEval[string](t, ctx, `(`+stripEl+`).querySelector('strong').textContent`); got != "Send with 1 skipped?" {
				t.Errorf("the strip says %q", got)
			}
			if got := ivEval[string](t, ctx, `document.querySelector('`+qSel(4, `textarea[data-interview-text]`)+`').value`); got != "typing" {
				t.Errorf("the key typed into the box: %q", got)
			}
			if len(tap.all()) != 0 {
				t.Fatalf("posted before the confirm: %v", tap.all())
			}
			// The same key again is the confirm's default button: it sends.
			ivRun(t, ctx, errs, pressCtrlEnter(c.mods), ivPoll(rowStates+` === 'sent:opened'`))
			if n := wire.answers.deliveries("iv-1"); n != 1 || len(tap.all()) != 1 {
				t.Errorf("%d deliveries, posted %v", n, tap.all())
			}
			focusIsInPanelNotTerminal(t, ctx, "after the key")
		})
	}

	t.Run("fully answered: sends at once", func(t *testing.T) {
		_, ctx, errs, wire, _ := sendPage(t)
		answerFirst(t, ctx, errs, 5)
		ivRun(t, ctx, errs, chromedp.Focus(qSel(4, `textarea[data-interview-text]`), chromedp.ByQuery), pressCtrlEnter(input.ModifierCtrl), ivPoll(rowStates+` === 'sent:opened'`))
		if n := wire.answers.deliveries("iv-1"); n != 1 {
			t.Errorf("%d deliveries", n)
		}
	})
}

// A send in one tab appears in a second tab on the same session through the
// feed: the card moves to Earlier there, its draft is dropped, "Interview
// answered in another tab" is announced there and not in the sending tab.
func TestASendInOneTabAppearsInASecondTab(t *testing.T) {
	_, ctx, errs, wire, _ := sendPage(t)
	wire.answers.delivery = domain.InterviewDelivered

	second, cancel := chromedp.NewContext(ctx)
	t.Cleanup(cancel)
	if err := chromedp.Run(second); err != nil {
		t.Fatal(err)
	}
	ivRun(t, second, errs, chromedp.EmulateViewport(1700, 800), chromedp.Navigate(wire.url+"/projects/p1/tasks/t1"), bgPoll(terminalLive), bgPoll(`document.querySelector('[data-ssr]') === null`), bgPoll(docked), chromedp.Evaluate(watchSaid, nil))
	ivRun(t, second, errs, chromedp.Click(optSel(1, "1"), chromedp.ByQuery))
	if !ivEval[bool](t, second, `Object.keys(localStorage).some(k => k.includes('iv-1'))`) {
		t.Fatal("the second tab holds no draft to drop")
	}

	answerFirst(t, ctx, errs, 5)
	clickSend(t, ctx, errs)
	ivRun(t, ctx, errs, bgPoll(rowStates+` === 'sent:opened'`))
	ivRun(t, second, errs, bgPoll(rowStates+` === 'sent:folded'`), bgPoll(`!document.querySelector('[data-interview-card]')`))
	if n := ivEval[int](t, second, `Object.keys(localStorage).filter(k => k.includes('iv-1')).length`); n != 0 {
		t.Errorf("%d drafts of iv-1 left in the second tab", n)
	}
	ivRun(t, second, errs, bgPoll(`window.__said.includes('Interview answered in another tab')`))
	if said := ivEval[[]string](t, ctx, `window.__said`); fmt.Sprint(said) != "[Answers sent]" {
		t.Errorf("the sending tab said %q", said)
	}
	if n := wire.answers.deliveries("iv-1"); n != 1 {
		t.Errorf("%d deliveries", n)
	}
}
