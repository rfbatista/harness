//go:build !windows

package web_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// enabledInSetAgents sets a project's enabled agents through the API, as the
// project's settings page would.
func enabledInSetAgents(t *testing.T, w *tokenSaverWorld, projectID, mode string, agents ...string) {
	t.Helper()
	list := make([]string, len(agents))
	for i, a := range agents {
		list[i] = `"` + a + `"`
	}
	body := `{"project_id":"` + projectID + `","mode":"` + mode + `","agents":[` + strings.Join(list, ",") + `]}`
	res, err := w.srv.Client().Post(w.srv.URL+"/api/set_project_agents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if raw, _ := io.ReadAll(res.Body); res.StatusCode != 200 {
		t.Fatalf("set_project_agents %s: %d %s", body, res.StatusCode, raw)
	}
}

// enabledInSaved is what the server has saved for a project, as "mode:m agents: a b".
func enabledInSaved(t *testing.T, w *tokenSaverWorld, projectID string) string {
	t.Helper()
	res, err := w.srv.Client().Get(w.srv.URL + "/api/get_project_agents?project_id=" + projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return strings.Join(strings.Fields(strings.NewReplacer(`"`, "", "{", " ", "}", " ", "[", " ", "]", " ", ",", " ").Replace(string(raw))), " ")
}

// TestAnAgentIsEnabledPerProjectFromItsDetailEditorAndDeleteDialog_EndToEnd is
// W1 for story 2 of the addendum to spec "Web UI — the Agents page", against a
// real server and a temporary catalog: an agent's detail shows Enabled in (an
// All-agents project, and a hint naming the Selected projects that do not list
// it); the editor's Projects section fixes the All-agents row and ticks the
// Selected ones; Review change shows each project's list; the save puts the
// agent on those lists, and the detail and the project's settings show it; the
// delete dialog lists those projects and says when one is left empty; and a
// project's list changing elsewhere refreshes the detail, down to the
// enabled-nowhere banner.
func TestAnAgentIsEnabledPerProjectFromItsDetailEditorAndDeleteDialog_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{"agents": {
  "designer": {"description": "Designs screens."},
  "backend": {"description": "APIs and services."}
}}
`)
	// harness uses All agents; atlas-api lists backend; lab lists nothing.
	atlas, err := w.cat.Projects.CreateProject(w.ctx, "atlas-api", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lab, err := w.cat.Projects.CreateProject(w.ctx, "lab", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enabledInSetAgents(t, w, atlas.ID, "selected", "backend")
	enabledInSetAgents(t, w, lab.ID, "selected")

	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	rows := `[...document.querySelectorAll('[data-enabled-row]')].map(r => r.querySelector('[data-project-link]').textContent + ':' + r.dataset.mode).join()`
	row := func(name string) string { return `[data-project-row][data-name="` + name + `"]` }
	checked := func(name string) string { return `document.querySelector('` + row(name) + ` input').checked` }
	tickProject := func(name string) chromedp.Action {
		return waitUntil(`(() => { const i = document.querySelector('` + row(name) + ` input'); if (!i || i.disabled) return false; i.click(); return true; })()`)
	}
	save := `document.querySelector('[data-save-footer] [data-save]')`
	setPath := "/projects/" + w.project.ID + "/settings"

	b := newTLSBrowser(t)
	b.run("the detail shows Enabled in: the All-agents project linking to its settings, and a hint naming the Selected projects that do not list the agent",
		chromedp.EmulateViewport(1280, 1100),
		chromedp.Navigate(w.srv.URL+"/agents/designer"),
		waitUntil(flat(`[data-enabled-heading]`)+` === 'Enabled in · 1 of 3 projects'`),
		waitUntil(rows+` === 'harness:all'`),
		waitUntil(flat(`[data-enabled-row] .badge`)+` === 'All agents'`),
		waitUntil(flat(`[data-enabled-row]`)+`.includes('Enabled automatically')`),
		waitUntil(`document.querySelector('[data-enabled-row] [data-project-link]').getAttribute('href') === `+quote(setPath)),
		waitUntil(flat(`[data-enabled-hint]`)+` === 'Not enabled in atlas-api and lab (Selected agents).'`),
		waitUntil(`!`+visible(`[data-enabled-nowhere]`)),
		waitUntil(`document.querySelector('[data-change=projects]').getAttribute('href') === '/agents/designer/edit#projects'`),
		bothSchemes("agent-enabled-in"),
	)
	b.run("a Selected project that lists the agent shows with its own badge",
		chromedp.Navigate(w.srv.URL+"/agents/backend"),
		waitUntil(flat(`[data-enabled-heading]`)+` === 'Enabled in · 2 of 3 projects'`),
		waitUntil(rows+` === 'atlas-api:selected,harness:all'`),
		waitUntil(flat(`[data-enabled-row][data-mode=selected] .badge`)+` === 'Selected agents'`),
	)
	b.run("the editor's Projects section fixes the All-agents row, and ticks the Selected ones from nothing",
		chromedp.Navigate(w.srv.URL+"/agents/designer/edit#projects"),
		waitUntil(visible(`[data-agent-editor]`)),
		waitUntil(`document.querySelectorAll('[data-project-row]').length === 3`),
		waitUntil(`[...document.querySelectorAll('[data-project-row]')].map(r => r.dataset.name).join() === 'atlas-api,harness,lab'`),
		waitUntil(`document.querySelector('[data-section=projects]')?.textContent.replace(/\s+/g, ' ').trim() === 'Projects 1'`),
		waitUntil(`document.querySelector('`+row("harness")+` input').disabled === true && `+checked("harness")),
		waitUntil(flat(row("harness")+` .badge`)+` === 'All agents'`),
		waitUntil(`document.querySelector('`+row("harness")+` [data-project-settings]').getAttribute('href') === `+quote(setPath)),
		waitUntil(`!`+checked("atlas-api")+` && !`+checked("lab")+` && document.querySelector('`+row("atlas-api")+` input').disabled === false`),
		waitUntil(flat(`[data-projects-summary]`)+` === 'Enabled in 1 of 3 projects · 1 by All agents, 0 Selected-agents lists'`),
		waitUntil(save+`.disabled === true`),
		bothSchemes("agent-editor-projects"),
	)
	b.run("the All-agents row cannot be unticked",
		chromedp.Evaluate(`document.querySelector('`+row("harness")+` input').click()`, nil),
		waitUntil(checked("harness")+` && `+save+`.disabled === true`),
	)
	b.run("ticking two Selected projects counts in the summary, the contents list and the footer, and Review change shows each project's list",
		tickProject("atlas-api"),
		tickProject("lab"),
		waitUntil(flat(`[data-projects-summary]`)+` === 'Enabled in 3 of 3 projects · 1 by All agents, 2 Selected-agents lists'`),
		waitUntil(`document.querySelector('[data-section=projects]')?.textContent.replace(/\s+/g, ' ').trim() === 'Projects 3'`),
		waitUntil(save+`.disabled === false`),
		waitUntil(`document.querySelector('[data-save-footer] [data-review]').disabled === false`),
		clickButton(`document.querySelector('[data-save-footer]')`, "Review change"),
		waitUntil(visible(`[data-review-panel]`)),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-file]')].map(f => f.dataset.path).includes('atlas-api · enabled agents')`),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-file][data-path="atlas-api · enabled agents"] [data-diff-line]')].map(l => l.dataset.kind + ' ' + l.textContent.trim()).join('|') === 'add +designer|same backend'`),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-file][data-path="lab · enabled agents"] [data-diff-line]')].map(l => l.dataset.kind + ' ' + l.textContent.trim()).join('|') === 'add +designer'`),
		bothSchemes("agent-editor-projects-review"),
	)
	b.run("saving puts the agent on those lists and lands on its detail",
		clickButton(`document.querySelector('[data-save-footer]')`, "Save agent"),
		atPath(w.srv.URL+"/agents/designer"),
		waitUntil(flat(`[data-enabled-heading]`)+` === 'Enabled in · 3 of 3 projects'`),
		waitUntil(rows+` === 'atlas-api:selected,harness:all,lab:selected'`),
		waitUntil(`!`+visible(`[data-enabled-hint]`)),
	)
	if got := enabledInSaved(t, w, atlas.ID); got != "mode:selected agents: backend designer" {
		t.Fatalf("atlas-api: %s", got)
	}
	if got := enabledInSaved(t, w, lab.ID); got != "mode:selected agents: designer" {
		t.Fatalf("lab: %s", got)
	}
	b.run("the project's settings show the agent ticked",
		chromedp.Navigate(w.srv.URL+"/projects/"+atlas.ID+"/settings"),
		waitUntil(`document.querySelector('[data-agent-row][data-name=designer] input')?.checked === true`),
	)
	b.run("the delete dialog lists those projects, says lab is left empty, and that All-agents projects just stop offering the agent",
		chromedp.Navigate(w.srv.URL+"/agents/designer"),
		waitUntil(visible(`[data-agent-detail]`)),
		deleteMenu(`[data-agent-detail]`),
		waitUntil(visible(`[data-delete-dialog]`)),
		waitUntil(deleteGroup("projects")+` === 'atlas-api,lab'`),
		waitUntil(flat(`[data-delete-group-note]`)+`.startsWith('lab is left with no agent enabled')`),
		waitUntil(flat(`[data-delete-total]`)+` === 'Projects that use All agents (harness) just stop offering it.'`),
		waitUntil(flat(`[data-delete-note]`)+`.includes('The catalog is not in git')`),
		bothSchemes("agent-delete-projects"),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Cancel"),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
	)
	b.run("a project's list changing elsewhere refreshes the open detail",
		waitUntil(`document.querySelector('[data-agent-detail]') !== null`),
		chromedp.ActionFunc(func(_ context.Context) error {
			enabledInSetAgents(t, w, lab.ID, "selected", "backend")
			return nil
		}),
		waitUntil(flat(`[data-enabled-heading]`)+` === 'Enabled in · 2 of 3 projects'`),
		waitUntil(flat(`[data-enabled-hint]`)+` === 'Not enabled in lab (Selected agents).'`),
	)
	b.run("an agent enabled nowhere shows the attention banner with Enable in a project",
		chromedp.ActionFunc(func(_ context.Context) error {
			enabledInSetAgents(t, w, w.project.ID, "selected", "backend")
			enabledInSetAgents(t, w, atlas.ID, "selected", "backend")
			return nil
		}),
		waitUntil(flat(`[data-enabled-heading]`)+` === 'Enabled in · 0 of 3 projects'`),
		waitUntil(visible(`[data-enabled-nowhere]`)),
		waitUntil(flat(`[data-enabled-nowhere]`)+`.includes('Not enabled in any project.') && `+flat(`[data-enabled-nowhere]`)+`.includes('No session can start as designer')`),
		waitUntil(`document.querySelector('[data-enable-in-project]').getAttribute('href') === '/agents/designer/edit#projects'`),
		waitUntil(`!`+visible(`[data-enabled-table]`)),
		bothSchemes("agent-enabled-nowhere"),
	)
}
