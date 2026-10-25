package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// layerSel is a selector inside the Nth layer's editor on the Layers tab.
func layerSel(i int, rest string) string {
	return fmt.Sprintf(`#architecture-panel-layers [data-layer="%d"] %s`, i, rest)
}

// openLayersTab opens the Layers tab at a width the editor has room at.
func openLayersTab(t *testing.T, rig *archRig) {
	t.Helper()
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("/layers")),
		poll(`!!document.querySelector('#architecture-panel-layers [data-add-layer=folder]') && document.querySelector('#architecture-panel-layers').checkVisibility()`),
	)
}

func addLayerBy(kind string) chromedp.Action {
	return chromedp.Click(fmt.Sprintf(`#architecture-panel-layers [data-add-layer=%s]`, kind), chromedp.ByQuery)
}

func setLayerField(i int, field, value string) chromedp.Action {
	return chromedp.Tasks{
		poll(fmt.Sprintf(`!!document.querySelector('%s')`, layerSel(i, `[data-field=`+field+`]`))),
		chromedp.SetValue(layerSel(i, `[data-field=`+field+`]`), value, chromedp.ByQuery),
	}
}

func layerTextOf(sel string, out *string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(document.querySelector(%q)?.textContent ?? '').replace(/\s+/g, ' ').trim()`, sel), out)
}

// A layer added by folder and one by regex each show how many files they match and a sample.
func TestAddingLayersByFolderAndByRegexShowsTheirFileCountsAndSamples(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var c0, s0, c1, s1 string
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "pattern", "internal/domain"),
		poll(`document.querySelector('`+layerSel(0, `[data-layer-count]`)+`')?.textContent.includes('2 files')`),
		addLayerBy("regex"),
		setLayerField(1, "pattern", "^internal/ports/|^cmd/"),
		poll(`document.querySelector('`+layerSel(1, `[data-layer-count]`)+`')?.textContent.includes('2 files')`),
		layerTextOf(layerSel(0, `[data-layer-count]`), &c0),
		layerTextOf(layerSel(0, `[data-layer-sample]`), &s0),
		layerTextOf(layerSel(1, `[data-layer-count]`), &c1),
		layerTextOf(layerSel(1, `[data-layer-sample]`), &s1),
	)
	if c0 != "2 files" || !strings.Contains(s0, "internal/domain/zone.go") || !strings.Contains(s0, "internal/domain/session.go") {
		t.Errorf("folder layer: %q, sample %q", c0, s0)
	}
	if c1 != "2 files" || !strings.Contains(s1, "internal/ports/zones.go") || !strings.Contains(s1, "cmd/server/main.go") {
		t.Errorf("regex layer: %q, sample %q", c1, s1)
	}
	for _, call := range rig.api.called() {
		if call == "save_repository_architecture" {
			t.Errorf("the preview saved: %v", rig.api.called())
		}
	}
}

// Two layers that overlap show the overlap and its winner; unmatched files are shown.
func TestOverlappingLayersShowTheirWinnerAndUnmatchedFilesAreListed(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var overlap, unmatched string
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "name", "all-internal"),
		setLayerField(0, "pattern", "internal"),
		addLayerBy("regex"),
		setLayerField(1, "name", "ports"),
		setLayerField(1, "pattern", "^internal/ports/"),
		poll(`document.querySelectorAll('#architecture-panel-layers [data-overlap]').length === 1`),
		layerTextOf(`#architecture-panel-layers [data-overlap]`, &overlap),
		poll(`document.querySelector('#architecture-panel-layers [data-unmatched-count]')?.textContent.includes('2 files')`),
		layerTextOf(`#architecture-panel-layers [data-unmatched]`, &unmatched),
	)
	if !strings.Contains(overlap, "internal/ports/zones.go") || !strings.Contains(overlap, "ports wins") {
		t.Errorf("overlap = %q", overlap)
	}
	if !strings.Contains(unmatched, "README.md") || !strings.Contains(unmatched, "cmd/server/main.go") {
		t.Errorf("unmatched = %q", unmatched)
	}
}

// An invalid regex is marked on the field at once, and a valid one clears it.
func TestAnInvalidRegexIsMarkedOnTheFieldAndAValidOneClearsIt(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	const errSel = `#architecture-panel-layers [data-error-for="/layers/0/match/regex"]`
	var invalid, msg string
	rig.run(t,
		addLayerBy("regex"),
		setLayerField(0, "pattern", "(^tests/"),
		poll(`!!document.querySelector('`+errSel+`')`),
		chromedp.Evaluate(`document.querySelector('`+layerSel(0, `[data-field=pattern]`)+`').getAttribute('aria-invalid')`, &invalid),
		layerTextOf(errSel, &msg),
	)
	if invalid != "true" || !strings.Contains(msg, "regex") {
		t.Fatalf("aria-invalid = %q, message %q", invalid, msg)
	}
	var ok bool
	rig.run(t,
		setLayerField(0, "pattern", "^tests/"),
		poll(`!document.querySelector('`+errSel+`')`),
		chromedp.Evaluate(`document.querySelector('`+layerSel(0, `[data-field=pattern]`)+`').getAttribute('aria-invalid') !== 'true'`, &ok),
	)
	if !ok {
		t.Fatal("a valid regex stays marked invalid")
	}
}

// The server's own verdict on a pattern (the preview's per-layer error) is marked on the field too.
func TestTheServersVerdictOnARegexIsMarkedOnTheField(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	rig.run(t,
		addLayerBy("regex"),
		setLayerField(0, "pattern", "^(?=a)"), // JavaScript accepts a lookahead; RE2 and so the preview do not
		poll(`!!document.querySelector('#architecture-panel-layers [data-error-for="/layers/0/match/regex"]')`),
	)
}

// Rename, reorder and remove change the draft and survive a save.
func TestRenamingReorderingAndRemovingLayersSurviveASave(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	openLayersTab(t, rig)
	rig.run(t,
		poll(`document.querySelectorAll('#architecture-panel-layers [data-layer]').length === 2`),
		setLayerField(0, "name", "core"),
		chromedp.Click(layerSel(0, `[data-move=down]`), chromedp.ByQuery),
		poll(`document.querySelector('`+layerSel(1, `[data-field=name]`)+`')?.value === 'core'`),
		chromedp.Click(layerSel(0, `[data-remove-layer]`), chromedp.ByQuery),
		poll(`document.querySelectorAll('#architecture-panel-layers [data-layer]').length === 1`),
		chromedp.Click(`[data-save-bar] [data-variant=primary]`, chromedp.ByQuery),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	saved := rig.api.lastSaved()
	layers, _ := saved["layers"].([]any)
	if len(layers) != 1 {
		t.Fatalf("saved layers = %v", layers)
	}
	l, _ := layers[0].(map[string]any)
	if l["name"] != "core" {
		t.Fatalf("saved layer = %v", l)
	}
	if m, _ := l["match"].(map[string]any); m["folder"] != "internal/domain" {
		t.Fatalf("saved match = %v", l["match"])
	}
}

// Layers of a saved record come back on the tab with their purpose and bounded context fields.
func TestALayerHasAnOptionalBoundedContextAndPurpose(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "pattern", "internal/domain"),
		setLayerField(0, "bounded_context", "sessions"),
		setLayerField(0, "purpose", "Pure domain types."),
		chromedp.Click(`[data-save-bar] [data-variant=primary]`, chromedp.ByQuery),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	l, _ := rig.api.lastSaved()["layers"].([]any)[0].(map[string]any)
	if l["bounded_context"] != "sessions" || l["purpose"] != "Pure domain types." {
		t.Fatalf("saved layer = %v", l)
	}
}
