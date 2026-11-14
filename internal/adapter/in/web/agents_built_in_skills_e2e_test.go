//go:build !windows

package web_test

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// skipUnlessServerListsBuiltInSkills reads the catalog from the test server and
// skips the test while its skills[] carry no built_in field: the server side of
// ADR 0088 (spec "Harness server — built-in skills in the catalog") has not
// landed on this branch yet.
func skipUnlessServerListsBuiltInSkills(t *testing.T, w *tokenSaverWorld) {
	t.Helper()
	res, err := w.srv.Client().Get(w.srv.URL + "/api/get_agent_catalog")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, s := range body.Skills {
		if _, ok := s["built_in"]; ok {
			return
		}
	}
	t.Skip("server does not list built-in skills yet (server spec: built-in skills in the catalog)")
}

// TestSkillsTabListsBuiltInSkillsWithTheirBadgeAndModes_EndToEnd is W1 for
// stories 1, 2 and 8 of spec "Web UI — built-in skills on the Agents page",
// against a real server: a built-in skill is listed beside the catalog's with
// its Built-in badge and its modes, the text filter finds it, and the footer
// count includes it.
func TestSkillsTabListsBuiltInSkillsWithTheirBadgeAndModes_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	skipUnlessServerListsBuiltInSkills(t, w)
	listsBuiltInSkills(t, w, nil)
}

// TestSkillsTabListsBuiltInSkillsFromTheFakeCatalog_EndToEnd is the same
// scenario against the page alone: the server's catalog answer gains built-in
// skills in the contract's wire format, so it runs before the server lists them.
func TestSkillsTabListsBuiltInSkillsFromTheFakeCatalog_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	listsBuiltInSkills(t, w, &builtInFake{catalog: func(cat map[string]any) {
		cat["skills"] = append(cat["skills"].([]any), builtInSkill("task-run-app", "Runs the task's application.", "architect"))
	}})
}

func listsBuiltInSkills(t *testing.T, w *tokenSaverWorld, fake *builtInFake) {
	t.Helper()
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")

	const rows = `[...document.querySelectorAll('[data-skill-row]')].map(r => r.dataset.key)`
	const runApp = `document.querySelector('[data-skill-row][data-key="task-run-app"]')`
	const filterBox = `input[type=search][aria-label="Filter skills"]`

	b := newTLSBrowser(t)
	if fake != nil {
		fake.install(t, b)
	}
	b.run("the Skills tab lists a built-in skill beside the catalog's, with its badge and modes",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/skills"),
		waitUntil(rows+`.includes('lint') && `+rows+`.includes('task-run-app')`),
		waitUntil(runApp+`.querySelector('[data-built-in]')?.checkVisibility() === true`),
		waitUntil(runApp+`.querySelector('[data-built-in]').textContent.trim() === 'Built-in'`),
		waitUntil(runApp+`.querySelector('[data-modes]').textContent.includes('architect')`),
		waitUntil(`document.querySelector('[data-skill-row][data-key="lint"] [data-built-in]')?.checkVisibility() !== true`),
	)
	b.run("the footer counts every row, built-in skills included",
		waitUntil(`document.querySelector('[data-skill-foot]').textContent.trim() === 'skills/ · ' + `+rows+`.length + ' folders'`),
	)
	b.run("the text filter finds a built-in skill by its name",
		setField(filterBox, "task-run-app"),
		waitUntil(rows+`.join() === 'task-run-app'`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestABuiltInSkillOpensReadOnly_EndToEnd is W1 for stories 3 and 4 of the
// spec, against a real server: a built-in skill opens with its body and files,
// says it ships with the server, and offers no Edit, Rename or Delete.
func TestABuiltInSkillOpensReadOnly_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	skipUnlessServerListsBuiltInSkills(t, w)
	opensReadOnly(t, w, nil)
}

// TestABuiltInSkillOpensReadOnlyFromTheFakeCatalog_EndToEnd is the same
// scenario against the page alone.
func TestABuiltInSkillOpensReadOnlyFromTheFakeCatalog_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	listed := builtInSkill("task-run-app", "Runs the task's application.", "architect")
	listed["agents"], listed["agent_count"] = []string{"dev"}, 1
	body := builtInSkillBody("task-run-app", "Runs the task's application.", "# Run the app\n\nStart it.\n", "architect")
	body["skill"].(map[string]any)["agents"], body["skill"].(map[string]any)["agent_count"] = []string{"dev"}, 1
	opensReadOnly(t, w, &builtInFake{
		catalog: func(cat map[string]any) { cat["skills"] = append(cat["skills"].([]any), listed) },
		skills:  map[string]map[string]any{"task-run-app": body},
	})
}

func opensReadOnly(t *testing.T, w *tokenSaverWorld, fake *builtInFake) {
	t.Helper()
	writeCatalogFile(t, w, "agents.json", `{"agents": {"dev": {"description": "Builds.", "skills": ["task-run-app"]}}}`)
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")

	const detail = `document.querySelector('[data-skill-detail]')`
	b := newTLSBrowser(t)
	if fake != nil {
		fake.install(t, b)
	}
	b.run("opening a built-in skill shows its body, files and the line that it ships with the server",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/skills/task-run-app"),
		waitUntil(detail+`?.querySelector('h2').textContent.trim() === 'task-run-app'`),
		waitUntil(detail+`.querySelector('[data-built-in]')?.checkVisibility() === true`),
		waitUntil(detail+`.querySelector('[data-skill-body]')?.textContent.length > 0`),
		waitUntil(detail+`.querySelector('[data-built-in-note]')?.checkVisibility() === true && `+detail+`.querySelector('[data-built-in-note]').textContent.includes('Ships with the server')`),
		waitUntil(detail+`.querySelector('[data-built-in-note]').textContent.includes('architect')`),
		waitUntil(`document.querySelector('[data-skill-row][data-key="task-run-app"]').getAttribute('aria-current') === 'true'`),
	)
	b.run("it offers no Edit, no Rename and no Delete, and Duplicate stays",
		waitUntil(detail+`.querySelector('a[data-edit]')?.checkVisibility() !== true`),
		chromedp.Click(`[data-skill-detail] .menu > button`, chromedp.ByQuery),
		waitUntil(detail+`.querySelector('[data-action=delete]')?.checkVisibility() !== true`),
		waitUntil(detail+`.querySelector('a[data-duplicate]')?.checkVisibility() === true`),
		waitUntil(detail+`.querySelector('a[data-duplicate]').getAttribute('href') === '/agents/skills/new?from=task-run-app'`),
	)
	b.run("its reach names the agents that have it",
		waitUntil(`[...`+detail+`.querySelectorAll('[data-reach="agents"] a')].map(a => a.textContent.trim()).join() === 'dev'`),
		waitUntil(detail+`.querySelector('[data-reach="total"]').textContent.trim() === '1 agent in total'`),
	)
	b.run("the edit address of a built-in skill lands on its read-only detail",
		chromedp.Navigate(w.srv.URL+"/agents/skills/task-run-app/edit"),
		atPath(w.srv.URL+"/agents/skills/task-run-app"),
		waitUntil(detail+`?.querySelector('[data-built-in-note]')?.checkVisibility() === true`),
	)
	b.run("a catalog skill keeps Edit and Delete",
		chromedp.Navigate(w.srv.URL+"/agents/skills/lint"),
		waitUntil(detail+`?.querySelector('a[data-edit]')?.checkVisibility() === true`),
		waitUntil(detail+`.querySelector('[data-built-in-note]')?.checkVisibility() !== true`),
		waitUntil(detail+`.querySelector('[data-built-in]')?.checkVisibility() !== true`),
	)
	b.run("Duplicate opens the new-skill form copied from it, with a name of its own to choose (last: a typed name makes the page ask before it is left)",
		chromedp.Navigate(w.srv.URL+"/agents/skills/new?from=task-run-app"),
		waitUntil(`document.querySelector('#sf-name') !== null && document.querySelector('#sf-name').value === ''`),
		setField(`#sf-name`, "task-run-app"),
		waitUntil(`document.querySelector('#sf-name-error')?.checkVisibility() === true`),
		setField(`#sf-name`, "my-run-app"),
		waitUntil(`document.querySelector('#sf-name-error')?.checkVisibility() !== true`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
