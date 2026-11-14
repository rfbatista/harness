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
