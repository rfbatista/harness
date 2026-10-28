package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

const (
	featureBtn = `document.getElementById('task-feature-button')`
	featurePop = `document.getElementById('task-feature')`
)

// featureWorld is a feature task with n children: the first three in review,
// the next two in progress, the rest todo.
func featureWorld(n int) Deps {
	deps := taskPageDeps()
	tickets := []*domain.Ticket{{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}
	for i := 0; i < n; i++ {
		status := domain.TicketStatusTodo
		switch {
		case i < 3:
			status = domain.TicketStatusReview
		case i < 5:
			status = domain.TicketStatusInProgress
		}
		tickets = append(tickets, &domain.Ticket{ID: fmt.Sprintf("c%02d", i), ProjectID: "p1", Title: fmt.Sprintf("Child task %d", i), Status: status, ParentID: "t-feed"})
	}
	deps.Tasks = fakeTickets{tickets}
	return deps
}

// A feature task with children shows a Feature button with the count, first
// among the actions; its popover lists them in review, in progress, todo
// order, stops at its cap and scrolls inside, and the agents keep their
// height; Esc closes it and the focus returns to the button. (The cap is
// min(70dvh, 40rem); a headless window's dvh is its real size, not the
// emulated one, so forty children make sure the cap is reached.)
func TestTheFeaturePopoverScrollsInsideItsCapAndTheAgentsKeepTheirHeight(t *testing.T) {
	url, _ := pageServer(t, featureWorld(40))
	ctx, errs := browser(t)

	var label, foot, order string
	var closedAtStart, opened, closed bool
	var firstInRow bool
	var popHeight, capHeight, innerHeight, scrollClient, scrollHeight, listTopBefore, listTopOpen float64
	var focused string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(featureBtn+`.textContent.replace(/\s+/g, ' ').trim()`, &label),
		chromedp.Evaluate(`document.querySelector('.toolbar .actions').querySelector('.button:not([hidden])') === `+featureBtn, &firstInRow),
		chromedp.Evaluate(`!`+featurePop+`.matches(':popover-open')`, &closedAtStart),
		chromedp.Evaluate(`document.querySelector('[role=listbox]').getBoundingClientRect().top`, &listTopBefore),
		chromedp.Evaluate(featureBtn+`.click()`, nil),
		chromedp.Poll(featurePop+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('#task-feature .row[data-child-id]').length === 40`, &opened),
		chromedp.Evaluate(`[...document.querySelectorAll('#task-feature .row[data-child-id]')].map(r => r.dataset.childId).slice(0, 6).join(',')`, &order),
		chromedp.Evaluate(`document.querySelector('#task-feature .pop-foot').textContent.replace(/\s+/g, ' ').trim()`, &foot),
		chromedp.Evaluate(featurePop+`.getBoundingClientRect().height`, &popHeight),
		chromedp.Evaluate(`document.querySelector('#task-feature .pop-body').clientHeight`, &scrollClient),
		chromedp.Evaluate(`document.querySelector('#task-feature .pop-body').scrollHeight`, &scrollHeight),
		chromedp.Evaluate(`parseFloat(getComputedStyle(`+featurePop+`).maxHeight)`, &capHeight),
		chromedp.Evaluate(`window.innerHeight`, &innerHeight),
		chromedp.Evaluate(`document.querySelector('[role=listbox]').getBoundingClientRect().top`, &listTopOpen),
		chromedp.KeyEvent("\x1b"),
		chromedp.Poll(`!`+featurePop+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`!`+featurePop+`.matches(':popover-open')`, &closed),
		chromedp.Evaluate(`document.activeElement.id`, &focused),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if label != "Feature 40" || !firstInRow {
		t.Errorf("the Feature button reads %q and leads the actions: %v", label, firstInRow)
	}
	if !closedAtStart || !opened {
		t.Errorf("closed at start %v, all forty rows once open %v", closedAtStart, opened)
	}
	if order != "c00,c01,c02,c03,c04,c05" {
		t.Errorf("rows lead with %q: review first, then in progress, then todo", order)
	}
	if foot != "3 in review · 2 in progress · 35 todo See on the board ↗" {
		t.Errorf("the foot reads %q", foot)
	}
	if popHeight > capHeight+1 {
		t.Errorf("the popover is %.0fpx high, over its cap of %.0fpx", popHeight, capHeight)
	}
	if scrollHeight <= scrollClient+1 {
		t.Errorf("forty rows should scroll inside the popover's body: content %.0f, visible %.0f", scrollHeight, scrollClient)
	}
	if listTopOpen != listTopBefore {
		t.Errorf("opening the popover moved the agents: top %.0f → %.0f", listTopBefore, listTopOpen)
	}
	if !closed || focused != "task-feature-button" {
		t.Errorf("Esc closes it (%v) and returns the focus to the button, got %q", closed, focused)
	}
	_ = innerHeight
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Children move and arrive over the feed: the count, the foot and the row
// order follow while the popover is open.
func TestTheFeaturePopoverFollowsTheChildrenLive(t *testing.T) {
	url, feed := pageServer(t, taskPageDeps())
	ctx, errs := browser(t)

	label := featureBtn + `.textContent.replace(/\s+/g, ' ').trim()`
	order := `[...document.querySelectorAll('#task-feature .row[data-child-id]')].map(r => r.dataset.childId).slice(0, 4).join(',')`
	foot := `document.querySelector('#task-feature .pop-foot').textContent.replace(/\s+/g, ' ').trim()`
	var before, after, orderBefore, orderAfter, footAfter string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(featureBtn+`.click()`, nil),
		chromedp.Poll(featurePop+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(label, &before),
		chromedp.Evaluate(order, &orderBefore),
		chromedp.ActionFunc(func(context.Context) error {
			for feed.followers() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			// c10 (todo) goes to review; a new child arrives in backlog.
			feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "c10", "project_id": "p1", "title": "Child task 10", "status": "review", "parent_id": "t-feed"}})
			feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "t-new", "project_id": "p1", "title": "New one", "status": "backlog", "parent_id": "t-feed"}})
			return nil
		}),
		chromedp.Poll(`(`+label+`) === 'Feature 17'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(label, &after),
		chromedp.Evaluate(order, &orderAfter),
		chromedp.Evaluate(foot, &footAfter),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if before != "Feature 16" || after != "Feature 17" {
		t.Errorf("button before %q, after %q", before, after)
	}
	if orderBefore != "c00,c01,c02,c03" || orderAfter != "c00,c01,c02,c10" {
		t.Errorf("row order before %q, after %q", orderBefore, orderAfter)
	}
	if !strings.HasPrefix(footAfter, "4 in review · 2 in progress · 10 todo · 1 backlog") {
		t.Errorf("the foot after the change reads %q", footAfter)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A task with no children shows no Feature button, and none appears for a
// task with nothing to summarise.
func TestATaskWithNoChildrenShowsNoFeatureButton(t *testing.T) {
	url, _ := pageServer(t, boardDeps())
	ctx, errs := browser(t)

	var shown bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`(() => { const b = `+featureBtn+`; return !!b && b.checkVisibility(); })()`, &shown),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if shown {
		t.Error("a Feature button on a task without children")
	}
}
