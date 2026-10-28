package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const taskMenuBtn = `document.getElementById('task-menu-button')`

// controls are the title row's visible interactive items: links, buttons and
// the status picker.
const controls = `[...document.querySelectorAll('.toolbar a, .toolbar button, .toolbar select')].filter(c => c.checkVisibility())`

// The title row ends in one ⋯ task menu that holds Design assets, Resources
// and Delete task (last, after a separator); the row holds nine controls on a
// plain task; Delete task keeps its confirmation.
func TestTheTaskMenuHoldsDesignAssetsResourcesAndDeleteTask(t *testing.T) {
	url, _ := stripServer(t, resourcesDeps())
	ctx, errs := browser(t)

	const popup = `document.querySelector('#task-menu-button').parentElement.querySelector('[role=menu]')`
	var rowControls int
	var lastInRow bool
	var items, directLinks string
	var separatorBeforeDelete, opened, confirm bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(controls+`.length`, &rowControls),
		chromedp.Evaluate(`(() => { const all = `+controls+`; return all[all.length - 1] === `+taskMenuBtn+`; })()`, &lastInRow),
		chromedp.Evaluate(`[...document.querySelectorAll('.toolbar > .actions > a, .toolbar > .actions > button')].map(c => c.textContent.replace(/\s+/g, ' ').trim()).join('|')`, &directLinks),
		chromedp.Evaluate(taskMenuBtn+`.click()`, nil),
		chromedp.Poll(popup+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...`+popup+`.querySelectorAll('[role=menuitem]')].map(i => i.textContent.replace(/\s+/g, ' ').trim()).join('|')`, &items),
		chromedp.Evaluate(popup+`.querySelector('[role=separator]').nextElementSibling === `+popup+`.querySelector('[data-action=delete-task]') && `+popup+`.lastElementChild === `+popup+`.querySelector('[data-action=delete-task]')`, &separatorBeforeDelete),
		chromedp.Evaluate(popup+`.querySelector('[data-action=delete-task]').click()`, nil),
		// The editor checks the task can go (this fake server has no sessions API, so it answers with its error banner): the confirmation flow of before runs.
		chromedp.Poll(`!!document.querySelector('[role=alertdialog][aria-label="Delete this task?"], .banner[role=alert]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`!!document.querySelector('[role=alertdialog][aria-label="Delete this task?"], .banner[role=alert]')`, &opened),
		chromedp.Evaluate(`true`, &confirm),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if items != "Design assets 0|Resources 2|Delete task" {
		t.Errorf("the task menu holds %q", items)
	}
	if strings.Contains(directLinks, "Design assets") || strings.Contains(directLinks, "Resources") || strings.Contains(directLinks, "Delete task") {
		t.Errorf("the title row still holds a folded control directly: %q", directLinks)
	}
	if rowControls > 9 {
		t.Errorf("the title row holds %d controls on a plain task, want at most nine", rowControls)
	}
	if !lastInRow || !separatorBeforeDelete {
		t.Errorf("the ⋯ ends the row (%v) and Delete task is last after a separator (%v)", lastInRow, separatorBeforeDelete)
	}
	if !opened || !confirm {
		t.Errorf("Delete task asks its confirmation: %v %v", opened, confirm)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// Below 48rem the task menu is an action sheet.
func TestTheTaskMenuIsAnActionSheetOnAPhone(t *testing.T) {
	url, _ := stripServer(t, resourcesDeps())
	ctx, errs := browser(t)

	const popup = `document.querySelector('#task-menu-button').parentElement.querySelector('[role=menu]')`
	var sheet bool
	var width, innerWidth float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(taskMenuBtn+`.click()`, nil),
		chromedp.Poll(popup+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(popup+`.hasAttribute('data-sheet')`, &sheet),
		chromedp.Evaluate(popup+`.getBoundingClientRect().width`, &width),
		chromedp.Evaluate(`window.innerWidth`, &innerWidth),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !sheet || width != innerWidth {
		t.Errorf("below 48rem the task menu is an action sheet: sheet %v, width %.0f of %.0f", sheet, width, innerWidth)
	}
}

// A resource or a design asset arriving over the feed counts in the menu;
// Documents and Comments keep counting in the row.
func TestTheTaskMenuCountsFollowTheFeed(t *testing.T) {
	url, feed := stripServer(t, resourcesDeps())
	ctx, errs := browser(t)

	menuCount := func(sel string) string {
		return `document.querySelector('` + sel + `').querySelector('.meta').textContent.trim()`
	}
	var before, after, comments string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(menuCount("[data-task-resources]"), &before),
		chromedp.ActionFunc(func(ctx2 context.Context) error {
			for feed.followers() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			at := time.Now().UTC().Format(time.RFC3339)
			feed.pushRaw(t, map[string]any{"ticket_resource": map[string]any{"id": "r-new", "ticket_id": "t-feed", "project_id": "p1", "kind": "link", "title": "Spec", "url": "https://example.com/spec", "created_at": at, "updated_at": at}})
			feed.pushRaw(t, map[string]any{"ticket_comment": map[string]any{"id": "c-new", "ticket_id": "t-feed", "project_id": "p1", "body": "hello", "author": "person", "created_at": at, "updated_at": at}})
			return nil
		}),
		chromedp.Poll(menuCount("[data-task-resources]")+` === '3'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(menuCount("[data-task-resources]"), &after),
		chromedp.Evaluate(`document.querySelector('[data-task-comments] .badge').textContent.trim()`, &comments),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if before != "2" || after != "3" || comments != "2" {
		t.Errorf("resources %q → %q in the menu, comments %q in the row", before, after, comments)
	}
}

// resourcesDeps is the task page with two resources and a comment on Add SSE feed.
func resourcesDeps() Deps {
	w := resourcesBoard()
	deps := boardDeps()
	deps.Resources = fakeResources(w.resources)
	deps.Comments = fakeComments(w.comments)
	return deps
}

// With a waiting review on a feature task the row still holds at most eleven
// controls (the Feature and Reviews buttons are the two it gains).
func TestTheTitleRowHoldsElevenControlsAtWorst(t *testing.T) {
	srv, _ := architectServerWith(t, featureChildren(3), nil, pendingReviews(1)...)
	ctx, errs := browser(t)
	var n int
	var names string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`document.getElementById('task-reviews-button').checkVisibility() && document.getElementById('task-feature-button').checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(controls+`.length`, &n),
		chromedp.Evaluate(controls+`.map(c => c.textContent.replace(/\s+/g, ' ').trim() || c.getAttribute('aria-label')).join(' | ')`, &names),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if n > 11 {
		t.Errorf("the title row holds %d controls with a feature and a waiting review, want at most eleven: %s", n, names)
	}
}
