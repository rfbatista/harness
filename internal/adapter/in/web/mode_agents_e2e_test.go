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
