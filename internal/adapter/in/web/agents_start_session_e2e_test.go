//go:build !windows

package web_test

import (
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestAnAgentDetailShowsTargetsStartsASessionAndCountsUsage_EndToEnd is W1 for
// ticket 17 of the Agents page, against a real server and a temporary catalog:
// the MCP table shows each server's transport and Target (the URL of a remote
// server, the command line of a local one, ${VAR} as written, no plain env or
// header value anywhere in the page); Start a session as <agent> lists exactly
// the projects that enable the agent, is keyboard operable, and choosing a
// project lands on its new-session form with the agent chosen; an agent enabled
// nowhere has the button disabled with the reason and the way to enable it;
// and the usage line shows what the server sends, and nothing when it sends
// none.
func TestAnAgentDetailShowsTargetsStartsASessionAndCountsUsage_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	const plainEnv, plainHeader = "plain-env-secret-value-1", "ghp_" + "a1B2c3D4e5F6g7H8i9J0k1L2"
	writeCatalogFile(t, w, "agents.json", `{"agents": {
  "designer": {"description": "Designs screens.", "mcp_servers": ["docs", "local-tool", "gone"]},
  "backend": {"description": "APIs and services."},
  "loner": {"description": "Enabled nowhere."}
}}
`)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {
  "docs": {"type": "http", "url": "https://mcp.example.com/mcp", "headers": {"Authorization": "Bearer `+plainHeader+`", "X-Team": "${DOCS_TEAM}"}},
  "local-tool": {"command": "npx", "args": ["-y", "tool-mcp", "--token=${TOOL_TOKEN}"], "env": {"PLAIN": "`+plainEnv+`", "REF": "${TOOL_REF}"}}
}}
`)
	for _, n := range []string{"designer", "backend", "loner"} {
		if _, err := w.cat.Agents.CreateAgent(w.ctx, n, n+" agent", "", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	// harness lists designer and backend, atlas-api lists designer, lab lists backend only.
	atlas, err := w.cat.Projects.CreateProject(w.ctx, "atlas-api", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lab, err := w.cat.Projects.CreateProject(w.ctx, "lab", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enabledInSetAgents(t, w, w.project.ID, "selected", "designer", "backend")
	enabledInSetAgents(t, w, atlas.ID, "selected", "designer")
	enabledInSetAgents(t, w, lab.ID, "selected", "backend")

	// The server may not send usage yet: the page shows the line when it does, and hides it when it does not.
	usage := startSessionUsage(t, w, "designer")
	t.Logf("usage line from the server: %q", usage)

	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	const detail = `[data-agent-detail]`
	rows := `[...document.querySelectorAll('[data-mcp-server]')].map(r => [...r.querySelectorAll('td')].slice(0, 3).map(c => c.innerText.replace(/\s+/g, ' ').trim()).join(' | ')).join(' ;; ')`
	items := `[...document.querySelectorAll('[data-start-session] [role=menuitem]')].map(a => a.querySelector('span').textContent + '=' + a.getAttribute('href')).join(' ')`
	startBtn := `document.querySelector('[data-start-session] [data-start-button]')`
	pid := w.project.ID

	b := newTLSBrowser(t)
	b.run("the MCP table has a Target column: the URL of a remote server, the command line of a local one, ${VAR} as written, and no plain value anywhere",
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(w.srv.URL+"/agents/designer"),
		waitUntil(visible(detail+` table`)),
		waitUntil(`[...document.querySelectorAll('[data-mcp-table] th')].map(h => h.textContent.trim()).slice(0, 3).join() === 'Server,Transport,Target'`),
		waitUntil(rows+` === 'docs | http | https://mcp.example.com/mcp ;; local-tool | stdio | npx -y tool-mcp --token=${TOOL_TOKEN} ;; gone | not in the catalog | '`),
		// The page's seed script carries the catalog as the server read it; what the detail renders must not.
		waitUntil(`(() => { const shown = document.body.innerText + document.querySelector('[data-agent-detail]').outerHTML; return !shown.includes(`+quote(plainEnv)+`) && !shown.includes(`+quote(plainHeader)+`) && shown.includes('${DOCS_TEAM}'); })()`),
		bothSchemes("agent-detail-target-column"),
	)
	if usage != "" {
		b.run("the usage line shows what the server counts", waitUntil(flat(`[data-usage]`)+` === `+quote(usage)), waitUntil(visible(`[data-usage]`)))
	} else {
		b.run("the server sends no usage: the line does not show", waitUntil(`document.querySelector('[data-usage]') !== null && !`+visible(`[data-usage]`)))
	}
	b.run("Start a session as lists exactly the projects that enable the agent, and is operable by keyboard, returning the focus to its button",
		waitUntil(`(`+startBtn+`?.textContent.trim()) === 'Start a session as designer' && `+startBtn+`.disabled === false`),
		waitUntil(`!`+visible(`[data-start-reason]`)),
		chromedp.Evaluate(startBtn+`.focus()`, nil),
		pressKey("Enter", 13),
		waitUntil(startBtn+`.getAttribute('aria-expanded') === 'true' && `+startBtn+`.getAttribute('aria-haspopup') === 'menu'`),
		waitUntil(items+` === 'atlas-api=/projects/`+atlas.ID+`/sessions?agent=designer harness=/projects/`+pid+`/sessions?agent=designer'`),
		waitUntil(`document.activeElement?.getAttribute('role') === 'menuitem' && document.activeElement.textContent.includes('atlas-api')`),
		pressKey("ArrowDown", 40),
		waitUntil(`document.activeElement?.textContent.includes('harness')`),
		bothSchemes("agent-detail-start-menu"),
		pressKey("Escape", 27),
		waitUntil(startBtn+`.getAttribute('aria-expanded') === 'false' && document.activeElement === `+startBtn),
	)
	b.run("choosing a project lands on its new-session form with the agent chosen",
		pressKey("Enter", 13),
		waitUntil(startBtn+`.getAttribute('aria-expanded') === 'true'`),
		chromedp.Evaluate(`document.querySelector('[data-start-session] [role=menuitem][data-project="`+pid+`"]').click()`, nil),
		atPath(w.srv.URL+"/projects/"+pid+"/sessions?agent=designer"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length > 1`),
		waitUntil(`document.querySelector('#new-session-agent')?.selectedOptions[0]?.textContent.trim() === 'designer'`),
		bothSchemes("start-session-form-with-agent"),
	)
	b.run("an agent enabled nowhere has the button disabled, says why, and links to Enable in a project",
		chromedp.Navigate(w.srv.URL+"/agents/loner"),
		waitUntil(`(`+startBtn+`?.disabled) === true`),
		waitUntil(visible(`[data-start-reason]`)),
		waitUntil(flat(`[data-start-reason]`)+` === 'Not enabled in any project. Enable in a project'`),
		waitUntil(`document.querySelector('[data-start-enable]').getAttribute('href') === '/agents/loner/edit#projects'`),
		waitUntil(startBtn+`.getAttribute('aria-describedby') === 'agent-start-reason'`),
		bothSchemes("agent-detail-start-disabled"),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// startSessionUsage is the usage line the catalog read implies for an agent:
// "" when the server sends no usage for it.
func startSessionUsage(t *testing.T, w *tokenSaverWorld, name string) string {
	t.Helper()
	res, err := w.srv.Client().Get(w.srv.URL + "/api/get_agent_catalog")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var body struct {
		Agents []struct {
			Name  string `json:"name"`
			Usage *struct {
				Sessions int `json:"sessions"`
				Zones    int `json:"zones"`
			} `json:"usage"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("catalog read: %v\n%s", err, raw)
	}
	for _, a := range body.Agents {
		if a.Name == name && a.Usage != nil {
			count := func(n int, word string) string {
				return fmt.Sprintf("%d %s%s", n, word, map[bool]string{true: "", false: "s"}[n == 1])
			}
			return "Used by " + count(a.Usage.Sessions, "session") + " · " + count(a.Usage.Zones, "zone")
		}
	}
	return ""
}
