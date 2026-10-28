//go:build !windows

package web_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// writeCatalogFile puts a file in the temporary catalog the server reads.
func writeCatalogFile(t *testing.T, w *tokenSaverWorld, rel, text string) {
	t.Helper()
	p := filepath.Join(w.root, "agents", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSkillsTabFiltersAndShowsASkillItsBodyFilesAndReach_EndToEnd is W1 for
// story 5 of spec "Web UI — the Agents page", against a real server and a
// temporary catalog: a skill, a bundle holding it and an agent naming it show
// the rendered body (embedded HTML never runs), the file tree and Reaches, and
// the quick filters narrow the list.
func TestSkillsTabFiltersAndShowsASkillItsBodyFilesAndReach_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"core": ["tdd"]},
  "agents": {"dev": {"description": "Builds.", "skills": ["tdd"]}}
}`)
	writeCatalogFile(t, w, "skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\n# Test-Driven Development\n\nWrite the **test** first.\n\n- red\n- green\n\n<script>window.__skillXSS = 1</script>\n\n<img src=x onerror=\"window.__skillXSS = 2\">\n")
	writeCatalogFile(t, w, "skills/tdd/mocking.md", "mocks\n")
	writeCatalogFile(t, w, "skills/tdd/refs/examples.md", "examples\n")
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")
	writeCatalogFile(t, w, "skills/solo/SKILL.md", "---\nname: solo\ndescription: Alone.\n---\nbody\n")

	const rows = `[...document.querySelectorAll('[data-skill-row]')].map(r => r.dataset.key).join(',')`
	const detail = `document.querySelector('[data-skill-detail]')`
	const filterBox = `input[type=search][aria-label="Filter skills"]`
	const filters = `document.querySelector('[data-skill-filters]')`

	b := newTLSBrowser(t)
	b.run("the Skills tab lists every skill and names its folders in the footer",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/skills"),
		waitUntil(rows+` === 'lint,solo,tdd'`),
		waitUntil(`document.querySelector('[data-skill-foot]').textContent.trim() === 'skills/ · 3 folders'`),
		waitUntil(`document.querySelector('[data-skill-list]').getAttribute('aria-label') === 'Skills'`),
		waitUntil(`document.querySelector('[data-skill-row][data-key="tdd"]').textContent.replace(/\s+/g, ' ').includes('reaches 1 agent')`),
		waitUntil(`document.querySelector('[data-skill-row][data-key="solo"]').textContent.replace(/\s+/g, ' ').includes('unused')`),
	)
	b.run("the text filter and the quick filters narrow the list",
		setField(filterBox, "lint"),
		waitUntil(rows+` === 'lint'`),
		setField(filterBox, "nothing-here"),
		waitUntil(`document.querySelector('[data-skill-empty]')?.checkVisibility() === true`),
		setField(filterBox, ""),
		clickButton(filters, "Unused"),
		waitUntil(rows+` === 'lint,solo' && document.querySelector('[data-skill-filters] button[aria-pressed=true]').textContent.trim() === 'Unused'`),
		clickButton(filters, "In a bundle"),
		waitUntil(rows+` === 'tdd'`),
		clickButton(filters, "Has extra files"),
		waitUntil(rows+` === 'tdd'`),
		clickButton(filters, "All"),
		waitUntil(rows+` === 'lint,solo,tdd'`),
	)
	b.run("selecting a skill opens its detail at its deep link, with the rendered SKILL.md",
		chromedp.Click(`[data-skill-row][data-key="tdd"]`, chromedp.ByQuery),
		atPath(w.srv.URL+"/agents/skills/tdd"),
		waitUntil(`document.querySelector('[data-skill-row][data-key="tdd"]').getAttribute('aria-current') === 'true'`),
		waitUntil(detail+`?.querySelector('h2').textContent.trim() === 'tdd'`),
		waitUntil(detail+`.querySelector('[data-path]').textContent.trim() === 'skills/tdd/SKILL.md'`),
		waitUntil(detail+`.querySelector('[data-skill-body] h1')?.textContent === 'Test-Driven Development'`),
		waitUntil(detail+`.querySelector('[data-skill-body] strong')?.textContent === 'test'`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-skill-body] li')].map(l => l.textContent).join(',') === 'red,green'`),
		waitUntil(detail+`.querySelector('[data-skill-body]').textContent.includes('window.__skillXSS = 1')`),
	)
	b.run("embedded HTML in the body is shown as text and never runs",
		waitUntil(`window.__skillXSS === undefined && `+detail+`.querySelectorAll('[data-skill-body] script, [data-skill-body] img').length === 0`),
	)
	b.run("its file tree lists the extra files, folders first",
		waitUntil(`[...`+detail+`.querySelectorAll('[data-file]')].map(f => f.dataset.depth + ':' + f.textContent.trim().replace(/\s+/g, ' ')).join('|').replace(/ \d+ B/g, '') === '0:SKILL.md|0:refs|1:examples.md|0:mocking.md'`),
	)
	b.run("Reaches names the agents directly, the bundles and the total",
		waitUntil(`[...`+detail+`.querySelectorAll('[data-reach="agents"] a')].map(a => a.textContent.trim() + '=' + a.getAttribute('href')).join() === 'dev=/agents/dev'`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-reach="bundles"] a')].map(a => a.textContent.trim() + '=' + a.getAttribute('href')).join() === '@core=/agents/bundles/core'`),
		waitUntil(detail+`.querySelector('[data-reach="total"]').textContent.trim() === '1 agent in total'`),
	)
	b.run("its actions link to the form and the copy, and Delete is on",
		waitUntil(detail+`.querySelector('a[data-edit]').getAttribute('href') === '/agents/skills/tdd/edit'`),
		waitUntil(detail+`.querySelector('a[data-duplicate]').getAttribute('href') === '/agents/skills/new?from=tdd'`),
		waitUntil(detail+`.querySelector('[data-action=delete]').getAttribute('aria-disabled') === null`),
		bothSchemes("agents-skill-detail"),
	)
	b.run("a deep link to a skill loads it; a name the catalog lacks says so",
		chromedp.Navigate(w.srv.URL+"/agents/skills/lint"),
		waitUntil(detail+`?.querySelector('[data-skill-body]')?.textContent.trim() === 'body'`),
		waitUntil(detail+`.querySelector('[data-reach="total"]').textContent.trim() === '0 agents in total'`),
		chromedp.Navigate(w.srv.URL+"/agents/skills/ghost"),
		waitUntil(`document.querySelector('[data-skill-unknown]')?.checkVisibility() === true`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestSkillsTabStaysFastWith475Skills_EndToEnd is W1 for the spec's
// constraint that the catalog renders quickly with 475 skills: the first
// paint mounts a page of rows, not all of them, the rest come on request, and
// the text filter still reaches every skill.
func TestSkillsTabStaysFastWith475Skills_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{"bundles": {}, "agents": {}}`)
	for i := 0; i < 475; i++ {
		writeCatalogFile(t, w, fmt.Sprintf("skills/skill-%03d/SKILL.md", i), fmt.Sprintf("---\nname: skill-%03d\ndescription: Number %d.\n---\nbody\n", i, i))
	}
	const mounted = `document.querySelectorAll('[data-skill-row]').length`

	b := newTLSBrowser(t)
	b.run("the first paint mounts one page of rows and says how many wait",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/skills"),
		waitUntil(mounted+` > 0`),
		waitUntil(`document.querySelector('[data-skill-foot]').textContent.trim() === 'skills/ · 475 folders'`),
		waitUntil(`document.querySelectorAll('[data-skill-row]').length <= 100`),
		waitUntil(`document.querySelector('[data-skill-more]').textContent.replace(/\s+/g, ' ').includes('415 more')`),
		waitUntil(`document.querySelector('[role=tab][data-tab=skills] [data-count]').textContent.trim() === '475'`),
	)
	start := time.Now()
	b.run("the first paint lands within its bound",
		chromedp.Navigate(w.srv.URL+"/agents/skills"),
		waitUntil(mounted+` > 0 && `+mounted+` <= 100`),
	)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("475 skills took %v to first paint, want under 3s", took)
	}
	for _, want := range []int{120, 180, 240, 300, 360, 420, 475} {
		b.run(fmt.Sprintf("Show more mounts the next page (%d rows)", want),
			chromedp.Click(`[data-skill-more] button`, chromedp.ByQuery),
			waitUntil(fmt.Sprintf(`%s === %d`, mounted, want)),
		)
	}
	b.run("with every skill mounted, there is nothing more to show",
		waitUntil(`document.querySelector('[data-skill-more]')?.checkVisibility() === false`),
	)
	b.run("the text filter reaches skills that were not mounted",
		setField(`input[type=search][aria-label="Filter skills"]`, "skill-474"),
		waitUntil(mounted+` === 1 && document.querySelector('[data-skill-row]').dataset.key === 'skill-474'`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestMcpServersTabShowsEachServerAndChecksItsVariablesAgain_EndToEnd is W1
// for stories 7 and 8, against a real server and a temporary catalog: each
// server's detail shows transport, target and references with set or not set
// and never a value, Check again re-reads the variables, Who gets it lists
// agents and projects or says Unused, and the Tools tab lists each built-in
// tool with the agents that allow it.
func TestMcpServersTabShowsEachServerAndChecksItsVariablesAgain_EndToEnd(t *testing.T) {
	const secret = "value-that-must-never-render"
	t.Setenv("HARNESS_TEST_MCP_SET", secret)
	t.Setenv("HARNESS_TEST_MCP_LATER", "")
	os.Unsetenv("HARNESS_TEST_MCP_LATER")

	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {},
  "agents": {"dev": {"description": "Builds.", "skills": [], "mcp_servers": ["nb"], "allowed_tools": ["Read"]}}
}`)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {
  "nb": {"command": "npx", "args": ["-y", "nb-mcp"], "env": {"SET_TOKEN": "${HARNESS_TEST_MCP_SET}", "LATER_TOKEN": "${HARNESS_TEST_MCP_LATER}", "LEVEL": "hunter2-plain"}},
  "linear": {"type": "http", "url": "https://linear.example/mcp", "headers": {"Authorization": "Bearer ${HARNESS_TEST_MCP_SET}"}},
  "spare": {"command": "uvx", "args": ["spare-mcp"]}
}}`)
	body, _ := json.Marshal(map[string]any{"project_id": w.project.ID, "mcp_servers": []string{"linear"}})
	resp, err := w.srv.Client().Post(w.srv.URL+"/api/set_project_mcp_servers", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("project servers: %v %v", err, resp)
	}
	resp.Body.Close()

	const rows = `[...document.querySelectorAll('[data-mcp-row]')].map(r => r.dataset.key).join(',')`
	const detail = `document.querySelector('[data-mcp-detail]')`
	const filters = `document.querySelector('[data-mcp-filters]')`
	const text = `document.body.innerText`
	rowText := func(key string) string {
		return `document.querySelector('[data-mcp-row][data-key="` + key + `"]').textContent.replace(/\s+/g, ' ').trim()`
	}
	refs := `[...` + detail + `.querySelectorAll('[data-ref]')].map(r => r.dataset.ref + ' ' + r.dataset.state).join(', ')`

	b := newTLSBrowser(t)
	b.run("the list names every server, the one with an unset secret wears its badge, the footer names the file",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/mcp"),
		waitUntil(rows+` === 'nb,linear,spare'`),
		waitUntil(`document.querySelector('[data-mcp-foot]').textContent.trim() === 'mcp-servers.json · 3 servers'`),
		waitUntil(rowText("nb")+`.includes('secret not set')`),
		waitUntil(rowText("spare")+`.includes('stdio · Unused')`),
		waitUntil(rowText("linear")+`.includes('https://linear.example/mcp')`),
	)
	b.run("the quick filters narrow the list",
		clickButton(filters, "stdio"),
		waitUntil(rows+` === 'nb,spare'`),
		clickButton(filters, "http"),
		waitUntil(rows+` === 'linear'`),
		clickButton(filters, "Needs a secret"),
		waitUntil(rows+` === 'nb,linear'`),
		clickButton(filters, "Unused"),
		waitUntil(rows+` === 'spare'`),
		clickButton(filters, "All"),
		waitUntil(rows+` === 'nb,linear,spare'`),
	)
	b.run("a stdio server shows its command, its references with set or not set, and a banner to check again",
		chromedp.Click(`[data-mcp-row][data-key="nb"]`, chromedp.ByQuery),
		atPath(w.srv.URL+"/agents/mcp/nb"),
		waitUntil(detail+`?.querySelector('h2').textContent.trim() === 'nb'`),
		waitUntil(detail+`.querySelector('[data-transport]').textContent.trim() === 'stdio'`),
		waitUntil(detail+`.querySelector('[data-path]').textContent.trim() === 'mcp-servers.json › mcpServers.nb'`),
		waitUntil(detail+`.querySelector('[data-target]').textContent.trim() === 'npx -y nb-mcp'`),
		waitUntil(refs+` === '${HARNESS_TEST_MCP_LATER} not set, ${HARNESS_TEST_MCP_SET} set'`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-entry]')].map(e => e.dataset.key + ' ' + e.textContent.replace(/\s+/g, ' ').trim()).join(' | ').includes('LEVEL LEVEL: plain text, hidden')`),
		waitUntil(detail+`.querySelector('[data-unset-banner]').checkVisibility() && `+detail+`.querySelector('[data-unset-banner]').textContent.includes('${HARNESS_TEST_MCP_LATER}')`),
	)
	b.run("no value of a variable or a plain entry is ever on the page",
		waitUntil(`!`+text+`.includes(`+quote(secret)+`) && !`+text+`.includes('hunter2-plain') && !document.documentElement.innerHTML.includes(`+quote(secret)+`)`),
	)
	t.Setenv("HARNESS_TEST_MCP_LATER", "now-set")
	b.run("Check again re-reads the variable state",
		clickButton(detail+`.querySelector('[data-unset-banner]')`, "Check again"),
		waitUntil(refs+` === '${HARNESS_TEST_MCP_LATER} set, ${HARNESS_TEST_MCP_SET} set'`),
		waitUntil(`!`+detail+`.querySelector('[data-unset-banner]')?.checkVisibility()`),
		waitUntil(`!`+text+`.includes('now-set')`),
	)
	b.run("Who gets it lists the agents; Edit, Duplicate and Copy path are there, Delete is on",
		waitUntil(`[...`+detail+`.querySelectorAll('[data-who="agents"] a')].map(a => a.textContent.trim() + '=' + a.getAttribute('href')).join() === 'dev=/agents/dev'`),
		waitUntil(detail+`.querySelector('a[data-edit]').getAttribute('href') === '/agents/mcp/nb/edit'`),
		waitUntil(detail+`.querySelector('a[data-duplicate]').getAttribute('href') === '/agents/mcp/new?from=nb'`),
		waitUntil(detail+`.querySelector('[data-action=delete]').getAttribute('aria-disabled') === null`),
		bothSchemes("agents-mcp-detail"),
	)
	b.run("an http server shows its URL and its headers with their references; its project links to the project's settings",
		chromedp.Click(`[data-mcp-row][data-key="linear"]`, chromedp.ByQuery),
		atPath(w.srv.URL+"/agents/mcp/linear"),
		waitUntil(detail+`?.querySelector('[data-transport]').textContent.trim() === 'http'`),
		waitUntil(detail+`.querySelector('[data-target]').textContent.trim() === 'https://linear.example/mcp'`),
		waitUntil(detail+`.querySelector('[data-entries-label]').textContent.trim() === 'Headers'`),
		waitUntil(detail+`.querySelector('[data-entry][data-key="Authorization"]').textContent.replace(/\s+/g, ' ').includes('Bearer ${HARNESS_TEST_MCP_SET}')`),
		waitUntil(refs+` === '${HARNESS_TEST_MCP_SET} set'`),
		waitUntil(`!`+detail+`.querySelector('[data-unset-banner]')?.checkVisibility()`),
		waitUntil(`[...`+detail+`.querySelectorAll('[data-who="projects"] a')].map(a => a.textContent.trim() + '=' + a.getAttribute('href')).join() === 'harness=/projects/`+w.project.ID+`/settings'`),
	)
	b.run("a server nobody gets says Unused and that this is allowed",
		chromedp.Navigate(w.srv.URL+"/agents/mcp/spare"),
		waitUntil(detail+`?.querySelector('[data-unused-badge]')?.checkVisibility() === true`),
		waitUntil(detail+`.querySelector('[data-unused-note]').textContent.includes('allowed')`),
		waitUntil(detail+`.querySelector('[data-who="agents"]').checkVisibility() === false`),
	)
	b.run("the Tools tab lists each built-in tool with the agents that allow it, read-only",
		chromedp.Navigate(w.srv.URL+"/agents/tools"),
		waitUntil(`document.querySelectorAll('[data-tool-row]').length > 5`),
		waitUntil(`document.querySelector('[data-tool-row][data-key="Read"]').textContent.replace(/\s+/g, ' ').includes('1 agent')`),
		waitUntil(`document.querySelector('[data-tool-row][data-key="Bash"]').textContent.replace(/\s+/g, ' ').includes('no agent')`),
		chromedp.Click(`[data-tool-row][data-key="Read"]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-tool-detail] h2').textContent.trim() === 'Read'`),
		waitUntil(`[...document.querySelectorAll('[data-tool-detail] [data-allowed-by] a')].map(a => a.textContent.trim() + '=' + a.getAttribute('href')).join() === 'dev=/agents/dev'`),
		waitUntil(`document.querySelector('[data-tool-detail] [data-tools-explain]').textContent.includes('lists none')`),
		waitUntil(`document.querySelector('[data-tool-detail] a[data-edit], [data-tool-detail] [role=menu]') === null`),
		clickButton(`document.querySelector('[data-tool-filters]')`, "Unused"),
		waitUntil(`!document.querySelector('[data-tool-row][data-key="Read"]') && !!document.querySelector('[data-tool-row][data-key="Bash"]')`),
		bothSchemes("agents-tools"),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
