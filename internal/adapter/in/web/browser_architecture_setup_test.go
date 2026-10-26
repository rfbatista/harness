package web

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// archClick clicks the first element matching a selector through the page, so
// a button inside a panel that scrolls needs no scrolling first.
func archClick(sel string) chromedp.Action {
	return poll(fmt.Sprintf(`(() => { const e = document.querySelector(%q); if (!e || e.disabled) return false; e.click(); return true; })()`, sel))
}

// archFill types into a field of the nth entry of a tab, the way a person does:
// an input event for the model, a change event for the comma-separated lists.
func archFill(tab string, n int, field, value string) chromedp.Action {
	return poll(fmt.Sprintf(`(() => {
		const e = document.querySelectorAll('#architecture-panel-%s [data-entry]')[%d]?.querySelector('[data-field=%s]');
		if (!e) return false;
		if (e.type === 'checkbox') { if (e.checked !== (%q === 'on')) e.click(); return true; }
		e.value = %q;
		if (e.tagName === 'SELECT' && e.value !== %q) return false; // its options have not arrived yet
		e.dispatchEvent(new Event('input', { bubbles: true }));
		e.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	})()`, tab, n, field, value, value, value))
}

func archCounts() string {
	return `JSON.stringify(Object.fromEntries([...document.querySelectorAll('[data-count]')].map(e => [e.dataset.count, e.textContent.trim()])))`
}

const archSave = `[data-save-bar] button[data-variant=primary]`

// Starting from what the harness found: the candidates are listed to confirm,
// confirming puts them in the draft, and saving creates the record.
func TestSettingUpAnArchitectureFromWhatTheHarnessFound(t *testing.T) {
	rig := newArchRig(t)
	var counts string
	var empty bool
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[data-candidates] input[type=checkbox]').length === 3`),
		poll(`document.querySelector('#architecture-empty').textContent.includes('software-architect, go-developer, devops')`),
		// the person unticks nothing: all three are confirmed
		archClick(`#architecture-empty [data-action=confirm]`),
		poll(`!!document.querySelector('[data-setup-done]')`),
		poll(`!!document.querySelector('[data-save-bar]')?.checkVisibility()`),
		archClick(archSave),
		poll(`document.querySelector('[data-count=instructions]')?.textContent.trim() === '1'`),
		chromedp.Evaluate(archCounts(), &counts),
		chromedp.Evaluate(`!document.querySelector('#architecture-empty')`, &empty),
	)
	want := `{"instructions":"1","tools":"1","sandboxes":"1","agents":"0","hooks":"1","layers":"0"}`
	if counts != want {
		t.Fatalf("counts = %s, want %s", counts, want)
	}
	if !empty {
		t.Fatal("the empty state stays after the record exists")
	}
	rec := rig.api.lastSaved()
	ins, _ := rec["instructions"].([]any)
	if len(ins) != 1 || ins[0].(map[string]any)["path"] != "CLAUDE.md" {
		t.Fatalf("saved instructions = %v", ins)
	}
	if first := ins[0].(map[string]any); fmt.Sprint(first["applies_to"]) != "[all]" {
		t.Fatalf("a discovered file applies to %v, want all", first["applies_to"])
	}
}

// A candidate the person unticks is not added.
func TestAnUntickedCandidateIsNotAdded(t *testing.T) {
	rig := newArchRig(t)
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[data-candidates] input[type=checkbox]').length === 3`),
		archClick(`[data-candidate="tools:0"]`),
		poll(`document.querySelector('[data-action=confirm]').textContent.trim() === 'Add 2 to the architecture'`),
		archClick(`#architecture-empty [data-action=confirm]`),
		poll(`document.querySelector('[data-count=instructions]')?.textContent.trim() === '1'`),
		poll(`document.querySelector('[data-count=tools]')?.textContent.trim() === '0'`),
	)
}

// An entry added in each of the four descriptive tabs is saved, and the page
// shows it again after a reload.
func TestEntriesAddedInTheDescriptiveTabsAreSavedAndReloaded(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var counts string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[data-count]').length === 6`),
		// Instructions & rules
		archClick(`#architecture-panel-instructions [data-add]`),
		archFill("instructions", 2, "path", "docs/ARCH.md"),
		archFill("instructions", 2, "kind", "skill"),
		archFill("instructions", 2, "loaded", "on_demand"),
		archPick("instructions", 2, "applies_to", "go-developer", "devops"),
		archFill("instructions", 2, "note", "Read before a refactor."),
		// Tools
		clickTab("Tools"),
		archClick(`#architecture-panel-tools [data-add]`),
		archFill("tools", 1, "name", "gh"),
		archFill("tools", 1, "kind", "cli"),
		archPick("tools", 1, "used_by", "devops"),
		archFill("tools", 1, "writes", "on"),
		archFill("tools", 1, "usage", "Open a pull request."),
		// Sandboxes
		clickTab("Sandboxes"),
		archClick(`#architecture-panel-sandboxes [data-add]`),
		archFill("sandboxes", 1, "name", "docker"),
		archFill("sandboxes", 1, "runs", "Integration tests."),
		archFill("sandboxes", 1, "can_reach", "the test network"),
		archFill("sandboxes", 1, "cannot_reach", "~/.ssh, the host"),
		archPick("sandboxes", 1, "used_by", "all"),
		// Orchestration: a route and a hand-off
		clickTab("Orchestration"),
		archClick(`#architecture-panel-orchestration [data-add=route]`),
		archFill("orchestration", 1, "agent", "devops"),
		archFill("orchestration", 1, "model", "claude-sonnet-5-5"),
		poll(`document.querySelectorAll('#architecture-panel-orchestration [data-entry=route]').length === 2`),
		archClick(`#architecture-panel-orchestration [data-add=handoff]`),
		poll(`document.querySelectorAll('#architecture-panel-orchestration [data-entry=handoff]').length === 2`),
	)
	rig.run(t,
		archFillHandoff(1, "from", "devops"),
		archFillHandoff(1, "to", "software-architect"),
		archFillHandoff(1, "when", "A deploy fails."),
		archClick(archSave),
		poll(`document.querySelector('[data-save-bar]')?.checkVisibility() === false`),
		chromedp.Navigate(rig.url("/orchestration")),
		poll(`document.querySelectorAll('[data-count]').length === 6`),
		poll(`document.querySelector('[data-count=agents]').textContent.trim() === '2'`),
		poll(`document.querySelector('#architecture-tab-orchestration [data-tab-count]').textContent.trim() === '4'`),
		chromedp.Evaluate(archCounts(), &counts),
	)
	want := `{"instructions":"3","tools":"2","sandboxes":"2","agents":"2","hooks":"2","layers":"2"}`
	if counts != want {
		t.Fatalf("counts after reload = %s, want %s", counts, want)
	}
	var shown string
	rig.run(t,
		poll(`document.querySelectorAll('#architecture-panel-orchestration [data-entry=handoff]').length === 2`),
		chromedp.Evaluate(`JSON.stringify([...document.querySelectorAll('#architecture-panel-orchestration [data-entry]')].map(e => [...e.querySelectorAll('[data-field]')].map(f => f.value)))`, &shown),
	)
	if want := `[["go-developer","claude-opus-5-5",""],["devops","claude-sonnet-5-5",""],["software-architect","go-developer","A spec is approved."],["devops","software-architect","A deploy fails."]]`; shown != want {
		t.Fatalf("orchestration fields after reload = %s, want %s", shown, want)
	}
	rec := rig.api.lastSaved()
	tools := rec["tools"].([]any)
	gh := tools[1].(map[string]any)
	if gh["name"] != "gh" || gh["kind"] != "cli" || gh["writes"] != true || gh["usage"] != "Open a pull request." || fmt.Sprint(gh["used_by"]) != "[devops]" {
		t.Fatalf("saved tool = %v", gh)
	}
	sb := rec["sandboxes"].([]any)[1].(map[string]any)
	if fmt.Sprint(sb["cannot_reach"]) != "[~/.ssh the host]" || sb["runs"] != "Integration tests." {
		t.Fatalf("saved sandbox = %v", sb)
	}
	in := rec["instructions"].([]any)[2].(map[string]any)
	if in["kind"] != "skill" || in["loaded"] != "on_demand" || fmt.Sprint(in["applies_to"]) != "[go-developer devops]" || in["note"] != "Read before a refactor." {
		t.Fatalf("saved instruction = %v", in)
	}
	orch := rec["orchestration"].(map[string]any)
	if r := orch["routing"].([]any)[1].(map[string]any); r["agent"] != "devops" || r["model"] != "claude-sonnet-5-5" {
		t.Fatalf("saved route = %v", r)
	}
	if h := orch["handoffs"].([]any)[1].(map[string]any); h["from"] != "devops" || h["to"] != "software-architect" || h["when"] != "A deploy fails." {
		t.Fatalf("saved hand-off = %v", h)
	}
}

func archFillHandoff(n int, field, value string) chromedp.Action {
	return poll(fmt.Sprintf(`(() => {
		const e = document.querySelectorAll('#architecture-panel-orchestration [data-entry=handoff]')[%d]?.querySelector('[data-field=%s]');
		if (!e) return false;
		e.value = %q;
		if (e.tagName === 'SELECT' && e.value !== %q) return false;
		e.dispatchEvent(new Event('input', { bubbles: true }));
		e.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	})()`, n, field, value, value))
}

// A tool that writes needs usage prose: the save is refused, the error shows on
// the field, and the page says what stopped it.
func TestAToolThatWritesWithNoUsageShowsItsErrorOnTheField(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var msg, invalid string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/tools")),
		poll(`document.querySelectorAll('[data-count]').length === 6`),
		archClick(`#architecture-panel-tools [data-add]`),
		archFill("tools", 1, "name", "deploy"),
		archFill("tools", 1, "writes", "on"),
		archClick(archSave),
		poll(`!!document.querySelector('[data-error-for="/tools/1/usage"]')`),
		chromedp.Evaluate(`document.querySelector('[data-error-for="/tools/1/usage"]').textContent.trim()`, &msg),
		chromedp.Evaluate(`document.querySelectorAll('#architecture-panel-tools [data-entry]')[1].querySelector('[data-field=usage]').getAttribute('aria-invalid')`, &invalid),
		poll(`!!document.querySelector('[data-problems]')`),
	)
	if msg != "✕ A tool that writes needs usage prose." || invalid != "true" {
		t.Fatalf("usage error = %q, aria-invalid = %q", msg, invalid)
	}
	if rig.api.lastSaved() != nil {
		t.Fatal("a refused save stored a record")
	}
	// Typing the prose and saving again goes through.
	rig.run(t,
		archFill("tools", 1, "usage", "Run only after the checks pass."),
		archClick(archSave),
		poll(`document.querySelector('[data-count=tools]').textContent.trim() === '2' && !document.querySelector('[data-error-for="/tools/1/usage"]') && document.querySelector('[data-save-bar]').checkVisibility() === false`),
	)
	if rig.api.lastSaved() == nil {
		t.Fatal("the corrected tool was not saved")
	}
}
