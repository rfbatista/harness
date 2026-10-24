package web

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// realStepper is a ports.WritingHelper behind the real httpapi router, so the
// browser reads the server's own SSE framing: it answers analyze with one
// question, streams a draft, and can hold a draft until the request ends.
type realStepper struct {
	mu        sync.Mutex
	requests  []ports.WritingHelperRequest
	holdDraft bool
	cancelled chan struct{}
}

func (r *realStepper) Run(ctx context.Context, req ports.WritingHelperRequest, emit func(ports.WritingHelperEvent)) error {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	hold := r.holdDraft && req.Step == "draft"
	r.mu.Unlock()
	switch req.Step {
	case "analyze":
		emit(ports.WritingHelperEvent{Name: "result", Data: ports.WritingHelperAnalyzeResult{Questions: []ports.WritingHelperQuestion{
			{ID: "q1", Text: "How will you know it is done?", Why: "No done criteria yet.", Chips: []string{"A person drafts it in a minute"}},
		}}})
	case "titles":
		emit(ports.WritingHelperEvent{Name: "result", Data: ports.WritingHelperTitlesResult{Titles: []string{"One", "Two", "Three"}}})
	default:
		if hold {
			emit(ports.WritingHelperEvent{Name: "delta", Data: ports.WritingHelperDelta{Text: "Add "}})
			<-ctx.Done()
			close(r.cancelled)
			return ctx.Err()
		}
		emit(ports.WritingHelperEvent{Name: "delta", Data: ports.WritingHelperDelta{Text: "Add a magic "}})
		emit(ports.WritingHelperEvent{Name: "delta", Data: ports.WritingHelperDelta{Text: "pen."}})
		emit(ports.WritingHelperEvent{Name: "result", Data: ports.WritingHelperDraftResult{Text: "Add a magic pen."}})
	}
	return nil
}

func (r *realStepper) Availability(context.Context) ports.WritingHelperAvailability {
	return ports.WritingHelperAvailability{Available: true}
}

func realBrowser(t *testing.T, stepper *realStepper) (context.Context, string, *errorLog) {
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
		Projects:      fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:         fakeTickets{},
		Sessions:      fakeSessions{},
		Agents:        fakeAgents{},
		Repositories:  fakeRepos{},
		WritingHelper: func(context.Context) shell.Helper { return shell.Helper{Available: true} },
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{WritingHelper: stepper})))
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return ctx, srv.URL, errs
}

func TestWritingHelperReadsTheServersOwnStreamInTheBrowser(t *testing.T) {
	stepper := &realStepper{cancelled: make(chan struct{})}
	ctx, url, errs := realBrowser(t, stepper)

	var field, partial string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPoll(`!!document.querySelector('[data-panel-id=pen-task-description]')?._x_dataStack`),
		setField(`#task-description`, "rough", "input"),
		chromedp.Click(`#task-description ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("How will you know it is done?")),
		whClick(`#pen-task-description-panel`, "A person drafts it in a minute"),
		whClick(`#pen-task-description-panel`, "Draft"),
		whPoll(whPhase("Proposed description")),
		chromedp.Evaluate(`document.querySelector('#pen-task-description-panel .draft').textContent`, &partial),
		whClick(`#pen-task-description-panel`, "Use this"),
		whPoll(`document.querySelector('#task-description').value === 'Add a magic pen.'`),
		chromedp.Evaluate(`document.querySelector('#task-description').value`, &field),
	)
	if err != nil {
		t.Fatalf("real stream: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(partial, "Add a magic pen.") || field != "Add a magic pen." {
		t.Errorf("draft %q, field %q", partial, field)
	}
	stepper.mu.Lock()
	got := stepper.requests
	stepper.mu.Unlock()
	if len(got) != 2 || got[0].Step != "analyze" || got[1].Step != "draft" || got[0].ProjectID != "p1" {
		t.Fatalf("requests = %+v", got)
	}
	if len(got[1].Answers) != 1 || got[1].Answers[0].Answer != "A person drafts it in a minute" || got[1].Field.Kind != "task_description" {
		t.Errorf("draft request = %+v", got[1])
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestWritingHelperStopEndsTheServersStepInTheBrowser(t *testing.T) {
	stepper := &realStepper{cancelled: make(chan struct{}), holdDraft: true}
	ctx, url, errs := realBrowser(t, stepper)

	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/new"),
		whPoll(`!!document.querySelector('[data-panel-id=pen-task-description]')?._x_dataStack`),
		setField(`#task-description`, "rough", "input"),
		chromedp.Click(`#task-description ~ .pen`, chromedp.ByQuery),
		whPoll(whPhase("How will you know it is done?")),
		whClick(`#pen-task-description-panel`, "Skip questions"),
		whPoll(`document.querySelector('#pen-task-description-panel .draft[data-streaming]')?.textContent.includes('Add')`),
		whClick(`#pen-task-description-panel`, "Stop"),
		whPoll(whPhase("How will you know it is done?")),
	)
	if err != nil {
		t.Fatalf("stop: %v\nJS errors: %v", err, errs.all())
	}
	select {
	case <-stepper.cancelled:
	case <-time.After(5 * time.Second):
		t.Error("Stop did not end the server's step: its request context never ended")
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
