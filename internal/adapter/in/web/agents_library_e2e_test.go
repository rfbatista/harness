//go:build !windows

package web_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestAgentsTabListsFiltersAndShowsEachAgentInTheBrowser_EndToEnd is W1 for
// stories 3 and 4 of spec "Web UI — the Agents page", against a real server
// and a temporary catalog: the list narrows by text and by quick filters, a
// broken agent shows its badge, and an agent's detail shows its bundles as
// collapsible groups, the resolved total, MCP servers, tools and the Change
// links.
func TestAgentsTabListsFiltersAndShowsEachAgentInTheBrowser_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	catalogDir := filepath.Join(w.root, "agents")
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(catalogDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("agents.json", `{
  "bundles": {"ux": ["design", "@core"], "core": ["tdd", "design"]},
  "agents": {
    "ux-designer": {"description": "Designs screens.", "aliases": ["ux"], "skills": ["@ux", "tdd", "ghost"], "mcp_servers": ["notebooklm"], "allowed_tools": ["Read"]},
    "plain": {"skills": ["tdd"]},
    "solo": {"description": "Works alone.", "skills": ["tdd"]}
  }
}`)
	write("skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nbody\n")
	write("skills/design/SKILL.md", "---\nname: design\ndescription: Design things.\n---\nbody\n")
	write("mcp-servers.json", `{"mcpServers":{"notebooklm":{"command":"npx","args":["-y","nb"],"env":{"TOKEN":"${HARNESS_TEST_NB_TOKEN_UNSET}"}}}}`)

	const rows = `[...document.querySelectorAll('[data-agent-row]')].map(r => r.dataset.key).join(',')`
	const pressed = `[...document.querySelectorAll('[data-agent-filters] button[aria-pressed=true]')].map(b => b.textContent.trim()).join(',')`
	rowText := func(key string) string {
		return `document.querySelector('[data-agent-row][data-key="` + key + `"]').textContent.replace(/\s+/g, ' ').trim()`
	}
	const detail = `document.querySelector('[data-agent-detail]')`

	b := newTLSBrowser(t)
	b.run("the list names every agent, each broken one with its badge, and the footer names the source",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents"),
		waitUntil(rows+` === 'ux-designer,plain,solo'`),
		waitUntil(`document.querySelector('[data-agent-foot]').textContent.trim() === 'agents.json · 3 agents, 2 bundles'`),
		waitUntil(rowText("plain")+`.includes('No description')`),
		waitUntil(rowText("ux-designer")+`.includes('1 missing skill')`),
		waitUntil(rowText("solo")+`.includes('1 skill')`),
		waitUntil(`document.querySelector('[data-agent-list]').getAttribute('aria-label') === 'Agents'`),
	)
	b.run("the text filter narrows by name, alias and skill; the quick filters narrow too",
		chromedp.Focus(`input[type=search][aria-label="Filter agents"]`, chromedp.ByQuery),
		setField(`input[type=search][aria-label="Filter agents"]`, "ux"),
		waitUntil(rows+` === 'ux-designer'`),
		setField(`input[type=search][aria-label="Filter agents"]`, "nothing-here"),
		waitUntil(`document.querySelector('[data-agent-empty]')?.checkVisibility() === true`),
		setField(`input[type=search][aria-label="Filter agents"]`, ""),
		clickButton(`document.querySelector('[data-agent-filters]')`, "Has MCP"),
		waitUntil(rows+` === 'ux-designer' && `+pressed+` === 'Has MCP'`),
		clickButton(`document.querySelector('[data-agent-filters]')`, "Has problems"),
		waitUntil(rows+` === 'ux-designer,plain'`),
		clickButton(`document.querySelector('[data-agent-filters]')`, "All"),
		waitUntil(rows+` === 'ux-designer,plain,solo'`),
	)
	b.run("selecting an agent opens its detail at its deep link",
		chromedp.Click(`[data-agent-row][data-key="ux-designer"]`, chromedp.ByQuery),
		atPath(w.srv.URL+"/agents/ux-designer"),
		waitUntil(detail+`?.checkVisibility() === true`),
		waitUntil(`document.querySelector('[data-agent-row][data-key="ux-designer"]').getAttribute('aria-current') === 'true'`),
		waitUntil(detail+`.querySelector('h2').textContent.trim() === 'ux-designer'`),
		waitUntil(detail+`.textContent.includes('alias: ux')`),
		waitUntil(detail+`.querySelector('[data-path]').textContent.trim() === 'agents.json › agents.ux-designer'`),
	)
	b.run("its bundles are collapsible groups with their size, beside the single skills and the resolved total",
		waitUntil(detail+`.querySelector('[data-skills-heading]').textContent.trim() === 'Skills · 3 after bundles'`),
		waitUntil(`(() => { const g = `+detail+`.querySelector('[data-bundle-group]'); const t = g.querySelector('button[aria-expanded]');
			return t.textContent.replace(/\s+/g, ' ').includes('@ux') && t.textContent.includes('2 skills'); })()`),
		// One bundle: it starts expanded, listing its entries (the nested bundle with its size).
		waitUntil(`(() => { const g = `+detail+`.querySelector('[data-bundle-group]'); return g.querySelector('button[aria-expanded]').getAttribute('aria-expanded') === 'true'
			&& g.querySelector('[data-bundle-entries]').checkVisibility() && [...g.querySelectorAll('[data-bundle-entries] a')].map(a => a.textContent.trim()).join(' ') === 'design @core (2)'; })()`),
		chromedp.Click(`[data-agent-detail] [data-bundle-group] button[aria-expanded]`, chromedp.ByQuery),
		waitUntil(`(() => { const g = `+detail+`.querySelector('[data-bundle-group]'); return g.querySelector('button[aria-expanded]').getAttribute('aria-expanded') === 'false'
			&& !g.querySelector('[data-bundle-entries]').checkVisibility(); })()`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-single-skill]')].map(s => s.textContent.trim() + (s.hasAttribute('data-missing') ? '!' : '')).join(',') === 'tdd,ghost!'`),
	)
	b.run("it lists its MCP servers with whether each secret is set, its tools, and a Change link per section",
		waitUntil(detail+`.querySelector('[data-mcp-heading]').textContent.trim() === 'MCP servers · 1'`),
		waitUntil(`(() => { const r = `+detail+`.querySelector('[data-mcp-server]').textContent.replace(/\s+/g, ' ');
			return r.includes('notebooklm') && r.includes('stdio') && r.includes('${HARNESS_TEST_NB_TOKEN_UNSET}') && r.includes('not set'); })()`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-tool]')].map(c => c.textContent.trim()).join(',') === 'Read'`),
		waitUntil(`[...`+detail+`.querySelectorAll('a[data-change]')].map(a => a.dataset.change + '=' + a.getAttribute('href')).join(' ')
			=== 'skills=/agents/ux-designer/edit#skills mcp=/agents/ux-designer/edit#mcp tools=/agents/ux-designer/edit#tools projects=/agents/ux-designer/edit#projects'`),
		waitUntil(detail+`.querySelector('a[data-edit]').getAttribute('href') === '/agents/ux-designer/edit'`),
		waitUntil(detail+`.querySelector('a[data-duplicate]').getAttribute('href') === '/agents/new?from=ux-designer'`),
		bothSchemes("agents-library-detail"),
	)
	b.run("an agent with no description shows the badge and a banner into the editor's Description field; no tools says the default",
		chromedp.Navigate(w.srv.URL+"/agents/plain"),
		waitUntil(detail+`?.checkVisibility() === true`),
		waitUntil(detail+`.querySelector('h2').textContent.trim() === 'plain'`),
		waitUntil(detail+`.querySelector('.badge')?.textContent.trim() === 'No description'`),
		waitUntil(detail+`.querySelector('[data-no-description] a').getAttribute('href') === '/agents/plain/edit#identity'`),
		waitUntil(detail+`.querySelector('[data-tools-default]').textContent.includes('default tools')`),
		waitUntil(detail+`.querySelector('[data-no-servers]').checkVisibility()`),
		bothSchemes("agents-library-nodesc"),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
