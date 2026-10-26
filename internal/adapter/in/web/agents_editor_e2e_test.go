//go:build !windows

package web_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestAgentEditorCreatesEditsAndRenamesAnAgent_EndToEnd is W1 for story 9 of
// spec "Web UI — the Agents page", against a real server and a temporary
// catalog: an agent is created through the editor with a bundle and skills
// chosen by keyboard only, the summary follows each choice, Review change shows
// the diff, and the saved agent is in the library with its toast. An existing
// agent is edited and renamed, an imported agent without a description cannot
// save until it has one, a missing skill is shown by the server's refusal and
// removed, and the trip to the skill form keeps the half-done edit.
func TestAgentEditorCreatesEditsAndRenamesAnAgent_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"ux": ["lint", "tdd"]},
  "agents": {
    "ux-designer": {"description": "Designs.", "aliases": ["ux"], "skills": ["@ux", "tdd"], "mcp_servers": ["github"], "allowed_tools": ["Read"]},
    "legacy": {"skills": ["@ux", "ghost"]}
  }
}
`)
	for _, s := range []string{"lint", "tdd", "dev-browser"} {
		writeCatalogFile(t, w, "skills/"+s+"/SKILL.md", "---\nname: "+s+"\ndescription: The "+s+" skill.\n---\nbody\n")
	}
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {
  "github": {"type": "http", "url": "https://example.test/mcp", "headers": {"Authorization": "Bearer ${HARNESS_E2E_UNSET_TOKEN}"}},
  "notes": {"command": "npx", "args": ["notes-mcp"]}
}}
`)
	agentsJSON := func() string {
		raw, err := os.ReadFile(filepath.Join(w.root, "agents", "agents.json"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	const editor = `document.querySelector('[data-agent-editor]')`
	flat := func(sel string) string { return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()` }
	visible := func(sel string) string { return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true` }
	attr := func(sel, name string) string { return `document.querySelector(` + quote(sel) + `)?.getAttribute(` + quote(name) + `)` }
	chips := `[...document.querySelectorAll('[data-agent-editor] [data-chips] [data-chip]')].map(c => c.dataset.chip).join()`
	active := `document.activeElement?.id`
	save := `document.querySelector('[data-save-footer] [data-save]')`
	opens := func(path string) []chromedp.Action {
		return []chromedp.Action{
			chromedp.Navigate(w.srv.URL + path),
			waitUntil(visible(`[data-agent-editor]`)),
		}
	}
	run := func(b *browser, name string, actions ...chromedp.Action) {
		for i, a := range actions {
			b.run(fmt.Sprintf("%s [action %d]", name, i), a)
		}
	}

	b := newTLSBrowser(t)
	run(b, "/agents/new opens the full-page editor: a contents list with counts, empty fields and Save off",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/new"),
		waitUntil(visible(`[data-agent-editor]`)),
		waitUntil(`document.querySelector('[data-agent-editor] h1').textContent.trim() === 'New agent'`),
		waitUntil(`!document.querySelector('[role=tablist]')?.checkVisibility()`),
		waitUntil(`[...document.querySelectorAll('.agent-toc a')].map(a => a.dataset.section + '=' + a.querySelector('.count').textContent.trim()).join() === 'identity=,skills=0,mcp=0,tools=default'`),
		waitUntil(save+`.disabled === true && `+save+`.textContent.trim() === 'Create agent'`),
		waitUntil(`[...document.querySelectorAll('[data-agent-editor] [data-mcp-row]')].map(r => r.dataset.name).join() === 'github,notes'`),
		waitUntil(flat(`[data-agent-editor] [data-mcp-row][data-name=github]`)+`.includes('http') && `+flat(`[data-agent-editor] [data-mcp-row][data-name=github]`)+`.includes('${HARNESS_E2E_UNSET_TOKEN} not set')`),
		waitUntil(`document.querySelectorAll('[data-agent-editor] [data-tool-row]').length > 0 && !!document.querySelector('[data-agent-editor] [data-tool-row][data-name=Read]')`),
		waitUntil(flat(`[data-tools-summary]`)+` === 'None ticked · sessions get Claude’s default tools'`),
		waitUntil(`[...document.querySelectorAll('[data-agent-editor] button')].some(b => b.dataset.newMcp !== undefined && b.getAttribute('aria-haspopup') === 'dialog')`),
	)
	run(b, "a name that is not a slug is linked to its field; an empty description turns Save off with the message and a banner",
		setField(`#agent-name`, "iOS Designer"),
		waitUntil(visible(`#agent-name-error`)),
		waitUntil(attr(`#agent-name`, "aria-describedby")+` === 'agent-name-hint agent-name-error' && `+attr(`#agent-name`, "aria-invalid")+` === 'true'`),
		setField(`#agent-name`, "ios-designer"),
		waitUntil(`!`+visible(`#agent-name-error`)),
		waitUntil(visible(`#agent-desc-error`)),
		waitUntil(flat(`#agent-desc-error`)+` === 'Add a description to save.'`),
		waitUntil(attr(`#agent-desc`, "aria-describedby")+` === 'agent-desc-hint agent-desc-error' && `+attr(`#agent-desc`, "aria-invalid")+` === 'true'`),
		waitUntil(visible(`[data-description-banner]`)),
		waitUntil(flat(`[data-save-state]`)+` === '1 error · description is required'`),
		waitUntil(save+`.disabled === true`),
		bothSchemes("agents-editor-no-description"),
		setField(`#agent-desc`, "Designs iOS screens."),
		waitUntil(`!`+visible(`#agent-desc-error`)+` && !`+visible(`[data-description-banner]`)),
		waitUntil(save+`.disabled === false`),
	)
	run(b, "an alias is added with Enter and removed from its chip",
		chromedp.SendKeys(`#agent-aliases`, "ios", chromedp.ByQuery),
		pressKey("Enter", 13),
		waitUntil(`[...document.querySelectorAll('[data-alias-chip]')].map(c => c.dataset.alias).join() === 'ios'`),
		chromedp.SendKeys(`#agent-aliases`, "Bad Alias", chromedp.ByQuery),
		pressKey("Enter", 13),
		waitUntil(visible(`#agent-aliases-error`)),
		setField(`#agent-aliases`, ""),
		waitUntil(`!`+visible(`#agent-aliases-error`)),
	)
	run(b, "by keyboard alone: Tab reaches the skills combobox, which lists bundles first, and Enter adds the bundle",
		chromedp.Focus(`#agent-desc`, chromedp.ByQuery),
		pressKey("Tab", 9),
		waitUntil(active+` === 'skillPicker-input'`),
		waitUntil(attr(`#skillPicker-input`, "role")+` === 'combobox' && `+attr(`#skillPicker-input`, "aria-expanded")+` === 'true'`),
		waitUntil(`document.querySelector('#skillPicker-list [role=option]').dataset.key === '@ux'`),
		waitUntil(`document.querySelector('#skillPicker-input').getAttribute('aria-activedescendant') === 'skillPicker-opt-0'`),
		chromedp.SendKeys(`#skillPicker-input`, "ux", chromedp.ByQuery),
		waitUntil(`[...document.querySelectorAll('#skillPicker-list [role=option]')].map(o => o.dataset.key).join() === '@ux,+create'`),
		pressKey("Enter", 13),
		waitUntil(chips+` === '@ux'`),
		waitUntil(flat(`[data-chip="@ux"]`)+`.startsWith('@ux (2)') && `+attr(`[data-chip="@ux"]`, "data-kind")+` === 'bundle'`),
		waitUntil(flat(`[data-skills-summary]`)+` === '2 skills after bundles are expanded'`),
		waitUntil(`document.querySelector('[data-section=skills] .count').textContent.trim() === '2'`),
	)
	run(b, "a skill the bundle already brings is marked, and added only because the person insists; the summary says what was dropped",
		chromedp.SendKeys(`#skillPicker-input`, "tdd", chromedp.ByQuery),
		waitUntil(flat(`#skillPicker-list [role=option][data-key=tdd]`)+`.includes('already in @ux')`),
		pressKey("Enter", 13),
		waitUntil(chips+` === '@ux,tdd'`),
		waitUntil(flat(`[data-skills-summary]`)+` === '2 skills after bundles are expanded · 1 duplicate dropped (tdd)'`),
		chromedp.SendKeys(`#skillPicker-input`, "dev", chromedp.ByQuery),
		pressKey("Enter", 13),
		waitUntil(chips+` === '@ux,tdd,dev-browser'`),
		waitUntil(flat(`[data-skills-summary]`)+` === '3 skills after bundles are expanded · 1 duplicate dropped (tdd)'`),
		waitUntil(`document.querySelector('[data-section=skills] .count').textContent.trim() === '3'`),
	)
	run(b, "Backspace in the empty input removes the last chip, and Escape closes the list; the arrow reopens it",
		pressKey("Backspace", 8),
		waitUntil(chips+` === '@ux,tdd'`),
		waitUntil(flat(`[data-skills-summary]`)+` === '2 skills after bundles are expanded · 1 duplicate dropped (tdd)'`),
		pressKey("Escape", 27),
		waitUntil(attr(`#skillPicker-input`, "aria-expanded")+` === 'false' && !`+visible(`#skillPicker-list`)),
		pressKey("ArrowDown", 40),
		waitUntil(attr(`#skillPicker-input`, "aria-expanded")+` === 'true' && `+visible(`#skillPicker-list`)),
		chromedp.SendKeys(`#skillPicker-input`, "dev", chromedp.ByQuery),
		pressKey("Enter", 13),
		waitUntil(chips+` === '@ux,tdd,dev-browser'`),
	)
	run(b, "nothing matching offers to create the skill typed, and Create a new skill… ends the list",
		chromedp.SendKeys(`#skillPicker-input`, "figma", chromedp.ByQuery),
		waitUntil(`[...document.querySelectorAll('#skillPicker-list [role=option]')].map(o => o.dataset.key + ':' + o.textContent.replace(/\s+/g, ' ').trim()).join() === '+create:+ Create skill “figma”…'`),
		setField(`#skillPicker-input`, ""),
		waitUntil(`[...document.querySelectorAll('#skillPicker-list [role=option]')].pop().textContent.includes('Create a new skill…')`),
		pressKey("Escape", 27),
	)
	run(b, "MCP servers and allowed tools are ticked, and their counts and summaries follow",
		chromedp.Click(`[data-agent-editor] [data-mcp-row][data-name=github] input`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-section=mcp] .count').textContent.trim() === '1' && `+flat(`[data-mcp-summary]`)+` === '1 of 2 servers'`),
		chromedp.Click(`[data-agent-editor] [data-tool-row][data-name=Read] input`, chromedp.ByQuery),
		chromedp.Click(`[data-agent-editor] [data-tool-row][data-name=Bash] input`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-section=tools] .count').textContent.trim() === '2'`),
		waitUntil(flat(`[data-tools-summary]`)+` === '2 allowed · everything else is not allowed to this agent'`),
		bothSchemes("agents-editor-new-filled"),
	)
	run(b, "Review change shows the server's diff of agents.json",
		clickButton(`document.querySelector('[data-save-footer]')`, "Review change"),
		waitUntil(visible(`[data-review-panel]`)),
		waitUntil(`document.querySelector('[data-review-panel] [data-diff-file]')?.dataset.path === 'agents.json'`),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-line][data-kind=add]')].some(l => l.textContent.includes('ios-designer'))`),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-line][data-kind=add]')].some(l => l.textContent.includes('@ux'))`),
		bothSchemes("agents-editor-review"),
	)
	run(b, "Create agent saves, shows the toast, and lands on the agent in the library",
		clickButton(`document.querySelector('[data-save-footer]')`, "Create agent"),
		atPath(w.srv.URL+"/agents/ios-designer"),
		waitUntil(visible(`[data-toast]`)),
		waitUntil(flat(`[data-toast]`)+`.includes(' agents.json — ') && `+flat(`[data-toast]`)+`.includes('Running sessions keep what they started with')`),
		waitUntil(`[...document.querySelectorAll('[data-agent-row]')].map(r => r.dataset.key).includes('ios-designer')`),
		waitUntil(`document.querySelector('[data-agent-detail] h2').textContent.trim() === 'ios-designer'`),
		waitUntil(`!document.querySelector('[data-agent-editor]')`),
		bothSchemes("agents-editor-saved-toast"),
	)
	got := agentsJSON()
	for _, want := range []string{`"ios-designer"`, `"Designs iOS screens."`, `"ios"`, `"@ux"`, `"dev-browser"`, `"github"`, `"Bash"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("agents.json lacks %s:\n%s", want, got)
		}
	}

	run(b, "a Change link lands on its section of an existing agent's editor; one more skill by keyboard changes the summary and the footer",
		append(opens("/agents/ux-designer/edit#skills"),
			waitUntil(`document.querySelector('#agent-name').value === 'ux-designer' && document.querySelector('#agent-desc').value === 'Designs.'`),
			waitUntil(`document.querySelector('[data-section=skills]').getAttribute('aria-current') === 'true'`),
			waitUntil(chips+` === '@ux,tdd'`),
			waitUntil(flat(`[data-skills-summary]`)+` === '2 skills after bundles are expanded · 1 duplicate dropped (tdd)'`),
			waitUntil(flat(`[data-save-state]`)+` === 'No changes · writes agents.json'`),
			waitUntil(save+`.disabled === true && `+save+`.textContent.trim() === 'Save agent'`),
			chromedp.Focus(`#skillPicker-input`, chromedp.ByQuery),
			chromedp.SendKeys(`#skillPicker-input`, "dev", chromedp.ByQuery),
			pressKey("Enter", 13),
			waitUntil(flat(`[data-skills-summary]`)+` === '3 skills after bundles are expanded · 1 duplicate dropped (tdd)'`),
			waitUntil(flat(`[data-save-state]`)+` === '1 unsaved change · writes agents.json'`),
		)...,
	)
	run(b, "renaming sends previous_name: the entries move to the new name and the old one is gone",
		setField(`#agent-name`, "design-lead"),
		waitUntil(flat(`[data-save-state]`)+` === '2 unsaved changes · writes agents.json'`),
		clickButton(`document.querySelector('[data-save-footer]')`, "Save agent"),
		atPath(w.srv.URL+"/agents/design-lead"),
		waitUntil(flat(`[data-toast]`)+`.includes(' agents.json — ')`),
		waitUntil(`![...document.querySelectorAll('[data-agent-row]')].some(r => r.dataset.key === 'ux-designer') && [...document.querySelectorAll('[data-agent-row]')].some(r => r.dataset.key === 'design-lead')`),
	)
	if got := agentsJSON(); strings.Contains(got, `"ux-designer"`) || !strings.Contains(got, `"design-lead"`) || !strings.Contains(got, `"dev-browser"`) || !strings.Contains(got, `"aliases": [`) {
		t.Fatalf("after the rename agents.json =\n%s", got)
	}

	run(b, "an imported agent without a description opens on it, cannot save, and shows its missing skill as a red chip",
		append(opens("/agents/legacy/edit#identity"),
			waitUntil(visible(`[data-description-banner]`)),
			waitUntil(active+` === 'agent-desc'`),
			waitUntil(visible(`#agent-desc-error`)),
			waitUntil(save+`.disabled === true`),
			waitUntil(chips+` === '@ux,ghost'`),
			waitUntil(attr(`[data-chip="ghost"]`, "data-missing")+` === 'true' && `+flat(`[data-chip="ghost"]`)+`.includes('missing')`),
			waitUntil(flat(`[data-skills-summary]`)+` === '2 skills after bundles are expanded · 1 missing (ghost)'`),
			bothSchemes("agents-editor-missing-skill"),
		)...,
	)
	run(b, "saving with the missing skill is refused by the server, shown at the skills field, and focus lands there",
		setField(`#agent-desc`, "Writes the backend."),
		waitUntil(save+`.disabled === false`),
		clickButton(`document.querySelector('[data-save-footer]')`, "Save agent"),
		waitUntil(visible(`#agent-entries-error`)),
		waitUntil(flat(`#agent-entries-error`)+` === 'Not in the catalog: ghost. Remove it and save again.'`),
		waitUntil(attr(`#skillPicker-input`, "aria-describedby")+` === 'agent-entries-hint agent-entries-error' && `+attr(`#skillPicker-input`, "aria-invalid")+` === 'true'`),
		waitUntil(active+` === 'skillPicker-input'`),
		bothSchemes("agents-editor-unknown-reference"),
	)
	run(b, "the person removes the missing chip, and the save goes through",
		chromedp.Evaluate(`document.querySelector('[data-chip="ghost"] button').click()`, nil),
		waitUntil(chips+` === '@ux' && !`+visible(`#agent-entries-error`)),
		clickButton(`document.querySelector('[data-save-footer]')`, "Save agent"),
		atPath(w.srv.URL+"/agents/legacy"),
		waitUntil(flat(`[data-toast]`)+`.includes(' agents.json — ')`),
	)
	if got := agentsJSON(); strings.Contains(got, "ghost") || !strings.Contains(got, "Writes the backend.") {
		t.Fatalf("after fixing legacy agents.json =\n%s", got)
	}

	run(b, "Cancel on an edited form asks first; Keep editing stays and Discard changes leaves",
		append(opens("/agents/design-lead/edit"),
			setField(`#agent-desc`, "Changed my mind."),
			waitUntil(flat(`[data-save-state]`)+` === '1 unsaved change · writes agents.json'`),
			clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
			waitUntil(visible(`[data-leave]`)),
			waitUntil(`document.activeElement === document.querySelector('[data-keep]')`),
			clickButton(`document.querySelector('[data-leave]')`, "Keep editing"),
			waitUntil(`!`+visible(`[data-leave]`)+` && document.querySelector('#agent-desc').value === 'Changed my mind.'`),
			clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
			clickButton(`document.querySelector('[data-leave]')`, "Discard changes"),
			atPath(w.srv.URL+"/agents/design-lead"),
			waitUntil(`!document.querySelector('[data-agent-editor]')`),
		)...,
	)

	run(b, "Create a new skill… leaves for the skill form with the editor to return to, keeping the half-done edit",
		append(opens("/agents/new"),
			setField(`#agent-name`, "trip"),
			setField(`#agent-desc`, "Takes a trip."),
			chromedp.Focus(`#skillPicker-input`, chromedp.ByQuery),
			chromedp.SendKeys(`#skillPicker-input`, "figma", chromedp.ByQuery),
			waitUntil(`document.querySelector('#skillPicker-list [role=option]').dataset.key === '+create'`),
			pressKey("Enter", 13),
			atPath(w.srv.URL+"/agents/skills/new?name=figma&return=%2Fagents%2Fnew%23skills"),
		)...,
	)
	writeCatalogFile(t, w, "skills/figma/SKILL.md", "---\nname: figma\ndescription: Figma things.\n---\nbody\n")
	run(b, "coming back with the new skill restores the form with it chosen",
		append(opens("/agents/new?added=figma"),
			waitUntil(`document.querySelector('#agent-name').value === 'trip' && document.querySelector('#agent-desc').value === 'Takes a trip.'`),
			waitUntil(chips+` === 'figma' && `+attr(`[data-chip="figma"]`, "data-missing")+` !== 'true'`),
			waitUntil(`location.search === ''`),
			waitUntil(flat(`[data-skills-summary]`)+` === '1 skill after bundles are expanded'`),
			waitUntil(save+`.disabled === false`),
		)...,
	)
	run(b, "Discard leaves the unsaved agent behind",
		clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
		clickButton(`document.querySelector('[data-leave]')`, "Discard changes"),
		atPath(w.srv.URL+"/agents"),
	)
	if got := agentsJSON(); strings.Contains(got, `"trip"`) {
		t.Fatalf("a discarded agent was saved:\n%s", got)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
