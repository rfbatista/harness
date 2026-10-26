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

func ruleItem(i int) string {
	return fmt.Sprintf(`#architecture-panel-hooks [data-rule-item="%d"]`, i)
}

func selectRule(i int) chromedp.Action {
	return chromedp.Click(ruleItem(i), chromedp.ByQuery)
}

func hooksOpen() chromedp.Action {
	return poll(`!!document.querySelector('#architecture-panel-hooks')?.checkVisibility()`)
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
		poll(`!!document.querySelector('#architecture-panel-hooks [data-add-edit]')`),
		chromedp.Click(`#architecture-panel-hooks [data-add-edit]`, chromedp.ByQuery),
		poll(`!!document.querySelector('`+ruleField(0, "message")+`')`),
		setField(ruleField(0, "id"), "stay-in-your-layer", "input"),
		setField(ruleField(0, "message"), "Ask the architect first.", "input"),
		chromedp.Click(`#architecture-panel-hooks [data-add-rule]`, chromedp.ByQuery),
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
		poll(`!!document.querySelector('#architecture-panel-hooks [data-add-edit]')`),
		chromedp.Click(`#architecture-panel-hooks [data-add-edit]`, chromedp.ByQuery),
		poll(`!!document.querySelector('`+ruleField(0, "message")+`')`),
		setField(ruleField(0, "id"), "stay", "input"),
		setField(ruleField(0, "message"), "Ask the architect.", "input"),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rule-index="0"] [data-preview]').textContent.trim()`, &blocking),
		chromedp.Click(`#architecture-panel-hooks [data-rule-index="0"] [data-mode-card=warning]`, chromedp.ByQuery),
		poll(`document.querySelector('#architecture-panel-hooks [data-rule-index="0"] [data-preview]').textContent.includes('(warning)')`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rule-index="0"] [data-preview]').textContent.trim()`, &warning),
		chromedp.Click(`#architecture-panel-hooks [data-add-rule]`, chromedp.ByQuery),
		poll(`!!document.querySelector('`+ruleField(1, "message")+`')`),
		setField(ruleField(1, "id"), "vet", "input"),
		setField(ruleField(1, "at"), "after_edit", "change"),
		setField(ruleField(1, "message"), "Vet failed.", "input"),
		chromedp.Click(`#architecture-panel-hooks [data-rule-index="1"] [data-mode-card=blocking]`, chromedp.ByQuery),
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
		poll(`!!document.querySelector('`+ruleItem(1)+` [data-mode]')`),
		chromedp.Evaluate(`document.querySelector('`+ruleItem(0)+` [data-mode]').textContent.trim()`, &a),
		chromedp.Evaluate(`document.querySelector('`+ruleItem(1)+` [data-mode]').textContent.trim()`, &b),
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
		hooksOpen(),
		poll(`!!document.querySelector('#architecture-panel-hooks [data-add-scratch]')`),
		chromedp.Click(`#architecture-panel-hooks [data-add-scratch]`, chromedp.ByQuery),
		poll(`!!document.querySelector('`+ruleField(0, "message")+`')`),
		setField(ruleField(0, "command"), "make check", "input"),
		poll(barVisible()),
		clickButton(saveBar, "Save"),
		poll(`!!document.querySelector('[data-error-for="/rules/0/message"]')`),
		chromedp.Evaluate(`document.querySelector('[data-error-for="/rules/0/message"]').textContent.trim()`, &msg),
		poll(`document.querySelector('`+ruleField(0, "message")+`').getAttribute('aria-invalid') === 'true'`),
	)
	if msg != "A message is required. Say what was refused and what to do instead." {
		t.Fatalf("error = %q", msg)
	}
	var banner string
	rig.run(t,
		poll(`!!document.querySelector('#architecture-panel-hooks [data-validation]')`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-validation]').textContent.replace(/\s+/g, ' ').trim()`, &banner),
	)
	if !strings.HasPrefix(banner, "“check” cannot be saved.") || !strings.Contains(banner, "A message is required.") {
		t.Fatalf("banner = %q", banner)
	}
}

// The empty state offers both starts, and each adds a rule to the draft.
func TestHooksTabEmptyStateOffersBothStarts(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(recordWithoutRules())
	var title, text string
	var buttons []string
	var listShown bool
	rig.run(t,
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(rig.url("/rules")),
		hooksOpen(),
		poll(`!!document.querySelector('#architecture-panel-hooks [data-no-rules]')?.checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-no-rules] h3').textContent.trim()`, &title),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-no-rules]').textContent.replace(/\s+/g, ' ')`, &text),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-hooks [data-no-rules] button')].map(b => b.textContent.trim())`, &buttons),
		chromedp.Evaluate(`!!document.querySelector('#architecture-panel-hooks [data-rules-list]')?.checkVisibility()`, &listShown),
	)
	if title != "No hook rules yet" || !strings.Contains(text, "Layers are only advice until a rule enforces them.") {
		t.Errorf("empty state = %q / %q", title, text)
	}
	if len(buttons) != 2 || buttons[0] != "Add “Edit outside change layers” (blocking)" || buttons[1] != "Add a rule from scratch" {
		t.Errorf("buttons = %q", buttons)
	}
	if listShown {
		t.Error("the rule list shows beside an empty state")
	}
	var kind, mode, msg string
	rig.run(t,
		chromedp.Click(`#architecture-panel-hooks [data-add-edit]`, chromedp.ByQuery),
		poll(`!!document.querySelector('`+ruleField(0, "message")+`')`),
		chromedp.Evaluate(`document.querySelector('`+ruleField(0, "kind")+`').value`, &kind),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rule-index="0"] [data-mode-card][aria-pressed=true]').dataset.modeCard`, &mode),
		chromedp.Evaluate(`document.querySelector('`+ruleField(0, "message")+`').value`, &msg),
		chromedp.Evaluate(`!!document.querySelector('#architecture-panel-hooks [data-no-rules]')?.checkVisibility()`, &listShown),
	)
	if kind != "edit_outside_layer" || mode != "blocking" || msg == "" || listShown {
		t.Errorf("edit rule: kind %q, mode %q, message %q, empty state still shown %v", kind, mode, msg, listShown)
	}
}

// The tab follows the hook-rules design: the heading, then the rule list
// beside the editor with the preview under it, and a Condition that holds
// only what the record holds.
func TestHooksTabFollowsTheDesignsStructure(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var headings []string
	var listLeft, editorLeft, editorTop, previewTop, listTop float64
	var keys0, keys1 []string
	var modeCards []string
	var text string
	rig.run(t,
		chromedp.EmulateViewport(1440, 1000),
		chromedp.Navigate(rig.url("/rules")),
		hooksOpen(),
		poll(`!!document.querySelector('`+ruleItem(1)+`')`),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-hooks h2, #architecture-panel-hooks h3')].filter(h => h.checkVisibility()).map(h => h.textContent.trim())`, &headings),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rules-list]').getBoundingClientRect().left`, &listLeft),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-rules-list]').getBoundingClientRect().top`, &listTop),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-editor]').getBoundingClientRect().left`, &editorLeft),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-editor]').getBoundingClientRect().top`, &editorTop),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-preview-card]').getBoundingClientRect().top`, &previewTop),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-hooks [data-editor] [data-cond-key]')].map(k => k.textContent.trim())`, &keys0),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-hooks [data-mode-card]')].map(b => b.querySelector('b').textContent.trim())`, &modeCards),
		selectRule(1),
		poll(`!!document.querySelector('`+ruleField(1, "command")+`')`),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-hooks [data-editor] [data-cond-key]')].map(k => k.textContent.trim())`, &keys1),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks').textContent.replace(/\s+/g, ' ')`, &text),
	)
	if got := strings.Join(headings, "|"); got != "Hook rules|Rules|stay-in-your-layer|What the agent sees" {
		t.Errorf("headings = %q", got)
	}
	if !(listLeft < editorLeft) || listTop > editorTop+1 || previewTop <= editorTop {
		t.Errorf("layout: list left %v top %v, editor left %v top %v, preview top %v", listLeft, listTop, editorLeft, editorTop, previewTop)
	}
	if strings.Join(keys0, "|") != "When|By" || strings.Join(keys1, "|") != "When|In|By" {
		t.Errorf("condition keys: edit rule %q, command rule %q", keys0, keys1)
	}
	if strings.Join(modeCards, "|") != "Blocking|Warning only" {
		t.Errorf("mode cards = %q", modeCards)
	}
	for _, left := range []string{"Except", "Escape hatch", "Roll out", "Run against last 7 days", "Warn for 7 days", "would block", "Insert:"} {
		if strings.Contains(text, left) {
			t.Errorf("the tab shows %q, which nothing in the record backs", left)
		}
	}
	for _, want := range []string{"Trigger", "Tools it watches", "Condition", "Mode", "Message the agent sees", "Duplicate", "Delete"} {
		if !strings.Contains(text, want) {
			t.Errorf("the tab lacks %q", want)
		}
	}
}

// Blocking and Warning only change the draft and the preview, nothing else.
func TestHooksTabModeCardsChangeTheDraftAndThePreview(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var pressed, badge, preview, listBadge string
	rig.run(t,
		chromedp.EmulateViewport(1440, 1000),
		chromedp.Navigate(rig.url("/rules")),
		hooksOpen(),
		poll(`!!document.querySelector('`+ruleField(0, "message")+`')`),
		chromedp.Click(`#architecture-panel-hooks [data-mode-card=warning]`, chromedp.ByQuery),
		poll(`document.querySelector('#architecture-panel-hooks [data-mode-card=warning]').getAttribute('aria-pressed') === 'true'`),
		poll(`document.querySelector('#architecture-panel-hooks [data-preview]').textContent.includes('(warning)')`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-mode-card=blocking]').getAttribute('aria-pressed')`, &pressed),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-editor] [data-mode]').textContent.trim()`, &badge),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-preview]').textContent.trim()`, &preview),
		chromedp.Evaluate(`document.querySelector('`+ruleItem(0)+` [data-mode]').textContent.trim()`, &listBadge),
		poll(barVisible()),
		clickButton(saveBar, "Save"),
		poll(`!`+barVisible()),
	)
	if pressed != "false" || badge != "! Warning only" || listBadge != "! Warning only" || !strings.Contains(preview, "Architecture rule stay-in-your-layer (warning): Ask the architect.") {
		t.Errorf("pressed %q, badge %q, list badge %q, preview %q", pressed, badge, listBadge, preview)
	}
	rules, _ := rig.api.lastSaved()["rules"].([]any)
	if first := rules[0].(map[string]any); first["mode"] != "warning" || first["message"] != "Ask the architect." {
		t.Errorf("saved rule = %v", first)
	}
}

// Duplicate copies a command rule under a fresh id, Delete removes the open rule,
// and the When select turns a command rule into the edit-outside-layer rule only
// when there is none yet.
func TestHooksTabDuplicatesDeletesAndSwitchesWhat(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var ids []string
	var editOffered bool
	var dupDisabled bool
	rig.run(t,
		chromedp.EmulateViewport(1440, 1000),
		chromedp.Navigate(rig.url("/rules")),
		hooksOpen(),
		poll(`!!document.querySelector('`+ruleItem(1)+`')`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-duplicate]').disabled`, &dupDisabled),
		selectRule(1),
		poll(`!!document.querySelector('`+ruleField(1, "command")+`')`),
		chromedp.Evaluate(`[...document.querySelector('`+ruleField(1, "kind")+`').options].some(o => o.value === 'edit_outside_layer' && !o.disabled)`, &editOffered),
		chromedp.Click(`#architecture-panel-hooks [data-duplicate]`, chromedp.ByQuery),
		poll(`document.querySelectorAll('#architecture-panel-hooks [data-rule-item]').length === 3`),
		poll(`!!document.querySelector('`+ruleField(2, "command")+`')`),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-hooks [data-rule-item] [data-rule-name]')].map(n => n.textContent.trim())`, &ids),
	)
	if !dupDisabled {
		t.Error("the edit-outside-layer rule can be duplicated: only one may exist")
	}
	if editOffered {
		t.Error("a second edit-outside-layer rule is offered")
	}
	if strings.Join(ids, "|") != "stay-in-your-layer|archtest|archtest-copy" {
		t.Errorf("ids = %q", ids)
	}
	rig.run(t,
		chromedp.Click(`#architecture-panel-hooks [data-delete-rule]`, chromedp.ByQuery),
		poll(`document.querySelectorAll('#architecture-panel-hooks [data-rule-item]').length === 2`),
		poll(`!`+barVisible()), // the draft is the saved record again
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-hooks [data-rule-item] [data-rule-name]')].map(n => n.textContent.trim())`, &ids),
	)
	if strings.Join(ids, "|") != "stay-in-your-layer|archtest" {
		t.Errorf("ids after Delete = %q", ids)
	}
	// Delete the edit rule, then a command rule can become one.
	var kind string
	rig.run(t,
		selectRule(0),
		poll(`document.querySelector('`+ruleField(0, "kind")+`')?.value === 'edit_outside_layer'`),
		chromedp.Click(`#architecture-panel-hooks [data-delete-rule]`, chromedp.ByQuery),
		poll(`document.querySelectorAll('#architecture-panel-hooks [data-rule-item]').length === 1`),
		poll(`!!document.querySelector('`+ruleField(0, "kind")+`')`),
		setField(ruleField(0, "kind"), "edit_outside_layer", "change"),
		poll(`!document.querySelector('`+ruleField(0, "command")+`')`),
		chromedp.Evaluate(`document.querySelector('`+ruleField(0, "kind")+`').value`, &kind),
	)
	if kind != "edit_outside_layer" {
		t.Errorf("kind = %q", kind)
	}
}

// Agents are chosen as chips from the catalog; none chosen means every agent.
func TestHooksTabAgentChipsWriteTheRulesAgents(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var chips []string
	rig.run(t,
		chromedp.EmulateViewport(1440, 1000),
		chromedp.Navigate(rig.url("/rules")),
		hooksOpen(),
		poll(`document.querySelectorAll('#architecture-panel-hooks [data-agent-chip]').length >= 3`),
		chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-hooks [data-agent-chip]')].map(c => c.textContent.trim())`, &chips),
		chromedp.Click(`#architecture-panel-hooks [data-agent-chip="go-developer"] input`, chromedp.ByQuery),
		poll(barVisible()),
		clickButton(saveBar, "Save"),
		poll(`!`+barVisible()),
	)
	if !contains(chips, "go-developer") || !contains(chips, "software-architect") {
		t.Errorf("chips = %q", chips)
	}
	rules, _ := rig.api.lastSaved()["rules"].([]any)
	agents, _ := rules[0].(map[string]any)["agents"].([]any)
	if len(agents) != 1 || agents[0] != "go-developer" {
		t.Errorf("agents = %v", agents)
	}
	rig.run(t,
		chromedp.Click(`#architecture-panel-hooks [data-agent-chip="go-developer"] input`, chromedp.ByQuery),
		poll(barVisible()),
		clickButton(saveBar, "Save"),
		poll(`!`+barVisible()),
	)
	rules, _ = rig.api.lastSaved()["rules"].([]any)
	if _, has := rules[0].(map[string]any)["agents"]; has {
		t.Errorf("an empty choice is saved as %v, not left out", rules[0])
	}
}
