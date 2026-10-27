package web

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The diagram view as the published architecture-diagram design lays it out:
// a head with Download SVG and Copy Mermaid, the Show options, the Key legend,
// and nothing for the file write the build does not have.
func TestDiagramHeadShowOptionsAndKeyMatchTheMockup(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var head, buttons, show, key, focusTitle string
	var guardsOn bool
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/diagram")),
		diagramReady(),
		chromedp.Evaluate(textOf("[data-diagram-head] h2"), &head),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-diagram-head] button')].map(b => b.textContent.trim()).join('|')`, &buttons),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-diagram-show] label')].map(l => l.textContent.replace(/\s+/g, ' ').trim()).join('|')`, &show),
		chromedp.Evaluate(`document.querySelector('[data-diagram-show] input[data-show=guards]').checked`, &guardsOn),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-diagram-key] li')].map(l => l.textContent.replace(/\s+/g, ' ').trim()).join('|')`, &key),
		chromedp.Evaluate(textOf("[data-focus-title]"), &focusTitle),
	)
	if head != "Architecture diagram" {
		t.Errorf("heading = %q", head)
	}
	if buttons != "Download SVG|Copy Mermaid" {
		t.Errorf("head buttons = %q, want Download SVG and Copy Mermaid only (no file write)", buttons)
	}
	if show != "Hooks as guardsBlocking ✕ and warning ! on the write edges|Sandbox regionsDashed = inside the OS sandbox|Hand-offsOrchestration arrows between agents" || !guardsOn {
		t.Errorf("show options = %q (guards on %v)", show, guardsOn)
	}
	if key != "may change (and read)|may read only|blocking hook guards the write|warning-only hook|sandbox (what runs inside)" {
		t.Errorf("key = %q", key)
	}
	if focusTitle != "All agents" {
		t.Errorf("focus card title = %q", focusTitle)
	}
}

// Show turns the guards, the sandbox regions and the hand-off arrows on and off.
func TestDiagramShowOptionsToggleGuardsSandboxesAndHandoffs(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var guards, regions, handoffs int
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/diagram")),
		diagramReady(),
		chromedp.Evaluate(nGuards, &guards),
		chromedp.Evaluate(nRegions, &regions),
		chromedp.Evaluate(`document.querySelectorAll('[data-diagram-picture] svg [data-handoff]').length`, &handoffs),
	)
	if guards == 0 || regions != 1 || handoffs != 0 {
		t.Fatalf("by default: guards %d, regions %d, hand-offs %d; want guards, one region, no hand-offs", guards, regions, handoffs)
	}
	rig.run(t,
		archClick(`[data-diagram-show] input[data-show=guards]`),
		poll(nGuards+` === 0`),
		archClick(`[data-diagram-show] input[data-show=sandboxes]`),
		poll(nRegions+` === 0`),
		archClick(`[data-diagram-show] input[data-show=handoffs]`),
		poll(`document.querySelectorAll('[data-diagram-picture] svg [data-handoff]').length === 1`),
		archClick(`[data-diagram-show] input[data-show=guards]`),
		poll(nGuards+` > 0`),
	)
}

// Focusing an agent fills the focus card; Clear focus goes back to all agents.
func TestDiagramFocusCardNamesTheAgentAndClearFocusReturns(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var title, clear string
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/diagram")),
		diagramReady(),
		poll(`document.querySelectorAll('[data-focus-agent]').length === 4`),
		chromedp.Click(`[data-focus-agent][data-agent-name="devops"]`, chromedp.ByQuery),
		poll(`document.querySelector('[data-focus-title]')?.textContent.trim() === 'devops'`),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-focus-card] button')].find(b => b.hasAttribute('data-clear-focus'))?.textContent.trim() ?? ''`, &clear),
		archClick(`[data-clear-focus]`),
		poll(`document.querySelector('[data-focus-title]')?.textContent.trim() === 'All agents'`),
		chromedp.Evaluate(textOf("[data-focus-title]"), &title),
	)
	if clear != "Clear focus" || title != "All agents" {
		t.Errorf("clear button = %q, title after = %q", clear, title)
	}
}

// An unsaved change shows the draft banner; overlaps and unmatched files show the problems banner.
func TestDiagramBannersSayDraftAndProblems(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var draft, problems string
	var draftBefore bool
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/diagram")),
		diagramReady(),
		chromedp.Evaluate(`!!document.querySelector('[data-draft-banner]')`, &draftBefore),
		// a layer over all of internal/ overlaps domain; the fake has two files in no layer
		chromedp.Evaluate(`document.querySelector('main')._x_dataStack[0].draft.layers.push({ name: 'internal-all', match: { folder: 'internal' }, access: { default: 'read', agents: { devops: 'change' } } })`, nil),
		poll(`!!document.querySelector('[data-draft-banner]')`),
		poll(`document.querySelector('[data-problems-banner]')?.textContent.includes('2 problems')`), // once the layers' preview answers for the new layer
		chromedp.Evaluate(textOf("[data-draft-banner] div"), &draft),
		chromedp.Evaluate(textOf("[data-problems-banner] div"), &problems),
	)
	if draftBefore {
		t.Error("the draft banner shows with nothing changed")
	}
	if !strings.HasPrefix(draft, "Showing an unsaved draft.") || !strings.Contains(draft, "last saved version") {
		t.Errorf("draft banner = %q", draft)
	}
	if !strings.HasPrefix(problems, "2 problems in the architecture.") || !strings.Contains(problems, "more than one layer") || !strings.Contains(problems, "match no layer") {
		t.Errorf("problems banner = %q", problems)
	}
	var marked int
	rig.run(t,
		poll(`document.querySelectorAll('[data-diagram-picture] svg g[data-layer][data-overlap]').length > 0`),
		chromedp.Evaluate(`document.querySelectorAll('[data-diagram-picture] svg g[data-layer][data-overlap]').length`, &marked),
	)
	if marked == 0 {
		t.Error("no layer is marked as overlapping on the picture")
	}
}

// With no layers there is nothing to draw: the empty state offers the layers editor.
func TestDiagramEmptyStateOpensTheLayersEditor(t *testing.T) {
	rig := newArchRig(t)
	rec := savedRecord()
	rec["layers"] = []any{}
	rig.api.seed(rec)
	var text string
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(rig.url("/diagram")),
		poll(`!!document.querySelector('[data-diagram-empty]')`),
		chromedp.Evaluate(textOf("[data-diagram-empty]"), &text),
		chromedp.Evaluate(`document.querySelectorAll('[data-diagram-picture] svg').length`, new(int)),
	)
	if !strings.Contains(text, "Nothing to draw yet") || !strings.Contains(text, "The diagram needs at least one layer and one agent with access. Define layers first.") || !strings.Contains(text, "Open layers editor") {
		t.Errorf("empty state = %q", text)
	}
	var none bool
	rig.run(t, chromedp.Evaluate(`!document.querySelector('[data-diagram-picture] svg')`, &none))
	if !none {
		t.Error("a picture is drawn with no layers")
	}
	rig.run(t,
		archClick(`[data-diagram-empty] [data-open-layers]`),
		poll(`location.pathname.endsWith('/architecture/layers')`),
		poll(`document.querySelector('[role=tab][aria-selected=true]')?.querySelector('[data-tab-label]').textContent.trim() === 'Layers'`),
	)
}

// The drawing's colours come from the design tokens, so they follow the colour scheme; the downloaded file keeps literal colours.
func TestDiagramColoursFollowTheColourScheme(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	probe := `(() => { const r = (s) => getComputedStyle(document.querySelector(s)); return JSON.stringify({
		layer: r('[data-diagram-picture] svg g[data-layer] rect').fill + '|' + r('[data-diagram-picture] svg g[data-layer] rect').stroke,
		text: r('[data-diagram-picture] svg g[data-layer] text').fill,
		edge: r('[data-diagram-picture] svg path[data-edge]').stroke,
		paper: r('[data-diagram-picture] svg > rect').fill,
		guard: r('[data-diagram-picture] svg g[data-guard][data-mode=blocking] circle').fill,
	}) })()`
	var light, dark, svg string
	rig.run(t,
		chromedp.EmulateViewport(1440, 900),
		emulateColorScheme("light"),
		chromedp.Navigate(rig.url("/diagram")),
		diagramReady(),
		chromedp.Evaluate(probe, &light),
		emulateColorScheme("dark"),
		poll(`matchMedia('(prefers-color-scheme: dark)').matches`),
		chromedp.Evaluate(probe, &dark),
		chromedp.Evaluate(`(() => {
			const create = URL.createObjectURL.bind(URL);
			URL.createObjectURL = (b) => { window.__blob = b; return create(b) };
			HTMLAnchorElement.prototype.click = function () {};
		})()`, nil),
		chromedp.Click(`[data-download-svg]`, chromedp.ByQuery),
		poll(`!!window.__blob`),
		chromedp.Evaluate(`window.__blob.text()`, &svg, awaitPromise),
	)
	if light == dark {
		t.Fatalf("the drawing looks the same in light and dark: %s", light)
	}
	for _, field := range []string{"layer", "text", "edge", "paper", "guard"} {
		if !strings.Contains(light, `"`+field+`"`) {
			t.Fatalf("probe lost %s: %s", field, light)
		}
	}
	if !strings.Contains(svg, "#2563eb") || !strings.Contains(svg, `fill="#ffffff"`) {
		t.Errorf("the downloaded SVG lost its literal colours:\n%.400s", svg)
	}
}
