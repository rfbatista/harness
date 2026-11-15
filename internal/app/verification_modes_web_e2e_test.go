//go:build !windows

package app

import (
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// Starting a Reviewer session from the task page's New session form (ADR 0090),
// end to end: the server as it runs and a headless Chrome as the person. The
// model picker reads opus for the mode, the hint names the base branch picker,
// and the started session's row is tagged with the mode value.
func TestStartingAReviewerSessionInTheWebUI_EndToEnd(t *testing.T) {
	e := newArchWeb(t)
	p := newPerson(t)

	mode := `document.querySelector('#new-session-mode')`
	p.run(t, "open the form on the task",
		chromedp.EmulateViewport(1280, 860),
		chromedp.Navigate(e.url("/projects/"+e.project+"/tasks/"+e.ticket)),
		poll(`!!document.querySelector('[x-on\\:click="startCreating"]')`),
		chromedp.Evaluate(`document.querySelector('[x-on\\:click="startCreating"]').click()`, nil),
		poll(`!!`+mode),
	)
	if got := p.text(t, `[...`+mode+`.options].slice(-4).map(o => o.textContent).join()`); got != "Reviewer,Tester,QA,Operator" {
		t.Errorf("the last four modes = %q", got)
	}
	p.run(t, "pick Reviewer",
		chromedp.Evaluate(mode+`.value = 'review'; `+mode+`.dispatchEvent(new Event('change', {bubbles: true}))`, nil),
		poll(`document.querySelector('#new-session-model').value === 'opus'`),
		poll(`document.querySelector('[data-mode-models]').textContent.includes('Name the branch under check in Branch off')`),
		both("verification-modes-1-reviewer-form"),
	)
	p.run(t, "start it",
		poll(`!document.querySelector('[data-mode-models] [type=submit]').disabled`),
		chromedp.Evaluate(`document.querySelector('[data-mode-models] [type=submit]').click()`, nil),
		poll(`[...document.querySelectorAll('[role=listbox] .row [data-kind=mode]')].some(t => t.textContent.trim() === 'review')`),
		both("verification-modes-2-reviewer-row"),
	)
	var found bool
	for _, s := range e.sessionsOf(e.ticket) {
		if s.Mode == domain.SessionModeReview {
			found = true
		}
	}
	if !found {
		t.Error("no review-mode session on the task")
	}
	if errs := p.errors; len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}
