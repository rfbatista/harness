//go:build !windows

package web_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// liveCatalogPost is a save sent from inside the page of another person's tab:
// the same API the page uses, so the server publishes the catalog event.
func liveCatalogPost(t *testing.T, b *browser, step, verb, body string) {
	t.Helper()
	var status int
	b.run(step, chromedp.Evaluate(`fetch('/api/`+verb+`', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(`+body+`)}).then(r => r.status)`, &status,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }))
	if status != 200 {
		t.Fatalf("%s: POST %s answered %d", step, verb, status)
	}
}

const liveCatalogFeedLive = `document.querySelector('main[x-data="agentsPage"]')?.dataset.feed === 'live'`

// TestAnOpenAgentsPageFollowsTheCatalogInAnotherTab_EndToEnd is W1 for story
// 18 of spec "Web UI — the Agents page", against a real server and a temporary
// catalog, with two browsers: a save made through the API from the second
// refreshes the first, the row and the open detail say what changed on disk,
// an editor open on that agent keeps its draft and shows the conflict banner,
// and an edit by hand shows up on Reload from disk and on regaining focus.
func TestAnOpenAgentsPageFollowsTheCatalogInAnotherTab_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"ux": ["lint"]},
  "agents": {
    "ux-designer": {"description": "Designs screens.", "skills": ["@ux", "tdd"]},
    "docs": {"description": "Writes docs.", "skills": ["tdd"]}
  }
}
`)
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")
	writeCatalogFile(t, w, "skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nbody\n")

	const rows = `[...document.querySelectorAll('[data-agent-row]')].map(r => r.dataset.key).join(',')`
	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	row := func(key string) string { return `[data-agent-row][data-key="` + key + `"]` }

	mine := newTLSBrowser(t)
	other := newTLSBrowser(t)
	mine.run("the first tab shows the agents, with the page following the feed",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/ux-designer"),
		waitUntil(rows+` === 'ux-designer,docs'`),
		waitUntil(liveCatalogFeedLive),
		waitUntil(`!`+visible(`[data-disk-changed]`)),
	)
	other.run("the second tab is on the same catalog", chromedp.Navigate(w.srv.URL+"/agents"), waitUntil(rows+` === 'ux-designer,docs'`))

	liveCatalogPost(t, other, "the second tab adds an agent through the API",
		"save_catalog_agent", `{agent: {name: 'fresh', description: 'Brand new.', aliases: [], entries: ['tdd'], mcp_servers: [], allowed_tools: []}}`)
	mine.run("the first tab lists the new agent without a reload, and flags nothing it did not change",
		waitUntil(rows+` === 'ux-designer,docs,fresh'`),
		waitUntil(`!`+visible(`[data-disk-changed]`)+` && !document.querySelector('[data-agent-row] [data-tone]:not([hidden])')`),
	)

	liveCatalogPost(t, other, "the second tab changes the agent open in the first",
		"save_catalog_agent", `{previous_name: 'ux-designer', agent: {name: 'ux-designer', description: 'Designs better screens.', aliases: [], entries: ['tdd'], mcp_servers: [], allowed_tools: ['Read']}}`)
	mine.run("its row says changed on disk, and its detail says what changed and shows what is on disk now",
		waitUntil(visible(`[data-disk-changed]`)),
		waitUntil(flat(`[data-disk-changed] strong`)+` === 'This agent changed on disk.'`),
		waitUntil(flat(`[data-disk-changed]`)+`.includes('description changed') && `+flat(`[data-disk-changed]`)+`.includes('skills and bundles: removed @ux') && `+flat(`[data-disk-changed]`)+`.includes('allowed tools: added Read')`),
		waitUntil(flat(`[data-agent-detail] > p`)+` === 'Designs better screens.'`),
		waitUntil(flat(row("ux-designer")+` .badge`)+` === 'changed on disk'`),
		waitUntil(`!`+flat(row("docs")+` .badge`)),
		bothSchemes("agents-live-changed-on-disk"),
	)
	mine.run("Dismiss clears the banner and the badge",
		clickButton(`document.querySelector('[data-disk-changed]')`, "Dismiss"),
		waitUntil(`!`+visible(`[data-disk-changed]`)),
		waitUntil(`!`+flat(row("ux-designer")+` .badge`)),
	)

	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"ux": ["lint"]},
  "agents": {
    "ux-designer": {"description": "Designs better screens.", "skills": ["tdd"], "allowed_tools": ["Read"]},
    "docs": {"description": "Writes docs.", "skills": ["tdd"]},
    "fresh": {"description": "Brand new.", "skills": ["tdd"]},
    "handmade": {"description": "Typed in an editor.", "skills": ["tdd"]}
  }
}
`)
	mine.run("an edit by hand shows up on Reload from disk",
		clickButton(`document.querySelector('[data-reload-disk]').parentElement`, "Reload from disk"),
		waitUntil(rows+` === 'ux-designer,docs,fresh,handmade'`),
	)
	writeCatalogFile(t, w, "agents.json", strings.Replace(readCatalogFile(t, w, "agents.json"), `"handmade": {"description": "Typed in an editor."`, `"handmade": {"description": "Typed again."`, 1))
	mine.run("and when the window regains focus",
		chromedp.Evaluate(`window.dispatchEvent(new Event('focus'))`, nil),
		waitUntil(flat(row("handmade")+` .desc`)+` === 'Typed again.'`),
		waitUntil(flat(row("handmade")+` .badge`)+` === 'changed on disk'`),
	)

	mine.run("an editor open on the agent keeps what was typed",
		chromedp.Navigate(w.srv.URL+"/agents/ux-designer/edit"),
		waitUntil(`document.querySelector('#agent-desc')?.value === 'Designs better screens.'`),
		waitUntil(liveCatalogFeedLive),
		setField(`#agent-desc`, "My own wording."),
		waitUntil(`document.querySelector('[data-save-footer] [data-save]').disabled === false`),
	)
	liveCatalogPost(t, other, "the second tab saves the same agent again",
		"save_catalog_agent", `{previous_name: 'ux-designer', agent: {name: 'ux-designer', description: 'Someone else wrote this.', aliases: [], entries: ['tdd'], mcp_servers: [], allowed_tools: ['Read']}}`)
	mine.run("the editor shows the conflict banner with the diff, Save off, and the draft untouched",
		waitUntil(visible(`[data-conflict]`)),
		waitUntil(`document.querySelector('[data-save-alerts]').contains(document.querySelector('[data-conflict]'))`),
		waitUntil(flat(`[data-conflict]`)+`.includes('agents.json changed on disk')`),
		waitUntil(`[...document.querySelectorAll('[data-conflict] [data-diff-line]')].some(l => l.textContent.includes('Someone else wrote this.'))`),
		waitUntil(flat(`[data-conflict]`)+`.includes('Reload (lose my edits)') && `+flat(`[data-conflict]`)+`.includes('Overwrite')`),
		waitUntil(`document.querySelector('[data-save-footer] [data-save]').disabled === true`),
		waitUntil(`document.querySelector('#agent-desc').value === 'My own wording.'`),
		bothSchemes("agents-live-editor-conflict"),
	)
	// The dirty form would make leaving ask; Reload drops the edit first.
	mine.run("Reload shows what is on disk now", clickButton(`document.querySelector('[data-conflict]')`, "Reload (lose my edits)"),
		waitUntil(`document.querySelector('#agent-desc').value === 'Someone else wrote this.'`),
		waitUntil(`!`+visible(`[data-conflict]`)),
	)
	if e := mine.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestABrokenAgentsFileShowsItsLineAndTurnsSavesOffUntilItIsFixed_EndToEnd is
// W1 for story 15: agents.json broken on disk shows the last lists that
// parsed, the file, the line and the column, every save and delete is off,
// and fixing the file then Check again turns them back on.
func TestABrokenAgentsFileShowsItsLineAndTurnsSavesOffUntilItIsFixed_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	good := `{
  "bundles": {"ux": ["lint"]},
  "agents": {
    "ux-designer": {"description": "Designs screens.", "skills": ["@ux"]}
  }
}
`
	writeCatalogFile(t, w, "agents.json", good)
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")
	writeCatalogFile(t, w, "skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nbody\n")

	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	save := `document.querySelector('[data-save-footer] [data-save]')`

	b := newTLSBrowser(t)
	b.run("the bundle editor is open on a good catalog, with Save on after an edit",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/bundles/ux/edit"),
		waitUntil(`document.querySelector('#bundle-name')?.value === 'ux'`),
		waitUntil(liveCatalogFeedLive),
		waitUntil(`!`+visible(`[data-broken]`)),
		chromedp.Click(`#bundlePicker-input`, chromedp.ByQuery),
		waitUntil(`[...document.querySelectorAll('#bundlePicker-list [role=option]')].some(o => o.dataset.key === 'tdd')`),
		chromedp.Click(`#bundlePicker-list [role=option][data-key=tdd]`, chromedp.ByQuery),
		waitUntil(save+`.disabled === false`),
	)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"ux": ["lint"]},
  "agents": {
    "ux-designer": {"description": "Designs screens.", "skills": ["@ux"],}
  }
}
`)
	b.run("the file stops parsing: the banner names the file, the line and the column, and Save is off",
		chromedp.Evaluate(`window.dispatchEvent(new Event('focus'))`, nil),
		waitUntil(visible(`[data-broken]`)),
		waitUntil(flat(`[data-broken-title]`)+` === 'agents.json does not parse'`),
		waitUntil(`/^Line 4, column \d+$/.test(`+flat(`[data-broken-where]`)+`)`),
		waitUntil(flat(`[data-broken-message]`)+`.length > 0`),
		waitUntil(flat(`[data-broken]`)+`.includes('Saving and deleting are off until the file is fixed by hand')`),
		waitUntil(`document.querySelector('[data-broken]').getAttribute('role') === 'alert'`),
		waitUntil(save+`.disabled === true`),
		waitUntil(flat(`[data-save-state]`)+`.startsWith('Saving is off')`),
		waitUntil(`document.querySelector('[data-broken-check]').disabled === false`),
		bothSchemes("agents-live-broken"),
	)
	b.run("Check again while it is still broken keeps saves off",
		clickButton(`document.querySelector('[data-broken]')`, "Check again"),
		waitUntil(`document.querySelector('[data-broken-check]').disabled === false`),
		waitUntil(visible(`[data-broken]`)+` && `+save+`.disabled === true`),
	)

	b.run("the last lists that parsed are still there, and a delete is off too",
		chromedp.Click(`[data-cancel]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-leave]') !== null`),
		clickButton(`document.querySelector('[data-leave]')`, "Discard changes"),
		atPath(w.srv.URL+"/agents/bundles/ux"),
		waitUntil(visible(`[data-bundle-detail]`)),
		waitUntil(visible(`[data-broken]`)),
		deleteMenu(`[data-bundle-detail]`),
		waitUntil(visible(`[data-delete-dialog]`)),
		setField(`#delete-name`, "ux"),
		waitUntil(visible(`[data-delete-blocked]`)),
		waitUntil(flat(`[data-delete-blocked]`)+`.startsWith('Deleting is off · agents.json does not parse')`),
		waitUntil(`document.querySelector('[data-delete-confirm]').disabled === true`),
		bothSchemes("agents-live-broken-delete"),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Cancel"),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
	)

	writeCatalogFile(t, w, "agents.json", good)
	b.run("fixing the file and choosing Check again turns the page back on",
		clickButton(`document.querySelector('[data-broken]')`, "Check again"),
		waitUntil(`!`+visible(`[data-broken]`)),
		deleteMenu(`[data-bundle-detail]`),
		waitUntil(visible(`[data-delete-dialog]`)),
		setField(`#delete-name`, "ux"),
		waitUntil(`document.querySelector('[data-delete-confirm]').disabled === false`),
		waitUntil(`!`+visible(`[data-delete-blocked]`)),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Cancel"),
	)
	b.run("and an editor can save again",
		chromedp.Click(`[data-bundle-detail] a[data-edit]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('#bundle-name')?.value === 'ux'`),
		setField(`#bundle-name`, "design"),
		waitUntil(save+`.disabled === false`),
		waitUntil(flat(`[data-save-state]`)+`.startsWith('1 unsaved change')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// readCatalogFile is a file of the temporary catalog, as text.
func readCatalogFile(t *testing.T, w *tokenSaverWorld, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.root, "agents", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
