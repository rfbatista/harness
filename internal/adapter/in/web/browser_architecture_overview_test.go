package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The overview matches architecture-overview: a title with the repository's
// name, the sync pill, six stat cards and a count on every tab.
func TestArchitectureOverviewHasTheTitleSyncPillStatCardsAndTabCounts(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	rig.api.export = map[string]any{"state": "in_sync", "path": ".harness/architecture.yaml", "exported_version": 1}
	var title, pill, cards, tabs string
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[data-stat]').length === 6`),
		poll(`!!document.querySelector('[data-export-state]')`),
		chromedp.Evaluate(`document.querySelector('h1').textContent.trim()`, &title),
		chromedp.Evaluate(`document.querySelector('[data-export-state]').textContent.trim()`, &pill),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-stat]')].map(c => c.querySelector('b').textContent.trim() + ' ' + c.querySelector('span').textContent.trim()).join('|')`, &cards),
		chromedp.Evaluate(`[...document.querySelectorAll('[role=tab]')].map(t => (t.querySelector('[data-tab-label]').textContent.trim() + ' ' + (t.querySelector('[data-tab-count]')?.textContent.trim() ?? '')).trim()).join('|')`, &tabs),
	)
	if title != "Repository architecture: harness" {
		t.Errorf("title = %q", title)
	}
	if pill != "In sync with .harness/architecture.yaml" {
		t.Errorf("sync pill = %q", pill)
	}
	wantCards := "2 instruction files|1 tools|1 sandboxes|1 routed agents|2 hook rules · 1 blocking|2 layers · 0 overlaps"
	if cards != wantCards {
		t.Errorf("stat cards = %q, want %q", cards, wantCards)
	}
	wantTabs := "Instructions & rules 2|Tools 1|Sandboxes 1|Orchestration 2|Hooks 2|Layers 2|Output"
	if tabs != wantTabs {
		t.Errorf("tabs = %q, want %q", tabs, wantTabs)
	}
}

// A deep link opens its tab with the tab list at the top of the viewport and
// no event list above it.
func TestArchitectureDeepLinkOpensTheTabAtTheTopOfTheViewport(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	rig.api.events = []map[string]any{
		ruleEvent("e2", "go-developer", "stay-in-your-layer", "blocked", "internal/domain/x.go", "domain"),
		ruleEvent("e1", "plain", "archtest", "warned", "cmd/server/main.go", ""),
	}
	var top float64
	var eventsAbove bool
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/layers")),
		poll(`document.querySelectorAll('[data-rule-event]').length === 2`),
		poll(`(() => { const t = document.querySelector('[role=tablist]').getBoundingClientRect().top; return t >= 0 && t < 120 })()`),
		chromedp.Evaluate(`document.querySelector('[role=tablist]').getBoundingClientRect().top`, &top),
		chromedp.Evaluate(`(() => { const e = document.querySelector('[data-rule-event]'); const t = document.querySelector('[role=tablist]'); return !!(e.compareDocumentPosition(t) & Node.DOCUMENT_POSITION_FOLLOWING) })()`, &eventsAbove),
	)
	if eventsAbove {
		t.Fatal("the rule events sit above the tab list")
	}
	if top < 0 || top >= 120 {
		t.Fatalf("the tab list is %vpx from the top", top)
	}
}

// Recent rule events are a compact panel of at most five rows; Show all
// reveals the rest.
func TestArchitectureEventsPanelShowsFiveRowsUntilShowAll(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	for i := 0; i < 8; i++ {
		rig.api.events = append(rig.api.events, ruleEvent(fmt.Sprintf("e%d", i), "go-developer", "stay-in-your-layer", "blocked", "x.go", "domain"))
	}
	var rows int
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[data-rule-event]').length === 5`),
		poll(hasButton(`[aria-label="Recent rule events"]`, "Show all")),
		clickButton(`[aria-label="Recent rule events"]`, "Show all"),
		poll(`document.querySelectorAll('[data-rule-event]').length === 8`),
		chromedp.Evaluate(`document.querySelectorAll('[data-rule-event]').length`, &rows),
	)
	if rows != 8 {
		t.Fatalf("rows after Show all = %d", rows)
	}
}

// An empty record shows the mockup's empty-state card with the discovery banner.
func TestArchitectureEmptyRecordShowsTheEmptyStateCard(t *testing.T) {
	rig := newArchRig(t)
	var card string
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("")),
		poll(`!!document.querySelector('#architecture-empty [data-found]')`),
		chromedp.Evaluate(`document.querySelector('#architecture-empty').textContent.replace(/\s+/g, ' ')`, &card),
	)
	for _, want := range []string{
		"No architecture for “harness” yet",
		"Start from the hexagonal template",
		"We found CLAUDE.md",
		"Confirm them to add them to the architecture.",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("empty state lacks %q: %q", want, card)
		}
	}
	for _, left := range []string{"Draft with assistant", "View as document", "Mock state"} {
		if strings.Contains(card, left) {
			t.Errorf("empty state renders %q, which has no backing feature", left)
		}
	}
	rig.run(t,
		clickButton(`#architecture-empty`, "Start from the hexagonal template"),
		poll(barVisible()),
		poll(`document.querySelector('[data-stat=layers] b').textContent.trim() === '8'`),
	)
}

// Unsaved edits show the sticky save bar: a change summary, Discard, Review
// diff and Save to database, kept at the bottom of the viewport.
func TestArchitectureSaveBarIsStickyWithTheChangeSummary(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var bar string
	var gap float64
	rig.run(t,
		chromedp.EmulateViewport(1440, 500),
		chromedp.Navigate(rig.url("/layers")),
		poll(`!!document.querySelector('`+patternField+`')`),
		setField(patternField, "hexagonal", "input"),
		poll(barVisible()),
		chromedp.Evaluate(`document.querySelector('[data-save-bar]').textContent.replace(/\s+/g, ' ')`, &bar),
		poll(`document.querySelector('[data-scroll]').getBoundingClientRect().bottom - document.querySelector('[data-save-bar]').getBoundingClientRect().bottom < 8`),
		chromedp.Evaluate(`document.querySelector('[data-scroll]').getBoundingClientRect().bottom - document.querySelector('[data-save-bar]').getBoundingClientRect().bottom`, &gap),
	)
	for _, want := range []string{"1 change: 1 in Pattern", "Discard", "Review diff", "Save to database"} {
		if !strings.Contains(bar, want) {
			t.Errorf("save bar lacks %q: %q", want, bar)
		}
	}
	if gap < 0 {
		t.Errorf("the save bar is below the scrolling column by %vpx", -gap)
	}
}
