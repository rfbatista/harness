package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

const (
	noteText   = `document.querySelector('.brief > p')`
	noteToggle = `document.querySelector('.brief > button')`
)

func noteDeps(description string) Deps {
	deps := boardDeps()
	deps.Tasks = fakeTickets{[]*domain.Ticket{
		{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Description: description, Status: domain.TicketStatusInProgress},
	}}
	return deps
}

// A long description shows one line beside a More toggle; More shows it
// whole and Less cuts it to one line again.
func TestALongDescriptionShowsOneLineWithAMoreToggle(t *testing.T) {
	long := strings.Repeat("Line of the description that the person wrote.\n", 12)
	url, _ := stripServer(t, noteDeps(long))
	ctx, errs := browser(t)

	var lineHeight, clamped, whole, again float64
	var word, openWord, expanded, closedAgain string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady+` && !!`+noteToggle+` && `+noteToggle+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`parseFloat(getComputedStyle(`+noteText+`).lineHeight)`, &lineHeight),
		chromedp.Evaluate(noteText+`.getBoundingClientRect().height`, &clamped),
		chromedp.Evaluate(noteToggle+`.textContent.trim()`, &word),
		chromedp.Evaluate(noteToggle+`.click()`, nil),
		chromedp.Poll(noteToggle+`.textContent.trim() === 'Less'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(noteText+`.getBoundingClientRect().height`, &whole),
		chromedp.Evaluate(noteToggle+`.textContent.trim()`, &openWord),
		chromedp.Evaluate(noteToggle+`.getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(noteToggle+`.click()`, nil),
		chromedp.Poll(noteToggle+`.textContent.trim() === 'More'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(noteText+`.getBoundingClientRect().height`, &again),
		chromedp.Evaluate(noteToggle+`.getAttribute('aria-expanded')`, &closedAgain),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if clamped > lineHeight+1 || word != "More" {
		t.Errorf("clamped: %.0fpx high with a %.0fpx line (want one line), toggle %q", clamped, lineHeight, word)
	}
	if whole < 10*lineHeight || openWord != "Less" || expanded != "true" {
		t.Errorf("shown whole: %.0fpx high, toggle %q, aria-expanded %q", whole, openWord, expanded)
	}
	if again != clamped || closedAgain != "false" {
		t.Errorf("one line again: %.0fpx (was %.0fpx), aria-expanded %q", again, clamped, closedAgain)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Opening the description is remembered for that task, in the browser: a
// reload shows it whole, and another task's description is still one line.
func TestTheOpenDescriptionIsRememberedPerTask(t *testing.T) {
	long := strings.Repeat("Line of the description that the person wrote.\n", 12)
	deps := boardDeps()
	deps.Tasks = fakeTickets{[]*domain.Ticket{
		{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Description: long, Status: domain.TicketStatusInProgress},
		{ID: "t-other", ProjectID: "p1", Title: "Other", Description: long, Status: domain.TicketStatusInProgress},
	}}
	url, _ := stripServer(t, deps)
	ctx, errs := browser(t)

	var word, other string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady+` && !!`+noteToggle+` && `+noteToggle+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(noteToggle+`.click()`, nil),
		chromedp.Poll(noteToggle+`.textContent.trim() === 'Less'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady+` && !!`+noteToggle+` && `+noteToggle+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(noteToggle+`.textContent.trim()`, &word),
		chromedp.Navigate(url+"/projects/p1/tasks/t-other"),
		chromedp.Poll(pageReady+` && !!`+noteToggle+` && `+noteToggle+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(noteToggle+`.textContent.trim()`, &other),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if word != "Less" || other != "More" {
		t.Errorf("after a reload the toggle reads %q (want Less); another task's reads %q (want More)", word, other)
	}
}

// A description that fits has no toggle.
func TestAShortDescriptionHasNoToggle(t *testing.T) {
	url, _ := stripServer(t, noteDeps("One short line."))
	ctx, errs := browser(t)

	var text string
	var toggleShown bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady+` && !!`+noteText, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond), // the overflow is measured after the text is drawn
		chromedp.Evaluate(noteText+`.textContent.trim()`, &text),
		chromedp.Evaluate(`!!`+noteToggle+` && `+noteToggle+`.checkVisibility()`, &toggleShown),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if text != "One short line." || toggleShown {
		t.Errorf("text %q, toggle shown %v", text, toggleShown)
	}
}

// What the person asked for is never cut off: the edit form and the delete
// answer keep their whole height (here the answer is the coded error the
// stubbed API gives: the editor's errors stay inline and whole too).
func TestTheEditFormAndTheDeleteAnswerAreNeverClamped(t *testing.T) {
	long := strings.Repeat("Line of the description that the person wrote.\n", 12)
	url, _ := stripServer(t, noteDeps(long))
	ctx, errs := browser(t)

	var formHeight, textareaHeight, textareaScroll, bannerHeight, bannerScroll float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('main button')].find(b => b.textContent.trim() === 'Edit').click()`, nil),
		chromedp.Poll(`!!document.querySelector('#task-edit-description')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('#task-edit-description').closest('form').getBoundingClientRect().height`, &formHeight),
		chromedp.Evaluate(`document.querySelector('#task-edit-description').getBoundingClientRect().height`, &textareaHeight),
		chromedp.Evaluate(`document.querySelector('#task-edit-description').scrollHeight`, &textareaScroll),
		chromedp.Evaluate(`[...document.querySelectorAll('main button')].find(b => b.textContent.trim() === 'Cancel').click()`, nil),
		chromedp.Evaluate(`[...document.querySelectorAll('main button')].find(b => b.textContent.trim() === 'Delete task').click()`, nil),
		chromedp.Poll(`!!document.querySelector('[role=alert]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=alert]').getBoundingClientRect().height`, &bannerHeight),
		chromedp.Evaluate(`document.querySelector('[role=alert]').scrollHeight`, &bannerScroll),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if formHeight < 150 || textareaHeight < 40 {
		t.Errorf("the edit form is %.0fpx high, its field %.0fpx: cut off", formHeight, textareaHeight)
	}
	if bannerHeight < bannerScroll {
		t.Errorf("the delete answer is %.0fpx high but its content %.0fpx: cut off", bannerHeight, bannerScroll)
	}
	_ = textareaScroll
}
