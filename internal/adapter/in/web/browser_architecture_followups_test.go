package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// drawerSel is the edit drawer of one kind of entry (instruction, tool, sandbox, route, handoff).
func drawerSel(tab, kind, rest string) string {
	return fmt.Sprintf(`#architecture-panel-%s [data-drawer="%s"] %s`, tab, kind, rest)
}

// openDrawerOf clicks Edit on the nth entry of a kind and waits for the drawer to show that entry.
func openDrawerOf(tab, kind string, n int) chromedp.Action {
	return poll(fmt.Sprintf(`(() => {
		const d = document.querySelector('#architecture-panel-%s [data-drawer="%s"]');
		if (d && d.checkVisibility() && d.dataset.drawerIndex === '%d') return true;
		document.querySelectorAll('#architecture-panel-%s [data-entry="%s"]')[%d]?.querySelector('button[data-edit]')?.click();
		return false;
	})()`, tab, kind, n, tab, kind, n))
}

// setDrawerField types into a field of the open drawer.
func setDrawerField(tab, kind, field, value string) chromedp.Action {
	return poll(fmt.Sprintf(`(() => {
		const e = document.querySelector('#architecture-panel-%s [data-drawer="%s"] [data-field=%s]');
		if (!e) return false;
		e.value = %q;
		e.dispatchEvent(new Event('input', { bubbles: true }));
		e.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	})()`, tab, kind, field, value))
}

func discardChanges() chromedp.Action { return clickButton(saveBar, "Discard") }

// Delete layer and Delete rule are outlined danger buttons, not filled ones.
func TestDeleteButtonsAreOutlinedDanger(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	probe := func(sel string) string {
		return `(() => { const s = getComputedStyle(document.querySelector(` + "`" + sel + "`" + `)); return JSON.stringify({ bg: s.backgroundColor, line: s.borderTopColor, ink: s.color }) })()`
	}
	var layerBtn, ruleBtn string
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/layers")),
		poll(`!!document.querySelector('#architecture-panel-layers [data-remove-layer]')`),
		chromedp.Evaluate(probe(`#architecture-panel-layers [data-remove-layer]`), &layerBtn),
	)
	rig.run(t,
		chromedp.Navigate(rig.url("/rules")),
		archClick(ruleItem(0)),
		poll(`!!document.querySelector('#architecture-panel-hooks [data-delete-rule]')`),
		chromedp.Evaluate(probe(`#architecture-panel-hooks [data-delete-rule]`), &ruleBtn),
	)
	for name, got := range map[string]string{"Delete layer": layerBtn, "Delete rule": ruleBtn} {
		var v struct{ Bg, Line, Ink string }
		if err := json.Unmarshal([]byte(got), &v); err != nil {
			t.Fatal(err)
		}
		if v.Bg == v.Line || v.Ink != v.Line {
			t.Errorf("%s: background %s, border %s, text %s; want an outline (text and border the danger colour, background not)", name, v.Bg, v.Line, v.Ink)
		}
	}
}

// A row whose draft differs from the saved version carries an edited mark and an amber edge; Discard clears them.
func TestEditingARowMarksItEditedAndDiscardingClearsTheMark(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	openDescTab(t, rig, "instructions", "instructions")
	edited := `document.querySelectorAll('#architecture-panel-instructions [data-entry][data-edited=true]').length`
	flags := `document.querySelectorAll('#architecture-panel-instructions [data-entry] [data-flag=edited]').length`
	var before, afterEntries, afterFlags, edge, flagRow int
	var which string
	rig.run(t,
		chromedp.Evaluate(edited, &before),
		openDrawerOf("instructions", "instruction", 1),
		setDrawerField("instructions", "instruction", "note", "Read first."),
		archClick(drawerSel("instructions", "instruction", "[data-apply]")),
		poll(edited+` === 1`),
		chromedp.Evaluate(edited, &afterEntries),
		chromedp.Evaluate(flags, &afterFlags),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-instructions [data-entry]')].findIndex(e => e.dataset.edited === 'true')`, &flagRow),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('#architecture-panel-instructions [data-entry][data-edited=true]')).boxShadow.length > 4 ? 1 : 0`, &edge),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-instructions [data-flag=edited]').textContent.trim()`, &which),
	)
	if before != 0 || afterEntries != 1 || afterFlags != 1 || flagRow != 1 || which != "edited" || edge != 1 {
		t.Fatalf("edited rows before %d, after %d (flags %d, row %d, text %q, amber edge %d); want the second row alone, marked", before, afterEntries, afterFlags, flagRow, which, edge)
	}
	rig.run(t,
		discardChanges(),
		poll(edited+` === 0`),
		poll(flags+` === 0`),
	)
}

// An instruction path that discovery did not find carries a "missing" tag and a note; paths of harness:// and found paths do not.
func TestAnInstructionMissingFromDiscoveryShowsTheTagAndNote(t *testing.T) {
	rig := newArchRig(t)
	rec := savedRecord()
	rec["instructions"] = []any{
		map[string]any{"path": "CLAUDE.md", "kind": "rules", "applies_to": []any{"all"}, "loaded": "always"},
		map[string]any{"path": "GEMINI.md", "kind": "rules", "applies_to": []any{"all"}, "loaded": "always"},
		map[string]any{"path": "harness://task-brief", "kind": "rules", "applies_to": []any{"all"}, "loaded": "always"},
	}
	rig.api.seed(rec)
	row := func(i int) string {
		return fmt.Sprintf(`document.querySelectorAll('#architecture-panel-instructions [data-entry]')[%d]`, i)
	}
	var tags, notes string
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/instructions")),
		poll(`document.querySelectorAll('#architecture-panel-instructions [data-entry]').length === 3`),
		poll(`!!`+row(1)+`.querySelector('[data-flag=missing]')`),
		chromedp.Evaluate(`[0, 1, 2].map(i => document.querySelectorAll('#architecture-panel-instructions [data-entry]')[i].querySelector('[data-flag=missing]')?.textContent.trim() ?? '-').join('|')`, &tags),
		chromedp.Evaluate(`[0, 1, 2].map(i => document.querySelectorAll('#architecture-panel-instructions [data-entry]')[i].querySelector('[data-missing-note]')?.textContent.trim() ?? '-').join('|')`, &notes),
	)
	if tags != "-|missing|-" || notes != "-|Not in the repository|-" {
		t.Fatalf("missing tags = %q, notes = %q", tags, notes)
	}
}

// The marks come from the page's one discovery (no second call), read when the Instructions tab opens; when it fails nothing is marked and nothing breaks.
func TestMissingInstructionsShareThePagesDiscoveryAndFailureIsSilent(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/tools")),
		poll(`document.querySelectorAll('#architecture-panel-tools [data-entry]').length === 2`),
	)
	if n := rig.api.discoverCalls(); n != 1 { // the page's one discovery, which the agent pickers read
		t.Fatalf("discovery asked %d times before the Instructions tab opened, want the page's one", n)
	}
	rig.run(t,
		archClick(`#architecture-tab-instructions`),
		poll(`document.querySelectorAll('#architecture-panel-instructions [data-entry]').length === 2`),
		poll(`document.querySelectorAll('#architecture-panel-instructions [data-flag=missing]').length === 1`), // .claude/skills/go/SKILL.md is not in the fake's discovery
	)
	if n := rig.api.discoverCalls(); n != 1 { // the marks read the same discovery: no second call
		t.Fatalf("discovery asked %d times after the tab opened, want the same one", n)
	}

	failing := newArchRig(t)
	failing.api.discoverFails = true
	failing.api.seed(descRecord())
	failing.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(failing.url("/instructions")),
		poll(`document.querySelectorAll('#architecture-panel-instructions [data-entry]').length === 2`),
	)
	var marks int
	failing.run(t, chromedp.Sleep(300*1e6), chromedp.Evaluate(`document.querySelectorAll('#architecture-panel-instructions [data-flag=missing]').length`, &marks))
	if marks != 0 {
		t.Fatalf("%d rows marked missing though discovery failed", marks)
	}
	if e := failing.errs.all(); len(e) > 0 {
		for _, line := range e {
			if !strings.Contains(line, "discover") && !strings.Contains(line, "500") {
				t.Errorf("JavaScript error: %s", line)
			}
		}
	}
}

// The hand-off chain card follows the record's hand-offs in order, joining hand-offs that continue each other.
func TestTheHandoffChainCardShowsTheRecordsHandoffsInOrder(t *testing.T) {
	rig := newArchRig(t)
	rec := descRecord()
	rec["orchestration"] = map[string]any{
		"routing": []any{},
		"handoffs": []any{
			map[string]any{"from": "software-architect", "to": "go-developer", "when": "approved"},
			map[string]any{"from": "go-developer", "to": "code-reviewer", "when": "built"},
			map[string]any{"from": "code-reviewer", "to": "documentator", "when": "reviewed"},
			map[string]any{"from": "devops", "to": "go-developer", "when": "a deploy fails"},
		},
	}
	rig.api.seed(rec)
	var title, chains string
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/orchestration")),
		poll(`!!document.querySelector('#architecture-panel-orchestration [data-handoff-chain]')`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-orchestration [data-chain-card] header h3').textContent.trim()`, &title),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-orchestration [data-handoff-chain]')].map(c => [...c.querySelectorAll('[data-chain-agent]')].map(a => a.textContent.trim()).join(' → ')).join(' | ')`, &chains),
	)
	if title != "Hand-off chain" || chains != "software-architect → go-developer → code-reviewer → documentator | devops → go-developer" {
		t.Fatalf("card %q, chains %q", title, chains)
	}
	// no hand-offs: no card
	none := newArchRig(t)
	rec2 := descRecord()
	rec2["orchestration"] = map[string]any{"routing": []any{}, "handoffs": []any{}}
	none.api.seed(rec2)
	var cards int
	none.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(none.url("/orchestration")),
		poll(`document.querySelectorAll('[data-count]').length === 6`),
		chromedp.Sleep(200*1e6),
		chromedp.Evaluate(`document.querySelectorAll('#architecture-panel-orchestration [data-chain-card]').length`, &cards),
	)
	if cards != 0 {
		t.Fatalf("the chain card shows with no hand-offs")
	}
}

// Edit opens a drawer with a working copy: Apply writes it to the draft, Cancel leaves the draft alone, and the fields keep their labels.
func TestTheEditDrawerAppliesOrCancelsAndKeepsTheFieldLabels(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	openDescTab(t, rig, "instructions", "instructions")
	var closed, labels, title string
	var open bool
	pathOf := `document.querySelectorAll('#architecture-panel-instructions [data-entry]')[0].querySelector('[data-row] [data-path]').textContent.trim()`
	var shown string
	rig.run(t,
		chromedp.Evaluate(`String(!!document.querySelector('#architecture-panel-instructions [data-drawer]')?.checkVisibility())`, &closed),
		openDrawerOf("instructions", "instruction", 0),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-instructions [data-drawer] .field > label')].map(l => [...l.childNodes].find(n => n.nodeType === 3).textContent.trim()).join('|')`, &labels),
		chromedp.Evaluate(textOf(`#architecture-panel-instructions [data-drawer] header h3`), &title),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-instructions [data-entry] [data-edit]').getAttribute('aria-expanded') === 'true'`, &open),
		// typing changes the working copy only
		setDrawerField("instructions", "instruction", "path", "AGENTS.md"),
		chromedp.Evaluate(pathOf, &shown),
	)
	if closed != "false" || !open {
		t.Fatalf("drawer visible before Edit = %q; Edit button aria-expanded after = %v", closed, open)
	}
	if title != "Edit instruction file" {
		t.Errorf("drawer title = %q", title)
	}
	if labels != "Path|Kind|Loaded|Note" {
		t.Errorf("drawer labels = %q", labels)
	}
	if shown != "CLAUDE.md" {
		t.Fatalf("the row shows %q while typing: the working copy leaked into the draft", shown)
	}
	rig.run(t,
		archClick(drawerSel("instructions", "instruction", "[data-cancel]")),
		poll(`!document.querySelector('#architecture-panel-instructions [data-drawer]').checkVisibility()`),
		chromedp.Evaluate(pathOf, &shown),
	)
	var saveBarShown bool
	rig.run(t, chromedp.Evaluate(`!!document.querySelector('[data-save-bar]')?.checkVisibility()`, &saveBarShown))
	if shown != "CLAUDE.md" || saveBarShown {
		t.Fatalf("after Cancel the row shows %q, save bar visible %v; want the draft untouched", shown, saveBarShown)
	}
	rig.run(t,
		openDrawerOf("instructions", "instruction", 0),
		poll(`document.querySelector('#architecture-panel-instructions [data-drawer] [data-field=path]').value === 'CLAUDE.md'`), // the working copy starts over from the draft
		setDrawerField("instructions", "instruction", "path", "AGENTS.md"),
		archClick(drawerSel("instructions", "instruction", "[data-apply]")),
		poll(pathOf+` === 'AGENTS.md'`),
		poll(`!document.querySelector('#architecture-panel-instructions [data-drawer]').checkVisibility()`),
		poll(`!!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
}

// A new row opens the drawer; Cancel on it drops the row, Apply keeps it.
func TestANewRowOpensTheDrawerAndCancelDropsIt(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	openDescTab(t, rig, "tools", "tools")
	count := `document.querySelectorAll('#architecture-panel-tools [data-entry]').length`
	var n int
	rig.run(t,
		archClick(`#architecture-panel-tools [data-add]`),
		poll(count+` === 3`),
		poll(`document.querySelector('#architecture-panel-tools [data-drawer]').checkVisibility() && document.querySelector('#architecture-panel-tools [data-drawer]').dataset.drawerIndex === '2'`),
		archClick(drawerSel("tools", "tool", "[data-cancel]")),
		poll(count+` === 2`),
		poll(`!!document.querySelector('[data-save-bar]') && !document.querySelector('[data-save-bar]').checkVisibility()`),
		archClick(`#architecture-panel-tools [data-add]`),
		poll(count+` === 3`),
		setDrawerField("tools", "tool", "name", "gh"),
		archClick(drawerSel("tools", "tool", "[data-apply]")),
		poll(`document.querySelectorAll('#architecture-panel-tools [data-entry]')[2].querySelector('[data-name]').textContent.trim() === 'gh'`),
		chromedp.Evaluate(count, &n),
	)
	if n != 3 {
		t.Fatalf("tools = %d, want the applied row kept", n)
	}
}
