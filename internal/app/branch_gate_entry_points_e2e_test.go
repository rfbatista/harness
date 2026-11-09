//go:build !windows

package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// Where a pending branch request is answered, from the person's side (ADR
// 0079): the inbox lists its notification, and following it opens the task
// page's Branch requests popover at that request.

func TestABranchRequestNotificationInTheInboxLeadsToItsCard(t *testing.T) {
	e := newBranchWeb(t)
	p := newPerson(t)

	pending := e.ask("git checkout -b feature-y", "Try another layout")
	req := e.pendingRequests(1)[0]

	p.run(t, "open the inbox", chromedp.EmulateViewport(1280, 860), chromedp.Navigate(e.srv.URL+"/notifications"),
		poll(`document.querySelector('.notifications')?.dataset.feed === 'live' && !document.querySelector('main [data-ssr]')`))
	row := `main .notification a.title[href$="#branch-request-` + req.ID + `"]`
	p.run(t, "the notification is listed", poll(`document.querySelector('`+row+`') !== null`))
	if title := p.text(t, `document.querySelector(`+"`"+row+"`"+`).textContent.trim()`); !strings.HasPrefix(title, "Branch") {
		t.Errorf("the notification is titled %q", title)
	}
	p.run(t, "follow it", chromedp.Evaluate(`setTimeout(() => document.querySelector('`+row+`').click())`, nil))
	eventually(t, "the popover opens at the request", func() error {
		var open bool
		// The page is navigating: a read that lands in between fails and is retried.
		if err := chromedp.Run(p.ctx, chromedp.Evaluate(`location.hash.startsWith('#branch-request-') && document.querySelector('#task-branch-requests')?.matches(':popover-open') === true && `+cardJS+` !== null`, &open)); err != nil {
			return err
		}
		if !open {
			return errors.New("not open yet")
		}
		return nil
	})
	if heading := p.text(t, cardJS+`.querySelector('[data-branch-request-heading]').innerText`); !strings.Contains(heading, "Create branch feature-y") {
		t.Errorf("the card opened from the inbox is %q", heading)
	}
	p.run(t, "deny", clickInPopover(`#task-branch-requests button[data-action="deny"]`))
	if v := e.verdict(pending); v.Decision != "deny" {
		t.Errorf("the agent got %+v", v)
	}
	if len(p.errors) > 0 {
		t.Errorf("JS errors: %v", p.errors)
	}
}
