//go:build !windows

package web_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBundleEditorRenamesPreviewsSavesUndoesAndGuardsLeaving_EndToEnd is W1
// for stories 6, 12, 13, 14 and 17 of spec "Web UI — the Agents page",
// against a real server and a temporary catalog: renaming a bundle names who
// is rewritten, Review change shows the server's diff, a save offers Undo and
// Undo restores, an outside edit between open and save shows the conflict and
// Overwrite writes, a bundle cycle is refused, and leaving a dirty editor asks.
func TestBundleEditorRenamesPreviewsSavesUndoesAndGuardsLeaving_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"ux": ["lint", "tdd"], "all": ["@ux", "tdd"], "a": ["tdd"], "b": ["@a"]},
  "agents": {
    "ux-designer": {"description": "Designs.", "skills": ["@ux"]},
    "docs": {"description": "Writes.", "skills": ["@ux", "lint", "@all"]}
  }
}
`)
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")
	writeCatalogFile(t, w, "skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nbody\n")
	agentsJSON := func() string {
		raw, err := os.ReadFile(filepath.Join(w.root, "agents", "agents.json"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	const rows = `[...document.querySelectorAll('[data-bundle-row]')].map(r => r.dataset.key).join(',')`
	const detail = `document.querySelector('[data-bundle-detail]')`
	const editor = `document.querySelector('[data-bundle-editor]')`
	flat := func(sel string) string { return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()` }
	visible := func(sel string) string { return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true` }
	save := `document.querySelector('[data-save-footer] [data-save]')`
	nameField := `#bundle-name`

	b := newTLSBrowser(t)
	b.run("the Bundles tab lists every bundle with its size and who names it, and the footer names the source",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/bundles"),
		waitUntil(rows+` === 'ux,all,a,b'`),
		waitUntil(`document.querySelector('[data-bundle-foot]').textContent.trim() === 'agents.json › bundles · 4 bundles'`),
		waitUntil(flat(`[data-bundle-row][data-key="ux"]`)+`.includes('@ux') && `+flat(`[data-bundle-row][data-key="ux"]`)+`.includes('2 skills · 2 agents')`),
		waitUntil(flat(`[data-bundle-row][data-key="b"]`)+`.includes('1 skill · unused')`),
	)
	b.run("the quick filters narrow the list",
		clickButton(`document.querySelector('[data-bundle-filters]')`, "Unused"),
		waitUntil(rows+` === 'b'`),
		clickButton(`document.querySelector('[data-bundle-filters]')`, "Holds bundles"),
		waitUntil(rows+` === 'all,b'`),
		clickButton(`document.querySelector('[data-bundle-filters]')`, "All"),
		waitUntil(rows+` === 'ux,all,a,b'`),
	)
	b.run("a bundle's detail shows what it contains, its size and who names it",
		chromedp.Click(`[data-bundle-row][data-key="ux"]`, chromedp.ByQuery),
		atPath(w.srv.URL+"/agents/bundles/ux"),
		waitUntil(detail+`?.querySelector('h2').textContent.trim() === '@ux'`),
		waitUntil(detail+`.querySelector('[data-path]').textContent.trim() === 'agents.json › bundles.ux'`),
		waitUntil(detail+`.querySelector('[data-bundle-size]').textContent.trim() === '2 skills after nested bundles'`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-contains] a')].map(a => a.textContent.trim() + '=' + a.getAttribute('href')).join() === 'lint=/agents/skills/lint,tdd=/agents/skills/tdd'`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-used="agents"] a')].map(a => a.textContent.trim()).join() === 'ux-designer,docs'`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-used="bundles"] a')].map(a => a.textContent.trim()).join() === '@all'`),
		waitUntil(`!`+detail+`.querySelector('[data-unused-badge]').checkVisibility()`),
		bothSchemes("agents-bundle-detail"),
	)

	b.run("Edit opens the editor in the detail pane, with Save off until something changes",
		chromedp.Click(`[data-bundle-detail] a[data-edit]`, chromedp.ByQuery),
		atPath(w.srv.URL+"/agents/bundles/ux/edit"),
		waitUntil(editor+`?.querySelector('h2').textContent.trim() === 'Edit @ux'`),
		waitUntil(`document.querySelector('`+nameField+`').value === 'ux'`),
		waitUntil(`[...`+editor+`.querySelectorAll('[data-chip]')].map(c => c.dataset.chip).join() === 'lint,tdd'`),
		waitUntil(save+`.disabled === true`),
		waitUntil(flat(`[data-save-state]`)+` === 'No changes · writes agents.json'`),
		waitUntil(`!`+visible(`[data-rename-banner]`)),
	)
	b.run("renaming names every agent and bundle whose @name will be rewritten, and the footer counts the change",
		setField(nameField, "design"),
		waitUntil(visible(`[data-rename-banner]`)),
		waitUntil(flat(`[data-rename-banner]`)+`.includes('@ux') && `+flat(`[data-rename-banner]`)+`.includes('@design')`),
		waitUntil(flat(`[data-rename-banner]`)+`.includes('2 agents and 1 bundle (ux-designer, docs, @all)')`),
		waitUntil(flat(`[data-save-state]`)+` === '1 unsaved change · writes agents.json'`),
		waitUntil(save+`.disabled === false`),
		waitUntil(`document.querySelector('`+nameField+`').getAttribute('aria-describedby') === 'bundle-name-hint'`),
		bothSchemes("agents-bundle-editor-rename"),
	)
	b.run("Review change shows the server's diff of every file it will write",
		waitUntil(`document.querySelector('[data-save-footer] [data-review]').disabled === false`),
		clickButton(`document.querySelector('[data-save-footer]')`, "Review change"),
		waitUntil(visible(`[data-review-panel]`)),
		waitUntil(`document.querySelectorAll('[data-review-panel] [data-diff-file]').length === 1 && document.querySelector('[data-review-panel] [data-diff-file]').dataset.path === 'agents.json'`),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-line][data-kind=add]')].some(l => l.textContent.includes('"design"'))`),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-line][data-kind=del]')].some(l => l.textContent.includes('"ux"'))`),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-line][data-kind=add]')].some(l => l.textContent.includes('@design'))`),
		bothSchemes("agents-bundle-review"),
	)
	b.run("saving shows the server's summary, Undo and what running sessions keep, and lands on the renamed bundle",
		clickButton(`document.querySelector('[data-save-footer]')`, "Save bundle"),
		atPath(w.srv.URL+"/agents/bundles/design"),
		waitUntil(visible(`[data-toast]`)),
		waitUntil(flat(`[data-toast]`)+`.includes('saved agents.json')`),
		waitUntil(flat(`[data-toast]`)+`.includes('Running sessions keep what they started with')`),
		waitUntil(`document.querySelector('.toast-region').getAttribute('role') === 'status'`),
		waitUntil(detail+`?.querySelector('h2').textContent.trim() === '@design'`),
		waitUntil(rows+` === 'design,all,a,b'`),
		bothSchemes("agents-bundle-saved-toast"),
	)
	if got := agentsJSON(); !strings.Contains(got, `"design"`) || strings.Contains(got, `"ux"`) || strings.Contains(got, "@ux") || strings.Count(got, "@design") != 3 {
		t.Fatalf("after the rename agents.json =\n%s", got)
	}
	b.run("Undo puts everything back",
		clickButton(`document.querySelector('[data-toast]')`, "Undo"),
		waitUntil(rows+` === 'ux,all,a,b'`),
		waitUntil(`!document.querySelector('[data-toast] [data-undo]')`),
	)
	if got := agentsJSON(); strings.Contains(got, "@design") || strings.Count(got, "@ux") != 3 {
		t.Fatalf("after Undo agents.json =\n%s", got)
	}

	b.run("an outside edit between opening and saving blocks the save and shows what changed",
		chromedp.Navigate(w.srv.URL+"/agents/bundles/ux/edit"),
		waitUntil(editor+`?.querySelector('h2').textContent.trim() === 'Edit @ux'`),
		setField(nameField, "design2"),
		waitUntil(`document.querySelector('[data-save-footer] [data-review]').disabled === false`),
	)
	writeCatalogFile(t, w, "agents.json", strings.Replace(agentsJSON(), `"docs": {`, `"intruder": {"description": "Added by hand.", "skills": ["tdd"]},
    "docs": {`, 1))
	b.run("Save is refused with the conflict banner, announced, with Reload and Overwrite",
		clickButton(`document.querySelector('[data-save-footer]')`, "Save bundle"),
		waitUntil(visible(`[data-conflict]`)),
		waitUntil(`document.querySelector('[data-save-alerts]').getAttribute('role') === 'alert' && document.querySelector('[data-save-alerts]').contains(document.querySelector('[data-conflict]'))`),
		waitUntil(flat(`[data-conflict]`)+`.includes('changed on disk')`),
		waitUntil(`[...document.querySelectorAll('[data-conflict] [data-diff-line][data-kind=add]')].some(l => l.textContent.includes('intruder'))`),
		waitUntil(flat(`[data-conflict]`)+`.includes('Reload (lose my edits)') && `+flat(`[data-conflict]`)+`.includes('Overwrite')`),
		waitUntil(save+`.disabled === true`),
		waitUntil(`document.querySelector('`+nameField+`').value === 'design2'`),
		bothSchemes("agents-bundle-conflict"),
	)
	b.run("Overwrite writes the save anyway",
		clickButton(`document.querySelector('[data-conflict]')`, "Overwrite"),
		atPath(w.srv.URL+"/agents/bundles/design2"),
		waitUntil(flat(`[data-toast]`)+`.includes('saved agents.json')`),
		waitUntil(rows+` === 'design2,all,a,b'`),
	)
	if got := agentsJSON(); !strings.Contains(got, `"design2"`) || strings.Contains(got, "@ux") {
		t.Fatalf("after Overwrite agents.json =\n%s", got)
	}

	b.run("the picker is a combobox: focusing it lists bundles first",
		chromedp.Navigate(w.srv.URL+"/agents/bundles/a/edit"),
		waitUntil(editor+`?.querySelector('h2').textContent.trim() === 'Edit @a'`),
		chromedp.Click(`#bundlePicker-input`, chromedp.ByQuery),
		waitUntil(visible(`#bundlePicker-list`)),
		waitUntil(`document.querySelector('#bundlePicker-input').getAttribute('aria-expanded') === 'true' && document.querySelector('#bundlePicker-input').getAttribute('role') === 'combobox'`),
		waitUntil(`[...document.querySelectorAll('#bundlePicker-list [role=option]')].map(o => o.dataset.key).join() === '@all,@b,@design2,lint,tdd'`),
	)
	b.run("typing narrows it and the keyboard adds the active option as a chip",
		setField(`#bundlePicker-input`, "b"),
		waitUntil(`[...document.querySelectorAll('#bundlePicker-list [role=option]')].map(o => o.dataset.key).join() === '@b'`),
		pressKey("ArrowDown", 40),
		pressKey("Enter", 13),
		waitUntil(`[...`+editor+`.querySelectorAll('[data-chip]')].map(c => c.dataset.chip).join() === 'tdd,@b'`),
	)
	b.run("a bundle that would include itself, through another, is refused and linked to the entries field",
		waitUntil(visible(`#bundle-entries-error`)),
		waitUntil(flat(`#bundle-entries-error`)+`.includes('Bundles cannot include each other')`),
		waitUntil(`document.querySelector('#bundlePicker-input').getAttribute('aria-describedby') === 'bundle-entries-hint bundle-entries-error'`),
		waitUntil(`document.querySelector('#bundlePicker-input').getAttribute('aria-invalid') === 'true'`),
		waitUntil(save+`.disabled === true`),
		bothSchemes("agents-bundle-cycle"),
	)
	b.run("an invalid name is linked to its field and turns Save off",
		setField(nameField, "Not A Slug"),
		waitUntil(visible(`#bundle-name-error`)),
		waitUntil(`document.querySelector('`+nameField+`').getAttribute('aria-describedby') === 'bundle-name-hint bundle-name-error'`),
		waitUntil(`document.querySelector('`+nameField+`').getAttribute('aria-invalid') === 'true'`),
		waitUntil(save+`.disabled === true`),
		setField(nameField, "a"),
		waitUntil(`!`+visible(`#bundle-name-error`)),
	)

	b.run("leaving the dirty editor asks first; Keep editing stays",
		chromedp.Click(`#agents-tab-skills`, chromedp.ByQuery),
		waitUntil(visible(`[data-leave]`)),
		waitUntil(`location.pathname === '/agents/bundles/a/edit' && document.querySelector('[role=tab][aria-selected=true]').dataset.tab === 'bundles'`),
		waitUntil(`document.querySelector('[data-leave]').getAttribute('role') === 'alertdialog' && document.activeElement === document.querySelector('[data-keep]')`),
		clickButton(`document.querySelector('[data-leave]')`, "Keep editing"),
		waitUntil(`!`+visible(`[data-leave]`)),
		waitUntil(`[...`+editor+`.querySelectorAll('[data-chip]')].map(c => c.dataset.chip).join() === 'tdd,@b'`),
	)
	b.run("Discard changes leaves, and the editor is gone",
		chromedp.Click(`#agents-tab-skills`, chromedp.ByQuery),
		waitUntil(visible(`[data-leave]`)),
		clickButton(`document.querySelector('[data-leave]')`, "Discard changes"),
		atPath(w.srv.URL+"/agents/skills"),
		waitUntil(`document.querySelector('[role=tab][aria-selected=true]').dataset.tab === 'skills'`),
		waitUntil(`!document.querySelector('[data-bundle-editor]')`),
	)
	if got := agentsJSON(); strings.Contains(got, "@b") {
		t.Fatalf("a discarded edit was saved:\n%s", got)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
