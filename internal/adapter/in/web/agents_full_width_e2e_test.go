//go:build !windows

package web_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// afwNoRail is true when the page has no tasks rail, no scrim, no Tasks toggle,
// and its main region starts at the left edge.
const afwNoRail = `!document.querySelector('#rail') && !document.querySelector('.tasks-toggle') && !document.querySelector('.scrim') && Math.round(document.querySelector('main').getBoundingClientRect().left) === 0 && document.documentElement.scrollWidth <= innerWidth`

// TestAgentsPagesRenderFullWidthWithoutTheRail_EndToEnd is W1 of ticket 16
// against a real server: the Agents library and both editors have the top bar
// only, while a project board and a task page keep the rail.
func TestAgentsPagesRenderFullWidthWithoutTheRail_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	b := newTLSBrowser(t)
	for _, width := range []int64{1280, 1440} {
		for _, path := range []string{"/agents", "/agents/new", "/agents/skills/new"} {
			b.run("full width at "+path,
				chromedp.EmulateViewport(width, 900),
				chromedp.Navigate(w.srv.URL+path),
				waitUntil(`!!document.querySelector('main[x-data="agentsPage"]') && document.querySelector('.topbar') !== null`),
				waitUntil(afwNoRail),
				bothSchemes(fmt.Sprintf("agents-full-width-%d-%s", width, strings.NewReplacer("/", "-").Replace(path))),
			)
		}
	}
	b.run("the project board and the task page keep the rail",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID),
		waitUntil(`!!document.querySelector('#rail') && !!document.querySelector('.tasks-toggle') && document.querySelector('main').getBoundingClientRect().left > 100`),
		chromedp.Navigate(w.taskURL()),
		waitUntil(`!!document.querySelector('#rail') && !!document.querySelector('.tasks-toggle') && document.querySelector('main').getBoundingClientRect().left > 100`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
