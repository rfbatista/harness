package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// The writing helper (the magic pen) in a real browser, against a fake
// run_writing_helper_step that answers the contract's SSE stream. The step
// endpoint itself belongs to the server; these tests are about the pen, the
// panel and what they do to the field.

// fakeHelperAPI answers POST /api/run_writing_helper_step like the server
// does and records every request. Everything else goes to the ticket API.
type fakeHelperAPI struct {
	*fakeTicketsAPI
	mu    sync.Mutex
	steps []map[string]any
	// refuse answers every step with WRITING_HELPER_UNAVAILABLE.
	refuse bool
	// failDraft makes the next draft end with an error event.
	failDraft bool
	// hold blocks a step until the channel closes (Stop tests).
	hold chan struct{}
	// draft is the text a draft step streams and returns.
	draft string
}

const defaultHelperDraft = "Add a magic pen to the task title and description.\n\nDone when: [open: what done looks like]"

func newFakeHelperAPI() *fakeHelperAPI {
	return &fakeHelperAPI{
		fakeTicketsAPI: &fakeTicketsAPI{sessions: map[string][]map[string]string{}, tickets: map[string]map[string]string{}},
		draft:          defaultHelperDraft,
	}
}

func (f *fakeHelperAPI) requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.steps...)
}

func (f *fakeHelperAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/add_ticket_comment", "/api/update_ticket_comment":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		b, _ := json.Marshal(in)
		f.fakeTicketsAPI.mu.Lock()
		f.fakeTicketsAPI.calls = append(f.fakeTicketsAPI.calls, r.URL.Path+" "+string(b))
		f.fakeTicketsAPI.mu.Unlock()
		id := in["comment_id"]
		if id == "" {
			id = "c-new"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"comment": map[string]any{
			"id": id, "ticket_id": "t-new", "project_id": "p1", "author": "person", "session_id": "", "agent_name": "",
			"body": in["body"], "created_at": time.Now().Format(time.RFC3339), "updated_at": time.Now().Format(time.RFC3339),
		}})
		return
	}
	if r.URL.Path != "/api/run_writing_helper_step" {
		f.fakeTicketsAPI.ServeHTTP(w, r)
		return
	}
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	f.steps = append(f.steps, in)
	refuse, failDraft, hold, draft := f.refuse, f.failDraft, f.hold, f.draft
	if in["step"] == "draft" {
		f.failDraft = false
	}
	f.mu.Unlock()

	if refuse {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "WRITING_HELPER_UNAVAILABLE", "message": "Claude is not signed in", "fix_url": "/agent-isolation#credential"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	event := func(name string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
		flusher.Flush()
	}
	if hold != nil {
		select {
		case <-hold:
		case <-r.Context().Done():
			return
		}
	}
	switch in["step"] {
	case "analyze":
		event("result", map[string]any{"already_good": false, "questions": []map[string]any{
			{"id": "q1", "text": "Which fields get the pen first?", "why": "Your text says titles; the draft should name them.", "chips": []string{"Task title and description", "Every field"}},
			{"id": "q2", "text": "How will you know it is done?", "why": "No done criteria yet.", "chips": []string{"A person drafts it in a minute"}},
		}})
	case "titles":
		n := 0
		if ex, ok := in["exclude"].([]any); ok {
			n = len(ex)
		}
		event("result", map[string]any{"titles": []string{fmt.Sprintf("Title option %d", n+1), fmt.Sprintf("Title option %d", n+2), fmt.Sprintf("Title option %d", n+3)}})
	case "draft":
		if failDraft {
			event("delta", map[string]string{"text": "Add "})
			event("error", map[string]string{"code": "WRITING_HELPER_FAILED", "message": "The helper stopped before it finished."})
			return
		}
		for _, word := range strings.SplitAfter(draft, " ") {
			event("delta", map[string]string{"text": word})
			time.Sleep(5 * time.Millisecond)
		}
		event("result", map[string]string{"text": draft})
	}
}

// helperBrowser serves the pages with the fake step API and the helper
// available (or not).
func helperBrowser(t *testing.T, api *fakeHelperAPI, helper shell.Helper) (context.Context, string, *errorLog) {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks: fakeTickets{[]*domain.Ticket{
			{ID: "t-new", ProjectID: "p1", Title: "Write docs", Description: "The TUI's live section.", Status: domain.TicketStatusTodo},
		}},
		Sessions:     fakeSessions{},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{},
		Resources:    fakeResources{},
		Comments: fakeComments{{
			ID: "c1", TicketID: "t-new", ProjectID: "p1", Author: domain.CommentByPerson, Body: "pen should be visible",
			CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
		}},
		WritingHelper: func(context.Context) shell.Helper { return helper },
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return ctx, srv.URL, errs
}

var whAvailable = shell.Helper{Available: true}

// pressKey dispatches a keydown on the element, as a person's key would.
func whKey(selector, key string, mods string) chromedp.Action {
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		el.focus();
		el.dispatchEvent(new KeyboardEvent('keydown', { key: %q, %s bubbles: true, cancelable: true }));
		return true;
	})()`, selector, key, mods)
	return chromedp.Evaluate(js, nil)
}

const whCtrl = "ctrlKey: true,"

func whPoll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(5*time.Second))
}

// whPanelShown is true when the writing helper panel is on screen.
const whPanelShown = `[...document.querySelectorAll('.helper')].some((p) => getComputedStyle(p).display !== 'none')`

// phaseShown is true when the panel shows a body with the text.
func whPhase(text string) string {
	return fmt.Sprintf(`[...document.querySelectorAll('.helper')].some((p) => getComputedStyle(p).display !== 'none' && Array.from(p.querySelectorAll('.body')).some((b) => getComputedStyle(b).display !== 'none' && b.textContent.includes(%q)))`, text)
}

// clickIn clicks the first visible button in the panel whose text starts with label.
func whClick(scope, label string) chromedp.Action {
	js := fmt.Sprintf(`(() => {
		const b = Array.from(document.querySelectorAll(%q + ' button')).find((x) => x.textContent.trim().startsWith(%q) && x.checkVisibility() && !x.disabled);
		if (!b) return false;
		b.click();
		return true;
	})()`, scope, label)
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(5 * time.Second)
		for {
			var ok bool
			if err := chromedp.Evaluate(js, &ok).Do(ctx); err != nil {
				return err
			}
			if ok {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("no enabled %q button under %s", label, scope)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

// whShot saves the page as name-scheme.png in WRITING_HELPER_SHOTS, once per
// color scheme; without it, it does nothing.
func whShot(name string) chromedp.Action {
	dir := os.Getenv("WRITING_HELPER_SHOTS")
	if dir == "" {
		return chromedp.ActionFunc(func(context.Context) error { return nil })
	}
	var tasks chromedp.Tasks
	for _, scheme := range []string{"light", "dark"} {
		var buf []byte
		tasks = append(tasks,
			emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}, {Name: "prefers-reduced-motion", Value: "reduce"}}),
			chromedp.Sleep(150*time.Millisecond),
			chromedp.CaptureScreenshot(&buf),
			chromedp.ActionFunc(func(context.Context) error {
				return os.WriteFile(filepath.Join(dir, name+"-"+scheme+".png"), buf, 0o644)
			}),
		)
	}
	return tasks
}

func whPenReady(selector string) chromedp.Action {
	return whPoll(fmt.Sprintf(`!!document.querySelector(%q)?._x_dataStack`, selector))
}

func TestWritingHelperDescriptionFlowInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var label, haspopup, expanded, tip string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		chromedp.WaitVisible(`#task-description`, chromedp.ByQuery),
		whPenReady(`[data-panel-id=pen-task-description]`),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').getAttribute('aria-label')`, &label),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').getAttribute('aria-haspopup')`, &haspopup),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').title`, &tip),
		setField(`#task-title`, "Add helper", "input"),
		setField(`#task-description`, "rough text", "input"),
		whShot("pen-rest"),
	)
	if err != nil {
		t.Fatalf("pen: %v\nJS errors: %v", err, errs.all())
	}
	if label != "Write the description with the helper" || haspopup != "dialog" || expanded != "false" || tip != "Write the description with the helper ⌘J" {
		t.Errorf("pen: label %q, haspopup %q, expanded %q, title %q", label, haspopup, expanded, tip)
	}

	// The pen opens the panel, which reads and asks.
	var reads string
	var open string
	err = chromedp.Run(ctx,
		chromedp.Click(`#task-description ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("Which fields get the pen first?")),
		whShot("questions"),
		chromedp.Evaluate(`[...document.querySelectorAll('#pen-task-description-panel .reads > span')].map((e) => e.textContent).join(' ')`, &reads),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').getAttribute('aria-expanded')`, &open),
	)
	if err != nil {
		t.Fatalf("open: %v\nJS errors: %v", err, errs.all())
	}
	if reads != "Reads Description Title project coding_pool" {
		t.Errorf("reads = %q", reads)
	}
	if open != "true" {
		t.Errorf("aria-expanded = %q after opening", open)
	}
	if steps := api.requests(); len(steps) != 1 || steps[0]["step"] != "analyze" {
		t.Fatalf("steps = %v, want one analyze", steps)
	}

	// Answer one with its chip, one by typing, and draft.
	var count, field string
	err = chromedp.Run(ctx,
		whClick(`#pen-task-description-panel`, "Task title and description"),
		whPoll(`[...document.querySelectorAll('#pen-task-description-panel footer .hint')].find((e) => e.checkVisibility())?.textContent.includes('1 of 2 answered')`),
		chromedp.Focus(`#pen-task-description-panel .ask:nth-of-type(2) input`, chromedp.ByQuery),
		chromedp.SendKeys(`#pen-task-description-panel .ask:nth-of-type(2) input`, "A person drafts it in a minute", chromedp.ByQuery),
		whPoll(`[...document.querySelectorAll('#pen-task-description-panel footer .hint')].find((e) => e.checkVisibility())?.textContent.includes('2 of 2 answered')`),
		chromedp.Evaluate(`[...document.querySelectorAll('#pen-task-description-panel footer .hint')].find((e) => e.checkVisibility())?.textContent.trim()`, &count),
		whClick(`#pen-task-description-panel`, "Draft"),
		whPoll(whPhase("Proposed description")),
		whShot("proposal"),
		chromedp.Evaluate(`document.querySelector('#task-description').value`, &field),
	)
	if err != nil {
		t.Fatalf("draft: %v\nJS errors: %v", err, errs.all())
	}
	if field != "rough text" {
		t.Errorf("the field changed before Use this: %q", field)
	}
	steps := api.requests()
	if len(steps) != 2 || steps[1]["step"] != "draft" {
		t.Fatalf("steps = %v, want analyze then draft", steps)
	}
	answers, _ := steps[1]["answers"].([]any)
	if len(answers) != 2 {
		t.Errorf("answers = %v", steps[1]["answers"])
	}
	if sib, _ := steps[1]["siblings"].([]any); len(sib) != 1 {
		t.Errorf("siblings = %v", steps[1]["siblings"])
	}

	// The proposal is marked against the field: insertions, removals and the open gap.
	var marks string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const d = document.querySelector('#pen-task-description-panel .draft'); return [d.querySelectorAll('ins').length > 0, d.querySelectorAll('del').length > 0, d.querySelector('.gap')?.textContent].join('|'); })()`, &marks),
	)
	if err != nil {
		t.Fatal(err)
	}
	if marks != "true|true|[open: what done looks like]" {
		t.Errorf("marks = %q", marks)
	}

	// Use this: the panel closes, the field takes the draft, focus returns, a toast offers Undo.
	var after, active, toast string
	var ticketBody, at string
	err = chromedp.Run(ctx,
		whClick(`#pen-task-description-panel`, "Use this"),
		whPoll(`!`+whPanelShown),
		chromedp.Evaluate(`document.querySelector('#task-description').value`, &after),
		chromedp.Evaluate(`document.activeElement.id`, &active),
		whPoll(`!!document.querySelector('#pen-task-description-panel ~ .toast-region .toast')?.checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('#pen-task-description-panel ~ .toast-region .toast').textContent.replace(/\s+/g, ' ').trim()`, &toast),
		whShot("used"),
	)
	if err != nil {
		t.Fatalf("use: %v\nJS errors: %v", err, errs.all())
	}
	if after != defaultHelperDraft {
		t.Errorf("field = %q, want the draft", after)
	}
	if active != "task-description" {
		t.Errorf("focus = %q, want the field", active)
	}
	if !strings.Contains(toast, "Description replaced by the helper's draft") || !strings.Contains(toast, "Undo") {
		t.Errorf("toast = %q", toast)
	}

	// The form's own state followed the write.
	err = chromedp.Run(ctx,
		clickButton(`main form`, "Create task"),
		waitForPath(url+"/projects/p1/tasks/t-new", &at),
	)
	if err != nil {
		t.Fatalf("create: %v\nJS errors: %v", err, errs.all())
	}
	ticketBody = api.log()
	if !strings.Contains(ticketBody, `"description":"Add a magic pen to the task title and description.\n\nDone when: [open: what done looks like]"`) {
		t.Errorf("the created task did not take the helper's text:\n%s", ticketBody)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperUndoRestoresTheFieldInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var undone string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPenReady(`[data-panel-id=pen-task-description]`),
		setField(`#task-description`, "rough text", "input"),
		whKey(`#task-description`, "j", whCtrl),
		whPoll(whPhase("How will you know")),
		whClick(`#pen-task-description-panel`, "Skip questions"),
		whPoll(whPhase("Proposed description")),
		whKey(`#task-description`, "Enter", `metaKey: true,`),
		whPoll(`!`+whPanelShown),
		whPoll(`document.querySelector('#task-description').value.startsWith('Add a magic pen')`),
		whClick(`#pen-task-description-panel ~ .toast-region`, "Undo"),
		whPoll(`document.querySelector('#task-description').value === 'rough text'`),
		chromedp.Evaluate(`document.querySelector('#task-description').value`, &undone),
		whPoll(`!document.querySelector('#pen-task-description-panel ~ .toast-region .toast')?.checkVisibility()`),
	)
	if err != nil {
		t.Fatalf("undo: %v\nJS errors: %v", err, errs.all())
	}
	if undone != "rough text" {
		t.Errorf("after Undo the field is %q", undone)
	}
	// Skip questions sent a draft with no answers.
	steps := api.requests()
	if len(steps) != 2 || steps[1]["step"] != "draft" || steps[1]["answers"] != nil {
		t.Errorf("steps = %v", steps)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperLeavesTheFieldAloneInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var field, active string
	var stale bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPenReady(`[data-panel-id=pen-task-description]`),
		setField(`#task-description`, "rough text", "input"),

		// Esc closes without changing the field, and hands the focus back.
		whKey(`#task-description`, "j", whCtrl),
		whPoll(whPhase("Which fields get the pen first?")),
		whKey(`#pen-task-description-panel .ask input`, "Escape", ""),
		whPoll(`!`+whPanelShown),
		chromedp.Evaluate(`document.querySelector('#task-description').value`, &field),
		chromedp.Evaluate(`document.activeElement.id`, &active),
	)
	if err != nil {
		t.Fatalf("esc: %v\nJS errors: %v", err, errs.all())
	}
	if field != "rough text" || active != "task-description" {
		t.Errorf("after Esc: field %q, focus %q", field, active)
	}

	// Typing after a draft marks it stale; Use anyway still applies it.
	err = chromedp.Run(ctx,
		whKey(`#task-description`, "j", whCtrl),
		whPoll(whPhase("How will you know")),
		whClick(`#pen-task-description-panel`, "Skip questions"),
		whPoll(whPhase("Proposed description")),
		setField(`#task-description`, "rough text, edited", "input"),
		whPoll(whPhase("You changed the description after this draft")),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('#pen-task-description-panel .draft[data-stale]')).opacity === '0.6'`, &stale),
		whClick(`#pen-task-description-panel`, "Use anyway"),
		whPoll(`!`+whPanelShown),
		whPoll(`document.querySelector('#task-description').value.startsWith('Add a magic pen')`),
	)
	if err != nil {
		t.Fatalf("stale: %v\nJS errors: %v", err, errs.all())
	}
	if !stale {
		t.Error("a stale draft is not dimmed")
	}

	// Redraft with my edits sends a fresh draft from the field as it is now.
	err = chromedp.Run(ctx,
		setField(`#task-description`, "second text", "input"),
		whKey(`#task-description`, "j", whCtrl),
		whPoll(whPhase("How will you know")),
		whClick(`#pen-task-description-panel`, "Skip questions"),
		whPoll(whPhase("Proposed description")),
		setField(`#task-description`, "second text edited", "input"),
		whPoll(whPhase("You changed the description")),
		whClick(`#pen-task-description-panel`, "Redraft with my edits"),
		whPoll(whPhase("Proposed description")),
	)
	if err != nil {
		t.Fatalf("redraft: %v\nJS errors: %v", err, errs.all())
	}
	last := api.requests()[len(api.requests())-1]
	if f, _ := last["field"].(map[string]any); f["text"] != "second text edited" || last["step"] != "draft" {
		t.Errorf("the redraft sent %v", last)
	}

	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperFailureKeepsAnswersInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var banner, field string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPenReady(`[data-panel-id=pen-task-description]`),
		setField(`#task-description`, "rough text", "input"),
		whKey(`#task-description`, "j", whCtrl),
		whPoll(whPhase("Which fields get the pen first?")),
		whClick(`#pen-task-description-panel`, "Every field"),
	)
	if err != nil {
		t.Fatalf("setup: %v\nJS errors: %v", err, errs.all())
	}
	api.mu.Lock()
	api.failDraft = true
	api.mu.Unlock()
	err = chromedp.Run(ctx,
		whClick(`#pen-task-description-panel`, "Draft"),
		whPoll(whPhase("WRITING_HELPER_FAILED")),
		chromedp.Evaluate(`[...document.querySelectorAll('#pen-task-description-panel [role=alert]')].find((e) => e.checkVisibility()).textContent.replace(/\s+/g, ' ').trim()`, &banner),
		chromedp.Evaluate(`document.querySelector('#task-description').value`, &field),
		whClick(`#pen-task-description-panel`, "Try again"),
		whPoll(whPhase("Proposed description")),
	)
	if err != nil {
		t.Fatalf("failure: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(banner, "The helper stopped before it finished.") || !strings.Contains(banner, "Your description is unchanged.") || field != "rough text" {
		t.Errorf("banner %q, field %q", banner, field)
	}
	// The retry sent the same answers again.
	steps := api.requests()
	if len(steps) != 3 {
		t.Fatalf("steps = %d, want analyze, failed draft and retry", len(steps))
	}
	if fmt.Sprint(steps[1]["answers"]) != fmt.Sprint(steps[2]["answers"]) || steps[2]["answers"] == nil {
		t.Errorf("answers were not kept: %v then %v", steps[1]["answers"], steps[2]["answers"])
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperStopWhileDraftingInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPenReady(`[data-panel-id=pen-task-description]`),
		setField(`#task-description`, "rough text", "input"),
		whKey(`#task-description`, "j", whCtrl),
		whPoll(whPhase("Which fields get the pen first?")),
	)
	if err != nil {
		t.Fatalf("setup: %v\nJS errors: %v", err, errs.all())
	}
	hold := make(chan struct{})
	api.mu.Lock()
	api.hold = hold
	api.mu.Unlock()
	var field string
	err = chromedp.Run(ctx,
		whClick(`#pen-task-description-panel`, "Draft"),
		whPoll(whPhase("Drafting")),
		whClick(`#pen-task-description-panel`, "Stop"),
		whPoll(whPhase("Which fields get the pen first?")),
		chromedp.Evaluate(`document.querySelector('#task-description').value`, &field),
	)
	close(hold)
	if err != nil {
		t.Fatalf("stop: %v\nJS errors: %v", err, errs.all())
	}
	if field != "rough text" {
		t.Errorf("field = %q after Stop", field)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperEmptyFieldStartsFromTheTitleInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var chip string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPenReady(`.pen-host`),
		setField(`#task-title`, "AI helper for text", "input"),
		chromedp.Click(`#task-description ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("What is this task about?")),
		chromedp.Evaluate(`document.querySelector('#pen-task-description-panel .chips .chip').textContent.trim()`, &chip),
		whClick(`#pen-task-description-panel`, "Draft from the title"),
		whPoll(whPhase("Proposed description")),
	)
	if err != nil {
		t.Fatalf("start: %v\nJS errors: %v", err, errs.all())
	}
	if chip != "Draft from the title" {
		t.Errorf("chip = %q", chip)
	}
	steps := api.requests()
	if len(steps) != 1 || steps[0]["step"] != "draft" || steps[0]["answers"] != nil {
		t.Errorf("steps = %v, want one draft with no answers", steps)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperUnavailableDisablesThePensInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, shell.Helper{Reason: "Claude is not signed in", FixURL: "/agent-isolation#credential"})

	var disabled bool
	var label, title string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPenReady(`[data-panel-id=pen-task-description]`),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').disabled`, &disabled),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').getAttribute('aria-label')`, &label),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').title`, &title),
		setField(`#task-description`, "still works", "input"),
		whKey(`#task-description`, "j", whCtrl),
		chromedp.Sleep(200*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("unavailable: %v\nJS errors: %v", err, errs.all())
	}
	if !disabled || label != "Writing helper unavailable" || !strings.Contains(title, "Claude is not signed in") {
		t.Errorf("disabled %v, label %q, title %q", disabled, label, title)
	}
	if len(api.requests()) != 0 {
		t.Errorf("an unavailable pen sent a step: %v", api.requests())
	}
	var shown bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(whPanelShown, &shown)); err != nil || shown {
		t.Errorf("panel shown = %v (%v)", shown, err)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperRefusalSwitchesThePensOffInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	api.refuse = true
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var disabled bool
	var toast string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPenReady(`[data-panel-id=pen-task-description]`),
		setField(`#task-description`, "rough text", "input"),
		chromedp.Click(`#task-description ~ .pen`, chromedp.ByQuery),
		whPoll(`document.querySelector('#task-description ~ .pen').disabled`),
		chromedp.Evaluate(`document.querySelector('#task-description ~ .pen').disabled`, &disabled),
		whPoll(`!!document.querySelector('#pen-task-description-panel ~ .toast-region .toast')?.checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('#pen-task-description-panel ~ .toast-region .toast').textContent.replace(/\s+/g, ' ').trim()`, &toast),
	)
	if err != nil {
		t.Fatalf("refusal: %v\nJS errors: %v", err, errs.all())
	}
	if !disabled || !strings.Contains(toast, "Claude is not signed in") {
		t.Errorf("disabled %v, toast %q", disabled, toast)
	}
	var shown bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(whPanelShown, &shown)); err != nil || shown {
		t.Errorf("panel shown = %v (%v)", shown, err)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperIsABottomSheetOnAPhoneInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var position, scrim string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(480, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPenReady(`[data-panel-id=pen-task-description]`),
		setField(`#task-description`, "rough text", "input"),
		chromedp.Click(`#task-description ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("Which fields get the pen first?")),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('#pen-task-description-panel')).position`, &position),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-panel-id=pen-task-description] .helper-scrim')).display`, &scrim),
	)
	if err != nil {
		t.Fatalf("sheet: %v\nJS errors: %v", err, errs.all())
	}
	if position != "fixed" || scrim == "none" {
		t.Errorf("position %q, scrim display %q", position, scrim)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperInTheTaskEditDialogInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var after string
	var sibling any
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/t-new"),
		clickButton(`main header`, "Edit"),
		chromedp.WaitVisible(`#task-edit-description`, chromedp.ByQuery),
		whPoll(`!!document.querySelector('#task-edit-description').closest('.pen-host')?._x_dataStack`),
		setField(`#task-edit-title`, "Write the live docs", "input"),
		whKey(`#task-edit-description`, "j", whCtrl),
		whPoll(whPhase("Which fields get the pen first?")),
		whClick(`#pen-task-edit-description-panel`, "Skip questions"),
		whPoll(whPhase("Proposed description")),
		// Esc closes the helper, not the edit form.
		whKey(`#task-edit-description`, "Escape", ""),
		whPoll(`!`+whPanelShown),
		chromedp.Evaluate(`!!document.querySelector('#task-edit-description')`, &sibling),
	)
	if err != nil {
		t.Fatalf("edit: %v\nJS errors: %v", err, errs.all())
	}
	if sibling != true {
		t.Error("Esc in the open helper closed the edit form too")
	}
	err = chromedp.Run(ctx,
		whKey(`#task-edit-description`, "j", whCtrl),
		whPoll(whPhase("How will you know")),
		whClick(`#pen-task-edit-description-panel`, "Skip questions"),
		whPoll(whPhase("Proposed description")),
		whClick(`#pen-task-edit-description-panel`, "Use this"),
		whPoll(`document.querySelector('#task-edit-description').value.startsWith('Add a magic pen')`),
		clickButton(`main`, "Save"),
		whPoll(`!document.querySelector('#task-edit-description')`),
		chromedp.Evaluate(`document.querySelector('.brief > p')?.textContent ?? ''`, &after),
	)
	if err != nil {
		t.Fatalf("save: %v\nJS errors: %v", err, errs.all())
	}
	last := api.requests()[len(api.requests())-1]
	if f, _ := last["field"].(map[string]any); f["kind"] != "task_description" {
		t.Errorf("last step = %v", last)
	}
	if !strings.Contains(api.log(), `"description":"Add a magic pen to the task title and description.`) {
		t.Errorf("the save did not carry the helper's text:\n%s", api.log())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperTitleOptionsInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var geometry string
	var picked, undone, toast, active string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		chromedp.WaitVisible(`#task-title`, chromedp.ByQuery),
		whPoll(`!!document.querySelector('#task-title').closest('.pen-host')?._x_dataStack`),
		setField(`#task-description`, "A pen that helps write task text", "input"),
		// The pen sits centred at the end of the one-line field.
		chromedp.Evaluate(`(() => {
			const input = document.querySelector('#task-title').getBoundingClientRect();
			const pen = document.querySelector('#task-title ~ .pen').getBoundingClientRect();
			const mid = (r) => Math.round((r.top + r.bottom) / 2);
			return [mid(input) === mid(pen) || Math.abs(mid(input) - mid(pen)) <= 1, pen.right <= input.right, pen.left > input.left + input.width / 2].join('|');
		})()`, &geometry),
		chromedp.Click(`#task-title ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("Title option 1")),
		whShot("titles"),
		whPoll(`document.activeElement.getAttribute('role') === 'option'`),
		chromedp.Evaluate(`document.activeElement.getAttribute('role')`, &active),
	)
	if err != nil {
		t.Fatalf("titles: %v\nJS errors: %v", err, errs.all())
	}
	if geometry != "true|true|true" {
		t.Errorf("pen geometry = %q", geometry)
	}
	if active != "option" {
		t.Errorf("focus role = %q, want the first option", active)
	}
	steps := api.requests()
	if len(steps) != 1 || steps[0]["step"] != "titles" {
		t.Fatalf("steps = %v, want one titles step", steps)
	}
	if f, _ := steps[0]["field"].(map[string]any); f["kind"] != "task_title" {
		t.Errorf("field = %v", steps[0]["field"])
	}
	if sib, _ := steps[0]["siblings"].([]any); len(sib) != 1 {
		t.Errorf("siblings = %v", steps[0]["siblings"])
	}

	// Three more asks again, naming what was offered.
	err = chromedp.Run(ctx,
		whClick(`.helper`, "Three more"),
		whPoll(whPhase("Title option 4")),
	)
	if err != nil {
		t.Fatalf("three more: %v\nJS errors: %v", err, errs.all())
	}
	steps = api.requests()
	if ex, _ := steps[1]["exclude"].([]any); len(steps) != 2 || len(ex) != 3 {
		t.Errorf("exclude = %v", steps[1]["exclude"])
	}

	// The key 2 picks the second option like Use this: the field takes it, with Undo.
	err = chromedp.Run(ctx,
		whKey(`.helper .options button`, "2", ""),
		whPoll(`!`+whPanelShown),
		chromedp.Evaluate(`document.querySelector('#task-title').value`, &picked),
		chromedp.Evaluate(`document.activeElement.id`, &active),
		whPoll(`!!document.querySelector('.toast')?.checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('.toast').textContent.replace(/\s+/g, ' ').trim()`, &toast),
		whClick(`.toast`, "Undo"),
		whPoll(`document.querySelector('#task-title').value === ''`),
		chromedp.Evaluate(`document.querySelector('#task-title').value`, &undone),
	)
	if err != nil {
		t.Fatalf("pick: %v\nJS errors: %v", err, errs.all())
	}
	if picked != "Title option 5" || undone != "" || active != "task-title" {
		t.Errorf("picked %q, after Undo %q, focus %q", picked, undone, active)
	}
	if !strings.Contains(toast, "Title replaced by the helper's pick") {
		t.Errorf("toast = %q", toast)
	}

	// A digit typed in the title field is typed, not a pick.
	err = chromedp.Run(ctx,
		chromedp.Click(`#task-title ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("Title option")),
		chromedp.Focus(`#task-title`, chromedp.ByQuery),
		chromedp.SendKeys(`#task-title`, "1", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('#task-title').value`, &picked),
	)
	if err != nil {
		t.Fatalf("typing: %v\nJS errors: %v", err, errs.all())
	}
	if picked != "1" {
		t.Errorf("a digit in the field gave %q", picked)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperTitleInTheTaskEditDialogInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var focused string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/t-new"),
		clickButton(`main header`, "Edit"),
		chromedp.WaitVisible(`#task-edit-title`, chromedp.ByQuery),
		// The title still takes the focus when the editor opens.
		whPoll(`document.activeElement.id === 'task-edit-title'`),
		chromedp.Evaluate(`document.activeElement.id`, &focused),
		whPoll(`!!document.querySelector('#task-edit-title').closest('.pen-host')?._x_dataStack`),
		chromedp.Click(`#task-edit-title ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("Title option 1")),
		whKey(`.helper .options button`, "3", ""),
		whPoll(`document.querySelector('#task-edit-title').value === 'Title option 3'`),
		clickButton(`main`, "Save"),
		whPoll(`!document.querySelector('#task-edit-title')`),
	)
	if err != nil {
		t.Fatalf("edit title: %v\nJS errors: %v", err, errs.all())
	}
	if focused != "task-edit-title" {
		t.Errorf("focus = %q", focused)
	}
	if !strings.Contains(api.log(), `"title":"Title option 3"`) {
		t.Errorf("the save did not carry the picked title:\n%s", api.log())
	}
	steps := api.requests()
	if sib, _ := steps[0]["siblings"].([]any); len(sib) != 1 {
		t.Errorf("the title step read %v", steps[0]["siblings"])
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperCommentComposerInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var reads string
	var geometry, field, active string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/t-new/resources"),
		chromedp.WaitVisible(`#comment-body`, chromedp.ByQuery),
		whPoll(`!!document.querySelector('[data-panel-id=pen-comment]')?._x_dataStack`),
		whPoll(`document.querySelector('main [data-summary]')?.textContent.includes('1 comment')`),
		setField(`#comment-body`, "pen should be visible always not on hover", "input"),
		chromedp.Click(`#comment-body ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("Which fields get the pen first?")),
		whShot("comment"),
		chromedp.Evaluate(`[...document.querySelectorAll('#pen-comment-panel .reads > span')].map((e) => e.textContent).join(' | ')`, &reads),
		// The panel opens above the composer's field.
		chromedp.Evaluate(`(() => {
			const panel = document.querySelector('#pen-comment-panel').getBoundingClientRect();
			const input = document.querySelector('#comment-body').getBoundingClientRect();
			return panel.bottom <= input.top + 1 ? 'above' : 'below';
		})()`, &geometry),
		whClick(`#pen-comment-panel`, "Skip questions"),
		whPoll(whPhase("Proposed comment")),
		// ⌘↵ while the helper is open runs its primary action, not the composer's send.
		whKey(`#comment-body`, "Enter", `metaKey: true,`),
		whPoll(`!`+whPanelShown),
		chromedp.Evaluate(`document.querySelector('#comment-body').value`, &field),
		chromedp.Evaluate(`document.activeElement.id`, &active),
	)
	if err != nil {
		t.Fatalf("comment: %v\nJS errors: %v", err, errs.all())
	}
	if reads != "Reads | Comment | Task title | Task description | project coding_pool" {
		t.Errorf("reads = %q", reads)
	}
	if geometry != "above" {
		t.Errorf("the panel is %s the composer's field", geometry)
	}
	if field != defaultHelperDraft || active != "comment-body" {
		t.Errorf("field %q, focus %q", field, active)
	}
	if strings.Contains(api.log(), "add_ticket_comment") {
		t.Errorf("⌘↵ in the open helper sent the comment:\n%s", api.log())
	}
	steps := api.requests()
	if f, _ := steps[0]["field"].(map[string]any); f["kind"] != "comment" {
		t.Errorf("field = %v", steps[0]["field"])
	}
	sib, _ := steps[0]["siblings"].([]any)
	if len(sib) != 2 {
		t.Errorf("siblings = %v, want the task's title and description", steps[0]["siblings"])
	}

	// The composer's own send takes the helper's text.
	err = chromedp.Run(ctx,
		whKey(`#comment-body`, "Enter", `metaKey: true,`),
		whPoll(`document.querySelectorAll('main .thread .comment').length === 2`),
	)
	if err != nil {
		t.Fatalf("send: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(api.log(), `/api/add_ticket_comment {"body":"Add a magic pen to the task title and description.\n\nDone when: [open: what done looks like]","ticket_id":"t-new"}`) {
		t.Errorf("the comment sent was not the draft:\n%s", api.log())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperCommentEditBoxInTheBrowser(t *testing.T) {
	api := newFakeHelperAPI()
	ctx, url, errs := helperBrowser(t, api, whAvailable)

	var stillEditing bool
	var value string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/t-new/resources"),
		chromedp.WaitVisible(`#comment-body`, chromedp.ByQuery),
		whPoll(`document.querySelectorAll('main .thread .comment').length === 1`),
		whPoll(`document.querySelector('main [data-summary]')?.textContent.includes('1 comment')`),
		whClick(`main .thread`, "Edit"),
		chromedp.WaitVisible(`textarea[aria-label="Edit comment"]`, chromedp.ByQuery),
		whPoll(`!!document.querySelector('textarea[aria-label="Edit comment"]').closest('.pen-host')?._x_dataStack`),
		chromedp.Click(`textarea[aria-label="Edit comment"] ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("Which fields get the pen first?")),
		whClick(`#pen-comment-edit-panel`, "Skip questions"),
		whPoll(whPhase("Proposed comment")),
		// Esc closes the helper, not the edit.
		whKey(`textarea[aria-label="Edit comment"]`, "Escape", ""),
		whPoll(`!`+whPanelShown),
		chromedp.Evaluate(`!!document.querySelector('textarea[aria-label="Edit comment"]')`, &stillEditing),
		chromedp.Click(`textarea[aria-label="Edit comment"] ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("Which fields get the pen first?")),
		whClick(`#pen-comment-edit-panel`, "Skip questions"),
		whPoll(whPhase("Proposed comment")),
		whClick(`#pen-comment-edit-panel`, "Use this"),
		whPoll(`document.querySelector('textarea[aria-label="Edit comment"]').value.startsWith('Add a magic pen')`),
		chromedp.Evaluate(`document.querySelector('textarea[aria-label="Edit comment"]').value`, &value),
		whClick(`main .thread`, "Save"),
		whPoll(`!document.querySelector('textarea[aria-label="Edit comment"]')`),
	)
	if err != nil {
		t.Fatalf("edit: %v\nJS errors: %v", err, errs.all())
	}
	if !stillEditing {
		t.Error("Esc in the open helper cancelled the edit")
	}
	if value != defaultHelperDraft {
		t.Errorf("edit box = %q", value)
	}
	if !strings.Contains(api.log(), `/api/update_ticket_comment {"body":"Add a magic pen`) {
		t.Errorf("the edit did not save the helper's text:\n%s", api.log())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
