package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A save announced on the feed while our own save is still in flight is not
// lost: once our save answers, a higher version means another save landed after
// ours, so the page takes it.
func TestArchitectureFeedEventDuringOwnSaveIsNotDropped(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	rig.api.saveDelay = 1500 * time.Millisecond
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`!!document.querySelector('`+patternField+`')`),
		setField(patternField, "mine", "input"),
		poll(barVisible()),
	)
	waitFor(t, "the page's feed", func() bool { return rig.feed.followers() >= 1 })
	rig.run(t, clickButton(saveBar, "Save"))
	// The save is stored (version 2) and announced; its answer is held back.
	waitFor(t, "our save to be stored", func() bool { return len(rig.api.savedRecords()) == 1 })
	rig.api.saveElsewhere(t, func(rec map[string]any) { rec["pattern"] = "from-another-tab" })
	rig.run(t,
		poll(`document.querySelector('`+patternField+`').value === 'from-another-tab'`),
		poll(`!`+barVisible()),
	)
}

// An event with no task has no link to its session (never /tasks/undefined).
func TestArchitectureRuleEventWithoutATaskHasNoLink(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	ev := ruleEvent("e1", "plain", "archtest", "warned", "cmd/server/main.go", "")
	delete(ev, "task_id")
	rig.api.events = []map[string]any{ev}
	var links int
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[data-rule-event]').length === 1`),
		chromedp.Evaluate(`document.querySelectorAll('[data-rule-event] a').length`, &links),
	)
	if links != 0 {
		t.Fatalf("an event with no task links to %d places", links)
	}
}

// The agents drawn in the picture are buttons the keyboard reaches: Enter and
// Space focus the agent, and the keyboard stays on it after the redraw.
func TestArchitectureDiagramAgentsDrawnAreKeyboardButtons(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	const node = `document.querySelector('[data-diagram-picture] svg g[data-agent="go-developer"]')`
	var role, label, tab string
	var kept bool
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/diagram")),
		diagramReady(),
		chromedp.Evaluate(node+`.getAttribute('role')`, &role),
		chromedp.Evaluate(node+`.getAttribute('aria-label')`, &label),
		chromedp.Evaluate(node+`.getAttribute('tabindex')`, &tab),
		chromedp.Evaluate(node+`.focus()`, nil),
		chromedp.KeyEvent("\r"),
		poll(`document.querySelector('[data-focus-name]')?.textContent.trim() === 'go-developer'`),
		chromedp.Evaluate(`document.activeElement === `+node, &kept),
	)
	if role != "button" || label != "Focus go-developer" || tab != "0" {
		t.Errorf("role %q, label %q, tabindex %q", role, label, tab)
	}
	if !kept {
		t.Error("the keyboard fell off the agent when the picture was redrawn")
	}
	rig.run(t,
		chromedp.KeyEvent(" "),
		poll(`!document.querySelector('[data-focus-panel]')`), // Space toggles it off again
		chromedp.Click(`[data-diagram-picture] svg g[data-agent="devops"]`, chromedp.ByQuery),
		poll(`document.querySelector('[data-focus-name]')?.textContent.trim() === 'devops'`),
	)
}

func (f *fakeArchitectureAPI) savedRecords() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.saved...)
}
