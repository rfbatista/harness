//go:build !windows

package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Bringing sessions under an architect in the web UI (ADR 0045), end to end:
// the server as it runs and a headless Chrome as the person, on the task
// page's session list. Two scenarios: a task with a running architect, and
// one with none.
func TestBringSessionsUnderTheArchitectInTheWebUI_EndToEnd(t *testing.T) {
	e := newArchWeb(t)
	p := newPerson(t)

	start := func(ticket string, req ports.InteractiveRequest) *domain.Session {
		t.Helper()
		req.ProjectID, req.RepositoryID, req.TicketID = e.project, e.repo, ticket
		s, _, err := e.orch.StartInteractive(e.ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	rows := `[...document.querySelectorAll('[role=listbox] .row')]`
	row := func(id string) string {
		return fmt.Sprintf(`document.querySelector('[role=listbox] .row[data-session-id=%q]')`, id)
	}
	pick := func(id string) string { return row(id) + `.querySelector('.pick input')` }
	toggle := `document.querySelector('[data-select-toggle]')`
	button := `document.querySelector('[data-bring-under]')`

	// A task with a running architect and two peers spun off by hand.
	arch := start(e.ticket, ports.InteractiveRequest{Mode: "architect", Prompt: "Shape it."})
	a := start(e.ticket, ports.InteractiveRequest{Prompt: "Server side."})
	b := start(e.ticket, ports.InteractiveRequest{Prompt: "Web side."})
	p.run(t, "open the task",
		chromedp.EmulateViewport(1280, 860),
		chromedp.Navigate(e.url("/projects/"+e.project+"/tasks/"+e.ticket)),
		poll(rows+`.length === 3 && !!`+toggle),
	)
	if text := p.text(t, toggle+`.textContent.trim()`); text != "Select" {
		t.Errorf("the toggle: %q", text)
	}
	if n := p.text(t, `String(document.querySelectorAll('.pick').length)`); n != "0" {
		t.Errorf("no checkboxes before Select: %s", n)
	}

	p.run(t, "select mode",
		chromedp.Evaluate(toggle+`.click()`, nil),
		poll(`!!`+pick(a.ID)+` && !!`+pick(b.ID)),
	)
	if has := p.text(t, `String(!!`+row(arch.ID)+`.querySelector('.pick'))`); has != "false" {
		t.Errorf("the architect has no checkbox")
	}
	if label := p.text(t, button+`.textContent.trim()`); label != "Bring under the architect" {
		t.Errorf("with a running architect the button reads %q", label)
	}
	p.run(t, "tick both",
		chromedp.Evaluate(pick(a.ID)+`.click()`, nil),
		chromedp.Evaluate(pick(b.ID)+`.click()`, nil),
		poll(`document.querySelector('[data-picked-count]')?.textContent.trim() === '2 selected'`),
		both("bring-under-1-select"),
		chromedp.Evaluate(button+`.click()`, nil),
		poll(rows+`.filter(r => r.dataset.role === 'delegate' && r.dataset.depth === '1').length === 2`),
		poll(`!document.querySelector('.pick') && !document.querySelector('[data-bring-under]')`), // select mode ended; with nothing left to move the toggle goes too
		both("bring-under-2-moved"),
	)
	for _, id := range []string{a.ID, b.ID} {
		if detail := p.text(t, row(id)+`.querySelector('.detail').textContent`); !strings.Contains(detail, "brought under") {
			t.Errorf("%s detail: %q", id, detail)
		}
		if role, _ := e.channel.SessionRole(e.ctx, id); role != domain.RoleDelegate {
			t.Errorf("%s is a %q on the server", id, role)
		}
		if got := e.orchGet(id); got.ParentSessionID != arch.ID || !got.BroughtUnder {
			t.Errorf("%s not moved: %+v", id, got)
		}
	}
	if checks, _ := e.channel.ListStatusChecks(e.ctx, e.ticket); len(checks) != 2 {
		t.Errorf("a loop for each running moved session: %+v", checks)
	}

	// A task with no architect: the button starts one over the selection.
	other, err := e.plan.CreateTicket(e.ctx, e.project, "Spun off by hand", "Two sessions the person started.", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	c := start(other.ID, ports.InteractiveRequest{Prompt: "First thing."})
	d := start(other.ID, ports.InteractiveRequest{Prompt: "Second thing."})
	p.run(t, "open the other task",
		chromedp.Navigate(e.url("/projects/"+e.project+"/tasks/"+other.ID)),
		poll(rows+`.length === 2 && !!`+toggle),
		chromedp.Evaluate(toggle+`.click()`, nil),
		poll(`!!`+pick(c.ID)+` && !!`+pick(d.ID)),
		chromedp.Evaluate(pick(c.ID)+`.click()`, nil),
		chromedp.Evaluate(pick(d.ID)+`.click()`, nil),
		poll(`document.querySelector('[data-picked-count]')?.textContent.trim() === '2 selected'`),
	)
	if label := p.text(t, button+`.textContent.trim()`); label != "Start an architect over these" {
		t.Errorf("with no architect the button reads %q", label)
	}
	p.run(t, "the form opens locked to architect mode",
		chromedp.Evaluate(button+`.click()`, nil),
		poll(`!!document.querySelector('#new-session-prompt') && !!document.querySelector('[data-locked-mode-word]')`),
		both("bring-under-3-form"),
	)
	if word := p.text(t, `document.querySelector('[data-locked-mode-word]').textContent`); !strings.Contains(word, "Architect") || !strings.Contains(word, "2 sessions") {
		t.Errorf("the locked mode line: %q", word)
	}
	if shown := p.text(t, `String(document.querySelector('#new-session-mode')?.checkVisibility() ?? false)`); shown != "false" {
		t.Errorf("the mode dropdown is hidden while the mode is locked: %s", shown)
	}
	p.run(t, "start it",
		poll(`!document.querySelector('form [type=submit]').disabled`),
		chromedp.Evaluate(`document.querySelector('form [type=submit]').click()`, nil),
		poll(rows+`.filter(r => r.dataset.role === 'architect').length === 1 && `+rows+`.filter(r => r.dataset.role === 'delegate' && r.dataset.depth === '1').length === 2`),
		both("bring-under-4-started"),
	)
	var newArch *domain.Session
	for _, s := range e.sessionsOf(other.ID) {
		if s.Mode == domain.SessionModeArchitect {
			newArch = s
		}
	}
	if newArch == nil {
		t.Fatal("no architect on the task")
	}
	for _, id := range []string{c.ID, d.ID} {
		if got := e.orchGet(id); got.ParentSessionID != newArch.ID || !got.BroughtUnder || got.Branch == "" {
			t.Errorf("%s not moved under the new architect: %+v", id, got)
		}
	}
	if !strings.Contains(newArch.Task, "Spun off by hand") {
		t.Errorf("architect: %+v", newArch)
	}
	if errs := p.errors; len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}

// orchGet is a session as the server holds it.
func (e *archWeb) orchGet(id string) *domain.Session {
	e.t.Helper()
	s, err := e.orch.Get(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// sessionsOf is the sessions of a task.
func (e *archWeb) sessionsOf(ticketID string) []*domain.Session {
	e.t.Helper()
	list, err := e.orch.List(e.ctx, ports.SessionFilter{TicketID: ticketID})
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}
