package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

const hooksPanel = `#architecture-panel-hooks`

func ruleField(i int, field string) string {
	return fmt.Sprintf(`#architecture-panel-hooks [data-rule-index="%d"] [data-field="%s"]`, i, field)
}

func recordWithoutRules() map[string]any {
	rec := savedRecord()
	rec["rules"] = []any{}
	rec["layers"] = []any{
		map[string]any{"name": "domain", "match": map[string]any{"folder": "internal/domain"}, "access": map[string]any{"default": "read", "agents": map[string]any{"software-architect": "change"}}},
	}
	return rec
}

// Add a blocking edit-outside-layer rule and a command rule, save, reload: each
// sits under its lifecycle point.
func TestHooksTabAddsRulesAndReloadsThemUnderTheirLifecyclePoints(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(recordWithoutRules())
	var groups map[string]string
	rig.run(t,
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(rig.url("/rules")),
		poll(`!!document.querySelector('#architecture-panel-hooks')?.checkVisibility()`),
		clickButton(hooksPanel, "Add edit-outside-layer rule"),
		poll(`!!document.querySelector('`+ruleField(0, "message")+`')`),
		setField(ruleField(0, "id"), "stay-in-your-layer", "input"),
		setField(ruleField(0, "message"), "Ask the architect first.", "input"),
		clickButton(hooksPanel, "Add command rule"),
		poll(`!!document.querySelector('`+ruleField(1, "message")+`')`),
		setField(ruleField(1, "id"), "archtest", "input"),
		setField(ruleField(1, "at"), "after_edit", "change"),
		setField(ruleField(1, "command"), "go vet ./...", "input"),
		setField(ruleField(1, "message"), "Vet failed.", "input"),
		poll(barVisible()),
		clickButton(saveBar, "Save"),
		poll(`!`+barVisible()),
		chromedp.Navigate(rig.url("/rules")),
		poll(`document.querySelectorAll('#architecture-panel-hooks [data-group]').length === 2`),
		chromedp.Evaluate(`Object.fromEntries([...document.querySelectorAll('#architecture-panel-hooks [data-group]')].map(g => [g.dataset.group, g.textContent.replace(/\s+/g, ' ')]))`, &groups),
	)
	rules, _ := rig.api.lastSaved()["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("saved rules = %v", rules)
	}
	edit, cmd := rules[0].(map[string]any), rules[1].(map[string]any)
	if edit["kind"] != "edit_outside_layer" || edit["mode"] != "blocking" || edit["id"] != "stay-in-your-layer" {
		t.Errorf("edit rule = %v", edit)
	}
	if cmd["kind"] != "command" || cmd["at"] != "after_edit" || cmd["command"] != "go vet ./..." {
		t.Errorf("command rule = %v", cmd)
	}
	if g := groups["before_tool_and_commit"]; !strings.Contains(g, "Before a tool call and at commit") || !strings.Contains(g, "stay-in-your-layer") {
		t.Errorf("edit group = %q", g)
	}
	if g := groups["after_edit"]; !strings.Contains(g, "After a file edit") || !strings.Contains(g, "archtest") {
		t.Errorf("after-edit group = %q", g)
	}
}

// The preview is what the agent sees, word for word, blocking and warning.
func TestHooksTabPreviewShowsTheAgentContractText(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(recordWithoutRules())
	var blocking, warning, afterEdit, note string
	rig.run(t,
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(rig.url("/rules")),
		poll(`!!document.querySelector('#architecture-panel-hooks')?.checkVisibility()`),
		clickButton(hooksPanel, "Add edit-outside-layer rule"),
		poll(`!!document.querySelector('`+ruleField(0, "message")+`')`),
		setField(ruleField(0, "id"), "stay", "input"),
		setField(ruleField(0, "message"), "Ask the architect.", "input"),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rule-index="0"] [data-preview]').textContent.trim()`, &blocking),
		clickButton(`#architecture-panel-hooks [data-rule-index="0"]`, "! Warning only"),
		poll(`document.querySelector('#architecture-panel-hooks [data-rule-index="0"] [data-preview]').textContent.includes('(warning)')`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rule-index="0"] [data-preview]').textContent.trim()`, &warning),
		clickButton(hooksPanel, "Add command rule"),
		poll(`!!document.querySelector('`+ruleField(1, "message")+`')`),
		setField(ruleField(1, "id"), "vet", "input"),
		setField(ruleField(1, "at"), "after_edit", "change"),
		setField(ruleField(1, "message"), "Vet failed.", "input"),
		clickButton(`#architecture-panel-hooks [data-rule-index="1"]`, "✕ Blocking"),
		poll(`document.querySelector('#architecture-panel-hooks [data-rule-index="1"] [data-preview]').textContent.includes('Undo or fix')`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rule-index="1"] [data-preview]').textContent.trim()`, &afterEdit),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks').textContent.replace(/\s+/g, ' ')`, &note),
	)
	wantB := "Architecture rule stay (blocking): Ask the architect. — internal/domain/example.go is in layer domain, which plain may not change."
	if blocking != wantB {
		t.Errorf("blocking preview = %q\nwant %q", blocking, wantB)
	}
	if want := strings.Replace(wantB, "(blocking)", "(warning)", 1); warning != want {
		t.Errorf("warning preview = %q\nwant %q", warning, want)
	}
	if !strings.HasPrefix(afterEdit, "Architecture rule vet (blocking): Vet failed.") || !strings.HasSuffix(afterEdit, "Undo or fix this change before you go on.") {
		t.Errorf("after-edit preview = %q", afterEdit)
	}
	if !strings.Contains(note, "edit proxy (ADR 0002)") {
		t.Errorf("no note on shell writes: %q", note)
	}
}

// Mode is readable without colour: its word and its glyph are in the text.
func TestHooksTabModeHasAWordAndAGlyph(t *testing.T) {
	rig := newArchRig(t)
	rec := recordWithoutRules()
	rec["rules"] = []any{
		map[string]any{"id": "a", "kind": "edit_outside_layer", "mode": "blocking", "message": "m"},
		map[string]any{"id": "b", "kind": "command", "at": "before_commit", "command": "x", "mode": "warning", "message": "m"},
	}
	rig.api.seed(rec)
	var a, b string
	rig.run(t,
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(rig.url("/rules")),
		poll(`!!document.querySelector('#architecture-panel-hooks [data-rule-index="1"] [data-mode]')`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rule-index="0"] [data-mode]').textContent.trim()`, &a),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rule-index="1"] [data-mode]').textContent.trim()`, &b),
	)
	if a != "✕ Blocking" || b != "! Warning only" {
		t.Fatalf("modes = %q, %q", a, b)
	}
}

// An empty message is an error on its field.
func TestHooksTabEmptyMessageShowsAnErrorOnItsField(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(recordWithoutRules())
	var msg string
	rig.run(t,
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(rig.url("/rules")),
		poll(`!!document.querySelector('#architecture-panel-hooks')?.checkVisibility()`),
		clickButton(hooksPanel, "Add command rule"),
		poll(`!!document.querySelector('`+ruleField(0, "message")+`')`),
		setField(ruleField(0, "command"), "make check", "input"),
		poll(barVisible()),
		clickButton(saveBar, "Save"),
		poll(`!!document.querySelector('[data-error-for="/rules/0/message"]')`),
		chromedp.Evaluate(`document.querySelector('[data-error-for="/rules/0/message"]').textContent.trim()`, &msg),
		poll(`document.querySelector('`+ruleField(0, "message")+`').getAttribute('aria-invalid') === 'true'`),
	)
	if msg != "A rule needs a message." {
		t.Fatalf("error = %q", msg)
	}
}
