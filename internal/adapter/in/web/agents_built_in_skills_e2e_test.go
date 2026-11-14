//go:build !windows

package web_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// TestPickersOfferBuiltInSkillsWithTheirBadge_EndToEnd is W1 for story 5 of
// the spec, against a real server: the agent editor's and the bundle editor's
// pickers offer a built-in skill with the Built-in badge, and a chosen one is
// not flagged as missing.
func TestPickersOfferBuiltInSkillsWithTheirBadge_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	skipUnlessServerListsBuiltInSkills(t, w)
	pickersOfferBuiltIns(t, w, nil)
}

// TestPickersOfferBuiltInSkillsFromTheFakeCatalog_EndToEnd is the same
// scenario against the page alone.
func TestPickersOfferBuiltInSkillsFromTheFakeCatalog_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	pickersOfferBuiltIns(t, w, &builtInFake{
		catalog: func(cat map[string]any) {
			cat["skills"] = append(cat["skills"].([]any), builtInSkill("task-run-app", "Runs the task's application.", "architect"))
		},
		// Whatever the page sends is recorded; the answer is a refusal so the page stays where it is.
		refuse: map[string]fakeRefusal{"save_catalog_agent": {status: 409, code: "STALE_VERSION", message: "stub"}},
	})
}

func pickersOfferBuiltIns(t *testing.T, w *tokenSaverWorld, fake *builtInFake) {
	t.Helper()
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")
	const option = `document.querySelector('#skillPicker-list [role=option][data-key="task-run-app"]')`
	const chip = `document.querySelector('[data-agent-editor] [data-chip="task-run-app"]')`

	b := newTLSBrowser(t)
	if fake != nil {
		fake.install(t, b)
	}
	b.run("the agent editor's picker offers a built-in skill with the badge, and a catalog skill without",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/new"),
		waitUntil(`document.querySelector('[data-agent-editor]')?.checkVisibility() === true`),
		chromedp.SendKeys(`#skillPicker-input`, "run-app", chromedp.ByQuery),
		waitUntil(option+`?.querySelector('[data-built-in]')?.checkVisibility() === true`),
		waitUntil(option+`.querySelector('[data-built-in]').textContent.trim() === 'Built-in'`),
		setField(`#skillPicker-input`, "lint"),
		waitUntil(`document.querySelector('#skillPicker-list [role=option][data-key="lint"]') !== null && document.querySelector('#skillPicker-list [data-built-in]')?.checkVisibility() !== true`),
	)
	b.run("choosing it makes a chip that is not missing, and the summary counts the skill",
		setField(`#skillPicker-input`, "run-app"),
		waitUntil(option+` !== null`),
		pressKey("Enter", 13),
		waitUntil(chip+` !== null && `+chip+`.getAttribute('data-missing') !== 'true'`),
		waitUntil(`document.querySelector('[data-skills-summary]').textContent.replace(/\s+/g, ' ').trim().startsWith('1 skill after bundles are expanded')`),
		waitUntil(`!document.querySelector('[data-skills-summary]').textContent.includes('missing')`),
	)
	if fake != nil {
		b.run("saving sends the built-in name in the entries unchanged",
			setField(`#agent-name`, "runner"),
			setField(`#agent-desc`, "Runs the app."),
			waitUntil(`document.querySelector('[data-save-footer] [data-save]').disabled === false`),
			chromedp.Click(`[data-save-footer] [data-save]`, chromedp.ByQuery),
			waitUntil(`true`),
		)
		var sent []string
		for i := 0; i < 50 && len(sent) == 0; i++ {
			time.Sleep(100 * time.Millisecond)
			sent = fake.sent()
		}
		if len(sent) == 0 || !strings.Contains(sent[0], `"task-run-app"`) {
			t.Fatalf("save_catalog_agent body = %v, want it to name task-run-app", sent)
		}
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestTheBundleEditorOffersBuiltInSkillsFromTheFakeCatalog_EndToEnd covers the
// bundle editor's picker: a built-in skill is offered with the badge. It runs
// against the page alone.
func TestTheBundleEditorOffersBuiltInSkillsFromTheFakeCatalog_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{"bundles": {"core": []}}`)
	fake := &builtInFake{catalog: func(cat map[string]any) {
		cat["skills"] = append(cat["skills"].([]any), builtInSkill("task-run-app", "Runs the task's application.", "architect"))
	}}
	b := newTLSBrowser(t)
	fake.install(t, b)
	b.run("the bundle picker offers a built-in skill with the badge",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/bundles/core/edit"),
		waitUntil(`document.querySelector('#bundlePicker-input') !== null`),
		chromedp.Click(`#bundlePicker-input`, chromedp.ByQuery),
		waitUntil(`document.querySelector('#bundlePicker-list [role=option][data-key="task-run-app"] [data-built-in]')?.checkVisibility() === true`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

const shadowMessage = "a catalog skill named task-run-app is hidden by the built-in one"

// TestAShadowedCatalogSkillShowsItsProblem_EndToEnd is W1 for story 6 of the
// spec, against a real server: a catalog folder named like a built-in skill is
// listed once, and its SHADOWED_BY_BUILT_IN problem shows on the row and in
// the detail.
func TestAShadowedCatalogSkillShowsItsProblem_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	skipUnlessServerListsBuiltInSkills(t, w)
	writeCatalogFile(t, w, "skills/task-run-app/SKILL.md", "---\nname: task-run-app\ndescription: A clash.\n---\nbody\n")
	showsShadowedProblem(t, w, nil)
}

// TestAShadowedCatalogSkillShowsItsProblemFromTheFakeCatalog_EndToEnd is the
// same scenario against the page alone.
func TestAShadowedCatalogSkillShowsItsProblemFromTheFakeCatalog_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	listed := builtInSkill("task-run-app", "Runs the task's application.", "architect")
	listed["problems"] = []map[string]string{{"code": "SHADOWED_BY_BUILT_IN", "message": shadowMessage}}
	showsShadowedProblem(t, w, &builtInFake{
		catalog: func(cat map[string]any) { cat["skills"] = append(cat["skills"].([]any), listed) },
		skills: map[string]map[string]any{
			"task-run-app": builtInSkillBody("task-run-app", "Runs the task's application.", "# Run the app\n", "architect"),
		},
	})
}

func showsShadowedProblem(t *testing.T, w *tokenSaverWorld, fake *builtInFake) {
	t.Helper()
	const row = `document.querySelector('[data-skill-row][data-key="task-run-app"]')`
	const detail = `document.querySelector('[data-skill-detail]')`
	b := newTLSBrowser(t)
	if fake != nil {
		fake.install(t, b)
	}
	b.run("the skill is listed once, with the problem on its row",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/skills"),
		waitUntil(`document.querySelectorAll('[data-skill-row][data-key="task-run-app"]').length === 1`),
		waitUntil(row+`.textContent.replace(/\s+/g, ' ').includes('Shadowed by built-in')`),
	)
	b.run("its detail shows the problem and the server's message",
		chromedp.Click(`[data-skill-row][data-key="task-run-app"]`, chromedp.ByQuery),
		waitUntil(detail+`?.querySelector('[data-skill-problem]')?.checkVisibility() === true`),
		waitUntil(detail+`.querySelector('[data-skill-problem]').textContent.trim() === 'Shadowed by built-in'`),
		waitUntil(detail+`.querySelector('[data-skill-problem-note]').textContent.trim().length > 0`),
	)
	b.run("it still offers no Edit, and its Delete is worded as deleting the shadowing skill, through the usual dialog",
		waitUntil(detail+`.querySelector('a[data-edit]')?.checkVisibility() !== true`),
		deleteMenu(`[data-skill-detail]`),
		waitUntil(`document.querySelector('#delete-title')?.textContent.trim() === 'Delete the shadowing skill “task-run-app”?'`),
		waitUntil(`document.querySelector('[data-delete-confirm]').textContent.trim() === 'Delete shadowing skill'`),
		waitUntil(`document.querySelector('[data-delete-path]').textContent.trim() === 'skills/task-run-app/'`),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Cancel"),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}

// TestARefusedWriteOnABuiltInSkillShowsItsCode_EndToEnd is W1 for story 7 of
// the spec: a save or a delete the server refuses with SKILL_BUILT_IN (one sent
// from a stale tab, say) shows the coded error and changes nothing. The page
// cannot cause such a write itself, so the refusal is the fake's.
func TestARefusedWriteOnABuiltInSkillShowsItsCode_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "skills/lint/SKILL.md", "---\nname: lint\ndescription: Lint it.\n---\nbody\n")
	refusal := fakeRefusal{status: 409, code: "SKILL_BUILT_IN", message: "lint ships with the server (architect mode)"}
	fake := &builtInFake{refuse: map[string]fakeRefusal{"save_catalog_skill": refusal, "delete_catalog_skill": refusal}}
	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}

	b := newTLSBrowser(t)
	fake.install(t, b)
	b.run("a refused delete shows the server's message and its code, and the skill stays",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/skills/lint"),
		waitUntil(`document.querySelector('[data-skill-detail] a[data-edit]')?.checkVisibility() === true`),
		deleteMenu(`[data-skill-detail]`),
		waitUntil(flat(`#delete-title`)+` === 'Delete skill “lint”?'`),
		chromedp.SendKeys(`#delete-name`, "lint", chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-delete-confirm]').disabled === false`),
		chromedp.Click(`[data-delete-confirm]`, chromedp.ByQuery),
		waitUntil(flat(`[data-delete-error]`)+`.includes('lint ships with the server (architect mode)') && `+flat(`[data-delete-error]`)+`.includes('SKILL_BUILT_IN')`),
		clickButton(`document.querySelector('[data-delete-dialog]')`, "Cancel"),
		waitUntil(`!document.querySelector('[data-delete-dialog]')`),
		waitUntil(`document.querySelector('[data-skill-row][data-key="lint"]') !== null`),
	)
	b.run("a refused save shows the server's message and its code in the form, which stays open",
		chromedp.Navigate(w.srv.URL+"/agents/skills/lint/edit"),
		waitUntil(`document.querySelector('#sf-desc') !== null && document.querySelector('#sf-desc').value !== ''`),
		setField(`#sf-desc`, "Lint it harder."),
		waitUntil(`document.querySelector('[data-save-footer] [data-save]').disabled === false`),
		chromedp.Click(`[data-save-footer] [data-save]`, chromedp.ByQuery),
		waitUntil(flat(`[data-form-error]`)+`.includes('lint ships with the server (architect mode)') && `+flat(`[data-form-error]`)+`.includes('SKILL_BUILT_IN')`),
		atPath(w.srv.URL+"/agents/skills/lint/edit"),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
