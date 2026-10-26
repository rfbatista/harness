package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// layerSel is a selector inside the selected layer's editor on the Layers tab,
// which carries the layer's position as data-layer.
func layerSel(i int, rest string) string {
	return fmt.Sprintf(`#architecture-panel-layers [data-layer="%d"] %s`, i, rest)
}

const (
	layerCountSel  = `#architecture-panel-layers [data-layer-count]`
	layerSampleSel = `#architecture-panel-layers [data-layer-sample]`
	layerItemsJS   = `document.querySelectorAll('#architecture-panel-layers [data-layer-item]')`
)

// openLayersTab opens the Layers tab at a width the editor has room at.
func openLayersTab(t *testing.T, rig *archRig) { openLayersTabAt(t, rig, 1280) }

func openLayersTabAt(t *testing.T, rig *archRig, width int64) {
	t.Helper()
	rig.run(t,
		chromedp.EmulateViewport(width, 900),
		chromedp.Navigate(rig.url("/layers")),
		poll(`!!document.querySelector('#architecture-panel-layers [data-add-layer]') && document.querySelector('#architecture-panel-layers').checkVisibility()`),
	)
}

// addLayerBy adds a layer with the one "Add layer" button, then switches it to
// a regex when asked to: there is no separate button for each kind.
func addLayerBy(kind string) chromedp.Action {
	tasks := chromedp.Tasks{chromedp.Click(`#architecture-panel-layers [data-add-layer]`, chromedp.ByQuery)}
	if kind == "regex" {
		tasks = append(tasks,
			poll(`!!document.querySelector('#architecture-panel-layers [data-match-kind=regex]')`),
			chromedp.Click(`#architecture-panel-layers [data-match-kind=regex]`, chromedp.ByQuery),
			poll(`document.querySelector('#architecture-panel-layers [data-match-kind=regex]').getAttribute('aria-pressed') === 'true'`),
		)
	}
	return tasks
}

func selectLayer(i int) chromedp.Action {
	return chromedp.Click(fmt.Sprintf(`#architecture-panel-layers [data-layer-item="%d"]`, i), chromedp.ByQuery)
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
		poll(`document.querySelector('`+layerCountSel+`')?.textContent.includes('2 files')`),
		layerTextOf(layerCountSel, &c0),
		layerTextOf(layerSampleSel, &s0),
		addLayerBy("regex"),
		setLayerField(1, "pattern", "^internal/ports/|^cmd/"),
		poll(`document.querySelector('`+layerCountSel+`')?.textContent.includes('2 files')`),
		poll(`document.querySelector('`+layerSampleSel+`')?.textContent.includes('cmd/server/main.go')`),
		layerTextOf(layerCountSel, &c1),
		layerTextOf(layerSampleSel, &s1),
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
		poll(layerItemsJS+`.length === 2`),
		setLayerField(0, "name", "core"),
		chromedp.Click(layerSel(0, `[data-move=down]`), chromedp.ByQuery),
		poll(`document.querySelector('`+layerSel(1, `[data-field=name]`)+`')?.value === 'core'`), // the selection follows the layer
		selectLayer(0),
		chromedp.Click(layerSel(0, `[data-remove-layer]`), chromedp.ByQuery),
		poll(layerItemsJS+`.length === 1`),
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
		poll(`!!document.querySelector('select[data-field=bounded_context] option[value=sessions]')`),
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

func cellSel(label string) string {
	return fmt.Sprintf(`#architecture-panel-layers [data-access-matrix] button[aria-label=%q]`, label)
}

func hasCell(label string) string {
	return fmt.Sprintf(`!!document.querySelector('%s')`, cellSel(label))
}

func savedLayer(t *testing.T, rig *archRig, i int) map[string]any {
	t.Helper()
	layers, _ := rig.api.lastSaved()["layers"].([]any)
	if i >= len(layers) {
		t.Fatalf("saved layers = %v", layers)
	}
	l, _ := layers[i].(map[string]any)
	return l
}

func accessAgents(access any) map[string]any {
	a, _ := access.(map[string]any)
	m, _ := a["agents"].(map[string]any)
	return m
}

func saveFromBar() chromedp.Action {
	return chromedp.Tasks{
		chromedp.Click(`[data-save-bar] [data-variant=primary]`, chromedp.ByQuery),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	}
}

// A new layer shows the default access; changed cells are stored by a save.
func TestANewLayerShowsTheDefaultAccessAndChangedCellsAreSaved(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "name", "domain"),
		setLayerField(0, "pattern", "internal/domain"),
		poll(hasCell("software-architect on domain: change")),
		poll(hasCell("go-developer on domain: read")),
		poll(hasCell("devops on domain: read")),
		poll(hasCell("default on domain: read")),
		chromedp.Click(cellSel("go-developer on domain: read"), chromedp.ByQuery),
		poll(hasCell("go-developer on domain: change")),
		chromedp.Click(cellSel("devops on domain: read"), chromedp.ByQuery),
		poll(hasCell("devops on domain: change")),
		chromedp.Click(cellSel("default on domain: read"), chromedp.ByQuery),
		poll(hasCell("default on domain: change")),
		saveFromBar(),
	)
	l := savedLayer(t, rig, 0)
	a, _ := l["access"].(map[string]any)
	if a["default"] != "change" {
		t.Errorf("saved default = %v", a["default"])
	}
	ag := accessAgents(l["access"])
	if ag["go-developer"] != "change" || ag["software-architect"] != "change" || ag["devops"] != "change" {
		t.Errorf("saved agents = %v", ag)
	}
}

// The unlayered and plain-session rows exist and can be set.
func TestTheUnlayeredAndPlainRowsExistAndCanBeSet(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "name", "domain"),
		setLayerField(0, "pattern", "internal/domain"),
		poll(hasCell("plain on domain: read")),
		poll(hasCell("plain on unlayered: read")),
		poll(hasCell("devops on unlayered: change")), // the unlayered default: the architect and devops change
		chromedp.Click(cellSel("plain on unlayered: read"), chromedp.ByQuery),
		poll(hasCell("plain on unlayered: change")),
		chromedp.Click(cellSel("plain on domain: read"), chromedp.ByQuery),
		poll(hasCell("plain on domain: change")),
		saveFromBar(),
	)
	saved := rig.api.lastSaved()
	if ag := accessAgents(saved["unlayered"]); ag["plain"] != "change" {
		t.Errorf("saved unlayered = %v", saved["unlayered"])
	}
	if ag := accessAgents(savedLayer(t, rig, 0)["access"]); ag["plain"] != "change" {
		t.Errorf("saved layer access = %v", ag)
	}
}

// A cell is a button with a label and works with the keyboard.
func TestAnAccessCellExposesItsLabelAndWorksWithTheKeyboard(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var tag, kind string
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "name", "domain"),
		setLayerField(0, "pattern", "internal/domain"),
		poll(hasCell("go-developer on domain: read")),
		chromedp.Evaluate(`document.querySelector('`+cellSel("go-developer on domain: read")+`').tagName`, &tag),
		chromedp.Evaluate(`document.querySelector('`+cellSel("go-developer on domain: read")+`').type`, &kind),
		chromedp.Focus(cellSel("go-developer on domain: read"), chromedp.ByQuery),
		chromedp.KeyEvent("\r"),
		poll(hasCell("go-developer on domain: change")),
		chromedp.KeyEvent(" "),
		poll(hasCell("go-developer on domain: none")),
	)
	if tag != "BUTTON" || kind != "button" {
		t.Fatalf("a cell is %s[type=%s]", tag, kind)
	}
}

// Read restrictions are marked as shown to agents, not enforced.
func TestReadRestrictionsAreMarkedAsNotEnforced(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var note string
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "name", "secrets"),
		setLayerField(0, "pattern", "secrets"),
		poll(hasCell("default on secrets: read")),
		chromedp.Click(cellSel("default on secrets: read"), chromedp.ByQuery),
		chromedp.Click(cellSel("default on secrets: change"), chromedp.ByQuery),
		poll(hasCell("default on secrets: none")),
		poll(`document.querySelector('#architecture-panel-layers [data-read-note]')?.checkVisibility()`),
		layerTextOf(`#architecture-panel-layers [data-read-note]`, &note),
	)
	if !strings.Contains(note, "shown to agents, not enforced") {
		t.Fatalf("note = %q", note)
	}
}

const layersPanel = `#architecture-panel-layers `

// evalLayers runs a JS expression on the page and fills out with its answer.
func evalLayers(js string, out any) chromedp.Action { return chromedp.Evaluate(js, out) }

// The three columns of the mockup: the layer list, the selected layer's editor and Matched paths.
func TestTheLayersTabHasTheListTheEditorAndMatchedPathsAsThreeColumns(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	openLayersTabAt(t, rig, 1440)
	var cols string
	rig.run(t,
		poll(layerItemsJS+`.length === 2`),
		evalLayers(`[...document.querySelectorAll('`+layersPanel+`[data-layers-work] > *')].map(e => e.getAttribute('aria-label') + '@' + Math.round(e.getBoundingClientRect().left) + ',' + Math.round(e.getBoundingClientRect().top)).join('|')`, &cols),
	)
	parts := strings.Split(cols, "|")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "Layers@") || !strings.HasPrefix(parts[1], "Layer definition@") || !strings.HasPrefix(parts[2], "Live preview@") {
		t.Fatalf("columns = %q", cols)
	}
	var lefts, tops [3]int
	for i, p := range parts {
		xy := strings.Split(p[strings.Index(p, "@")+1:], ",")
		fmt.Sscan(xy[0], &lefts[i])
		fmt.Sscan(xy[1], &tops[i])
	}
	if !(lefts[0] < lefts[1] && lefts[1] < lefts[2]) || tops[0] != tops[1] || tops[1] != tops[2] {
		t.Fatalf("not side by side at 1440 px: lefts %v tops %v", lefts, tops)
	}
	// the list says each layer's name and pattern, the editor the selected one
	var items, name, label string
	rig.run(t,
		layerTextOf(layersPanel+`[data-layer-item="0"]`, &items),
		chromedp.Evaluate(`document.querySelector('`+layerSel(0, `[data-field=name]`)+`').value`, &name),
	)
	if !strings.Contains(items, "domain") || !strings.Contains(items, "internal/domain/**") || name != "domain" {
		t.Fatalf("list item %q, editor name %q", items, name)
	}
	// selecting the other layer shows its editor, its regex and what it matches
	rig.run(t,
		selectLayer(1),
		poll(`document.querySelector('`+layerSel(1, `[data-field=name]`)+`')?.value === 'adapters'`),
		poll(`document.querySelector('`+layersPanel+`[data-layer-item="1"]').getAttribute('aria-current') === 'true'`),
		poll(`document.querySelector('`+layerSel(1, `[data-field=pattern]`)+`').value === '^internal/adapter/'`),
		poll(`document.querySelector('`+layerSel(1, `[data-match-kind=regex]`)+`').getAttribute('aria-pressed') === 'true'`),
		poll(`!!document.querySelector('`+layerCountSel+`')?.textContent.trim()`),
		layerTextOf(layersPanel+`[data-layer-item="1"]`, &items),
		layerTextOf(layersPanel+`label[for$="-pattern"]`, &label),
	)
	if !strings.Contains(items, "/^internal/adapter//") || !strings.Contains(label, "Regular expression") {
		t.Fatalf("regex layer: list %q, label %q", items, label)
	}
	// Folder / Regex switch keeps the text and changes the field's label and the list's pattern
	rig.run(t,
		chromedp.Click(layerSel(1, `[data-match-kind=folder]`), chromedp.ByQuery),
		poll(`document.querySelector('`+layerSel(1, `[data-match-kind=folder]`)+`').getAttribute('aria-pressed') === 'true'`),
		layerTextOf(layersPanel+`label[for$="-pattern"]`, &label),
		layerTextOf(layersPanel+`[data-layer-item="1"]`, &items),
	)
	if !strings.Contains(label, "Folder") || strings.Contains(label, "Regular") || !strings.Contains(items, "^internal/adapter//**") {
		t.Fatalf("folder layer: list %q, label %q", items, label)
	}
	// the mockup's Exclude field is not built
	var exclude bool
	rig.run(t, evalLayers(`/exclude/i.test(document.querySelector('`+layersPanel+`').textContent)`, &exclude))
	if exclude {
		t.Fatal("the Layers tab shows an Exclude field the record has no place for")
	}
}

// The preset adds the eight hexagonal layers with the default access; Add layer adds one; Delete layer removes the selected one.
func TestTheHexagonalPresetAddLayerAndDeleteLayer(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var empty, names string
	rig.run(t,
		poll(`!!document.querySelector('`+layersPanel+`[data-no-layers]')`),
		layerTextOf(layersPanel+`[data-no-layers]`, &empty),
	)
	if !strings.Contains(empty, "No layers yet") || !strings.Contains(empty, "Without layers, hooks cannot tell an edit inside an agent’s area from one outside it. Add the first one, or start from a preset.") {
		t.Fatalf("empty state = %q", empty)
	}
	rig.run(t,
		chromedp.Click(layersPanel+`[data-preset]`, chromedp.ByQuery),
		poll(layerItemsJS+`.length === 8`),
		poll(`!document.querySelector('`+layersPanel+`[data-no-layers]')`),
		evalLayers(`[...`+layerItemsJS+`].map(e => e.querySelector('[data-layer-name]').textContent.trim()).join(',')`, &names),
		poll(hasCell("software-architect on domain: change")),
		poll(hasCell("go-developer on domain: read")),
		poll(hasCell("default on docs: read")),
	)
	if want := "domain,ports,application,adapters-in,adapters-out,web,tests,docs"; names != want {
		t.Fatalf("preset layers = %q, want %q", names, want)
	}
	// the preset adds only what is missing: pressing it again changes nothing
	rig.run(t,
		chromedp.Click(layersPanel+`[data-preset]`, chromedp.ByQuery),
		chromedp.Click(layersPanel+`[data-add-layer]`, chromedp.ByQuery),
		poll(layerItemsJS+`.length === 9`),
		poll(`document.querySelector('`+layersPanel+`[data-layer-item="8"]').getAttribute('aria-current') === 'true'`), // the new layer is selected
		chromedp.Click(layersPanel+`[data-remove-layer]`, chromedp.ByQuery),
		poll(layerItemsJS+`.length === 8`),
	)
	rig.run(t, saveFromBar())
	layers, _ := rig.api.lastSaved()["layers"].([]any)
	if len(layers) != 8 {
		t.Fatalf("saved %d layers, want 8", len(layers))
	}
	first, _ := layers[0].(map[string]any)
	if a, _ := first["access"].(map[string]any); a["default"] != "read" || accessAgents(first["access"])["software-architect"] != "change" {
		t.Fatalf("saved access = %v", first["access"])
	}
}

// The invalid-regex state: a banner, the error on the list item and on the field, and no matches to show.
func TestAnInvalidRegexShowsTheValidationBannerAndTheMatchedPathsSayToFixIt(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var banner, matched, item string
	rig.run(t,
		addLayerBy("regex"),
		setLayerField(0, "name", "tests"),
		setLayerField(0, "pattern", "(^tests/"),
		poll(`!!document.querySelector('`+layersPanel+`[data-validation]')`),
		layerTextOf(layersPanel+`[data-validation]`, &banner),
		poll(`!!document.querySelector('`+layersPanel+`[data-layer-item="0"] [data-badge=error]')`),
		layerTextOf(layersPanel+`[data-layer-item="0"]`, &item),
		layerTextOf(layersPanel+`[data-matched]`, &matched),
	)
	if !strings.Contains(banner, "“tests” cannot be saved.") || !strings.Contains(banner, "regex") {
		t.Fatalf("banner = %q", banner)
	}
	if !strings.Contains(matched, "Cannot preview") || !strings.Contains(matched, "Fix the pattern to see matches.") {
		t.Fatalf("matched paths = %q", matched)
	}
	if !strings.Contains(item, "error") {
		t.Fatalf("list item = %q", item)
	}
	var note string
	rig.run(t,
		setLayerField(0, "pattern", "^tests/"),
		poll(`!document.querySelector('`+layersPanel+`[data-validation]')`),
		poll(`document.querySelector('`+layersPanel+`[data-default-note]').checkVisibility()`),
		layerTextOf(layersPanel+`[data-default-note]`, &note),
	)
	if !strings.Contains(note, "Default access: everyone reads, only listed agents change.") {
		t.Fatalf("default note = %q", note)
	}
}

// Two layers with one name are named in the banner.
func TestTwoLayersWithOneNameAreNamedInTheValidationBanner(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var banner string
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "name", "domain"),
		addLayerBy("folder"),
		setLayerField(1, "name", "domain"),
		poll(`!!document.querySelector('`+layersPanel+`[data-validation]')`),
		layerTextOf(layersPanel+`[data-validation]`, &banner),
	)
	if !strings.Contains(banner, "“domain” cannot be saved.") || !strings.Contains(banner, "Another layer is already called domain.") {
		t.Fatalf("banner = %q", banner)
	}
}

// At 760 px the three columns stack, in the same order.
func TestTheLayersColumnsStackAtNarrowWidths(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	openLayersTabAt(t, rig, 760)
	var geo string
	rig.run(t,
		poll(layerItemsJS+`.length === 2`),
		evalLayers(`[...document.querySelectorAll('`+layersPanel+`[data-layers-work] > *')].map(e => { const r = e.getBoundingClientRect(); return e.getAttribute('aria-label') + '@' + Math.round(r.left) + ',' + Math.round(r.top) + ',' + Math.round(r.right); }).join('|')`, &geo),
	)
	parts := strings.Split(geo, "|")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "Layers@") || !strings.HasPrefix(parts[1], "Layer definition@") || !strings.HasPrefix(parts[2], "Live preview@") {
		t.Fatalf("columns = %q", geo)
	}
	var l, tp, r [3]int
	for i, p := range parts {
		xy := strings.Split(p[strings.Index(p, "@")+1:], ",")
		fmt.Sscan(xy[0], &l[i])
		fmt.Sscan(xy[1], &tp[i])
		fmt.Sscan(xy[2], &r[i])
	}
	if l[0] != l[1] || l[1] != l[2] || !(tp[0] < tp[1] && tp[1] < tp[2]) {
		t.Fatalf("not stacked at 760 px: lefts %v tops %v", l, tp)
	}
	if r[0] > 760 || r[1] > 760 || r[2] > 760 {
		t.Fatalf("a column runs past the viewport: rights %v", r)
	}
}

// Repository coverage shows an Overlap and an Unmatched callout with their counts.
func TestRepositoryCoverageShowsOverlapAndUnmatchedCallouts(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var overlap, unmatched, headings string
	rig.run(t,
		addLayerBy("folder"),
		setLayerField(0, "name", "all-internal"),
		setLayerField(0, "pattern", "internal"),
		addLayerBy("regex"),
		setLayerField(1, "name", "ports"),
		setLayerField(1, "pattern", "^internal/ports/"),
		poll(`!!document.querySelector('`+layersPanel+`[data-callout=overlap] [data-overlap]')`),
		poll(`document.querySelector('`+layersPanel+`[data-callout=unmatched]')?.textContent.includes('2 files')`),
		layerTextOf(layersPanel+`[data-callout=overlap]`, &overlap),
		layerTextOf(layersPanel+`[data-callout=unmatched]`, &unmatched),
		evalLayers(`[...document.querySelectorAll('`+layersPanel+`[data-coverage] h3')].map(h => h.textContent.trim()).join('|')`, &headings),
	)
	if headings != "Repository coverage" {
		t.Fatalf("coverage headings = %q", headings)
	}
	if !strings.HasPrefix(overlap, "Overlap.") || !strings.Contains(overlap, "1 path matches more than one layer") || !strings.Contains(overlap, "narrower pattern wins") || !strings.Contains(overlap, "ports wins") {
		t.Fatalf("overlap callout = %q", overlap)
	}
	if !strings.HasPrefix(unmatched, "Unmatched.") || !strings.Contains(unmatched, "2 files") || !strings.Contains(unmatched, "README.md") || !strings.Contains(unmatched, "Unmatched paths use the repository default: everyone reads, only software-architect and devops change.") {
		t.Fatalf("unmatched callout = %q", unmatched)
	}
	// Matched paths tags the paths that also match another layer, and counts what overlaps and what is unmatched
	var badges string
	rig.run(t,
		selectLayer(0),
		poll(`document.querySelector('`+layersPanel+`[data-matched] [data-tag=overlap]')?.textContent.includes('1 overlap')`),
		layerTextOf(layersPanel+`[data-matched]`, &badges),
	)
	if !strings.Contains(badges, "2 unmatched") {
		t.Fatalf("matched paths = %q", badges)
	}
	// the list tags the overlapping layers
	var tagged bool
	rig.run(t, evalLayers(`!!document.querySelector('`+layersPanel+`[data-layer-item="1"] [data-badge=overlap]')`, &tagged))
	if !tagged {
		t.Fatal("the layer list does not tag an overlapping layer")
	}
}

type matrixShape struct {
	Rows, Cols, Legend, Writing string
	HasChips                    bool
	Hue                         string
}

// The matrix has agents as rows and layers as columns, with rotated headers, coloured agent chips and a legend.
func TestTheAccessMatrixHasAgentsAsRowsAndLayersAsColumns(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	openLayersTabAt(t, rig, 1440)
	var shape matrixShape
	rig.run(t,
		poll(`!!document.querySelector('[data-access-matrix] [data-row=devops]')`),
		poll(`document.querySelectorAll('[data-access-matrix] thead th[data-column]').length === 3`),
		evalLayers(`({
			rows: [...document.querySelectorAll('[data-access-matrix] tbody tr[data-row]')].map(r => r.dataset.row).join(','),
			cols: [...document.querySelectorAll('[data-access-matrix] thead th[data-column]')].map(c => c.dataset.column).join(','),
			legend: document.querySelector('[data-access-matrix] [data-legend]').textContent.replace(/\s+/g, ' ').trim(),
			writing: getComputedStyle(document.querySelector('[data-access-matrix] thead th[data-column] span')).writingMode,
			hasChips: document.querySelectorAll('[data-access-matrix] tbody th [data-agent-chip]').length === 5,
			hue: [...document.querySelectorAll('[data-access-matrix] tbody th [data-agent-chip] i')].map(i => getComputedStyle(i).backgroundColor).join('|'),
		})`, &shape),
	)
	if shape.Rows != "default,software-architect,go-developer,devops,plain" {
		t.Errorf("rows = %q", shape.Rows)
	}
	if shape.Cols != "domain,adapters,unlayered" {
		t.Errorf("columns = %q", shape.Cols)
	}
	if !strings.Contains(shape.Legend, "Change") || !strings.Contains(shape.Legend, "Read") || !strings.Contains(shape.Legend, "None") {
		t.Errorf("legend = %q", shape.Legend)
	}
	if !strings.HasPrefix(shape.Writing, "vertical") {
		t.Errorf("layer header writing-mode = %q, want a rotated header", shape.Writing)
	}
	if !shape.HasChips {
		t.Error("every agent row should carry an agent chip")
	}
	if colours := strings.Split(shape.Hue, "|"); len(colours) < 2 || colours[1] == colours[2] {
		t.Errorf("agent chip colours = %q, want a colour per agent", shape.Hue)
	}
	var hint string
	rig.run(t, layerTextOf(`[data-access-matrix] [data-matrix-hint]`, &hint))
	for _, want := range []string{"Click a cell to cycle none → read → change.", "Click an agent name to set a whole row.", "Unlisted agents keep the default."} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q lacks %q", hint, want)
		}
	}
}

// A cell cycles its access, and clicking an agent's name sets the whole row; the draft changes with them.
func TestClickingACellCyclesItAndClickingAnAgentNameSetsTheWholeRow(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	openLayersTabAt(t, rig, 1440)
	rig.run(t,
		poll(hasCell("go-developer on domain: read")),
		chromedp.Click(cellSel("go-developer on domain: read"), chromedp.ByQuery),
		poll(hasCell("go-developer on domain: change")),
		poll(`!!document.querySelector('[data-save-bar]')?.checkVisibility()`),
		// the selected layer is domain: the row takes go-developer's access there on every layer
		chromedp.Click(`[data-access-matrix] [data-set-row="go-developer"]`, chromedp.ByQuery),
		poll(hasCell("go-developer on adapters: change")),
		poll(hasCell("devops on adapters: read")),
		saveFromBar(),
	)
	for i := 0; i < 2; i++ {
		if ag := accessAgents(savedLayer(t, rig, i)["access"]); ag["go-developer"] != "change" || ag["devops"] != nil {
			t.Errorf("layer %d saved agents = %v", i, ag)
		}
	}
	// with another layer selected, the row takes that layer's access
	rig.run(t,
		selectLayer(1),
		chromedp.Click(cellSel("devops on adapters: read"), chromedp.ByQuery),
		poll(hasCell("devops on adapters: change")),
		chromedp.Click(`[data-access-matrix] [data-set-row="devops"]`, chromedp.ByQuery),
		poll(hasCell("devops on domain: change")),
	)
}

// At 760 px the matrix scrolls sideways and the agent names stay pinned.
func TestTheAccessMatrixScrollsSidewaysAtNarrowWidthsWithAgentNamesPinned(t *testing.T) {
	rig := newArchRig(t)
	openLayersTabAt(t, rig, 760)
	var m struct {
		Scrolls bool
		Before  float64
		After   float64
		Moved   float64
		Page    bool
	}
	rig.run(t,
		chromedp.Click(layersPanel+`[data-preset]`, chromedp.ByQuery),
		poll(layerItemsJS+`.length === 8`),
		poll(hasCell("go-developer on docs: read")),
		evalLayers(`(() => {
			const box = document.querySelector('[data-access-matrix] .access-matrix');
			const name = () => document.querySelector('[data-access-matrix] tbody tr[data-row=devops] th').getBoundingClientRect().left;
			const cell = () => document.querySelector('[data-access-matrix] tbody tr[data-row=devops] td:last-child').getBoundingClientRect().left;
			const before = name(), cellBefore = cell();
			box.scrollLeft = 240;
			return { scrolls: box.scrollWidth > box.clientWidth, before, after: name(), moved: cellBefore - cell(), page: document.documentElement.scrollWidth <= document.documentElement.clientWidth };
		})()`, &m),
	)
	if !m.Scrolls {
		t.Error("the matrix does not scroll sideways at 760 px")
	}
	if m.Before != m.After {
		t.Errorf("the agent name moved from %v to %v while the matrix scrolled", m.Before, m.After)
	}
	if m.Moved <= 0 {
		t.Errorf("the cells did not move when scrolled (%v)", m.Moved)
	}
	if !m.Page {
		t.Error("the page itself scrolls sideways at 760 px")
	}
}
