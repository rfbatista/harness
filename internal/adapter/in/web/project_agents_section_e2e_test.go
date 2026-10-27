//go:build !windows

package web_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

const projAgentsCatalog = `{"agents": {
  "designer": {"description": "Designs screens."},
  "reviewer": {},
  "backend": {"description": "APIs and services."}
}}
`

// projAgentsSaved reads what the server has saved for the world's project, as
// "mode: a b" with the list in catalog order.
func projAgentsSaved(t *testing.T, w *tokenSaverWorld) string {
	t.Helper()
	res, err := w.srv.Client().Get(w.srv.URL + "/api/get_project_agents?project_id=" + w.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return strings.Join(strings.Fields(strings.NewReplacer(`"`, "", "{", " ", "}", " ", "[", " ", "]", " ", ",", " ").Replace(string(raw))), " ")
}

// TestAProjectsSettingsSwitchBetweenAllAndSelectedAgents_EndToEnd is W1 for
// story 1 of the addendum to spec "Web UI — the Agents page", against a real
// server and a temporary catalog: a project starts with All agents; switching
// to Selected agents ticks every agent, one is unticked and saved; a reload
// keeps the selection; switching back to All agents hides the checklist; an
// empty list warns and Tick all fills it; and saving it empty is accepted.
func TestAProjectsSettingsSwitchBetweenAllAndSelectedAgents_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", projAgentsCatalog)
	settings := "/projects/" + w.project.ID + "/settings"
	flat := func(sel string) string { return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()` }
	visible := func(sel string) string { return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true` }
	const ticked = `[...document.querySelectorAll('[data-agent-row] input:checked')].map(i => i.closest('[data-agent-row]').dataset.name).join()`
	const pressed = `document.querySelector('[data-agents-mode] [aria-pressed=true]')?.textContent.trim()`
	row := func(n string) string { return `[data-agent-row][data-name="` + n + `"]` }
	tick := func(n string) chromedp.Action {
		return waitUntil(`(() => { const i = document.querySelector('` + row(n) + ` input'); if (!i || i.disabled) return false; i.click(); return true; })()`)
	}
	modeButton := func(label string) chromedp.Action {
		return waitUntil(`(() => { const b = [...document.querySelectorAll('[data-agents-mode] button')].find(b => b.textContent.trim() === ` + quote(label) + `); if (!b || b.disabled) return false; b.click(); return true; })()`)
	}
	root := `document.querySelector('[data-mcp-section]')`

	b := newTLSBrowser(t)
	b.run("a project starts with All agents: the list is hidden and everything is enabled",
		chromedp.EmulateViewport(1280, 1100),
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(`document.querySelector('[data-mcp-section]').dataset.feed === 'live'`),
		waitUntil(pressed+` === 'All agents'`),
		waitUntil(flat(`[data-agents-count]`)+` === 'All 3 enabled'`),
		waitUntil(flat(`[data-agents-hint]`)+`.includes('including agents created later')`),
		waitUntil(`!`+visible(`[data-agents-list]`)),
		waitUntil(`document.querySelector('[data-mcp-save]').disabled`),
		waitUntil(`[...document.querySelectorAll('h2')].some(e => e.textContent.trim() === 'Enabled agents')`),
		bothSchemes("project-agents-all"),
	)
	b.run("Selected agents starts with every agent ticked, with its description or No description",
		modeButton("Selected agents"),
		waitUntil(pressed+` === 'Selected agents'`),
		waitUntil(ticked+` === 'designer,reviewer,backend'`),
		waitUntil(flat(row("designer")+` [data-agent-description]`)+` === 'Designs screens.'`),
		waitUntil(flat(row("reviewer")+` [data-agent-description]`)+` === 'No description'`),
		waitUntil(flat(`[data-agents-count]`)+` === '3 of 3 enabled'`),
		waitUntil(`!document.querySelector('[data-mcp-save]').disabled && `+visible(`[data-mcp-cancel]`)),
		waitUntil(`document.querySelector('[data-agents-list]').tagName === 'FIELDSET'`),
	)
	b.run("unticking one agent and saving writes the Selected list with a toast",
		tick("reviewer"),
		waitUntil(flat(`[data-agents-count]`)+` === '2 of 3 enabled'`),
		bothSchemes("project-agents-unsaved"),
		clickButton(root, "Save"),
		waitUntil(flat(`[data-toast]`)+`.startsWith('Saved — harness now enables 2 agents.')`),
		waitUntil(`!`+visible(`[data-mcp-cancel]`)),
		bothSchemes("project-agents-saved"),
	)
	if got := projAgentsSaved(t, w); got != "mode:selected agents: designer backend" {
		t.Errorf("saved = %q, want selected designer and backend", got)
	}
	b.run("after a reload the selection is kept",
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(pressed+` === 'Selected agents'`),
		waitUntil(ticked+` === 'designer,backend'`),
		waitUntil(flat(`[data-agents-count]`)+` === '2 of 3 enabled'`),
	)
	b.run("switching back to All agents hides the checklist and saves as All",
		modeButton("All agents"),
		waitUntil(`!`+visible(`[data-agents-list]`)),
		waitUntil(`!document.querySelector('[data-mcp-save]').disabled`),
		clickButton(root, "Save"),
		waitUntil(flat(`[data-toast]`)+`.startsWith('Saved — harness now enables all agents.')`),
	)
	if got := projAgentsSaved(t, w); got != "mode:all agents:" {
		t.Errorf("saved = %q, want all", got)
	}
	b.run("Undo puts the Selected list back",
		chromedp.Click(`[data-toast] [data-undo]`, chromedp.ByQuery),
		waitUntil(flat(`[data-toast]`)+`.startsWith('Undone')`),
	)
	if got := projAgentsSaved(t, w); got != "mode:selected agents: designer backend" {
		t.Errorf("after Undo saved = %q, want selected designer and backend", got)
	}
	b.run("an empty Selected list warns, and Tick all fills it",
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(ticked+` === 'designer,backend'`),
		tick("designer"),
		tick("backend"),
		waitUntil(visible(`[data-agents-empty]`)),
		waitUntil(flat(`[data-agents-empty]`)+`.includes('No agent can be picked in this project')`),
		bothSchemes("project-agents-empty"),
		clickButton(`document.querySelector('[data-agents-empty]')`, "Tick all 3"),
		waitUntil(ticked+` === 'designer,reviewer,backend' && !`+visible(`[data-agents-empty]`)),
		tick("designer"),
		tick("reviewer"),
		tick("backend"),
		waitUntil(visible(`[data-agents-empty]`)),
	)
	b.run("saving the empty list is accepted",
		clickButton(root, "Save"),
		waitUntil(flat(`[data-toast]`)+`.startsWith('Saved — harness now enables 0 agents.')`),
		waitUntil(`!document.querySelector('[data-agents-refusal]')`),
	)
	if got := projAgentsSaved(t, w); got != "mode:selected agents:" {
		t.Errorf("saved = %q, want selected with no agent", got)
	}
	b.run("the existing sections are still on the page",
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(`['General', 'Ignored paths', 'Agents', 'Enabled agents', 'MCP servers', 'Repositories and their settings'].every(h => [...document.querySelectorAll('h2')].some(e => e.textContent.trim() === h))`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestAnAgentTakenOutOfTheCatalogIsNamedAtTheChecklistWhenSaving_EndToEnd covers
// the UNKNOWN_REFERENCE refusal: the catalog file loses an agent behind the
// open page's back, so Save names it at the checklist, linked to the list with
// aria-describedby, and only the part that did not save stays unsaved.
func TestAnAgentTakenOutOfTheCatalogIsNamedAtTheChecklistWhenSaving_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", projAgentsCatalog)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"linear": {"command": "npx"}}}`)
	settings := "/projects/" + w.project.ID + "/settings"
	visible := func(sel string) string { return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true` }
	root := `document.querySelector('[data-mcp-section]')`

	b := newTLSBrowser(t)
	b.run("the page lists three agents",
		chromedp.EmulateViewport(1280, 1100),
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(`document.querySelector('[data-mcp-section]').dataset.feed === 'live'`),
		waitUntil(`(() => { const b = [...document.querySelectorAll('[data-agents-mode] button')].find(b => b.textContent.trim() === 'Selected agents'); if (!b) return false; b.click(); return true; })()`),
		waitUntil(`document.querySelectorAll('[data-agent-row]').length === 3`),
	)
	writeCatalogFile(t, w, "agents.json", `{"agents": {"designer": {"description": "Designs screens."}, "backend": {"description": "APIs and services."}}}`)
	b.run("saving a list that names the removed agent is refused and names it at the checklist",
		waitUntil(`(() => { const i = document.querySelector('[data-mcp-row] input'); if (!i) return false; i.click(); return true; })()`),
		clickButton(root, "Save"),
		waitUntil(visible(`[data-agents-refusal]`)),
		waitUntil(`document.querySelector('[data-agents-refusal]').textContent.includes('reviewer')`),
		waitUntil(`document.querySelector('[data-agents-list]')?.getAttribute('aria-describedby')?.split(' ').includes('settings-agents-refusal') === true`),
		waitUntil(fmt.Sprintf(`!!document.getElementById('settings-agents-refusal')`)),
		bothSchemes("project-agents-unknown"),
	)
	b.run("the page read again, so the agent is gone and the unsaved MCP part is still unsaved",
		waitUntil(`document.querySelectorAll('[data-agent-row]').length === 2`),
		waitUntil(`!document.querySelector('[data-mcp-save]').disabled`),
	)
	if got := projAgentsSaved(t, w); got != "mode:all agents:" {
		t.Errorf("nothing saved for agents: %q", got)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestAProjectWithManyAgentsShowsASearchComboboxWithChips_EndToEnd checks the
// threshold of about fifteen agents.
func TestAProjectWithManyAgentsShowsASearchComboboxWithChips_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	var agents []string
	for i := 0; i < 16; i++ {
		agents = append(agents, fmt.Sprintf(`"agent-%02d": {"description": "Agent %d."}`, i, i))
	}
	writeCatalogFile(t, w, "agents.json", `{"agents": {`+strings.Join(agents, ",")+`}}`)
	settings := "/projects/" + w.project.ID + "/settings"
	visible := func(sel string) string { return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true` }

	b := newTLSBrowser(t)
	b.run("past fifteen agents Selected shows a combobox, not a checklist",
		chromedp.EmulateViewport(1280, 1100),
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(`document.querySelector('[data-mcp-section]').dataset.feed === 'live'`),
		waitUntil(`(() => { const b = [...document.querySelectorAll('[data-agents-mode] button')].find(b => b.textContent.trim() === 'Selected agents'); if (!b) return false; b.click(); return true; })()`),
		waitUntil(visible(`[data-agents-combobox]`)),
		waitUntil(`!`+visible(`[data-agents-list]`)),
		waitUntil(`document.querySelectorAll('[data-agents-combobox] [data-chip]').length === 16`),
		chromedp.Click(`[data-agents-combobox] [data-chip="agent-03"] button`, chromedp.ByQuery),
		waitUntil(`document.querySelectorAll('[data-agents-combobox] [data-chip]').length === 15`),
		chromedp.Focus(`#settings-agents-input`, chromedp.ByQuery),
		waitUntil(`document.querySelector('#settings-agents-input').getAttribute('aria-expanded') === 'true'`),
		waitUntil(`document.querySelectorAll('#settings-agents-options [role=option]').length === 1`),
		chromedp.KeyEvent("\r"),
		waitUntil(`document.querySelectorAll('[data-agents-combobox] [data-chip]').length === 16`),
		bothSchemes("project-agents-combobox"),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
