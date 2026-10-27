package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// archPick makes the checkboxes of an agent list field of the nth entry of a
// tab match exactly the given names (a person ticks "all" or agents), through
// the same clicks, in the entry's edit drawer, then applies. It waits for the
// catalog's choices to be there.
func archPick(tab string, n int, field string, names ...string) chromedp.Action {
	kind := drawerKinds[tab]
	return chromedp.Tasks{
		openDrawerOf(tab, kind, n),
		poll(fmt.Sprintf(`(() => {
			const box = document.querySelector('#architecture-panel-%s [data-drawer="%s"] [data-field=%s]');
			const want = %q.split(',').filter(Boolean);
			if (!box) return false;
			const all = [...box.querySelectorAll('input[type=checkbox]')];
			if (!want.every(w => all.some(c => c.value === w))) return false;
			// ticking "all" clears the others, ticking an agent clears "all": click the wanted ones in order
			for (const w of want) { const c = all.find(c => c.value === w); if (!c.checked) c.click(); }
			for (const c of all) { if (c.checked && !want.includes(c.value)) c.click(); }
			return all.filter(c => c.checked).map(c => c.value).sort().join(',') === [...want].sort().join(',');
		})()`, tab, kind, field, strings.Join(names, ","))),
		archClick(drawerSel(tab, kind, "[data-apply]")),
	}
}

// archOptions reads a select's options as "value=label" strings.
func archOptions(sel string) string {
	return fmt.Sprintf(`[...document.querySelector(%q).options].map(o => o.value + '=' + o.textContent.trim()).join('|')`, sel)
}

func contextSel(i int) string { return layerSel(i, `select[data-field=bounded_context]`) }

// A layer's bounded context is picked from the project's contexts, or none.
func TestALayerPicksItsBoundedContextFromTheProjectsContexts(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var options string
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "pattern", "internal/domain"),
		poll(fmt.Sprintf(`document.querySelector(%q)?.options.length === 3`, contextSel(0))),
		chromedp.Evaluate(archOptions(contextSel(0)), &options),
	)
	if want := "=None|sessions=sessions|billing=billing"; options != want {
		t.Fatalf("options = %q, want %q", options, want)
	}
	if got := rig.api.contextProjects; len(got) != 1 || got[0] != "p1" {
		t.Fatalf("list_bounded_contexts asked for projects %v, want [p1] once", got)
	}
	rig.run(t,
		setLayerField(0, "bounded_context", "billing"),
		archClick(archSave),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	if l := savedLayer(t, rig, 0); l["bounded_context"] != "billing" {
		t.Fatalf("saved layer = %v, want bounded_context billing", l)
	}
	// choosing none drops the key
	rig.run(t,
		setLayerField(0, "bounded_context", ""),
		archClick(archSave),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	if l := savedLayer(t, rig, 0); l["bounded_context"] != nil {
		t.Fatalf("saved layer = %v, want no bounded_context", l)
	}
}

// A saved context name that is no longer in the project's list still shows, marked, and is kept.
func TestASavedBoundedContextMissingFromTheListIsShownAndKept(t *testing.T) {
	rig := newArchRig(t)
	rec := savedRecord()
	rec["layers"].([]any)[0].(map[string]any)["bounded_context"] = "legacy"
	rig.api.seed(rec)
	var options, selected string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/layers")),
		poll(fmt.Sprintf(`document.querySelector(%q)?.options.length === 4`, contextSel(0))),
		chromedp.Evaluate(archOptions(contextSel(0)), &options),
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector(%q).value`, contextSel(0)), &selected),
	)
	if want := "=None|sessions=sessions|billing=billing|legacy=legacy (not in the list)"; options != want {
		t.Fatalf("options = %q, want %q", options, want)
	}
	if selected != "legacy" {
		t.Fatalf("selected = %q, want legacy", selected)
	}
	rig.run(t,
		setLayerField(0, "purpose", "Edited."),
		archClick(archSave),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	if l := savedLayer(t, rig, 0); l["bounded_context"] != "legacy" {
		t.Fatalf("saved layer = %v, bounded_context dropped", l)
	}
}

// The server's UNKNOWN_BOUNDED_CONTEXT stays on the field after a save attempt.
func TestUnknownBoundedContextFromTheServerShowsOnTheField(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	rig.api.refuseSaveWith = []archError{{"path": "/layers/0/bounded_context", "code": "UNKNOWN_BOUNDED_CONTEXT", "message": "No bounded context is called legacy."}}
	var msg, invalid string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/layers")),
		poll(fmt.Sprintf(`document.querySelector(%q)?.options.length === 3`, contextSel(0))),
		setLayerField(0, "bounded_context", "billing"),
		archClick(archSave),
		poll(`!!document.querySelector('[data-error-for="/layers/0/bounded_context"]')`),
		chromedp.Evaluate(`document.querySelector('[data-error-for="/layers/0/bounded_context"]').textContent.trim()`, &msg),
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector(%q).getAttribute('aria-invalid')`, contextSel(0)), &invalid),
	)
	if !strings.Contains(msg, "No bounded context is called legacy.") || invalid != "true" {
		t.Fatalf("error = %q, aria-invalid = %q", msg, invalid)
	}
}

// Agent lists are checkbox groups over the catalog plus all; a name the
// record has that the catalog lacks is shown marked and kept; the catalog is
// fetched once for every tab and the matrix.
func TestAgentListsArePickedFromTheCatalogAndKeepUnknownNames(t *testing.T) {
	rig := newArchRig(t)
	rec := savedRecord()
	rec["instructions"].([]any)[1].(map[string]any)["applies_to"] = []any{"go-developer", "ghost"}
	rig.api.seed(rec)
	var labels, checked string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/instructions")),
		openDrawerOf("instructions", "instruction", 1),
		poll(`document.querySelectorAll('#architecture-panel-instructions [data-drawer] [data-field=applies_to] input[type=checkbox]').length === 6`),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-instructions [data-drawer] [data-field=applies_to] label')].map(l => l.textContent.trim().replace(/\s+/g, ' ')).join('|')`, &labels),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-instructions [data-drawer] [data-field=applies_to] input:checked')].map(c => c.value).join(',')`, &checked),
	)
	if want := "all|software-architect|go-developer|devops|plain (a session with no agent)|ghost (not in the catalog)"; labels != want {
		t.Fatalf("labels = %q, want %q", labels, want)
	}
	if checked != "go-developer,ghost" {
		t.Fatalf("checked = %q", checked)
	}
	rig.run(t,
		archPick("instructions", 1, "applies_to", "go-developer", "ghost", "devops"),
		archClick(archSave),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	in := rig.api.lastSaved()["instructions"].([]any)[1].(map[string]any)
	if fmt.Sprint(in["applies_to"]) != "[go-developer ghost devops]" {
		t.Fatalf("saved applies_to = %v", in["applies_to"])
	}
	rig.run(t, chromedp.Navigate(rig.url("/layers")), poll(`!!document.querySelector('[data-access-matrix] [data-row=devops]')`))
	if n := rig.api.discoverCalls(); n != 2 { // one per page load, shared by every tab and the matrix
		t.Fatalf("discover called %d times over two page loads, want 2", n)
	}
}

// Ticking all clears the agents and ticking an agent clears all.
func TestTickingAllAndAnAgentExcludeEachOther(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var checked string
	usedBy := `document.querySelector('main')._x_dataStack[0].draft.tools[0].used_by.join(',')` // Apply wrote the pick to the draft
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/tools")),
		poll(`document.querySelectorAll('#architecture-panel-tools [data-entry]').length === 1`),
		openDrawerOf("tools", "tool", 0),
		poll(`!!document.querySelector('#architecture-panel-tools [data-field=used_by] input[value=devops]')`),
		archPick("tools", 0, "used_by", "devops"),
		chromedp.Evaluate(usedBy, &checked),
	)
	if checked != "devops" {
		t.Fatalf("after ticking devops: %q, want devops alone", checked)
	}
	rig.run(t,
		archPick("tools", 0, "used_by", "all"),
		chromedp.Evaluate(usedBy, &checked),
	)
	if checked != "all" {
		t.Fatalf("after ticking all: %q, want all alone", checked)
	}
}

// Single agent fields are selects with a blank first option; an unknown name stays, marked; UNKNOWN_AGENT stays on the field.
func TestRoutingAndHandoffAgentsAreSelectsAndKeepUnknownNames(t *testing.T) {
	rig := newArchRig(t)
	rec := savedRecord()
	rec["orchestration"].(map[string]any)["routing"].([]any)[0].(map[string]any)["agent"] = "ghost"
	rig.api.seed(rec)
	rig.api.refuseSaveWith = []archError{{"path": "/orchestration/handoffs/0/to", "code": "UNKNOWN_AGENT", "message": "No agent is called nobody."}}
	routeAgent := `#architecture-panel-orchestration [data-drawer=route] select[data-field=agent]`
	var options, selected, msg string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/orchestration")),
		poll(`document.querySelectorAll('#architecture-panel-orchestration [data-entry=route]').length === 1`),
		openDrawerOf("orchestration", "route", 0),
		poll(fmt.Sprintf(`document.querySelector(%q)?.options.length === 6`, routeAgent)),
		chromedp.Evaluate(archOptions(routeAgent), &options),
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector(%q).value`, routeAgent), &selected),
	)
	if want := "=|software-architect=software-architect|go-developer=go-developer|devops=devops|plain=plain (a session with no agent)|ghost=ghost (not in the catalog)"; len(strings.Split(options, "|")) != 6 || !strings.HasPrefix(options, "=") || !strings.HasSuffix(options, "ghost=ghost (not in the catalog)") {
		t.Fatalf("options = %q, want like %q", options, want)
	}
	if selected != "ghost" {
		t.Fatalf("selected = %q", selected)
	}
	rig.run(t,
		archFillHandoff(0, "to", "devops"),
		archClick(archSave),
		poll(`!!document.querySelector('[data-error-for="/orchestration/handoffs/0/to"]')`),
		chromedp.Evaluate(`document.querySelector('[data-error-for="/orchestration/handoffs/0/to"]').textContent.trim()`, &msg),
	)
	if !strings.Contains(msg, "No agent is called nobody.") {
		t.Fatalf("error = %q", msg)
	}
	rig.run(t,
		archClick(archSave),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	orch := rig.api.lastSaved()["orchestration"].(map[string]any)
	if r := orch["routing"].([]any)[0].(map[string]any); r["agent"] != "ghost" {
		t.Fatalf("saved route = %v, the unknown agent was dropped", r)
	}
	if h := orch["handoffs"].([]any)[0].(map[string]any); h["to"] != "devops" {
		t.Fatalf("saved hand-off = %v", h)
	}
}

// Zone assignment offers the project's enabled agents only (ADR 0038): an
// agent the project does not enable is absent from the lists, and one the
// record already names stays, ticked and marked, so nothing assigned is altered.
func TestAgentListsOfferTheProjectsEnabledAgentsOnly(t *testing.T) {
	rig := newArchRig(t)
	rig.api.enabled = []string{"software-architect", "go-developer"}
	rec := savedRecord()
	rec["instructions"].([]any)[1].(map[string]any)["applies_to"] = []any{"go-developer", "devops"}
	rig.api.seed(rec)
	var labels, checked string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/instructions")),
		openDrawerOf("instructions", "instruction", 1),
		poll(`document.querySelectorAll('#architecture-panel-instructions [data-drawer] [data-field=applies_to] input[type=checkbox]').length === 5`),
		chromedp.Evaluate(`[...document.querySelector('#architecture-panel-instructions [data-drawer]').querySelectorAll('[data-field=applies_to] label')].map(l => l.textContent.trim().replace(/\s+/g, ' ')).join('|')`, &labels),
		chromedp.Evaluate(`[...document.querySelector('#architecture-panel-instructions [data-drawer]').querySelectorAll('[data-field=applies_to] input:checked')].map(c => c.value).join(',')`, &checked),
	)
	if want := "all|software-architect|go-developer|plain (a session with no agent)|devops (not enabled in this project)"; labels != want {
		t.Fatalf("labels = %q, want %q", labels, want)
	}
	if checked != "go-developer,devops" {
		t.Fatalf("checked = %q; an existing assignment must stay", checked)
	}
	rig.api.mu.Lock()
	asked := fmt.Sprint(rig.api.enabledProjects)
	rig.api.mu.Unlock()
	if asked != "[p1]" {
		t.Fatalf("list_agents asked for projects %s, want [p1]", asked)
	}
}
