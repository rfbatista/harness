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
