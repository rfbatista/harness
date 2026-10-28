//go:build !windows

package web_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// a11yScheme switches the browser to a colour scheme and lets the theme's
// transition finish.
func a11yScheme(scheme string) chromedp.Action {
	return chromedp.Tasks{
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}}),
		chromedp.Sleep(300 * time.Millisecond),
	}
}

// TestAccessibilityKeyScreensReadInBothColourSchemes_EndToEnd is W1 for story
// 21 (and the checks of story 20 that a sweep can make) of spec "Web UI — the
// Agents page", against a real server and a temporary catalog: every key
// screen of the feature (the five library tabs with a detail open, the agent
// editor, the bundle editor, the skill and MCP forms, the delete dialog, the
// project's settings and the new-session form) is audited in light and in dark
// for controls without a name, errors not linked to their field, ids that go
// nowhere, checklists outside a group, targets under 24 px and text below 4.5:1
// (3:1 when large).
func TestAccessibilityKeyScreensReadInBothColourSchemes_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"ux": ["lint", "tdd"]},
  "agents": {
    "designer": {"description": "Designs screens.", "aliases": ["ux"], "skills": ["@ux", "tdd"], "mcp_servers": ["github"], "allowed_tools": ["Read"]},
    "backend": {"description": "APIs and services.", "skills": ["lint"]},
    "legacy": {"skills": ["@ux", "ghost"]}
  }
}
`)
	for _, s := range []string{"lint", "tdd"} {
		writeCatalogFile(t, w, "skills/"+s+"/SKILL.md", "---\nname: "+s+"\ndescription: The "+s+" skill.\n---\n# "+s+"\n\nBody of **"+s+"**.\n")
	}
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {
  "github": {"type": "http", "url": "https://example.test/mcp", "headers": {"Authorization": "Bearer ${HARNESS_E2E_UNSET_TOKEN}"}},
  "notes": {"command": "npx", "args": ["notes-mcp"]}
}}
`)
	atlas, err := w.cat.Projects.CreateProject(w.ctx, "atlas-api", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enabledInSetAgents(t, w, atlas.ID, "selected", "backend")
	nsAddCatalogAgents(t, w)

	settings := "/projects/" + w.project.ID + "/settings"
	live := chromedp.Sleep(200 * time.Millisecond)
	settle := chromedp.Sleep(500 * time.Millisecond)
	type screen struct {
		name    string
		url     string
		prepare []chromedp.Action
	}
	screens := []screen{
		{"agents tab with a detail", "/agents/designer", []chromedp.Action{waitUntil(`!!document.querySelector('[data-agent-detail] h2')`)}},
		{"skills tab with a detail", "/agents/skills/tdd", []chromedp.Action{waitUntil(`!!document.querySelector('[data-skill-row]')`)}},
		{"mcp tab with a detail", "/agents/mcp/github", []chromedp.Action{waitUntil(`!!document.querySelector('[data-mcp-row]')`)}},
		{"tools tab with a detail", "/agents/tools", []chromedp.Action{waitUntil(`!!document.querySelector('[data-tool-row]')`)}},
		{"bundles tab with a detail", "/agents/bundles/ux", []chromedp.Action{waitUntil(`!!document.querySelector('[data-bundle-row]')`)}},
		{"agent editor, new", "/agents/new", []chromedp.Action{waitUntil(`!!document.querySelector('[data-mcp-row]')`)}},
		{"agent editor, with a missing skill", "/agents/legacy/edit", []chromedp.Action{waitUntil(`!!document.querySelector('[data-chip="ghost"]')`)}},
		{"bundle editor", "/agents/bundles/ux/edit", nil},
		{"skill form", "/agents/skills/tdd/edit", nil},
		{"mcp form", "/agents/mcp/github/edit", nil},
		{"mcp form, new", "/agents/mcp/new", nil},
		{"delete dialog", "/agents/skills/tdd", []chromedp.Action{deleteMenu(`[data-skill-detail]`), waitUntil(`document.querySelector('[data-delete-dialog]')?.checkVisibility() === true`)}},
		{"project settings", settings, nil},
		{"new session form", "/projects/" + w.project.ID + "/tasks/" + w.task.ID, []chromedp.Action{nsClick(`main header`, "New session"), waitUntil(`document.querySelectorAll('#new-session-agent option').length > 1`)}},
	}

	b := newTLSBrowser(t)
	b.run("a wide window", chromedp.EmulateViewport(1280, 1000))
	for _, s := range screens {
		actions := append([]chromedp.Action{chromedp.Navigate(w.srv.URL + s.url), live}, s.prepare...)
		b.run(s.name+": opens", append(actions, settle)...)
		a11yBothSchemes(t, b, s.name)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// a11yBothSchemes audits the screen as it stands in light and in dark, and
// keeps a screenshot of each in PROJECT_SHOTS when it names a directory.
func a11yBothSchemes(t *testing.T, b *browser, name string) {
	t.Helper()
	slug := strings.NewReplacer(" ", "-", ",", "").Replace(name)
	for _, scheme := range []string{"light", "dark"} {
		var found []string
		b.run(name+": "+scheme, a11yScheme(scheme), a11yAudit(&found))
		if len(found) > 0 {
			t.Errorf("%s (%s): %d findings:\n%s", name, scheme, len(found), a11yJoin(found))
		}
	}
	b.run(name+": a screenshot of each", bothSchemes("a11y-"+slug))
	b.run(name+": back to light", a11yScheme("light"))
}

// TestAccessibilityStatesReadInBothColourSchemes_EndToEnd is W1 for story 21
// (and the live regions of story 20) of spec "Web UI — the Agents page":
// the states that sit on top of the key screens, audited in light and in
// dark like the screens are: the conflict, changed-on-disk and broken-file
// banners, the review panel, the saved toast, the MCP drawer, the project's
// settings in each of their states (a checklist, the empty-list warning, the
// combobox past fifteen agents) and the new-session form refused with
// AGENT_NOT_ENABLED. The banners that must be announced are live regions.
func TestAccessibilityStatesReadInBothColourSchemes_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	catalog := `{
  "bundles": {"ux": ["tdd"]},
  "agents": {
    "designer": {"description": "Designs screens.", "skills": ["@ux"], "mcp_servers": ["github"]},
    "backend": {"description": "APIs and services."},
    "reviewer": {"description": "Reviews code."},
    "writer": {}
  }
}
`
	writeCatalogFile(t, w, "agents.json", catalog)
	writeCatalogFile(t, w, "skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nbody\n")
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"github": {"type": "http", "url": "https://example.test/mcp"}}}
`)
	nsAddCatalogAgents(t, w)
	atlas, err := w.cat.Projects.CreateProject(w.ctx, "atlas-api", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enabledInSetAgents(t, w, atlas.ID, "selected")
	pid := w.project.ID
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	settle := chromedp.Sleep(400 * time.Millisecond)
	focus := chromedp.Evaluate(`window.dispatchEvent(new Event('focus'))`, nil)
	onDisk := func(from, to string) {
		writeCatalogFile(t, w, "agents.json", strings.Replace(readCatalogFile(t, w, "agents.json"), from, to, 1))
	}

	b := newTLSBrowser(t)
	b.run("a wide window", chromedp.EmulateViewport(1280, 1000))

	b.run("the agent changed on disk", chromedp.Navigate(w.srv.URL+"/agents/designer"), waitUntil(`!!document.querySelector('[data-agent-detail] h2')`), waitUntil(`document.querySelector('[data-agent-detail]') !== null`), settle)
	onDisk(`"Designs screens."`, `"Designs better screens."`)
	b.run("the detail says so", focus, waitUntil(visible(`[data-disk-changed]`)), settle)
	a11yBothSchemes(t, b, "changed on disk banner")

	b.run("the editor with a draft", chromedp.Navigate(w.srv.URL+"/agents/designer/edit"), waitUntil(`document.querySelector('#agent-desc')?.value === 'Designs better screens.'`), setField(`#agent-desc`, "My own wording."), settle)
	onDisk(`"Designs better screens."`, `"Someone else wrote this."`)
	b.run("the conflict banner", focus, waitUntil(visible(`[data-conflict]`)), waitUntil(`document.querySelector('[data-save-alerts]').getAttribute('role') === 'alert'`), settle)
	a11yBothSchemes(t, b, "conflict banner")
	b.run("Reload drops the draft", clickButton(`document.querySelector('[data-conflict]')`, "Reload (lose my edits)"), waitUntil(`!`+visible(`[data-conflict]`)))

	b.run("a change, reviewed",
		setField(`#agent-desc`, "Reviewed wording."),
		waitUntil(`document.querySelector('[data-save-footer] [data-review]').disabled === false`),
		clickButton(`document.querySelector('[data-save-footer]')`, "Review change"),
		waitUntil(visible(`[data-review-panel] [data-diff-line]`)),
		settle,
	)
	a11yBothSchemes(t, b, "review panel")
	b.run("saved, with its toast", clickButton(`document.querySelector('[data-save-footer]')`, "Save agent"), waitUntil(visible(`[data-toast]`)), waitUntil(`document.querySelector('[data-toast]').closest('[role=status]') !== null`), settle)
	a11yBothSchemes(t, b, "saved toast")
	var animation string
	b.run("with reduced motion asked for, the toast does not animate",
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-toast]')).animationDuration`, &animation),
	)
	if animation != "0.001s" {
		t.Errorf("the toast's animation lasts %q with prefers-reduced-motion: reduce, want 0.001s", animation)
	}
	b.run("motion back to normal", emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}}))

	b.run("the MCP drawer over the editor",
		chromedp.Navigate(w.srv.URL+"/agents/designer/edit"),
		waitUntil(`document.querySelector('#agent-desc')?.value === 'Reviewed wording.'`),
		waitUntil(`document.querySelector('[data-new-mcp]') !== null`),
		chromedp.Evaluate(`document.querySelector('[data-new-mcp]').click()`, nil),
		waitUntil(visible(`.mcp-form-host[data-mode=drawer]`)),
		settle,
	)
	a11yBothSchemes(t, b, "mcp drawer")

	b.run("project settings: Selected agents with a checklist, unsaved",
		chromedp.Navigate(w.srv.URL+"/projects/"+pid+"/settings"),
		waitUntil(visible(`[data-agents-mode]`)),
		chromedp.Evaluate(`document.querySelector('[data-agents-mode] [data-mode=selected]').click()`, nil),
		waitUntil(`document.querySelectorAll('[data-agents-list] [data-agent-row]').length === 4`),
		chromedp.Evaluate(`document.querySelector('[data-agents-list] [data-agent-row] input').click()`, nil),
		settle,
	)
	a11yBothSchemes(t, b, "project settings checklist")
	b.run("project settings: nothing enabled",
		chromedp.Navigate(w.srv.URL+"/projects/"+atlas.ID+"/settings"),
		waitUntil(visible(`[data-agents-empty]`)),
		settle,
	)
	a11yBothSchemes(t, b, "project settings empty list")

	// The new-session form is refused for an agent taken off the list.
	enabledInSetAgents(t, w, pid, "selected", "reviewer", "backend", "designer")
	b.run("a session is started as an agent that has just been taken off the list",
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length === 4`),
		waitUntil(nsLive),
		setField(`#new-session-prompt`, "Sketch the settings page"),
		chromedp.Evaluate(`(() => { const s = document.querySelector('#new-session-agent'); s.value = 'designer'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
	)
	enabledInSetAgents(t, w, pid, "selected", "reviewer", "backend")
	b.run("the refusal banner",
		waitUntil(`(`+nsOptions+`).includes('designer (no longer enabled)')`),
		nsClick(`main form`, "Start session"),
		waitUntil(visible(nsForm+` [data-agent-refusal]`)),
		waitUntil(`['alert', 'status'].includes(document.querySelector('`+nsForm+` [data-agent-refusal]').closest('[role]')?.getAttribute('role') ?? document.querySelector('`+nsForm+` [data-agent-refusal]').getAttribute('role'))`),
		settle,
	)
	a11yBothSchemes(t, b, "new session refused")
	enabledInSetAgents(t, w, pid, "selected")
	b.run("nothing to pick", waitUntil(visible(nsForm+` [data-no-agent-enabled]`)), settle)
	a11yBothSchemes(t, b, "new session without an agent")

	// A broken file last: it stays broken.
	writeCatalogFile(t, w, "agents.json", strings.Replace(readCatalogFile(t, w, "agents.json"), `"backend"`, `"backend",`, 1))
	b.run("the file stops parsing",
		chromedp.Navigate(w.srv.URL+"/agents"),
		waitUntil(`document.querySelectorAll('[data-agent-row]').length > 0`),
		focus,
		waitUntil(visible(`[data-broken]`)),
		waitUntil(`document.querySelector('[data-broken]').getAttribute('role') === 'alert'`),
		settle,
	)
	a11yBothSchemes(t, b, "broken file banner")
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestAccessibilityTheAgentsComboboxPastFifteenIsReadInBothColourSchemes_EndToEnd
// audits the project's settings when the list of agents is a combobox with
// chips, open on its options.
func TestAccessibilityTheAgentsComboboxPastFifteenIsReadInBothColourSchemes_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	var agents []string
	for i := 0; i < 16; i++ {
		agents = append(agents, fmt.Sprintf(`"agent-%02d": {"description": "Agent number %d."}`, i, i))
	}
	writeCatalogFile(t, w, "agents.json", `{"agents": {`+strings.Join(agents, ",")+`}}`)
	b := newTLSBrowser(t)
	b.run("Selected agents shows the combobox with its options open",
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/settings"),
		waitUntil(`document.querySelector('[data-agents-mode]')?.checkVisibility() === true`),
		chromedp.Evaluate(`document.querySelector('[data-agents-mode] [data-mode=selected]').click()`, nil),
		waitUntil(`document.querySelector('[data-agents-combobox]')?.checkVisibility() === true`),
		chromedp.Focus(`#settings-agents-input`, chromedp.ByQuery),
		a11yPress("ArrowDown", 40, 0),
		waitUntil(`document.querySelector('#settings-agents-input').getAttribute('aria-expanded') === 'true'`),
		chromedp.Sleep(400*time.Millisecond),
	)
	a11yBothSchemes(t, b, "project settings combobox")
}

// TestAccessibilityEveryFormErrorIsLinkedToItsField_EndToEnd is W1 for story
// 20 of spec "Web UI — the Agents page": a refusal on each of the four forms
// (an agent, a skill, an MCP server and a bundle with a name the form turns
// down) sits in the form with aria-invalid on its field and the message in its
// aria-describedby, and the screen is still readable in both colour schemes. Each form gets a browser of its own: a form with edits
// asks before the page is left.
func TestAccessibilityEveryFormErrorIsLinkedToItsField_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{"bundles": {"ux": ["tdd"]}, "agents": {"designer": {"description": "Designs.", "skills": ["tdd"]}}}
`)
	writeCatalogFile(t, w, "skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nbody\n")
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"github": {"type": "http", "url": "https://example.test/mcp"}}}
`)
	for _, c := range []struct{ name, path, field, value, error string }{
		{"agent form error", "/agents/new", "#agent-name", "Not A Slug", "#agent-name-error"},
		{"skill form error", "/agents/skills/new", "#sf-name", "tdd", "#sf-name-error"},
		{"mcp form error", "/agents/mcp/new", "#mf-name", "github", "#mf-name-error"},
		{"bundle form error", "/agents/bundles/new", "#bundle-name", "Not A Slug", "#bundle-name-error"},
	} {
		b := newTLSBrowser(t)
		b.run(c.name+": the refusal",
			chromedp.EmulateViewport(1280, 1000),
			chromedp.Navigate(w.srv.URL+c.path),
			waitUntil(`document.querySelector(`+quote(c.field)+`) !== null`),
			setField(c.field, c.value),
			waitUntil(`document.querySelector(`+quote(c.error)+`)?.checkVisibility() === true`),
			waitUntil(`document.querySelector(`+quote(c.field)+`).getAttribute('aria-invalid') === 'true'`),
			waitUntil(`document.querySelector(`+quote(c.field)+`).getAttribute('aria-describedby').split(' ').includes(`+quote(strings.TrimPrefix(c.error, "#"))+`)`),
			chromedp.Sleep(300*time.Millisecond),
		)
		a11yBothSchemes(t, b, c.name)
	}
}
