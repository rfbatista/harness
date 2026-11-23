//go:build !windows

package web_test

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

const maCatalog = `{"agents": {
  "designer": {"description": "Designs screens."},
  "reviewer": {"description": "Reviews code."},
  "backend": {"description": "APIs and services."}
}}
`

// maSetMode writes a mode's agents through the API, as another tab would.
func maSetMode(t *testing.T, w *tokenSaverWorld, body string) {
	t.Helper()
	res, err := w.srv.Client().Post(w.srv.URL+"/api/set_mode_agents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if raw, _ := io.ReadAll(res.Body); res.StatusCode != 200 {
		t.Fatalf("set_mode_agents %s: %d %s", body, res.StatusCode, raw)
	}
}

// maGet reads a mode's saved agents through the API as "state:agents:no_agent".
func maGet(t *testing.T, w *tokenSaverWorld, mode string) string {
	t.Helper()
	res, err := w.srv.Client().Get(w.srv.URL + "/api/get_mode_agents")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	// Cheap on purpose: the contract's entry for the mode, whitespace removed.
	s := strings.ReplaceAll(string(raw), " ", "")
	i := strings.Index(s, `"mode":"`+mode+`"`)
	if i < 0 {
		t.Fatalf("no entry for %q in %s", mode, raw)
	}
	s = s[i:]
	return s[:strings.Index(s, "}")]
}

// maRow is the Models page's Mode agents row of a mode.
func maRow(mode string) string {
	return `[data-mode-agents] [data-mode-agents-row="` + mode + `"]`
}

// maState is what a row shows: its state button, then the ticked agents.
func maState(mode string) string {
	return `(() => { const r = document.querySelector(` + quote(maRow(mode)) + `); if (!r) return '';
		const s = r.querySelector('[data-state][aria-pressed=true]')?.dataset.state ?? '';
		const ticked = [...r.querySelectorAll('input[type=checkbox]:checked')].map(i => i.closest('label').dataset.agent ?? 'no-agent').sort().join(',');
		return s + ':' + ticked; })()`
}

// maRefuseSaves answers every set_mode_agents of the page with a 422 and body,
// as a server that refuses the write would; other requests go through.
func maRefuseSaves(body string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		chromedp.ListenTarget(ctx, func(ev any) {
			p, ok := ev.(*fetch.EventRequestPaused)
			if !ok {
				return
			}
			go func() {
				c := chromedp.FromContext(ctx)
				ec := cdp.WithExecutor(ctx, c.Target)
				if strings.Contains(p.Request.URL, "set_mode_agents") {
					_ = fetch.FulfillRequest(p.RequestID, 422).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "application/json"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body))).Do(ec)
					return
				}
				_ = fetch.ContinueRequest(p.RequestID).Do(ec)
			}()
		})
		return fetch.Enable().Do(ctx)
	})
}

// maClick clicks the enabled button of a row whose text starts with label.
func maClick(mode, label string) chromedp.Action {
	return clickButton(`document.querySelector(`+quote(maRow(mode))+`)`, label)
}

// maTick ticks or unticks one checkbox of a row (an agent's name, or "no-agent").
func maTick(mode, name string) chromedp.Action {
	sel := maRow(mode) + ` label[data-agent="` + name + `"] input`
	if name == "no-agent" {
		sel = maRow(mode) + ` label[data-no-agent] input`
	}
	return waitUntil(`(() => { const i = document.querySelector(` + quote(sel) + `); if (!i || i.disabled) return false; i.click(); return true; })()`)
}

// TestTheModelsPageSetsEachModesAgents_EndToEnd covers stories 1 to 6 of the
// spec "Web UI — mode agents": one row per mode on All agents; switch one to
// Selected, tick agents and No agent, save, and find it stored and shown after
// a reload; switch it back to All agents; a Selected row that allows nothing
// says nothing can start; a refused save shows the server's message in the
// banner; and a change made elsewhere re-reads the open section.
func TestTheModelsPageSetsEachModesAgents_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", maCatalog)
	b := newTLSBrowser(t)

	var rows string
	b.run("one row per mode in order, each on All agents",
		chromedp.EmulateViewport(1280, 1400),
		chromedp.Navigate(w.srv.URL+"/models"),
		waitUntil(`document.querySelectorAll('[data-mode-agents] [data-mode-agents-row]').length === 10`),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-mode-agents] [data-mode-agents-row]')].map(r => r.dataset.modeAgentsRow + '=' + (r.querySelector('[data-state][aria-pressed=true]')?.dataset.state ?? '')).join(',')`, &rows),
		bothSchemes("models-mode-agents-all"),
	)
	if want := "=all,architect=all,implement=all,design=all,product=all,designsystem=all,review=all,test=all,qa=all,operate=all"; rows != want {
		t.Errorf("rows = %q, want %q", rows, want)
	}
	var names string
	b.run("the rows use the mode names",
		chromedp.Evaluate(`[...document.querySelectorAll('[data-mode-agents] [data-mode-agents-row] [data-mode-agents-name]')].map(e => e.textContent.trim()).join(',')`, &names),
	)
	if want := "Default,Architect,Implement,Design,Product,Design system,Reviewer,Tester,QA,Operator"; names != want {
		t.Errorf("names = %q, want %q", names, want)
	}

	b.run("switch Reviewer to Selected, tick reviewer and No agent, save",
		maClick("review", "Selected agents"),
		maTick("review", "reviewer"),
		maTick("review", "no-agent"),
		maClick("review", "Save"),
		waitUntil(`document.querySelector(`+quote(maRow("review"))+`).textContent.includes('Saved')`),
		bothSchemes("models-mode-agents-selected"),
	)
	if got, want := maGet(t, w, "review"), `"mode":"review","name":"Reviewer","state":"selected","agents":["reviewer"],"no_agent":true`; got != want {
		t.Errorf("stored = %s, want %s", got, want)
	}
	var state string
	b.run("a reload shows the saved choice",
		chromedp.Reload(),
		waitUntil(maState("review")+` === 'selected:no-agent,reviewer'`),
		chromedp.Evaluate(maState("architect"), &state),
	)
	if state != "all:" {
		t.Errorf("architect row = %q, want all:", state)
	}

	b.run("a Selected row that allows nothing says so",
		maClick("test", "Selected agents"),
		waitUntil(`!!document.querySelector(`+quote(maRow("test")+` [data-mode-agents-empty]`)+`)`),
	)
	var warning string
	b.run("the warning is plain",
		chromedp.Evaluate(`document.querySelector(`+quote(maRow("test")+` [data-mode-agents-empty]`)+`).innerText.replace(/\s+/g, ' ').trim()`, &warning),
		bothSchemes("models-mode-agents-empty"),
	)
	if !strings.Contains(warning, "Nothing can start in Tester mode") {
		t.Errorf("warning = %q", warning)
	}

	b.run("switch Reviewer back to All agents and save",
		maClick("review", "All agents"),
		maClick("review", "Save"),
		waitUntil(`document.querySelector(`+quote(maRow("review"))+`).textContent.includes('Saved')`),
	)
	if got, want := maGet(t, w, "review"), `"mode":"review","name":"Reviewer","state":"all","agents":[],"no_agent":true`; got != want {
		t.Errorf("stored after All = %s, want %s", got, want)
	}

	// The server refuses the save: the row keeps the person's choice and the banner says why.
	var banner string
	b.run("a refused save shows the server's message in the banner",
		maClick("design", "Selected agents"),
		maTick("design", "designer"),
		maRefuseSaves(`{"error":"unknown agent: designer","code":"UNKNOWN_REFERENCE"}`),
		maClick("design", "Save"),
		waitUntil(`!!document.querySelector('[data-mode-agents] [data-mode-agents-error]')`),
		chromedp.Evaluate(`document.querySelector('[data-mode-agents] [data-mode-agents-error]').innerText.replace(/\s+/g, ' ').trim()`, &banner),
		chromedp.Evaluate(maState("design"), &state),
		bothSchemes("models-mode-agents-refused"),
		fetch.Disable(), // the interception would hold the page's event stream back
	)
	if !strings.Contains(banner, "unknown agent: designer") {
		t.Errorf("banner = %q", banner)
	}
	if state != "selected:designer" {
		t.Errorf("the refused row = %q, want the person's choice kept", state)
	}

	// Another page changes the setting and the catalog: the open section re-reads.
	b.run("the section follows the feed", waitUntil(`document.querySelector('[data-mode-agents]').dataset.feed === 'live'`))
	maSetMode(t, w, `{"mode":"qa","state":"selected","agents":["backend"],"no_agent":false}`)
	b.run("a change made elsewhere updates the open section",
		waitUntil(maState("qa")+` === 'selected:backend'`),
	)
	res, err := w.srv.Client().Post(w.srv.URL+"/api/delete_catalog_agent", "application/json", strings.NewReader(`{"name":"backend"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	b.run("a catalog change re-reads the agents the rows offer",
		waitUntil(`!document.querySelector(`+quote(maRow("qa")+` label[data-agent="backend"]`)+`)`),
	)

	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}

// TestAnAgentShowsAndEditsTheModesItStartsIn_EndToEnd covers stories 7 and 8:
// the detail's "Available in modes" lists the modes the agent may start in;
// the editor ticks the Selected modes that list it, shows the All agents modes
// as included and fixed with a pointer to the Models page, and saving puts the
// agent on exactly those lists.
func TestAnAgentShowsAndEditsTheModesItStartsIn_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", maCatalog)
	// Reviewer mode lists reviewer only; Design mode lists designer only.
	maSetMode(t, w, `{"mode":"review","state":"selected","agents":["reviewer"],"no_agent":false}`)
	maSetMode(t, w, `{"mode":"design","state":"selected","agents":["designer"],"no_agent":false}`)

	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	chips := `[...document.querySelectorAll('[data-available-in-modes] [data-mode-chip]')].map(c => c.textContent.trim()).join(',')`
	row := func(mode string) string { return `[data-mode-row][data-mode="` + mode + `"]` }
	checked := func(mode string) string { return `document.querySelector('` + row(mode) + ` input').checked` }
	disabled := func(mode string) string { return `document.querySelector('` + row(mode) + ` input').disabled` }
	save := `document.querySelector('[data-save-footer] [data-save]')`

	b := newTLSBrowser(t)
	b.run("the detail lists the modes the agent may start in",
		chromedp.EmulateViewport(1280, 1200),
		chromedp.Navigate(w.srv.URL+"/agents/designer"),
		waitUntil(flat(`[data-available-in-modes] h3`)+` === 'Available in modes'`),
		waitUntil(chips+` === 'Default,Architect,Implement,Design,Product,Design system,Tester,QA,Operator'`),
		waitUntil(`document.querySelector('[data-change=modes]').getAttribute('href') === '/agents/designer/edit#modes'`),
		bothSchemes("agent-available-in-modes"),
	)
	b.run("an agent a Selected mode lists shows that mode",
		chromedp.Navigate(w.srv.URL+"/agents/reviewer"),
		waitUntil(chips+` === 'Default,Architect,Implement,Product,Design system,Reviewer,Tester,QA,Operator'`),
	)
	b.run("the editor fixes the All agents modes, ticks the Selected one that lists the agent and leaves the other free",
		chromedp.Navigate(w.srv.URL+"/agents/designer/edit#modes"),
		waitUntil(`document.querySelectorAll('[data-mode-row]').length === 10`),
		waitUntil(`document.querySelector('[data-section=modes]') !== null`),
		waitUntil(checked("architect")+` && `+disabled("architect")),
		waitUntil(checked("design")+` && !`+disabled("design")),
		waitUntil(`!`+checked("review")+` && !`+disabled("review")),
		waitUntil(`document.querySelector('[data-modes-hint] a').getAttribute('href') === '/models'`),
		waitUntil(save+`.disabled === true`),
		bothSchemes("agent-editor-modes"),
	)
	b.run("an All agents mode cannot be unticked",
		chromedp.Evaluate(`document.querySelector('`+row("architect")+` input').click()`, nil),
		waitUntil(checked("architect")+` && `+save+`.disabled === true`),
	)
	b.run("tick Reviewer and untick Design, then save",
		waitUntil(`(() => { const i = document.querySelector('`+row("review")+` input'); if (!i || i.disabled) return false; i.click(); return true; })()`),
		waitUntil(`(() => { const i = document.querySelector('`+row("design")+` input'); if (!i || i.disabled) return false; i.click(); return true; })()`),
		waitUntil(save+`.disabled === false`),
		clickButton(`document.querySelector('[data-save-footer]')`, "Save agent"),
		atPath(w.srv.URL+"/agents/designer"),
		waitUntil(chips+` === 'Default,Architect,Implement,Product,Design system,Reviewer,Tester,QA,Operator'`),
	)
	if got := maGet(t, w, "review"); !strings.Contains(got, `"agents":["reviewer","designer"]`) && !strings.Contains(got, `"agents":["designer","reviewer"]`) {
		t.Errorf("review mode after the save = %s, want reviewer and designer", got)
	}
	if got := maGet(t, w, "design"); !strings.Contains(got, `"state":"selected","agents":[]`) {
		t.Errorf("design mode after the save = %s, want an empty Selected list", got)
	}
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}

// TestTheNewSessionFormOffersOnlyTheModesAgents_EndToEnd covers stories 9 to
// 13: the agent picker offers the agents the chosen mode allows (within the
// project's), Plain claude only when the mode allows no agent, a picked agent
// the new mode does not allow is cleared with the reason, a mode that allows
// nothing says so instead of showing an empty picker, and a start the server
// refuses for the mode shows its message.
func TestTheNewSessionFormOffersOnlyTheModesAgents_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", nsAgentsCatalog)
	nsAddCatalogAgents(t, w)
	maSetMode(t, w, `{"mode":"review","state":"selected","agents":["reviewer"],"no_agent":false}`)
	maSetMode(t, w, `{"mode":"test","state":"selected","agents":["backend"],"no_agent":true}`)
	maSetMode(t, w, `{"mode":"qa","state":"selected","agents":[],"no_agent":false}`)

	const (
		picker    = `[...document.querySelectorAll('#new-session-agent option')].map(o => o.textContent.trim()).sort().join('|')`
		agentNote = `(document.querySelector(` + "`" + nsForm + ` [data-agent-note]` + "`" + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
		nothing   = nsForm + ` [data-mode-allows-nothing]`
	)
	setMode := func(mode string) chromedp.Action {
		return chromedp.Evaluate(`(() => { const s = document.querySelector('#new-session-mode'); s.value = `+quote(mode)+`; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil)
	}
	pick := func(agent string) chromedp.Action {
		return chromedp.Evaluate(`(() => { const s = document.querySelector('#new-session-agent'); s.value = [...s.options].find(o => o.textContent.trim() === `+quote(agent)+`).value; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil)
	}
	b := newTLSBrowser(t)
	var got string
	b.run("a mode on All agents offers every enabled agent and Plain claude",
		chromedp.EmulateViewport(1280, 1100),
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length > 1`),
		waitUntil(nsLive),
		chromedp.Evaluate(picker, &got),
	)
	if want := "Plain claude|backend|designer|reviewer|writer"; got != want {
		t.Errorf("default mode picker = %q, want %q", got, want)
	}

	b.run("a Selected mode offers only its agents, and no Plain claude without No agent",
		setMode("review"),
		waitUntil(`(`+picker+`) === 'Pick an agent|reviewer'`),
		bothSchemes("new-session-mode-selected"),
	)
	b.run("a Selected mode with No agent offers its agents and Plain claude",
		setMode("test"),
		waitUntil(`(`+picker+`) === 'Plain claude|backend'`),
	)

	b.run("a picked agent the new mode does not allow is cleared, with the reason",
		setMode(""),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length === 5`),
		pick("designer"),
		setMode("review"),
		waitUntil(`document.querySelector('#new-session-agent').value === ''`),
		waitUntil(`!!document.querySelector(`+quote(nsForm+` [data-agent-note]`)+`)`),
		chromedp.Evaluate(agentNote, &got),
		bothSchemes("new-session-mode-cleared"),
	)
	if !strings.Contains(got, "designer cannot start in Reviewer mode") {
		t.Errorf("note = %q", got)
	}

	var startDisabled bool
	b.run("a mode that allows nothing says so, with no empty picker, and Start is off",
		setMode("qa"),
		waitUntil(`!!document.querySelector(`+quote(nothing)+`)`),
		waitUntil(`document.querySelector('`+nsForm+` .dropdown')?.checkVisibility() === false`),
		chromedp.Evaluate(`document.querySelector('`+nsForm+` button[type=submit]').disabled`, &startDisabled),
		chromedp.Evaluate(`document.querySelector(`+quote(nothing)+`).innerText.replace(/\s+/g, ' ').trim()`, &got),
		bothSchemes("new-session-mode-nothing"),
	)
	if !startDisabled {
		t.Error("Start is enabled in a mode that allows nothing")
	}
	if !strings.Contains(got, "No agent can start in QA mode") {
		t.Errorf("warning = %q", got)
	}

	// The mode's list changes after the form was painted: the server refuses the start.
	const refusal = `(document.querySelector('` + nsForm + ` [data-agent-refusal]')?.innerText ?? '').replace(/\s+/g, ' ').trim()`
	var prompt string
	b.run("choose reviewer in Reviewer mode and type a prompt",
		setMode("review"),
		waitUntil(`(`+picker+`) === 'Pick an agent|reviewer'`),
		setField(`#new-session-prompt`, "Review the branch"),
		pick("reviewer"),
	)
	maSetMode(t, w, `{"mode":"review","state":"selected","agents":["backend"],"no_agent":false}`)
	b.run("the refusal shows its message and keeps the form",
		nsClick(`main form`, "Start session"),
		waitUntil(`!!document.querySelector('`+nsForm+` [data-agent-refusal]')`),
		chromedp.Evaluate(refusal, &got),
		chromedp.Evaluate(`document.querySelector('#new-session-prompt').value`, &prompt),
		bothSchemes("new-session-mode-refused"),
	)
	if !strings.Contains(got, "AGENT_NOT_AVAILABLE_IN_MODE") || !strings.Contains(got, "reviewer") || !strings.Contains(got, "Reviewer") {
		t.Errorf("banner = %q", got)
	}
	if prompt != "Review the branch" {
		t.Errorf("the prompt was not kept: %q", prompt)
	}
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}
