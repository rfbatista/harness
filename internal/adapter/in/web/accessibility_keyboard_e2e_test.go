//go:build !windows

package web_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// a11yPress sends one raw key press with the modifiers held.
func a11yPress(key string, code int64, mods input.Modifier) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchKeyEvent(input.KeyRawDown).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(code).WithModifiers(mods).Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(code).WithModifiers(mods).Do(ctx)
	})
}

// a11yTabUntil presses Tab (Shift+Tab with back) until cond, a JS expression,
// is true, and fails naming where the focus was when it gave up.
func a11yTabUntil(cond string, back bool) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var mods input.Modifier
		if back {
			mods = input.ModifierShift
		}
		var trail []string
		for i := 0; i < 80; i++ {
			var ok bool
			if err := chromedp.Evaluate(cond, &ok).Do(ctx); err != nil {
				return err
			}
			if ok {
				return nil
			}
			if err := a11yPress("Tab", 9, mods).Do(ctx); err != nil {
				return err
			}
			time.Sleep(20 * time.Millisecond)
			var at string
			_ = chromedp.Evaluate(a11yFocusJS, &at).Do(ctx)
			trail = append(trail, at)
		}
		return fmt.Errorf("Tab never reached %s; the focus went through:\n%s", cond, strings.Join(trail, "\n"))
	})
}

// a11yFocusJS describes the focused element.
const a11yFocusJS = `(() => { const e = document.activeElement; return e.tagName.toLowerCase() + (e.id ? '#' + e.id : '') + ' ' + (e.getAttribute('aria-label') || e.textContent || '').trim().slice(0, 30); })()`

// a11yRingsJS installs a recorder of every focus change in the page: each one
// must show a focus ring (the element or one of its four parents has an
// outline or a box shadow) and match :focus-visible.
const a11yRingsJS = `(() => {
  window.__rings = [];
  window.__stops = 0;
  document.addEventListener('focusin', (e) => {
    const el = e.target;
    window.__stops++;
    let ring = false;
    for (let n = el, i = 0; n && i < 5 && !ring; n = n.parentElement, i++) {
      const cs = getComputedStyle(n);
      ring = (cs.outlineStyle !== 'none' && parseFloat(cs.outlineWidth) > 0) || cs.boxShadow !== 'none';
    }
    if (!ring || !el.matches(':focus-visible')) window.__rings.push(el.tagName.toLowerCase() + (el.id ? '#' + el.id : '') + ' ' + (el.getAttribute('aria-label') || el.textContent || '').trim().slice(0, 30) + (ring ? ' not :focus-visible' : ' has no ring'));
  }, true);
})()`

// a11yRole is the element's role and accessible name, as "role: name".
func a11yRole(sel string) string {
	return `(() => { const e = document.querySelector(` + quote(sel) + `); if (!e) return 'missing';
	  const role = e.getAttribute('role') || ({BUTTON: 'button', A: 'link', SELECT: 'combobox', TEXTAREA: 'textbox'}[e.tagName]) || (e.tagName === 'INPUT' ? (e.type === 'checkbox' ? 'checkbox' : 'textbox') : e.tagName.toLowerCase());
	  const lb = e.getAttribute('aria-labelledby');
	  const name = lb ? lb.split(/\s+/).map((i) => document.getElementById(i)?.textContent ?? '').join(' ') : (e.getAttribute('aria-label') || [...(e.labels ?? [])].map((l) => l.innerText).join(' ') || e.innerText);
	  return role + ': ' + name.replace(/\s+/g, ' ').trim(); })()`
}

// TestAccessibilityAgentIsCreatedByKeyboardAlone_EndToEnd is W1 for stories 20
// and 21 of spec "Web UI — the Agents page", against a real server and a
// temporary catalog: one run with no mouse. The keyboard (Tab, Shift+Tab,
// arrows, Enter, Space, Escape and typing) fills /agents/new from the name to
// Save, with the roles and names a screen reader reads on the way: the skills
// combobox with its listbox and options, the checklists as named groups of
// checkboxes, an error linked to its field, the MCP drawer as a modal dialog
// that holds the focus and gives it back, the toast as a polite status. Then
// the tabs: a tablist with a roving tabindex, arrow keys, Home and End, each
// tab linked to its panel; and the delete dialog, an alert dialog that keeps
// the focus and returns it on Escape. Every focus stop shows a ring.
func TestAccessibilityAgentIsCreatedByKeyboardAlone_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"ux": ["lint", "tdd"]},
  "agents": {"designer": {"description": "Designs.", "skills": ["tdd"]}}
}
`)
	for _, s := range []string{"lint", "tdd"} {
		writeCatalogFile(t, w, "skills/"+s+"/SKILL.md", "---\nname: "+s+"\ndescription: The "+s+" skill.\n---\nbody\n")
	}
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {
  "github": {"type": "http", "url": "https://example.test/mcp"},
  "notes": {"command": "npx", "args": ["notes-mcp"]}
}}
`)
	atlas, err := w.cat.Projects.CreateProject(w.ctx, "atlas-api", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enabledInSetAgents(t, w, atlas.ID, "selected", "designer")

	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	attr := func(sel, name string) string {
		return `document.querySelector(` + quote(sel) + `)?.getAttribute(` + quote(name) + `)`
	}
	is := func(js, want string) chromedp.Action { return waitUntil(`(` + js + `) === ` + quote(want)) }
	active := `document.activeElement?.id`
	onSel := func(sel string) string {
		return `document.activeElement === document.querySelector(` + quote(sel) + `)`
	}
	chips := `[...document.querySelectorAll('[data-agent-editor] [data-chips] [data-chip]')].map(c => c.dataset.chip).join()`
	typeText := func(s string) chromedp.Action { return chromedp.KeyEvent(s) }
	tab := func() chromedp.Action { return a11yPress("Tab", 9, 0) }
	shiftTab := func() chromedp.Action { return a11yPress("Tab", 9, input.ModifierShift) }
	rings := func(b *browser, step string, min int) {
		var bad []string
		var stops int
		b.run(step, chromedp.Evaluate(`window.__rings`, &bad), chromedp.Evaluate(`window.__stops`, &stops))
		if stops < min {
			t.Errorf("%s: only %d focus stops were checked, want %d or more", step, stops, min)
		}
		if len(bad) > 0 {
			t.Errorf("%s: focus without a visible ring:\n%s", step, a11yJoin(bad))
		}
	}
	save := `document.querySelector('[data-save-footer] [data-save]')`

	b := newTLSBrowser(t)
	b.run("/agents/new opens with the focus on the page, and Tab reaches the name",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/new"),
		waitUntil(visible(`[data-agent-editor]`)),
		waitUntil(`document.querySelectorAll('[data-agent-editor] [data-mcp-row]').length === 2`),
		chromedp.Evaluate(a11yRingsJS, nil),
		a11yTabUntil(active+` === 'agent-name'`, false),
		is(a11yRole(`#agent-name`), "textbox: Name"),
	)

	b.run("a name that is not a slug is an error linked to its field and read with its hint",
		typeText("Keyboard Agent"),
		waitUntil(visible(`#agent-name-error`)),
		is(attr(`#agent-name`, "aria-invalid"), "true"),
		is(attr(`#agent-name`, "aria-describedby"), "agent-name-hint agent-name-error"),
		waitUntil(flat(`#agent-name-error`)+`.length > 0`),
	)
	for range "Keyboard Agent" {
		b.run("Backspace", chromedp.KeyEvent(kb.Backspace))
	}
	b.run("a slug clears the error",
		typeText("keyboard-agent"),
		waitUntil(`!`+visible(`#agent-name-error`)+` && `+attr(`#agent-name`, "aria-invalid")+` !== 'true'`),
	)

	b.run("Tab goes to the aliases, Enter adds one, and its chip has a named remove button reached by Shift+Tab",
		tab(),
		waitUntil(active+` === 'agent-aliases'`),
		typeText("kb"),
		a11yPress("Enter", 13, 0),
		waitUntil(`[...document.querySelectorAll('[data-alias-chip]')].map(c => c.dataset.alias).join() === 'kb'`),
		shiftTab(),
		waitUntil(onSel(`[data-alias-chip] button`)),
		is(a11yRole(`[data-alias-chip] button`), "button: Remove alias kb"),
		tab(),
		tab(),
		waitUntil(active+` === 'agent-desc'`),
		typeText("Keyboard-made agent."),
		waitUntil(`!`+visible(`[data-description-banner]`)),
		waitUntil(save+`.disabled === false`),
	)

	b.run("the skills combobox: role, listbox and options, the active option, Enter adds the bundle",
		tab(),
		waitUntil(active+` === 'skillPicker-input'`),
		is(a11yRole(`#skillPicker-input`), "combobox: Add a skill or a bundle"),
		waitUntil(attr(`#skillPicker-input`, "aria-expanded")+` === 'true'`),
		is(attr(`#skillPicker-input`, "aria-controls"), "skillPicker-list"),
		is(attr(`#skillPicker-list`, "role"), "listbox"),
		waitUntil(`document.querySelectorAll('#skillPicker-list [role=option]').length >= 3`),
		a11yPress("ArrowDown", 40, 0),
		waitUntil(`(() => { const a = document.querySelector('#skillPicker-input').getAttribute('aria-activedescendant'); const o = a && document.getElementById(a); return !!o && o.getAttribute('role') === 'option' && o.dataset.key === 'lint'; })()`),
		a11yPress("ArrowUp", 38, 0),
		waitUntil(`document.getElementById(document.querySelector('#skillPicker-input').getAttribute('aria-activedescendant'))?.dataset.key === '@ux'`),
		a11yPress("Enter", 13, 0),
		waitUntil(chips+` === '@ux'`),
		a11yPress("Escape", 27, 0),
		waitUntil(attr(`#skillPicker-input`, "aria-expanded")+` === 'false'`),
	)

	b.run("Tab goes past the chip's remove button into the MCP checklist, a named group of checkboxes; Space ticks one",
		a11yTabUntil(onSel(`[data-agent-editor] [data-mcp-row][data-name=github] input`), false),
		is(a11yRole(`[data-agent-editor] [data-mcp-row][data-name=github] input`), "checkbox: github http"),
		is(`document.querySelector('[data-agent-editor] [data-mcp-row][data-name=github] input').closest('[role=group]').getAttribute('aria-label')`, "MCP servers"),
		chromedp.KeyEvent(" "),
		waitUntil(`document.querySelector('[data-agent-editor] [data-mcp-row][data-name=github] input').checked && `+flat(`[data-mcp-summary]`)+` === '1 of 2 servers'`),
	)

	b.run("New MCP server… opens the drawer as a modal dialog: the focus moves in, Tab stays in, Escape closes and gives the focus back",
		a11yTabUntil(onSel(`[data-new-mcp]`), false),
		chromedp.KeyEvent(kb.Enter),
		waitUntil(visible(`.mcp-form-host`)),
		waitUntil(`document.activeElement.closest('.mcp-form-host') !== null`),
		waitUntil(`['dialog', 'alertdialog'].includes(document.querySelector('.mcp-form-host [role=dialog], .mcp-form-host[role=dialog]')?.getAttribute('role') ?? '')`),
		waitUntil(`(document.querySelector('.mcp-form-host [role=dialog], .mcp-form-host[role=dialog]').getAttribute('aria-modal')) === 'true'`),
	)
	for i := 0; i < 30; i++ { // more than the drawer has controls: it wraps
		b.run(fmt.Sprintf("Tab %d inside the drawer", i), tab(), waitUntil(`document.activeElement.closest('[data-mcp-form]') !== null`))
	}
	b.run("Escape closes the drawer and the focus is back on the button that opened it",
		a11yPress("Escape", 27, 0),
		waitUntil(`!`+visible(`.mcp-form-host`)),
		waitUntil(onSel(`[data-new-mcp]`)),
	)

	b.run("the tools are a named group; Space ticks Read",
		a11yTabUntil(onSel(`[data-agent-editor] [data-tool-row][data-name=Read] input`), false),
		is(`document.querySelector('[data-agent-editor] [data-tool-row][data-name=Read] input').closest('[role=group]').getAttribute('aria-label')`, "Allowed tools"),
		chromedp.KeyEvent(" "),
		waitUntil(`document.querySelector('[data-section=tools] .count').textContent.trim() === '1'`),
	)

	b.run("the projects are a named group; the All-agents row is a disabled, ticked checkbox; Space ticks atlas-api",
		a11yTabUntil(onSel(`[data-project-row][data-name=atlas-api] input`), false),
		is(`document.querySelector('[data-project-row][data-name=atlas-api] input').closest('[role=group]').getAttribute('aria-label')`, "Projects where this agent is enabled"),
		waitUntil(`document.querySelector('[data-project-row][data-name=harness] input').disabled && document.querySelector('[data-project-row][data-name=harness] input').checked`),
		chromedp.KeyEvent(" "),
		waitUntil(`document.querySelector('[data-project-row][data-name=atlas-api] input').checked`),
		waitUntil(flat(`[data-projects-summary]`)+`.startsWith('Enabled in 2 of 2')`),
	)
	rings(b, "the editor", 20)

	b.run("Review change opens the diff, and Create agent saves by keyboard",
		a11yTabUntil(onSel(`[data-save-footer] [data-review]`), false),
		chromedp.KeyEvent(kb.Enter),
		waitUntil(visible(`[data-review-panel]`)),
		is(attr(`[data-save-footer] [data-review]`, "aria-expanded"), "true"),
		tab(),
		tab(),
		waitUntil(onSel(`[data-save-footer] [data-save]`)),
		is(a11yRole(`[data-save-footer] [data-save]`), "button: Create agent"),
		chromedp.KeyEvent(kb.Enter),
		atPath(w.srv.URL+"/agents/keyboard-agent"),
		waitUntil(visible(`[data-toast]`)),
		waitUntil(`document.querySelector('[data-toast]').closest('[role=status]')?.getAttribute('aria-live') === 'polite'`),
	)
	raw, err := os.ReadFile(filepath.Join(w.root, "agents", "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"keyboard-agent"`, `"Keyboard-made agent."`, `"kb"`, `"@ux"`, `"github"`, `"Read"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("agents.json lacks %s:\n%s", want, raw)
		}
	}
	atlasAgents := enabledInSaved(t, w, atlas.ID)
	if !strings.Contains(atlasAgents, "keyboard-agent") {
		t.Errorf("atlas-api's agents = %q, want keyboard-agent ticked by keyboard", atlasAgents)
	}

	b.run("the tabs are a tablist: the selected tab is the only one in the Tab order, and each tab names its panel",
		chromedp.Navigate(w.srv.URL+"/agents/keyboard-agent"),
		waitUntil(`document.querySelectorAll('[data-agent-row]').length === 2`),
		chromedp.Evaluate(a11yRingsJS, nil),
		is(attr(`[role=tablist]`, "aria-label"), "Library sections"),
		is(`[...document.querySelectorAll('[role=tablist] > [role=tab]')].map(t => t.id + ':' + t.getAttribute('aria-selected') + ':' + t.tabIndex).join()`,
			"agents-tab-agents:true:0,agents-tab-skills:false:-1,agents-tab-bundles:false:-1,agents-tab-mcp:false:-1,agents-tab-tools:false:-1"),
		is(`[...document.querySelectorAll('[role=tab]')].map(t => { const p = document.getElementById(t.getAttribute('aria-controls')); return p && p.getAttribute('role') === 'tabpanel' && p.getAttribute('aria-labelledby') === t.id; }).join()`, "true,true,true,true,true"),
		is(`[...document.querySelectorAll('[role=tabpanel]')].filter(p => !p.hidden).map(p => p.id).join()`, "agents-panel-agents"),
		a11yTabUntil(onSel(`#agents-tab-agents`), false),
		is(a11yRole(`#agents-tab-agents`), "tab: Agents 2"),
		tab(),
		waitUntil(`document.activeElement.closest('[role=tabpanel]') !== null || document.activeElement.getAttribute('role') === 'tabpanel'`),
		shiftTab(),
		waitUntil(onSel(`#agents-tab-agents`)),
	)
	b.run("ArrowRight selects the next tab and moves the focus, End goes to the last, Home to the first",
		a11yPress("ArrowRight", 39, 0),
		waitUntil(onSel(`#agents-tab-skills`)),
		is(attr(`#agents-tab-skills`, "aria-selected"), "true"),
		is(`[...document.querySelectorAll('[role=tab]')].map(t => t.tabIndex).join()`, "-1,0,-1,-1,-1"),
		is(`[...document.querySelectorAll('[role=tabpanel]')].filter(p => !p.hidden).map(p => p.id).join()`, "agents-panel-skills"),
		a11yPress("End", 35, 0),
		waitUntil(onSel(`#agents-tab-tools`)),
		is(`[...document.querySelectorAll('[role=tabpanel]')].filter(p => !p.hidden).map(p => p.id).join()`, "agents-panel-tools"),
		a11yPress("ArrowRight", 39, 0),
		waitUntil(onSel(`#agents-tab-agents`)),
		a11yPress("ArrowLeft", 37, 0),
		waitUntil(onSel(`#agents-tab-tools`)),
		a11yPress("Home", 36, 0),
		waitUntil(onSel(`#agents-tab-agents`)),
		is(`[...document.querySelectorAll('[role=tabpanel]')].filter(p => !p.hidden).map(p => p.id).join()`, "agents-panel-agents"),
	)

	b.run("Enter on an agent's row opens its detail",
		a11yTabUntil(onSel(`[data-agent-row][data-key=keyboard-agent]`), false),
		chromedp.KeyEvent(kb.Enter),
		waitUntil(`document.querySelector('[data-agent-detail] h2')?.textContent.trim() === 'keyboard-agent'`),
	)
	b.run("the agent's menu opens by keyboard and Delete… opens an alert dialog named for what it deletes, with the focus in",
		a11yTabUntil(`document.activeElement?.getAttribute('aria-label') === 'More actions' && !!document.activeElement.closest('[data-agent-detail]')`, false),
		chromedp.KeyEvent(kb.Enter),
		waitUntil(`document.querySelector('[data-agent-detail] [role=menu]:has([data-duplicate])')?.checkVisibility() === true`),
		waitUntil(`document.activeElement?.getAttribute('role') === 'menuitem'`),
		a11yPress("ArrowDown", 40, 0),
		a11yPress("ArrowDown", 40, 0),
		waitUntil(`document.activeElement?.dataset.action === 'delete'`),
		chromedp.KeyEvent(kb.Enter),
		waitUntil(visible(`[data-delete-dialog]`)),
		is(attr(`[data-delete-dialog]`, "role"), "alertdialog"),
		waitUntil(`!!document.getElementById(document.querySelector('[data-delete-dialog]').getAttribute('aria-labelledby'))?.textContent.trim()`),
		waitUntil(`document.activeElement.closest('[data-delete-dialog]') !== null`),
		waitUntil(`document.activeElement === document.querySelector('[data-delete-name]')`),
	)
	for i := 0; i < 12; i++ {
		b.run(fmt.Sprintf("Tab %d inside the delete dialog", i), tab(), waitUntil(`document.activeElement.closest('[data-delete-dialog]') !== null`))
	}
	b.run("Escape closes the dialog and the focus goes back to the menu button",
		a11yPress("Escape", 27, 0),
		waitUntil(`!`+visible(`[data-delete-dialog]`)),
		waitUntil(`document.activeElement?.getAttribute('aria-label') === 'More actions'`),
	)
	rings(b, "the library", 15)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
