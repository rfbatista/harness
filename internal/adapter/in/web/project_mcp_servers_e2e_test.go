//go:build !windows

package web_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestAProjectsSettingsPickAndSaveItsMcpServers_EndToEnd is W1 for story 19 of
// spec "Web UI — the Agents page", against a real server and a temporary
// catalog: the project's settings list every catalog server (but the built-in
// services one) with its transport, whether its ${VAR} is set and who else uses
// it; two are set to Added and saved with a toast and Undo; a reload shows them
// Added; a server deleted in another
// tab while the page is open is named in a banner; and the New MCP server
// button returns here.
func TestAProjectsSettingsPickAndSaveItsMcpServers_EndToEnd(t *testing.T) {
	t.Setenv("PROJMCP_E2E_SET", "1")
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{"agents": {"designer": {"description": "Designs screens.", "mcp_servers": ["github"]}}}
`)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {
  "github": {"type": "http", "url": "https://example.test/mcp", "headers": {"Authorization": "Bearer ${PROJMCP_E2E_SET}"}},
  "linear": {"command": "npx", "args": ["linear-mcp"], "env": {"KEY": "${PROJMCP_E2E_UNSET}", "PLAIN": "hunter2-plain-secret"}},
  "postgres": {"command": "npx", "args": ["pg-mcp"]},
  "old-search": {"command": "npx", "args": ["search"]}
}}
`)
	settings := "/projects/" + w.project.ID + "/settings"
	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	const names = `[...document.querySelectorAll('[data-mcp-row]')].map(r => r.dataset.name).join()`
	// A server's project MCP state is the pressed button of its three-state control.
	const ticked = `[...document.querySelectorAll('[data-mcp-row]')].filter(r => r.querySelector('[data-mcp-state-choice][aria-pressed="true"]')?.dataset.state === 'added').map(r => r.dataset.name).join()`
	row := func(n string) string { return `[data-mcp-row][data-name="` + n + `"]` }
	tick := func(n string) chromedp.Action {
		return waitUntil(`(() => { const i = document.querySelector('` + row(n) + ` [data-mcp-state-choice][data-state="added"]'); if (!i || i.disabled) return false; i.click(); return true; })()`)
	}
	saved := func() string {
		res, err := w.srv.Client().Get(w.srv.URL + "/api/get_project_mcp_servers?project_id=" + w.project.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var body struct {
			McpServers []string `json:"mcp_servers"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		return strings.Join(body.McpServers, " ")
	}

	b := newTLSBrowser(t)
	other := newTLSBrowser(t)
	b.run("the section lists every catalog server with its transport, variables and who uses it, and no secret",
		chromedp.EmulateViewport(1280, 1100),
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(names+` === 'github,linear,postgres,old-search'`),
		waitUntil(`document.querySelector('[data-mcp-section]').dataset.feed === 'live'`),
		waitUntil(flat(row("github")+` .badge`)+` === 'http'`),
		waitUntil(flat(row("github")+` [data-mcp-var]`)+` === '${PROJMCP_E2E_SET} set'`),
		waitUntil(flat(row("github")+` [data-mcp-who]`)+` === 'Used by 1 agent'`),
		waitUntil(flat(row("linear")+` [data-mcp-var]`)+` === '${PROJMCP_E2E_UNSET} not set'`),
		waitUntil(flat(row("linear")+` [data-mcp-who]`)+` === 'Unused'`),
		waitUntil(`!document.querySelector('`+row("services")+`')`),
		waitUntil(`!document.querySelector('[data-mcp-section]').textContent.includes('hunter2')`),
		waitUntil(`document.querySelector('[data-mcp-manage]').getAttribute('href') === '/agents/mcp'`),
		waitUntil(`document.querySelector('[data-mcp-new]').getAttribute('href') === '/agents/mcp/new?return=`+strings.ReplaceAll(settings, "/", "%2F")+`'`),
		waitUntil(flat(`[data-mcp-count]`)+` === '4 servers: 0 Added, 4 Agent’s choice, 0 Banned'`),
		waitUntil(`!`+visible(`[data-mcp-save]`)+` || document.querySelector('[data-mcp-save]').disabled`),
		waitUntil(`!`+visible(`[data-mcp-cancel]`)),
		waitUntil(`document.querySelectorAll('[data-mcp-no-agent] .chip').length === 0 && `+visible(`[data-mcp-no-agent] span.text-sm`)),
		waitUntil(flat(`[data-mcp-with-agent]`)+`.startsWith('As designer')`),
	)
	b.run("picking two servers turns Save and Cancel on and updates the example",
		tick("linear"),
		tick("postgres"),
		waitUntil(ticked+` === 'linear,postgres'`),
		waitUntil(visible(`[data-mcp-cancel]`)+` && !document.querySelector('[data-mcp-save]').disabled`),
		waitUntil(flat(`[data-mcp-no-agent] .chip`)+` === 'linear'`),
		waitUntil(flat(`[data-mcp-with-agent]`)+`.includes('github agent') && `+flat(`[data-mcp-with-agent]`)+`.includes('linear project')`),
		bothSchemes("project-mcp-unsaved"),
	)
	b.run("Cancel puts the saved list back",
		clickButton(`document.querySelector('[data-mcp-section]')`, "Cancel"),
		waitUntil(ticked+` === ''`),
		tick("linear"),
		tick("postgres"),
	)
	b.run("Save writes the project's list and the toast offers Undo",
		clickButton(`document.querySelector('[data-mcp-section]')`, "Save"),
		waitUntil(flat(`[data-toast]`)+`.startsWith('Saved — harness now gives 2 MCP servers to its sessions.')`),
		waitUntil(visible(`[data-toast] [data-undo]`)),
		waitUntil(`!`+visible(`[data-mcp-cancel]`)),
		bothSchemes("project-mcp-saved"),
	)
	if got := saved(); got != "linear postgres" {
		t.Errorf("saved list = %q, want linear and postgres", got)
	}
	b.run("Undo puts the previous list back",
		chromedp.Click(`[data-toast] [data-undo]`, chromedp.ByQuery),
		waitUntil(flat(`[data-toast]`)+`.startsWith('Undone')`),
		waitUntil(ticked+` === ''`),
	)
	if got := saved(); got != "" {
		t.Errorf("after Undo the list = %q, want empty", got)
	}
	b.run("saved again and reloaded, the two servers are selected",
		tick("linear"),
		tick("postgres"),
		clickButton(`document.querySelector('[data-mcp-section]')`, "Save"),
		waitUntil(visible(`[data-toast]`)),
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(names+` === 'github,linear,postgres,old-search'`),
		waitUntil(ticked+` === 'linear,postgres'`),
		waitUntil(flat(`[data-mcp-count]`)+` === '4 servers: 2 Added, 2 Agent’s choice, 0 Banned'`),
	)
	b.run("an unsaved tick survives a catalog change; deleting a project server in another tab names it in a banner",
		tick("old-search"),
		waitUntil(ticked+` === 'linear,postgres,old-search'`),
	)
	other.run("the other tab opens the catalog", chromedp.Navigate(w.srv.URL+"/agents/mcp"))
	liveCatalogPost(t, other, "the other tab gives old-search to the project, then deletes it", "save_catalog_mcp_server",
		`{previous_name: 'postgres', server: {name: 'postgres', transport: 'stdio', command: 'npx', args: ['pg-mcp'], env: {}, url: '', headers: {}}, agents: [], projects: ['`+w.project.ID+`']}`)
	liveCatalogPost(t, other, "the other tab deletes postgres", "delete_catalog_mcp_server", `{name: 'postgres'}`)
	b.run("the banner names the server taken off the project and the list is up to date",
		waitUntil(visible(`[data-mcp-removed]`)),
		waitUntil(flat(`[data-mcp-removed]`)+`.includes('“postgres” was deleted from the catalog and was taken off this project too.')`),
		waitUntil(names+` === 'github,linear,old-search'`),
		waitUntil(ticked+` === 'linear,old-search'`),
		bothSchemes("project-mcp-removed"),
		clickButton(`document.querySelector('[data-mcp-removed]')`, "Dismiss"),
		waitUntil(`!`+visible(`[data-mcp-removed]`)),
	)
	b.run("New MCP server opens the form, which comes back here when cancelled",
		chromedp.Click(`[data-mcp-new]`, chromedp.ByQuery),
		atPath(w.srv.URL+"/agents/mcp/new?return="+strings.ReplaceAll(settings, "/", "%2F")),
		waitUntil(`!!document.querySelector('[data-mcp-form]')`),
	)
	b.run("the existing sections are still on the page",
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(`['General', 'Ignored paths', 'Agents', 'MCP servers', 'Repositories and their settings'].every(h => [...document.querySelectorAll('h2')].some(e => e.textContent.trim() === h))`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestAProjectsMcpSectionShowsTheEmptyStateAndASkippedSaveWithABrokenCatalog_EndToEnd
// covers the section's two off states: no server in the catalog, and a
// catalog file that does not parse.
func TestAProjectsMcpSectionShowsTheEmptyStateAndTurnsSaveOffWhenTheCatalogIsBroken_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	settings := "/projects/" + w.project.ID + "/settings"
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}

	b := newTLSBrowser(t)
	b.run("with no server of the person's own the section has no row (the built-in services server is not a project setting), says so and links to New MCP server",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(visible(`[data-mcp-empty]`)),
		waitUntil(`document.querySelector('[data-mcp-empty] a').getAttribute('href') === '/agents/mcp/new?return=`+strings.ReplaceAll(settings, "/", "%2F")+`'`),
		waitUntil(`document.querySelector('[data-mcp-empty]').textContent.trim().startsWith('No MCP servers of your own yet.')`),
		waitUntil(`document.querySelectorAll('[data-mcp-row]').length === 0`),
		bothSchemes("project-mcp-empty"),
	)

	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"linear": {"command": "npx"}}}`)
	b.run("a catalog file that parses shows the server",
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(`[...document.querySelectorAll('[data-mcp-row]')].map(r => r.dataset.name).join() === 'linear'`),
		waitUntil(`!`+visible(`[data-mcp-empty]`)),
	)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"linear": {"command": "npx",}}}`)
	b.run("once it stops parsing Save is off and the banner says why",
		chromedp.Navigate(w.srv.URL+settings),
		waitUntil(visible(`[data-mcp-broken]`)),
		waitUntil(`document.querySelector('[data-mcp-broken]').textContent.includes('mcp-servers.json does not parse')`),
		waitUntil(`document.querySelector('[data-mcp-save]').disabled`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
