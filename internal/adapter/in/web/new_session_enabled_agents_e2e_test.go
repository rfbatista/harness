//go:build !windows

package web_test

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

const nsAgentsCatalog = `{"agents": {
  "designer": {"description": "Designs screens."},
  "reviewer": {"description": "Reviews code."},
  "backend": {"description": "APIs and services."},
  "writer": {}
}}
`

// nsAddCatalogAgents puts the catalog's agents in the database the picker reads, as the server does.
func nsAddCatalogAgents(t *testing.T, w *tokenSaverWorld) {
	t.Helper()
	for _, n := range []string{"designer", "reviewer", "backend", "writer"} {
		desc := map[string]string{"designer": "Designs screens.", "reviewer": "Reviews code.", "backend": "APIs and services."}[n]
		if _, err := w.cat.Agents.CreateAgent(w.ctx, n, desc, "", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
}

// nsClick clicks the enabled button under scope (a selector) whose text starts with label.
func nsClick(scope, label string) chromedp.Action {
	return waitUntil(`(() => { const b = [...document.querySelectorAll(` + quote(scope+" button") + `)].find(b => b.textContent.trim().startsWith(` + quote(label) + `) && !b.disabled);
		if (!b) return false; b.click(); return true; })()`)
}

const (
	nsOptions  = `[...document.querySelectorAll('#new-session-agent option')].map(o => o.textContent.trim()).filter(t => t !== 'Plain claude').sort().join(',')`
	nsForm     = `form[x-data^=sessionsNewSession]`
	nsHint     = `(document.querySelector('` + nsForm + ` [data-agents-hint]')?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	nsCount    = `(document.querySelector('` + nsForm + ` [data-agents-count]')?.textContent ?? '').trim()`
	nsLive     = `document.querySelector('.stream-bar [data-state="live"]') !== null`
	nsHintLink = `document.querySelector('` + nsForm + ` [data-agents-hint] a')?.getAttribute('href')`
)

// TestTheNewSessionPickerOffersTheProjectsEnabledAgentsOnly_EndToEnd is W1 for
// stories 3 and 4 of the addendum to spec "Web UI — the Agents page", against a
// real server and a temporary catalog: a project on Selected agents narrows
// the new-session picker to its list, with the count, the hint and the link;
// All agents lists every agent again, and says new ones appear on their own;
// the list changing while the form is open refreshes the picker; a chosen
// agent taken off the list is refused with the banner, the form kept filled in
// and no agent selected; and an empty list replaces the picker with the
// warning and turns Start off.
func TestTheNewSessionPickerOffersTheProjectsEnabledAgentsOnly_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", nsAgentsCatalog)
	nsAddCatalogAgents(t, w)
	pid := w.project.ID
	settings := "/projects/" + pid + "/settings#settings-agents-enabled"
	b := newTLSBrowser(t)

	enabledInSetAgents(t, w, pid, "selected", "reviewer", "backend")
	var options, hint, count, link string
	b.run("Selected agents narrow the picker, with the count, the hint and the link",
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length > 1`),
		waitUntil(nsLive),
		chromedp.Evaluate(nsOptions, &options),
		chromedp.Evaluate(nsHint, &hint),
		chromedp.Evaluate(nsCount, &count),
		chromedp.Evaluate(nsHintLink, &link),
		bothSchemes("new-session-picker-selected"),
	)
	if options != "backend,reviewer" {
		t.Errorf("options = %q; the disabled agents must be absent", options)
	}
	if want := "Showing the 2 agents enabled in harness of 4 in the catalog. Change in project settings"; hint != want {
		t.Errorf("hint = %q, want %q", hint, want)
	}
	if count != "2 enabled" {
		t.Errorf("count = %q", count)
	}
	if link != settings {
		t.Errorf("settings link = %q, want %q", link, settings)
	}

	// The list changes while the form is open: the picker follows.
	enabledInSetAgents(t, w, pid, "selected", "reviewer", "backend", "writer", "designer")
	b.run("the picker refreshes while the form is open",
		waitUntil(`(`+nsOptions+`) === 'backend,designer,reviewer,writer'`),
		chromedp.Evaluate(nsHint, &hint),
	)
	if want := "Showing the 4 agents enabled in harness of 4 in the catalog. Change in project settings"; hint != want {
		t.Errorf("hint after the refresh = %q, want %q", hint, want)
	}

	// All agents lists every agent, and the hint says new ones appear.
	enabledInSetAgents(t, w, pid, "all")
	b.run("All agents lists every agent",
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length === 5`),
		chromedp.Evaluate(nsHint, &hint),
		bothSchemes("new-session-picker-all"),
	)
	if want := "All agents are enabled in harness: every agent in the catalog is offered, and new agents appear automatically. Change in project settings"; hint != want {
		t.Errorf("All-agents hint = %q, want %q", hint, want)
	}

	// The chosen agent leaves the list while the form is open; the person starts anyway.
	enabledInSetAgents(t, w, pid, "selected", "reviewer", "backend", "designer")
	const refusal = `(document.querySelector('` + nsForm + ` [data-agent-refusal]')?.innerText ?? '').replace(/\s+/g, ' ').trim()`
	var banner, chosen, prompt, href string
	var optionsAfter string
	b.run("choose designer, type a prompt, then take designer off the list",
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length === 4`),
		waitUntil(nsLive),
		setField(`#new-session-prompt`, "Sketch the settings page"),
		chromedp.Evaluate(`(() => { const s = document.querySelector('#new-session-agent'); s.value = 'designer'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
	)
	enabledInSetAgents(t, w, pid, "selected", "reviewer", "backend")
	b.run("the refusal shows the banner and keeps the form",
		waitUntil(`document.querySelector('#new-session-agent').value === 'designer' && (`+nsOptions+`).includes('designer (no longer enabled)')`),
		nsClick(`main form`, "Start session"),
		waitUntil(`!!document.querySelector('`+nsForm+` [data-agent-refusal]')`),
		chromedp.Evaluate(refusal, &banner),
		chromedp.Evaluate(`document.querySelector('#new-session-agent').value`, &chosen),
		chromedp.Evaluate(`document.querySelector('#new-session-prompt').value`, &prompt),
		chromedp.Evaluate(`document.querySelector('[data-agent-refusal-link]').getAttribute('href')`, &href),
		chromedp.Evaluate(nsOptions, &optionsAfter),
		bothSchemes("new-session-agent-not-enabled"),
	)
	for _, want := range []string{
		"The session was not started: designer is not enabled in harness.",
		"AGENT_NOT_ENABLED",
		"Open harness's Agents settings",
		"Pick another agent",
	} {
		if !strings.Contains(banner, want) {
			t.Errorf("banner = %q, missing %q", banner, want)
		}
	}
	if chosen != "" {
		t.Errorf("agent still selected: %q", chosen)
	}
	if prompt != "Sketch the settings page" {
		t.Errorf("the prompt was not kept: %q", prompt)
	}
	if href != settings {
		t.Errorf("banner link = %q, want %q", href, settings)
	}
	if optionsAfter != "backend,reviewer" {
		t.Errorf("options after the refusal = %q", optionsAfter)
	}
	b.run("Pick another agent clears the banner",
		nsClick(nsForm+` [data-agent-refusal]`, "Pick another agent"),
		waitUntil(`!document.querySelector('`+nsForm+` [data-agent-refusal]')`),
	)

	// Nothing enabled: the warning replaces the picker and Start is off.
	enabledInSetAgents(t, w, pid, "selected")
	var startDisabled bool
	var warning string
	b.run("an empty list replaces the picker with the warning",
		waitUntil(`!!document.querySelector('`+nsForm+` [data-no-agent-enabled]')`),
		chromedp.Evaluate(`document.querySelector('`+nsForm+` button[type=submit]').disabled`, &startDisabled),
		chromedp.Evaluate(`document.querySelector('`+nsForm+` [data-no-agent-enabled]').innerText.replace(/\s+/g, ' ').trim()`, &warning),
		waitUntil(`document.querySelector('`+nsForm+` .dropdown')?.checkVisibility() === false`),
		bothSchemes("new-session-picker-none"),
	)
	if !startDisabled {
		t.Error("Start is enabled with no agent to pick")
	}
	if !strings.Contains(warning, "No agent can be picked in this project.") || !strings.Contains(warning, "Open harness's Agents settings") {
		t.Errorf("warning = %q", warning)
	}
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}

// TestTheBrainstormDialogOffersTheProjectsEnabledAgentsOnly_EndToEnd: the
// board's "Brainstorm a feature" dialog narrows its agent picker like the
// new-session form, and refuses the same way when the project's list changed.
func TestTheBrainstormDialogOffersTheProjectsEnabledAgentsOnly_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", nsAgentsCatalog)
	nsAddCatalogAgents(t, w)
	pid := w.project.ID
	enabledInSetAgents(t, w, pid, "selected", "reviewer", "backend")
	const options = `[...document.querySelectorAll('#brainstorm-agent option')].map(o => o.textContent.trim()).filter(t => t !== 'Plain claude').sort().join(',')`
	var got, hint, banner string
	b := newTLSBrowser(t)
	b.run("the dialog lists the enabled agents with the hint",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+pid),
		waitUntil(`document.querySelectorAll('#brainstorm-agent option').length === 3`),
		waitUntil(`document.querySelector('.stream-bar [data-state="live"]') !== null`),
		chromedp.Evaluate(options, &got),
		chromedp.Evaluate(`document.querySelector('#brainstorm [data-agents-hint]').textContent.replace(/\s+/g, ' ').trim()`, &hint),
	)
	if got != "backend,reviewer" {
		t.Errorf("options = %q", got)
	}
	if want := "Showing the 2 agents enabled in harness of 4 in the catalog. Change in project settings"; hint != want {
		t.Errorf("hint = %q, want %q", hint, want)
	}
	b.run("the agent leaves the list while the dialog is closed, then Start is refused",
		nsClick(`body`, "Brainstorm a feature"),
		waitUntil(`document.querySelector('#brainstorm').matches(':popover-open')`),
		chromedp.Evaluate(`(() => { const s = document.querySelector('#brainstorm-agent'); s.value = 'reviewer'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
	)
	enabledInSetAgents(t, w, pid, "selected", "backend")
	b.run("the banner shows in the dialog",
		waitUntil(`(`+options+`).includes('reviewer (no longer enabled)')`),
		nsClick(`#brainstorm`, "Start brainstorm"),
		waitUntil(`!!document.querySelector('#brainstorm [data-agent-refusal]')`),
		chromedp.Evaluate(`document.querySelector('#brainstorm [data-agent-refusal]').innerText.replace(/\s+/g, ' ').trim()`, &banner),
	)
	if !strings.Contains(banner, "The session was not started: reviewer is not enabled in harness.") {
		t.Errorf("banner = %q", banner)
	}
}
