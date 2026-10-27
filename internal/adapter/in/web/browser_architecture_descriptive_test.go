package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// descRecord is a saved record whose rows have something to show in each part
// of a row of the published design.
func descRecord() map[string]any {
	rec := savedRecord()
	rec["instructions"] = []any{
		map[string]any{"path": "CLAUDE.md", "kind": "rules", "applies_to": []any{"all"}, "loaded": "always", "note": "Repository rules."},
		map[string]any{"path": ".claude/skills/go/SKILL.md", "kind": "skill", "applies_to": []any{"go-developer", "devops"}, "loaded": "on_demand"},
	}
	rec["tools"] = []any{
		map[string]any{"name": "codegraph", "kind": "mcp", "used_by": []any{"all"}, "writes": false, "usage": "Call it before Read."},
		map[string]any{"name": "deploy", "kind": "cli", "used_by": []any{"devops"}, "writes": true},
	}
	rec["sandboxes"] = []any{
		map[string]any{"name": "seatbelt", "runs": "Every session.", "can_reach": []any{"the worktree", "loopback"}, "cannot_reach": []any{"~/.ssh"}, "used_by": []any{"all"}},
		map[string]any{"name": "bare", "used_by": []any{"devops"}, "can_reach": []any{}, "cannot_reach": []any{}},
	}
	rec["orchestration"] = map[string]any{
		"routing":  []any{map[string]any{"agent": "go-developer", "model": "claude-opus-5-5", "note": "Tickets in internal."}, map[string]any{"agent": "devops", "model": "claude-sonnet-5-5"}},
		"handoffs": []any{map[string]any{"from": "software-architect", "to": "go-developer", "when": "A spec is approved."}},
	}
	return rec
}

// descShape reads a panel's structure: the head (title, text, where Add sits),
// and for each entry the text of each named part.
func descShape(panel string, parts ...string) string {
	return fmt.Sprintf(`(() => {
		const panel = document.querySelector('#architecture-panel-%s');
		const norm = (e) => (e ? e.textContent.replace(/\s+/g, ' ').trim() : null);
		const head = panel.querySelector('[data-panel-head]');
		const h2 = head.querySelector('h2').getBoundingClientRect();
		const adds = [...head.querySelectorAll('[data-add]')];
		const parts = %s;
		return JSON.stringify({
			h2: norm(head.querySelector('h2')),
			adds: adds.map(a => ({ text: norm(a), primary: a.dataset.variant === 'primary', right: a.getBoundingClientRect().left >= h2.right, sameBand: a.getBoundingClientRect().top < h2.bottom })),
			entries: [...panel.querySelectorAll('[data-entry]')].map(e => Object.fromEntries(parts.map(p => [p, norm(e.querySelector('[data-row] [' + p + ']'))]))),
			edits: [...panel.querySelectorAll('[data-entry] [data-row] button[data-edit]')].map(b => norm(b)),
		});
	})()`, panel, jsonArr(parts))
}

func jsonArr(s []string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

type descHead struct {
	H2   string `json:"h2"`
	Adds []struct {
		Text     string `json:"text"`
		Primary  bool   `json:"primary"`
		Right    bool   `json:"right"`
		SameBand bool   `json:"sameBand"`
	} `json:"adds"`
	Entries []map[string]*string `json:"entries"`
	Edits   []string             `json:"edits"`
}

func readDesc(t *testing.T, rig *archRig, panel string, parts ...string) descHead {
	t.Helper()
	var raw string
	rig.run(t, chromedp.Evaluate(descShape(panel, parts...), &raw))
	var d descHead
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return d
}

func openDescTab(t *testing.T, rig *archRig, slug, panel string) {
	t.Helper()
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/"+slug)),
		poll(`document.querySelectorAll('[data-count]').length === 6`),
		poll(fmt.Sprintf(`document.querySelectorAll('#architecture-panel-%s [data-entry]').length > 0 && document.querySelector('#architecture-panel-%s').checkVisibility()`, panel, panel)),
	)
}

func wantPart(t *testing.T, d descHead, i int, part, want string) {
	t.Helper()
	if i >= len(d.Entries) {
		t.Fatalf("no entry %d (%d entries)", i, len(d.Entries))
	}
	got := d.Entries[i][part]
	if want == "<none>" {
		if got != nil {
			t.Fatalf("entry %d %s = %q, want it not rendered", i, part, *got)
		}
		return
	}
	if got == nil || *got != want {
		g := "<none>"
		if got != nil {
			g = *got
		}
		t.Fatalf("entry %d %s = %q, want %q", i, part, g, want)
	}
}

func wantHead(t *testing.T, d descHead, h2 string, adds ...string) {
	t.Helper()
	if d.H2 != h2 {
		t.Fatalf("heading = %q, want %q", d.H2, h2)
	}
	if len(d.Adds) != len(adds) {
		t.Fatalf("add buttons = %+v, want %v", d.Adds, adds)
	}
	for i, a := range d.Adds {
		if a.Text != adds[i] || !a.Right || !a.SameBand {
			t.Fatalf("add button %d = %+v, want %q at the head's right, level with the title", i, a, adds[i])
		}
	}
	if !d.Adds[0].Primary {
		t.Fatal("the first Add button is not the primary one")
	}
}

// Instructions: path in mono, kind tag, a description line, Applies to, Loaded, Edit; Add file at the head's right.
func TestInstructionsRowsMatchThePublishedDesign(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	openDescTab(t, rig, "instructions", "instructions")
	d := readDesc(t, rig, "instructions", "data-path", "data-kind", "data-note", "data-meta=applies", "data-meta=loaded")
	wantHead(t, d, "Instructions & rule files", "Add file")
	if len(d.Entries) != 2 || len(d.Edits) != 2 || d.Edits[0] != "Edit" {
		t.Fatalf("entries = %d, edit buttons = %v", len(d.Entries), d.Edits)
	}
	wantPart(t, d, 0, "data-path", "CLAUDE.md")
	wantPart(t, d, 0, "data-kind", "Rules")
	wantPart(t, d, 0, "data-note", "Repository rules.")
	wantPart(t, d, 0, "data-meta=applies", "Applies to all")
	wantPart(t, d, 0, "data-meta=loaded", "Loaded Always")
	wantPart(t, d, 1, "data-path", ".claude/skills/go/SKILL.md")
	wantPart(t, d, 1, "data-kind", "Skill")
	wantPart(t, d, 1, "data-note", "<none>") // no note in the record: no description line
	wantPart(t, d, 1, "data-meta=applies", "Applies to go-developer, devops")
	wantPart(t, d, 1, "data-meta=loaded", "Loaded On demand")
	var mono string
	rig.run(t, chromedp.Evaluate(`getComputedStyle(document.querySelector('#architecture-panel-instructions [data-entry] [data-path]')).fontFamily`, &mono))
	if !containsFold(mono, "mono") {
		t.Fatalf("the path font = %q, want the mono family", mono)
	}
}

// Tools: name in mono, kind tag, writes flag, usage prose, Available to, Edit.
func TestToolsRowsMatchThePublishedDesign(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	openDescTab(t, rig, "tools", "tools")
	d := readDesc(t, rig, "tools", "data-name", "data-kind", "data-flag=writes", "data-flag=no-usage", "data-note", "data-meta=used-by")
	wantHead(t, d, "Tools", "Add tool")
	wantPart(t, d, 0, "data-name", "codegraph")
	wantPart(t, d, 0, "data-kind", "MCP server")
	wantPart(t, d, 0, "data-flag=writes", "<none>")
	wantPart(t, d, 0, "data-flag=no-usage", "<none>")
	wantPart(t, d, 0, "data-note", "Call it before Read.")
	wantPart(t, d, 0, "data-meta=used-by", "Available to all")
	wantPart(t, d, 1, "data-kind", "CLI")
	wantPart(t, d, 1, "data-flag=writes", "writes")
	wantPart(t, d, 1, "data-flag=no-usage", "no usage prose")
	wantPart(t, d, 1, "data-note", "The model gets no guidance on when to call this.")
	wantPart(t, d, 1, "data-meta=used-by", "Available to devops")
	if len(d.Edits) != 2 {
		t.Fatalf("edit buttons = %v", d.Edits)
	}
}

// Sandboxes: a card per environment with Runs where, Used by, and the two reach lists.
func TestSandboxesCardsMatchThePublishedDesign(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	openDescTab(t, rig, "sandboxes", "sandboxes")
	d := readDesc(t, rig, "sandboxes", "data-name", "data-meta=runs", "data-meta=used-by", "data-reach=can", "data-reach=cannot")
	wantHead(t, d, "Sandboxes & execution environments", "Add environment")
	wantPart(t, d, 0, "data-name", "seatbelt")
	wantPart(t, d, 0, "data-meta=runs", "Runs where Every session.")
	wantPart(t, d, 0, "data-meta=used-by", "Used by all")
	var lists string
	rig.run(t, chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-sandboxes [data-entry]')[0].querySelectorAll('[data-reach]')].map(r => r.querySelector('.entry-label').textContent.trim() + ': ' + [...r.querySelectorAll('li')].map(li => li.textContent.trim()).join(', ')).join(' | ')`, &lists))
	if lists != "Can reach: the worktree, loopback | Cannot reach: ~/.ssh" {
		t.Fatalf("reach lists = %q", lists)
	}
	wantPart(t, d, 1, "data-name", "bare")
	wantPart(t, d, 1, "data-meta=runs", "<none>")
	wantPart(t, d, 1, "data-reach=can", "<none>") // nothing recorded: no empty list
	wantPart(t, d, 1, "data-reach=cannot", "<none>")
	var marks string
	rig.run(t, chromedp.Evaluate(`[...document.querySelectorAll('#architecture-panel-sandboxes [data-entry]')[0].querySelectorAll('[data-reach] li')].map(li => getComputedStyle(li, '::before').content).join('')`, &marks))
	if marks != `"✓""✓""✕"` {
		t.Fatalf("reach markers = %s, want ✓ for can reach and ✕ for cannot", marks)
	}
}

// Orchestration: Model routing and Hand-offs as tables, a row per entry.
func TestOrchestrationTablesMatchThePublishedDesign(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	openDescTab(t, rig, "orchestration", "orchestration")
	var raw string
	rig.run(t, chromedp.Evaluate(`(() => {
		const panel = document.querySelector('#architecture-panel-orchestration');
		const norm = (e) => e.textContent.replace(/\s+/g, ' ').trim();
		const head = panel.querySelector('[data-panel-head]');
		const h2 = head.querySelector('h2').getBoundingClientRect();
		const card = (name) => [...panel.querySelectorAll('[data-card]')].find(c => norm(c.querySelector('header h3')) === name);
		const rows = (c, sel) => [...c.querySelectorAll('[data-entry] tr[data-row]')].map(tr => [...tr.querySelectorAll('td')].map(norm));
		const routing = card('Model routing'), hand = card('Hand-offs');
		return JSON.stringify({
			h2: norm(head.querySelector('h2')),
			adds: [...head.querySelectorAll('[data-add]')].map(a => [norm(a), a.dataset.variant === 'primary', a.getBoundingClientRect().left >= h2.right]),
			routingHead: [...routing.querySelectorAll('thead th')].map(norm),
			routing: rows(routing),
			handHead: [...hand.querySelectorAll('thead th')].map(norm),
			hand: rows(hand),
			edits: panel.querySelectorAll('[data-entry] [data-row] button[data-edit]').length,
		});
	})()`, &raw))
	var got struct {
		H2          string     `json:"h2"`
		Adds        [][]any    `json:"adds"`
		RoutingHead []string   `json:"routingHead"`
		Routing     [][]string `json:"routing"`
		HandHead    []string   `json:"handHead"`
		Hand        [][]string `json:"hand"`
		Edits       int        `json:"edits"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.H2 != "Orchestration" {
		t.Fatalf("heading = %q", got.H2)
	}
	if fmt.Sprint(got.Adds) != "[[Add route true true] [Add hand-off false true]]" {
		t.Fatalf("add buttons = %v", got.Adds)
	}
	if fmt.Sprint(got.RoutingHead) != "[Agent Model Used for Actions]" || fmt.Sprint(got.HandHead) != "[From To When Actions]" {
		t.Fatalf("table heads = %v / %v", got.RoutingHead, got.HandHead)
	}
	if fmt.Sprint(got.Routing) != "[[go-developer claude-opus-5-5 Tickets in internal. Edit] [devops claude-sonnet-5-5  Edit]]" {
		t.Fatalf("routing rows = %v", got.Routing)
	}
	if fmt.Sprint(got.Hand) != "[[software-architect go-developer A spec is approved. Edit]]" {
		t.Fatalf("hand-off rows = %v", got.Hand)
	}
	if got.Edits != 3 {
		t.Fatalf("edit buttons = %d, want one per row", got.Edits)
	}
}

// Edit opens the row in the drawer; Apply writes its fields to the draft (the row, the counts and the save bar follow) and closes it.
func TestEditingARowInTheDrawerUpdatesTheDraftOnApply(t *testing.T) {
	rig := newArchRig(t)
	rec := descRecord()
	rec["tools"].([]any)[1].(map[string]any)["usage"] = "Run it last." // so the record saves
	rig.api.seed(rec)
	openDescTab(t, rig, "instructions", "instructions")
	entry := `#architecture-panel-instructions [data-entry]:nth-child(2)` // the x-for template is the first child, so this is the first entry
	drawer := `#architecture-panel-instructions [data-drawer]`
	var closed, open string
	rig.run(t,
		chromedp.Evaluate(fmt.Sprintf(`String(!!document.querySelector(%q)?.checkVisibility())`, drawer), &closed),
		archClick(entry+` [data-row] button[data-edit]`),
		poll(fmt.Sprintf(`document.querySelector(%q).checkVisibility()`, drawer)),
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector(%q + ' [data-edit]').getAttribute('aria-expanded')`, entry), &open),
		setDrawerField("instructions", "instruction", "path", "AGENTS.md"),
		setDrawerField("instructions", "instruction", "note", "Read first."),
		archClick(drawer+` [data-apply]`),
		poll(fmt.Sprintf(`document.querySelector(%q + ' [data-row] [data-path]').textContent.trim() === 'AGENTS.md'`, entry)),
		poll(fmt.Sprintf(`document.querySelector(%q + ' [data-row] [data-note]').textContent.trim() === 'Read first.'`, entry)),
		poll(`!!document.querySelector('[data-save-bar]')?.checkVisibility()`),
		poll(fmt.Sprintf(`!document.querySelector(%q).checkVisibility()`, drawer)),
	)
	if closed != "false" || open != "true" {
		t.Fatalf("drawer visible before Edit = %q, aria-expanded after = %q", closed, open)
	}
	rig.run(t, archClick(archSave), poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`))
	if in := rig.api.lastSaved()["instructions"].([]any)[0].(map[string]any); in["path"] != "AGENTS.md" || in["note"] != "Read first." {
		t.Fatalf("saved instruction = %v", in)
	}
}

// A validation error from the server opens the drawer on the row and shows on the marked field.
func TestAValidationErrorOpensTheRowsDrawerAndMarksItsField(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	openDescTab(t, rig, "tools", "tools")
	var visible, invalid, inDrawer, index string
	rig.run(t,
		archFill("tools", 1, "usage", ""),
		archFill("tools", 1, "name", "deploy2"),
		archClick(archSave),
		poll(`!!document.querySelector('[data-error-for="/tools/1/usage"]')`),
		poll(`document.querySelector('[data-error-for="/tools/1/usage"]').checkVisibility()`),
		chromedp.Evaluate(`String(document.querySelector('[data-error-for="/tools/1/usage"]').checkVisibility())`, &visible),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-tools [data-drawer] [data-field=usage]').getAttribute('aria-invalid')`, &invalid),
		chromedp.Evaluate(`String(!!document.querySelector('#architecture-panel-tools [data-drawer] [data-error-for="/tools/1/usage"]'))`, &inDrawer),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-tools [data-drawer]').dataset.drawerIndex`, &index),
	)
	if visible != "true" || invalid != "true" || inDrawer != "true" || index != "1" {
		t.Fatalf("error visible = %q, field aria-invalid = %q, inside the drawer = %q, drawer on row %q", visible, invalid, inDrawer, index)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), sub)
}
