//go:build !windows

package web_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// deleteMenu opens the actions menu inside detail (a CSS selector) and
// chooses Delete…, as a person's click would.
func deleteMenu(detail string) chromedp.Action {
	return waitUntil(`(() => {
		const root = document.querySelector(` + quote(detail) + `);
		const trigger = root?.querySelector('.menu > button');
		const item = root?.querySelector('[data-action=delete]');
		if (!trigger || !item) return false;
		trigger.click();
		item.click();
		return true;
	})()`)
}

// deleteDialogText is the dialog's visible text on one line.
const deleteDialogText = `(document.querySelector('[data-delete-dialog]')?.innerText ?? '').replace(/\s+/g, ' ').trim()`

// deleteGroup lists what the dialog says one group reaches, joined by commas.
func deleteGroup(key string) string {
	return `[...document.querySelectorAll('[data-delete-group][data-group=` + key + `] [data-delete-item]')].map(i => i.textContent.trim()).join()`
}

// TestDeleteDialogForABundleAsksForTheNameAndUndoPutsItBack_EndToEnd is W1 for
// story 16 (and 17) of spec "Web UI — the Agents page", against a real server
// and a temporary catalog: deleting a bundle that two agents and a bundle name
// opens a dialog that lists them, says the catalog is not in git, and keeps
// Delete off until the name is typed exactly. The keyboard stays in the dialog,
// Escape closes it and gives the focus back, the delete shows the toast with
// Undo, and Undo writes the catalog back byte for byte.
func TestDeleteDialogForABundleAsksForTheNameAndUndoPutsItBack_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	const original = `{
  "bundles": {"ux": ["lint", "tdd"], "all": ["@ux", "tdd"], "solo": ["tdd"]},
  "agents": {
    "ux-designer": {"description": "Designs.", "skills": ["@ux"]},
    "docs": {"description": "Writes.", "skills": ["@ux", "lint"]}
  }
}
`
	writeCatalogFile(t, w, "agents.json", original)
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
	const detail = `[data-bundle-detail]`
	const confirm = `document.querySelector('[data-delete-confirm]')`
	const typed = `#delete-name`
	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	menuButton := `document.querySelector('[data-bundle-detail] .menu > button')`

	b := newTLSBrowser(t)
	b.run("the bundle's menu offers Delete, now on",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/bundles/ux"),
		waitUntil(rows+` === 'ux,all,solo'`),
		waitUntil(`document.querySelector('[data-bundle-detail] [data-action=delete]')?.getAttribute('aria-disabled') === null`),
	)
	b.run("Delete opens a dialog that names the bundle, lists who it reaches and says the catalog is not in git",
		deleteMenu(detail),
		waitUntil(visible(`[data-delete-dialog]`)),
		waitUntil(`document.querySelector('[data-delete-dialog]').getAttribute('role') === 'alertdialog' && document.querySelector('[data-delete-dialog]').getAttribute('aria-modal') === 'true'`),
		waitUntil(flat(`#delete-title`)+` === 'Delete bundle “@ux”?'`),
		waitUntil(deleteGroup("agents")+` === 'ux-designer,docs'`),
		waitUntil(deleteGroup("bundles")+` === '@all'`),
		waitUntil(flat(`[data-delete-total]`)+`.includes('Those agents lose up to 2 skills')`),
		waitUntil(flat(`[data-delete-note]`)+`.includes('Undo in the message that follows puts everything back, until the next catalog change or a server restart') && `+flat(`[data-delete-note]`)+`.includes('The catalog is not in git')`),
		waitUntil(`!/backup|restore|commit|checkout/i.test(`+deleteDialogText+`) && !/\bgit\b/i.test(`+deleteDialogText+`.replaceAll('not in git', ''))`),
		waitUntil(`document.querySelector('[aria-describedby="delete-intro delete-note"]') === document.querySelector('[data-delete-dialog]')`),
		bothSchemes("agents-delete-dialog"),
	)
	b.run("Delete is off until the name is typed exactly",
		waitUntil(`document.activeElement === document.querySelector('`+typed+`') && `+confirm+`.disabled === true`),
		setField(typed, "@ux"),
		waitUntil(confirm+`.disabled === true`),
		setField(typed, "UX"),
		waitUntil(confirm+`.disabled === true`),
		setField(typed, "u"),
		waitUntil(confirm+`.disabled === true`),
		setField(typed, "ux"),
		waitUntil(confirm+`.disabled === false`),
		bothSchemes("agents-delete-dialog-ready"),
		setField(typed, "ux "),
		waitUntil(confirm+`.disabled === true`),
	)
	b.run("Tab stays inside the dialog, both ways",
		chromedp.Focus(typed, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Tab),
		waitUntil(`document.activeElement === document.querySelector('[data-delete-cancel]')`),
		chromedp.KeyEvent(kb.Tab),
		waitUntil(`document.activeElement === document.querySelector('`+typed+`')`),
		chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(input.ModifierShift)),
		waitUntil(`document.activeElement === document.querySelector('[data-delete-cancel]')`),
	)
	b.run("Escape closes the dialog, deletes nothing and gives the focus back to the menu button",
		chromedp.KeyEvent(kb.Escape),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
		waitUntil(`document.activeElement === `+menuButton),
		waitUntil(rows+` === 'ux,all,solo'`),
	)
	if got := agentsJSON(); got != original {
		t.Fatalf("Escape changed agents.json:\n%s", got)
	}
	b.run("Cancel does the same, and what was typed is forgotten",
		deleteMenu(detail),
		waitUntil(visible(`[data-delete-dialog]`)),
		setField(typed, "ux"),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Cancel"),
		waitUntil(`!document.querySelector('[data-delete-dialog]') && document.activeElement === `+menuButton),
		deleteMenu(detail),
		waitUntil(visible(`[data-delete-dialog]`)+` && document.querySelector('`+typed+`').value === '' && `+confirm+`.disabled === true`),
	)
	b.run("typing the name and choosing Delete removes the bundle, shows the toast with Undo and goes back to the Bundles tab",
		setField(typed, "ux"),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Delete bundle"),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
		atPath(w.srv.URL+"/agents/bundles"),
		waitUntil(visible(`[data-toast]`)),
		waitUntil(flat(`[data-toast]`)+`.includes('ux')`),
		waitUntil(`!!document.querySelector('[data-toast] [data-undo]')`),
		waitUntil(`document.querySelector('[role=tab][aria-selected=true]').dataset.tab === 'bundles'`),
		waitUntil(rows+` === 'all,solo'`),
		bothSchemes("agents-delete-toast"),
	)
	if got := agentsJSON(); strings.Contains(got, "@ux") || strings.Contains(got, `"ux"`) {
		t.Fatalf("after the delete agents.json still names the bundle:\n%s", got)
	}
	b.run("Undo puts it back",
		clickButton(`document.querySelector('[data-toast]')`, "Undo"),
		waitUntil(rows+` === 'ux,all,solo'`),
		waitUntil(`!document.querySelector('[data-toast] [data-undo]')`),
	)
	if got := agentsJSON(); got != original {
		t.Fatalf("after Undo agents.json =\n%s\nwant\n%s", got, original)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestDeleteDialogListsWhatEachKindReachesAndRefusesAnOutsideEdit_EndToEnd is
// W1 for the dialog's reach and the conflict of story 16, against a real
// server and a temporary catalog: a skill reaches the agents that list it and
// the bundles that hold it, an MCP server reaches agents and projects, an
// agent reaches nothing the catalog can list, and an outside edit between
// opening and deleting shows the conflict banner with Reload and Delete anyway.
func TestDeleteDialogListsWhatEachKindReachesAndRefusesAnOutsideEdit_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"core": ["tdd", "lint"], "extra": ["tdd"]},
  "agents": {
    "dev": {"description": "Builds.", "skills": ["tdd", "@extra"], "mcp_servers": ["github"]},
    "ops": {"description": "Runs.", "skills": ["@core"], "mcp_servers": ["github"]},
    "idle": {"description": "Waits."}
  }
}
`)
	writeCatalogFile(t, w, "skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nbody\n")
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"github": {"command": "npx", "args": ["gh-mcp"]}, "lonely": {"command": "npx", "args": ["x"]}}}
`)
	res, err := w.srv.Client().Post(w.srv.URL+"/api/set_project_mcp_servers", "application/json", strings.NewReader(`{"project_id":"`+w.project.ID+`","mcp_servers":["github"]}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	const typed = `#delete-name`
	const confirm = `document.querySelector('[data-delete-confirm]')`
	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	closeDialog := chromedp.Tasks{
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Cancel"),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
	}

	b := newTLSBrowser(t)
	b.run("a skill reaches the agents that list it, the bundles that hold it and every agent through either",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/skills/tdd"),
		waitUntil(visible(`[data-skill-detail]`)),
		deleteMenu(`[data-skill-detail]`),
		waitUntil(flat(`#delete-title`)+` === 'Delete skill “tdd”?'`),
		waitUntil(deleteGroup("agents")+` === 'dev'`),
		waitUntil(deleteGroup("bundles")+` === '@core,@extra'`),
		waitUntil(flat(`[data-delete-total]`)+`.includes('2 agents')`),
		waitUntil(flat(`[data-delete-note]`)+`.includes('The catalog is not in git')`),
		waitUntil(`!/backup|restore|commit|checkout/i.test(`+deleteDialogText+`) && !/\bgit\b/i.test(`+deleteDialogText+`.replaceAll('not in git', ''))`),
		waitUntil(confirm+`.textContent.trim() === 'Delete skill' && `+confirm+`.disabled === true`),
		closeDialog,
	)
	b.run("an MCP server reaches the agents and the projects that get it",
		chromedp.Navigate(w.srv.URL+"/agents/mcp/github"),
		waitUntil(visible(`[data-mcp-detail]`)),
		deleteMenu(`[data-mcp-detail]`),
		waitUntil(flat(`#delete-title`)+` === 'Delete MCP server “github”?'`),
		waitUntil(deleteGroup("agents")+` === 'dev,ops'`),
		waitUntil(deleteGroup("projects")+` === 'harness'`),
		waitUntil(`!/\bgit\b/i.test(`+deleteDialogText+`.replaceAll('not in git', ''))`),
		bothSchemes("agents-delete-mcp"),
		closeDialog,
	)
	b.run("an unused MCP server says it reaches nobody",
		chromedp.Navigate(w.srv.URL+"/agents/mcp/lonely"),
		waitUntil(visible(`[data-mcp-detail]`)),
		deleteMenu(`[data-mcp-detail]`),
		waitUntil(flat(`[data-delete-nothing]`)+` === 'No agent or project gets it.' && `+visible(`[data-delete-nothing]`)),
		closeDialog,
	)
	b.run("an agent's dialog lists no one and says its skills, bundles and servers stay",
		chromedp.Navigate(w.srv.URL+"/agents/dev"),
		waitUntil(visible(`[data-agent-detail]`)),
		deleteMenu(`[data-agent-detail]`),
		waitUntil(flat(`#delete-title`)+` === 'Delete agent “dev”?'`),
		waitUntil(`document.querySelectorAll('[data-delete-group]').length === 0`),
		waitUntil(flat(`#delete-intro`)+`.includes('skills, bundles and MCP servers stay in the catalog')`),
		closeDialog,
	)
	b.run("a bundle's dialog is the same dialog",
		chromedp.Navigate(w.srv.URL+"/agents/bundles/extra"),
		waitUntil(visible(`[data-bundle-detail]`)),
		deleteMenu(`[data-bundle-detail]`),
		waitUntil(deleteGroup("agents")+` === 'dev'`),
		waitUntil(`document.querySelectorAll('[data-delete-group][data-group=bundles]').length === 0`),
	)
	writeCatalogFile(t, w, "agents.json", strings.Replace(readCatalog(t, w, "agents.json"), `"idle": {`, `"intruder": {"description": "By hand."},
    "idle": {`, 1))
	b.run("an outside edit refuses the delete: the banner shows what changed and nothing is deleted",
		setField(typed, "extra"),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Delete bundle"),
		waitUntil(visible(`[data-delete-conflict]`)),
		waitUntil(`document.querySelector('[data-delete-alerts]').getAttribute('role') === 'alert' && document.querySelector('[data-delete-alerts]').contains(document.querySelector('[data-delete-conflict]'))`),
		waitUntil(flat(`[data-delete-conflict]`)+`.includes('agents.json changed on disk')`),
		waitUntil(`[...document.querySelectorAll('[data-delete-conflict] [data-diff-line]')].some(l => l.textContent.includes('intruder'))`),
		waitUntil(confirm+`.disabled === true`),
		waitUntil(`!document.querySelector('[data-toast]')`),
		bothSchemes("agents-delete-conflict"),
	)
	if got := readCatalog(t, w, "agents.json"); !strings.Contains(got, `"extra"`) {
		t.Fatalf("the refused delete changed agents.json:\n%s", got)
	}
	b.run("Reload keeps the dialog and the typed name, and Delete then goes through",
		clickButton(`document.querySelector('[data-delete-conflict]')`, "Reload"),
		waitUntil(`!document.querySelector('[data-delete-conflict]')`),
		waitUntil(`document.querySelector('`+typed+`').value === 'extra' && `+confirm+`.disabled === false`),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Delete bundle"),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
		waitUntil(flat(`[data-toast]`)+`.includes('extra')`),
		waitUntil(`!!document.querySelector('[data-toast] [data-undo]')`),
	)
	if got := readCatalog(t, w, "agents.json"); strings.Contains(got, "@extra") || strings.Contains(got, `"extra"`) || !strings.Contains(got, "intruder") {
		t.Fatalf("after Reload and Delete agents.json =\n%s", got)
	}
	b.run("a second outside edit offers Delete anyway, which deletes the skill and its folder",
		chromedp.Navigate(w.srv.URL+"/agents/skills/lint"),
		waitUntil(visible(`[data-skill-detail]`)),
		deleteMenu(`[data-skill-detail]`),
		waitUntil(visible(`[data-delete-dialog]`)),
	)
	writeCatalogFile(t, w, "agents.json", strings.Replace(readCatalog(t, w, "agents.json"), `"idle": {`, `"intruder2": {"description": "Again."},
    "idle": {`, 1))
	b.run("Delete anyway",
		setField(typed, "lint"),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Delete skill"),
		waitUntil(visible(`[data-delete-conflict]`)),
		clickButton(`document.querySelector('[data-delete-conflict]')`, "Delete anyway"),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
		atPath(w.srv.URL+"/agents/skills"),
		waitUntil(`!!document.querySelector('[data-toast] [data-undo]')`),
		waitUntil(`![...document.querySelectorAll('[data-skill-row]')].some(r => r.dataset.key === 'lint')`),
	)
	if _, err := os.Stat(filepath.Join(w.root, "agents", "skills", "lint")); !os.IsNotExist(err) {
		t.Fatalf("the skill's folder is still there: %v", err)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

func readCatalog(t *testing.T, w *tokenSaverWorld, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.root, "agents", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
