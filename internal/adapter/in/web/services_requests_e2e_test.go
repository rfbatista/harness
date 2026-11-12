//go:build !windows

package web_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service requests against a real server (spec "Web UI — services: approve agents' service
// requests", testing decisions; contract section 13). An agent's changing tool call is made
// with the request module and blocks until the person answers on the page: the badge counts
// it, the card shows what the agent asks, and Approve carries the action out as the session
// (the tool call returns its outcome), while Deny hands the agent the person's note.

// fileRequest makes the agent's call in the background and returns where its answer arrives.
type agentCall struct {
	outcome *ports.ServiceRequestOutcome
	err     error
	done    chan struct{}
}

func (w *servicesWorld) agentAsks(t *testing.T, in ports.ServiceRequestInput) *agentCall {
	t.Helper()
	in.SessionID = "abcdef12-agent-session"
	call := &agentCall{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		defer close(call.done)
		call.outcome, call.err = w.requests.Submit(ctx, in)
	}()
	return call
}

func (c *agentCall) answer(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent's call was not answered")
	}
}

func waitForRequests(t *testing.T, w *servicesWorld, n int) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		list, err := w.requests.List(context.Background(), domain.ServiceRequestFilter{ProjectID: w.project.ID, State: domain.ServiceRequestPending})
		if err == nil && len(list) >= n {
			return
		}
	}
	t.Fatalf("fewer than %d service requests were filed", n)
}

func TestAServiceRequestIsApprovedOnThePageAndTheAgentGetsItsOutcome_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	svc, err := w.svc.CreateService(context.Background(), w.project.ID, domain.ServiceInput{
		Name: "web", Kind: domain.ServiceKindCommand, Command: &domain.ServiceCommand{Line: "echo up; sleep 30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := w.agentAsks(t, ports.ServiceRequestInput{
		Action: domain.ServiceRequestStart, ServiceID: svc.ID, ServiceName: "web", Reason: "to check the new build",
		Env: map[string]string{"LOG_LEVEL": "debug"}, EnvGiven: true,
	})
	waitForRequests(t, w, 1)
	const (
		card   = `document.querySelector('[data-service-request]')`
		value  = `document.querySelector('[data-request-variable][data-name=LOG_LEVEL] [data-request-value]').textContent.trim()`
		status = `document.querySelector('[data-service][data-name=web] [data-service-status]')?.textContent`
	)
	b := newTLSBrowser(t)
	b.run("the page shows the Requests badge, and the badge opens the card",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`document.querySelector('[data-requests-badge]')?.checkVisibility() && document.querySelector('[data-requests-badge]').textContent.includes('1')`),
		waitUntil(status+` === 'stopped'`),
		chromedp.Click(`[data-requests-badge]`, chromedp.ByQuery),
		waitUntil(`!!`+card),
	)
	b.run("the card says what the agent asks, and its override is masked until revealed",
		waitUntil(card+`.querySelector('h3').textContent.includes('Start web')`),
		waitUntil(card+`.textContent.includes('to check the new build')`),
		waitUntil(card+`.textContent.includes('Runs in the main checkout.')`),
		waitUntil(`(`+value+`).startsWith('••')`),
		waitUntil(`!document.body.innerText.includes('debug')`),
		chromedp.Click(`[data-request-toggle][data-name=LOG_LEVEL]`, chromedp.ByQuery),
		waitUntil(`(`+value+`).startsWith('debug')`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-start-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-start-dark"),
	)
	b.run("Approve: the service starts, the card says done, and the count goes",
		chromedp.Click(`[data-service-request] [data-action=approve]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-outcome]')?.textContent.includes('Approved and done.')`),
		waitUntil(status+` === 'running'`),
		waitUntil(`!document.querySelector('[data-requests-badge]').checkVisibility()`),
		chromedp.Sleep(300*time.Millisecond),
		shot("request-outcome-dark"),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-outcome-light"),
	)
	call.answer(t)
	if call.err != nil || call.outcome == nil || call.outcome.Service == nil || call.outcome.Service.ID != svc.ID {
		t.Fatalf("the agent's call: outcome %+v, err %v", call.outcome, call.err)
	}
}

func TestAServiceRequestIsDeniedWithANoteAndTheAgentGetsIt_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	svc, err := w.svc.CreateService(context.Background(), w.project.ID, domain.ServiceInput{
		Name: "db", Kind: domain.ServiceKindCommand, Command: &domain.ServiceCommand{Line: "sleep 30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := w.agentAsks(t, ports.ServiceRequestInput{Action: domain.ServiceRequestStart, ServiceID: svc.ID, ServiceName: "db"})
	waitForRequests(t, w, 1)
	b := newTLSBrowser(t)
	b.run("open the popover from the badge",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`document.querySelector('[data-requests-badge]')?.checkVisibility()`),
		chromedp.Click(`[data-requests-badge]`, chromedp.ByQuery),
		waitUntil(`!!document.querySelector('[data-service-request]')`),
	)
	b.run("write a note and Deny: the card says denied with the note, and nothing started",
		chromedp.SendKeys(`[data-service-request] textarea`, "Not while the build is red", chromedp.ByQuery),
		chromedp.Click(`[data-service-request] [data-action=deny]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-outcome]')?.textContent.includes('Denied. Your note: Not while the build is red')`),
		waitUntil(`document.querySelector('[data-service][data-name=db] [data-service-status]').textContent === 'stopped'`),
	)
	call.answer(t)
	if call.err == nil || !strings.Contains(call.err.Error(), "Not while the build is red") {
		t.Fatalf("the agent should get the note, got outcome %+v, err %v", call.outcome, call.err)
	}
}

func firstPendingRequestID(t *testing.T, w *servicesWorld) string {
	t.Helper()
	list, err := w.requests.List(context.Background(), domain.ServiceRequestFilter{ProjectID: w.project.ID, State: domain.ServiceRequestPending})
	if err != nil || len(list) == 0 {
		t.Fatalf("no pending request: %v", err)
	}
	return list[0].ID
}

// A request filed while the page is open arrives by the feed (the Requests badge, the row's
// badge), the row's badge opens the card, and the inbox link (#service-request-<id>) opens the
// page on it.
func TestAServiceRequestArrivesLiveAndOpensFromTheRowAndTheInboxLink_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	svc, err := w.svc.CreateService(context.Background(), w.project.ID, domain.ServiceInput{
		Name: "web", Kind: domain.ServiceKindCommand, Command: &domain.ServiceCommand{Line: "echo up; sleep 30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	const rowBadge = `document.querySelector('[data-service][data-name=web] [data-service-requests]')`
	b := newTLSBrowser(t)
	b.run("the page is open with nothing waiting",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!document.querySelector('[data-service][data-name=web]')`),
		waitUntil(`!document.querySelector('[data-requests-badge]').checkVisibility()`),
	)
	call := w.agentAsks(t, ports.ServiceRequestInput{Action: domain.ServiceRequestStop, ServiceID: svc.ID, ServiceName: "web", Reason: "done testing"})
	b.run("the request arrives by the feed: the toolbar badge and the row's badge",
		waitUntil(`document.querySelector('[data-requests-badge]').checkVisibility() && document.querySelector('[data-requests-badge]').textContent.includes('1')`),
		waitUntil(`(`+rowBadge+`).checkVisibility() && (`+rowBadge+`).textContent.trim() === '1 request'`),
		waitUntil(`(`+rowBadge+`).getAttribute('aria-label') === '1 request waiting on web'`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("requests-badge-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("requests-badge-dark"),
	)
	b.run("the row's badge opens the card, and Escape closes the popover",
		chromedp.Click(`[data-service][data-name=web] [data-service-requests]`, chromedp.ByQuery),
		waitUntil(`!!document.querySelector('[data-service-request]') && document.querySelector('[data-service-request] h3').textContent.includes('Stop web')`),
		waitUntil(`document.querySelector('[data-request-session]').textContent === 'go-developer · session abcdef12'`),
		chromedp.KeyEvent("\x1b"),
		waitUntil(`!document.querySelector('#services-requests').matches(':popover-open')`),
	)
	id := firstPendingRequestID(t, w)
	b.run("the inbox link opens the page on the card",
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services#service-request-"+id),
		waitUntil(`!!document.querySelector('[data-service-request]') && document.activeElement && document.activeElement.closest('[data-service-request]') !== null`),
		waitUntil(`document.querySelector('[data-service-request]').id === 'service-request-`+id+`'`),
	)
	b.run("deny it, so the agent's call ends",
		chromedp.Click(`[data-service-request] [data-action=deny]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-outcome]')?.textContent.includes('Denied.')`),
		waitUntil(`!(`+rowBadge+`).checkVisibility()`),
	)
	call.answer(t)
}

// A create request shows the whole proposed definition with its masked variables, and approving
// it creates the service, which then reads "created by go-developer".
func TestACreateRequestIsApprovedAndTheServiceReadsCreatedByTheAgent_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	call := w.agentAsks(t, ports.ServiceRequestInput{
		Action: domain.ServiceRequestCreate, ServiceName: "worker", Reason: "the queue needs a consumer",
		Summary: "create command service worker: npm run worker",
		Proposed: &domain.ServiceInput{
			Name: "worker", Kind: domain.ServiceKindCommand, Command: &domain.ServiceCommand{Line: "echo working; sleep 30", Subdir: "", Ports: []int{9000}},
			Env: "QUEUE=default",
		},
		EnvChanges: []domain.ServiceRequestEnv{{Name: "QUEUE", Value: "default", Change: "set"}},
	})
	waitForRequests(t, w, 1)
	b := newTLSBrowser(t)
	b.run("the card shows the whole proposed definition, the variable masked",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`document.querySelector('[data-requests-badge]')?.checkVisibility()`),
		chromedp.Click(`[data-requests-badge]`, chromedp.ByQuery),
		waitUntil(`!!document.querySelector('[data-service-request]') && document.querySelector('[data-service-request] h3').textContent.includes('Create worker')`),
		waitUntil(`document.querySelector('[data-request-definition]').textContent.includes('echo working; sleep 30')`),
		waitUntil(`document.querySelector('[data-request-definition]').textContent.includes('9000')`),
		waitUntil(`document.querySelector('[data-request-variable][data-name=QUEUE] [data-request-value]').textContent.trim().startsWith('••')`),
		waitUntil(`!document.body.innerText.includes('default')`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-create-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-create-dark"),
	)
	b.run("Approve: the service appears and says who created it",
		chromedp.Click(`[data-service-request] [data-action=approve]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-outcome]')?.textContent.includes('Approved and done.')`),
		waitUntil(`!!document.querySelector('[data-service][data-name=worker]')`),
		waitUntil(`document.querySelector('[data-service][data-name=worker] [data-service-created-by]').textContent === 'created by go-developer'`),
	)
	call.answer(t)
	if call.err != nil || call.outcome == nil || call.outcome.Service == nil || call.outcome.Service.Name != "worker" {
		t.Fatalf("the agent's call: outcome %+v, err %v", call.outcome, call.err)
	}
}

// An update request shows each changed field as before and after, and a variable change masked.
func TestAnUpdateRequestShowsBeforeAndAfterAndAMaskedVariable_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	svc, err := w.svc.CreateService(context.Background(), w.project.ID, domain.ServiceInput{
		Name: "web", Kind: domain.ServiceKindCommand, Command: &domain.ServiceCommand{Line: "make dev", Ports: []int{8080}}, Env: "PORT=8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	call := w.agentAsks(t, ports.ServiceRequestInput{
		Action: domain.ServiceRequestUpdate, ServiceID: svc.ID, ServiceName: "web", Reason: "port 8080 is taken by the database",
		Summary: "change command service web: command.line, command.ports, PORT",
		Proposed: &domain.ServiceInput{
			Name: "web", Kind: domain.ServiceKindCommand, Command: &domain.ServiceCommand{Line: "npm run dev", Ports: []int{3000}}, Env: "PORT=3000",
		},
		Changes: []domain.ServiceRequestChange{
			{Field: "command.line", Before: "make dev", After: "npm run dev"},
			{Field: "command.ports", Before: "8080", After: "3000"},
			{Field: "env.PORT"},
		},
		EnvChanges: []domain.ServiceRequestEnv{{Name: "PORT", Value: "3000", Change: "set", PreviousSet: true}},
	})
	waitForRequests(t, w, 1)
	b := newTLSBrowser(t)
	b.run("the card shows before and after, and the changed variable masked",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`document.querySelector('[data-requests-badge]')?.checkVisibility()`),
		chromedp.Click(`[data-requests-badge]`, chromedp.ByQuery),
		waitUntil(`!!document.querySelector('[data-request-changes]')`),
		waitUntil(`document.querySelector('[data-request-changes]').textContent.includes('Command line from make dev to npm run dev')`),
		waitUntil(`document.querySelector('[data-request-changes]').textContent.includes('Ports from 8080 to 3000')`),
		waitUntil(`document.querySelector('[data-request-variable][data-name=PORT]').textContent.includes('changed')`),
		waitUntil(`document.querySelector('[data-request-variable][data-name=PORT] [data-request-value]').textContent.trim().startsWith('••')`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-update-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-update-dark"),
	)
	b.run("deny it, so the agent's call ends",
		chromedp.Click(`[data-service-request] [data-action=deny]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-outcome]')?.textContent.includes('Denied.')`),
	)
	call.answer(t)
}
