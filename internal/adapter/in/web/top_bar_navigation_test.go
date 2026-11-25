package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/web/serversettings"
)

// The top bar island through in-place navigation (ADR 0064): a swap of <main>
// keeps it and the island follows the new path; a swap of the app shell brings
// a freshly rendered one. Back and Forward follow too.

const (
	// currentBar is the name of the top bar's current link, "" when none.
	currentBar = `(() => { const l = [...document.querySelectorAll('.topbar a[aria-current="page"]')]; return l.map(a => a.getAttribute('aria-label') || a.textContent.trim()).join('|'); })()`
	// markMount tags the island's mount, so a later read can tell whether it is the same element.
	markMount = `document.querySelector('.topbar [data-island]').dataset.keep = 'yes'; true`
	keptMount = `document.querySelector('.topbar [data-island]')?.dataset.keep === 'yes'`
)

func navigationHarness(t *testing.T) *inboxHarness {
	t.Helper()
	return newInboxHarnessWith(t, inboxWorld(), func(d *Deps) {
		d.ProjectSummaries = fakeSummaries{}
		d.DataPaths = &serversettings.Paths{}
	})
}

func (h *inboxHarness) waitBar(t *testing.T, want string) {
	t.Helper()
	h.run(t, "current link "+want, chromedp.Poll(currentBar+` === `+jsString(want), nil, chromedp.WithPollingTimeout(8*time.Second)))
}

func TestTheTopBarFollowsASwapOfMainAndBackAndForward(t *testing.T) {
	h := navigationHarness(t)
	h.run(t, "open", h.openReady("/projects"), chromedp.Evaluate(markMount, nil))
	h.waitBar(t, "Projects")

	h.run(t, "to the server page", jsClick(`.topbar a[href="/server"]`))
	h.waitURL(t, "/server")
	h.waitBar(t, "Server")
	var kept bool
	h.eval(t, keptMount, &kept)
	if !kept {
		t.Error("a swap of <main> must keep the island's mount")
	}

	h.run(t, "back", chromedp.Evaluate(`history.back()`, nil))
	h.waitURL(t, "/projects")
	h.waitBar(t, "Projects")
	h.run(t, "forward", chromedp.Evaluate(`history.forward()`, nil))
	h.waitURL(t, "/server")
	h.waitBar(t, "Server")
	h.noErrors(t)
}

func TestAnAppShellSwapBringsAFreshlyRenderedTopBar(t *testing.T) {
	h := navigationHarness(t)
	h.run(t, "open a project", h.openReady("/projects/p1"), chromedp.Evaluate(markMount, nil))
	var picker string
	h.eval(t, barPicker, &picker)
	if picker != "p1" {
		t.Fatalf("picker %q, want p1", picker)
	}

	h.run(t, "out of the project", jsClick(`.topbar a[href="/projects"]`))
	h.waitURL(t, "/projects")
	h.waitBar(t, "Projects")
	var kept bool
	h.eval(t, keptMount, &kept)
	if kept {
		t.Error("an app shell swap inserts a new mount")
	}
	h.eval(t, `document.querySelector('.topbar #project-picker').selectedOptions[0].textContent.trim()`, &picker)
	if picker != "Choose a project…" {
		t.Errorf("outside a project the picker asks to choose, got %q", picker)
	}
	var badge string
	h.run(t, "the bell is live", railPoll(bellBadge+` === '2'`), chromedp.Evaluate(bellBadge, &badge))

	h.run(t, "into another project through the picker",
		chromedp.Evaluate(`(() => { const s = document.querySelector('.topbar #project-picker'); s.value = 'p2'; s.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`, nil),
	)
	h.waitURL(t, "/projects/p2")
	h.run(t, "picker shows it", railPoll(barPicker+` === 'p2'`))
	h.noErrors(t)
}
