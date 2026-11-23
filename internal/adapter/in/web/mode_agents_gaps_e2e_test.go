//go:build !windows

package web_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
)

// These tests fill the gaps the scenarios of mode_agents_e2e_test.go leave, at
// the same seam (the browser against a real server): keyboard use of the
// Models page section and of the agent editor, a Selected row that allows only
// "No agent", switching a row back to All agents, the New session form with
// every mode on All agents and with the mode changed back and forth, and the
// board's brainstorm dialog when Product mode allows only "No agent".

// maFocus puts the focus on the element the selector finds.
func maFocus(sel string) chromedp.Action {
	return waitUntil(`(() => { const e = document.querySelector(` + quote(sel) + `); if (!e || e.disabled) return false; e.focus(); return document.activeElement === e; })()`)
}

// sortedPair is the two names in the order maState lists ticked boxes.
func sortedPair(a, b string) []string {
	p := []string{a, b}
	sort.Strings(p)
	return p
}

// maActive describes the focused element: "<row mode>/<what>" for an element
// of a Mode agents row (an agent's name, "no-agent", or the button's text).
const maActive = `(() => { const a = document.activeElement; const r = a?.closest('[data-mode-agents-row]'); if (!r) return ''; 
	const l = a.closest('label'); const what = l ? (l.dataset.agent ?? 'no-agent') : a.textContent.trim();
	return r.dataset.modeAgentsRow + '/' + what; })()`

// TestTheModelsSectionWorksFromTheKeyboardAndWithNoAgentOnly_EndToEnd covers
// the Models page's Mode agents section without a mouse: Enter on Selected
// agents, Tab into the checklist, Space to tick, Tab on to Save and Enter. It
// then saves a Selected row that allows only "No agent" (no warning, stored as
// such), and switches Selected rows back to All agents, saved and not.
func TestTheModelsSectionWorksFromTheKeyboardAndWithNoAgentOnly_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", maCatalog)
	b := newTLSBrowser(t)
	var got string

	b.run("open the Models page and focus Reviewer's Selected agents button",
		chromedp.EmulateViewport(1280, 1400),
		chromedp.Navigate(w.srv.URL+"/models"),
		waitUntil(`document.querySelectorAll('[data-mode-agents] [data-mode-agents-row]').length === 10`),
		maFocus(maRow("review")+` [data-state=selected]`),
		chromedp.KeyEvent(kb.Enter),
		waitUntil(maState("review")+` === 'selected:'`),
	)
	b.run("Tab goes into the row's checklist and Space ticks an agent",
		chromedp.KeyEvent(kb.Tab),
		chromedp.Evaluate(maActive, &got),
	)
	if !strings.HasPrefix(got, "review/") || got == "review/Save" {
		t.Fatalf("after Tab the focus is on %q, want an agent of the Reviewer row", got)
	}
	first := strings.TrimPrefix(got, "review/")
	b.run("Space ticks it",
		chromedp.KeyEvent(" "),
		waitUntil(maState("review")+` === `+quote("selected:"+first)),
	)
	for i := 0; i < 5 && got != "review/no-agent"; i++ {
		b.run("Tab on", chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(maActive, &got))
	}
	if got != "review/no-agent" {
		t.Fatalf("Tab never reached the No agent entry, ended on %q", got)
	}
	b.run("Space ticks No agent, Tab reaches Save and Enter saves",
		chromedp.KeyEvent(" "),
		waitUntil(maState("review")+` === `+quote("selected:"+strings.Join(sortedPair(first, "no-agent"), ","))),
		chromedp.KeyEvent(kb.Tab),
		chromedp.Evaluate(maActive, &got),
	)
	if got != "review/Save" {
		t.Fatalf("after No agent Tab goes to %q, want review/Save", got)
	}
	b.run("Enter saves",
		chromedp.KeyEvent(kb.Enter),
		waitUntil(`document.querySelector(`+quote(maRow("review"))+`).textContent.includes('Saved')`),
	)
	if got, want := maGet(t, w, "review"), `"mode":"review","name":"Reviewer","state":"selected","agents":["`+first+`"],"no_agent":true`; got != want {
		t.Errorf("stored = %s, want %s", got, want)
	}

	// A Selected row that allows only a session with no agent is a real choice, not "nothing".
	var warned bool
	b.run("Selected with only No agent shows no warning and saves",
		maClick("test", "Selected agents"),
		waitUntil(`!!document.querySelector(`+quote(maRow("test")+` [data-mode-agents-empty]`)+`)`),
		maTick("test", "no-agent"),
		waitUntil(`!document.querySelector(`+quote(maRow("test")+` [data-mode-agents-empty]`)+`)`),
		chromedp.Evaluate(`!!document.querySelector(`+quote(maRow("test")+` [data-mode-agents-empty]`)+`)`, &warned),
		maClick("test", "Save"),
		waitUntil(`document.querySelector(`+quote(maRow("test"))+`).textContent.includes('Saved')`),
	)
	if warned {
		t.Error("a Selected row with No agent ticked still warns that nothing can start")
	}
	if got, want := maGet(t, w, "test"), `"mode":"test","name":"Tester","state":"selected","agents":[],"no_agent":true`; got != want {
		t.Errorf("stored = %s, want %s", got, want)
	}

	b.run("an unsaved Selected choice switched back to All agents is not a change",
		maClick("design", "Selected agents"),
		maTick("design", "designer"),
		waitUntil(`!document.querySelector(`+quote(maRow("design")+` button[data-variant=primary]`)+`).disabled`),
		maClick("design", "All agents"),
		waitUntil(maState("design")+` === 'all:'`),
		waitUntil(`document.querySelector(`+quote(maRow("design")+` button[data-variant=primary]`)+`).disabled`),
	)
	if got := maGet(t, w, "design"); !strings.Contains(got, `"state":"all"`) {
		t.Errorf("design mode = %s, want it still on All agents", got)
	}

	b.run("a saved Selected row goes back to All agents from the keyboard",
		maFocus(maRow("review")+` [data-state=all]`),
		chromedp.KeyEvent(kb.Enter),
		waitUntil(maState("review")+` === 'all:'`),
		maClick("review", "Save"),
		waitUntil(`document.querySelector(`+quote(maRow("review"))+`).textContent.includes('Saved')`),
		chromedp.Reload(),
		waitUntil(maState("review")+` === 'all:'`),
		waitUntil(maState("test")+` === 'selected:no-agent'`),
	)
	if got, want := maGet(t, w, "review"), `"mode":"review","name":"Reviewer","state":"all","agents":[],"no_agent":true`; got != want {
		t.Errorf("stored after All = %s, want %s", got, want)
	}
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}

// TestTheAgentEditorsModesWorkFromTheKeyboardAndAreLabelled_EndToEnd covers the editor's
// Modes section without a mouse: every mode checkbox has a name, an All agents
// mode is disabled and so skipped by Tab, Space ticks a Selected mode and
// Enter on the Save button saves it onto that mode's list.
func TestTheAgentEditorsModesWorkFromTheKeyboardAndAreLabelled_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", maCatalog)
	maSetMode(t, w, `{"mode":"review","state":"selected","agents":["reviewer"],"no_agent":false}`)
	maSetMode(t, w, `{"mode":"design","state":"selected","agents":["reviewer"],"no_agent":false}`)

	const (
		boxes = `[...document.querySelectorAll('[data-mode-list] input[type=checkbox]')]`
		save  = `document.querySelector('[data-save-footer] [data-save]')`
	)
	b := newTLSBrowser(t)
	var names string
	var grouped bool
	b.run("every mode checkbox has an accessible name, in a labelled group",
		chromedp.EmulateViewport(1280, 1200),
		chromedp.Navigate(w.srv.URL+"/agents/designer/edit#modes"),
		waitUntil(`document.querySelectorAll('[data-mode-row]').length === 10`),
		chromedp.Evaluate(boxes+`.map(i => i.getAttribute('aria-label') || i.closest('label')?.textContent.trim() || '').join(',')`, &names),
		chromedp.Evaluate(`document.querySelector('[data-mode-list]').getAttribute('role') === 'group' && !!document.querySelector('[data-mode-list]').getAttribute('aria-label')`, &grouped),
	)
	if want := "Default, All agents,Architect, All agents,Implement, All agents,Design, Selected agents,Product, All agents,Design system, All agents,Reviewer, Selected agents,Tester, All agents,QA, All agents,Operator, All agents"; names != want {
		t.Errorf("checkbox names = %q, want %q", names, want)
	}
	if !grouped {
		t.Error("the modes are not in a labelled group")
	}

	// Reviewer and Design are Selected and list reviewer only, so both are free for designer.
	b.run("Tab skips the All agents modes and lands on the first free one",
		maFocus(`[data-mode-list]`),
		chromedp.KeyEvent(kb.Tab),
		waitUntil(`document.activeElement?.closest('[data-mode-row]')?.dataset.mode === 'design'`),
	)
	b.run("Space ticks Design, Tab and Space tick Reviewer, Save becomes enabled",
		chromedp.KeyEvent(" "),
		waitUntil(`document.querySelector('[data-mode-row][data-mode="design"] input').checked`),
		chromedp.KeyEvent(kb.Tab),
		waitUntil(`document.activeElement?.closest('[data-mode-row]')?.dataset.mode === 'review'`),
		chromedp.KeyEvent(" "),
		waitUntil(`document.querySelector('[data-mode-row][data-mode="review"] input').checked && `+save+`.disabled === false`),
	)
	b.run("Space on the same box again unticks it, and Enter on Save saves the rest",
		chromedp.KeyEvent(" "),
		waitUntil(`!document.querySelector('[data-mode-row][data-mode="review"] input').checked`),
		maFocus(`[data-save-footer] [data-save]`),
		chromedp.KeyEvent(kb.Enter),
		atPath(w.srv.URL+"/agents/designer"),
	)
	if got := maGet(t, w, "design"); !strings.Contains(got, "designer") || !strings.Contains(got, "reviewer") {
		t.Errorf("design mode = %s, want reviewer and designer", got)
	}
	if got := maGet(t, w, "review"); strings.Contains(got, "designer") {
		t.Errorf("review mode = %s, want designer left off it", got)
	}
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}

// TestTheNewSessionFormFollowsTheModeBackAndForth_EndToEnd covers the form
// when nothing is restricted (every mode on All agents: the same picker in
// every mode, no note, no warning), and then the mode changed back and forth
// across Selected modes: an agent is cleared with its reason and the reason
// goes when the next mode allows what is picked, a Selected mode with only
// "No agent" offers Plain claude alone, and a Selected mode with agents and no
// "No agent" turns Start off until an agent is picked.
func TestTheNewSessionFormFollowsTheModeBackAndForth_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", nsAgentsCatalog)
	nsAddCatalogAgents(t, w)

	const (
		picker    = `[...document.querySelectorAll('#new-session-agent option')].map(o => o.textContent.trim()).sort().join('|')`
		agentNote = `(document.querySelector(` + "`" + nsForm + ` [data-agent-note]` + "`" + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
		nothing   = `!!document.querySelector(` + "`" + nsForm + ` [data-mode-allows-nothing]` + "`" + `)`
		startOff  = `document.querySelector('` + nsForm + ` button[type=submit]').disabled`
		chosen    = `(() => { const s = document.querySelector('#new-session-agent'); return s.selectedOptions[0]?.textContent.trim() ?? ''; })()`
	)
	setMode := func(mode string) chromedp.Action {
		return chromedp.Evaluate(`(() => { const s = document.querySelector('#new-session-mode'); s.value = `+quote(mode)+`; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil)
	}
	pick := func(agent string) chromedp.Action {
		return chromedp.Evaluate(`(() => { const s = document.querySelector('#new-session-agent'); s.value = [...s.options].find(o => o.textContent.trim() === `+quote(agent)+`).value; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil)
	}

	b := newTLSBrowser(t)
	var modes []string
	b.run("open the form",
		chromedp.EmulateViewport(1280, 1100),
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length > 1`),
		waitUntil(nsLive),
		chromedp.Evaluate(`[...document.querySelectorAll('#new-session-mode option')].map(o => o.value)`, &modes),
	)
	if len(modes) < 5 {
		t.Fatalf("the mode select has %v", modes)
	}
	const everyAgent = "Plain claude|backend|designer|reviewer|writer"
	for _, m := range modes {
		var got, note string
		var none bool
		b.run("with every mode on All agents mode "+quote(m)+" offers everything",
			setMode(m),
			chromedp.Evaluate(picker, &got),
			chromedp.Evaluate(agentNote, &note),
			chromedp.Evaluate(nothing, &none),
		)
		if got != everyAgent || note != "" || none {
			t.Errorf("mode %q: picker = %q, note = %q, nothing-warning = %v; want the whole list and no note", m, got, note, none)
		}
	}

	maSetMode(t, w, `{"mode":"review","state":"selected","agents":["reviewer"],"no_agent":false}`)
	maSetMode(t, w, `{"mode":"test","state":"selected","agents":[],"no_agent":true}`)
	maSetMode(t, w, `{"mode":"implement","state":"selected","agents":["designer","backend"],"no_agent":false}`)
	b.run("reload the form now that three modes are Selected",
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length > 1`),
		waitUntil(nsLive),
	)

	var got, note string
	var off bool
	b.run("pick designer on Default, move to Reviewer (cleared, with the reason), then to Implement",
		pick("designer"),
		setMode("review"),
		waitUntil(`!!document.querySelector(`+quote(nsForm+` [data-agent-note]`)+`)`),
		chromedp.Evaluate(agentNote, &note),
	)
	if !strings.Contains(note, "designer cannot start in Reviewer mode") {
		t.Errorf("note = %q", note)
	}
	b.run("the cleared agent does not come back, and the reason goes with the next mode",
		setMode("implement"),
		waitUntil(`(`+picker+`) === 'Pick an agent|backend|designer'`),
		waitUntil(`!document.querySelector(`+quote(nsForm+` [data-agent-note]`)+`)`),
		chromedp.Evaluate(chosen, &got),
	)
	if got != "Pick an agent" {
		t.Errorf("after review -> implement the field shows %q, want nothing picked", got)
	}

	b.run("Implement allows no session without an agent: Start is off until one is picked",
		chromedp.Evaluate(startOff, &off),
		pick("designer"),
		setField(`#new-session-prompt`, "Build it"),
		waitUntil(`!`+startOff),
	)
	if !off {
		t.Error("Start was enabled with no agent picked in a mode that needs one")
	}

	b.run("moving from Implement to a mode that lists designer keeps the pick and says nothing",
		setMode(""),
		waitUntil(`(`+picker+`) === `+quote(everyAgent)),
		chromedp.Evaluate(chosen, &got),
		chromedp.Evaluate(agentNote, &note),
	)
	if got != "designer" || note != "" {
		t.Errorf("on Default the field shows %q with note %q, want designer and no note", got, note)
	}

	b.run("a Selected mode with only No agent offers Plain claude alone, and Start is on",
		setMode("test"),
		waitUntil(`(`+picker+`) === 'Plain claude'`),
		chromedp.Evaluate(nothing, &off),
		waitUntil(`!`+startOff),
	)
	if off {
		t.Error("a mode that allows only a session with no agent shows the allows-nothing warning")
	}
	b.run("and back to Implement the offered list is Implement's again",
		setMode("implement"),
		waitUntil(`(`+picker+`) === 'Pick an agent|backend|designer'`),
	)
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}

// TestTheBrainstormDialogWithOnlyNoAgentAndWithNothing_EndToEnd covers the
// board's brainstorm dialog under the two Product-mode lists the main scenario
// leaves out: only "No agent" (Plain claude alone, Start on) and nothing (the
// warning replaces the picker and Start is off). The dialog is painted by the
// server, so each list is read on its own page load.
func TestTheBrainstormDialogWithOnlyNoAgentAndWithNothing_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", nsAgentsCatalog)
	nsAddCatalogAgents(t, w)
	if _, err := w.cat.Settings.UpdateSettings(map[string]string{domain.ModeModelKey(domain.SessionModeProduct): "haiku"}); err != nil {
		t.Fatal(err)
	}

	const (
		options  = `[...document.querySelectorAll('#brainstorm-agent option')].map(o => o.textContent.trim()).sort().join('|')`
		startOff = `document.querySelector('#brainstorm button[type=submit]').disabled`
	)
	open := func(b *browser, step string) {
		b.run(step,
			chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID),
			waitUntil(`document.querySelectorAll('#brainstorm-agent option').length >= 1`),
			waitUntil(nsLive),
			nsClick(`body`, "Brainstorm a feature"),
			waitUntil(`document.querySelector('#brainstorm').matches(':popover-open')`),
		)
	}

	b := newTLSBrowser(t)
	b.run("size", chromedp.EmulateViewport(1280, 900))

	maSetMode(t, w, `{"mode":"product","state":"selected","agents":[],"no_agent":true}`)
	open(b, "Product mode allows only No agent")
	var got string
	var off, warned bool
	b.run("the picker offers Plain claude alone and Start is on",
		chromedp.Evaluate(options, &got),
		chromedp.Evaluate(`!!document.querySelector('#brainstorm [data-mode-allows-nothing]')`, &warned),
		waitUntil(`!`+startOff),
	)
	if got != "Plain claude" || warned {
		t.Errorf("options = %q, warning = %v; want Plain claude alone and no warning", got, warned)
	}

	maSetMode(t, w, `{"mode":"product","state":"selected","agents":[],"no_agent":false}`)
	open(b, "Product mode allows nothing")
	var text string
	b.run("the warning replaces the picker and Start is off",
		waitUntil(`!!document.querySelector('#brainstorm [data-mode-allows-nothing]')`),
		waitUntil(`document.querySelector('#brainstorm .dropdown')?.checkVisibility() === false`),
		chromedp.Evaluate(startOff, &off),
		chromedp.Evaluate(`document.querySelector('#brainstorm [data-mode-allows-nothing]').innerText.replace(/\s+/g, ' ').trim()`, &text),
		chromedp.Evaluate(`document.querySelector('#brainstorm [data-mode-allows-nothing] a')?.getAttribute('href')`, &got),
	)
	if !off {
		t.Error("Start brainstorm is enabled in a mode that allows nothing")
	}
	if !strings.Contains(text, "No agent can start in Product mode") {
		t.Errorf("warning = %q", text)
	}
	if got != "/models" {
		t.Errorf("the warning links to %q, want /models", got)
	}

	maSetMode(t, w, `{"mode":"product","state":"all","agents":[],"no_agent":true}`)
	open(b, "Product mode back on All agents")
	b.run("the picker offers everything again",
		chromedp.Evaluate(options, &got),
		chromedp.Evaluate(startOff, &off),
	)
	if got != "Plain claude|backend|designer|reviewer|writer" || off {
		t.Errorf("options = %q, Start off = %v; want every agent and Start on", got, off)
	}
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}
