//go:build !windows

package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestSkillFormCreatesPreviewsAndEditsASkill_EndToEnd is W1 for story 10 of
// spec "Web UI — the Agents page", against a real server and a temporary
// catalog: a skill is created with a description and a body and added to a
// bundle and an agent, the What will be written panel follows the typing, the
// coach and the meter answer, a taken name shows its link and a name to try,
// a description over 1024 characters blocks the save, the saved skill is in
// the Skills tab and on the agent, and an edit renames it. Errors are linked
// to their fields with aria-describedby.
func TestSkillFormCreatesPreviewsAndEditsASkill_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", `{
  "bundles": {"ux": ["lint"]},
  "agents": {
    "designer": {"description": "Designs screens.", "skills": ["@ux"]},
    "backend": {"description": "Builds the API.", "skills": ["tdd"]}
  }
}
`)
	for _, s := range []string{"lint", "tdd"} {
		writeCatalogFile(t, w, "skills/"+s+"/SKILL.md", "---\nname: "+s+"\ndescription: The "+s+" skill.\n---\nbody\n")
	}
	catalogFile := func(rel string) string {
		raw, err := os.ReadFile(filepath.Join(w.root, "agents", rel))
		if err != nil {
			return ""
		}
		return string(raw)
	}
	upload := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(upload, []byte("uploaded notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	flat := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	}
	visible := func(sel string) string {
		return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true`
	}
	attr := func(sel, name string) string {
		return `document.querySelector(` + quote(sel) + `)?.getAttribute(` + quote(name) + `)`
	}
	form := `[data-skill-form]`
	save := `document.querySelector('[data-save-footer] [data-save]')`
	coach := `[...document.querySelectorAll('[data-coach-item]')].map(c => c.dataset.coachItem + '=' + c.dataset.state).join()`
	previewOf := func(path string) string {
		return `(document.querySelector(` + quote(`[data-preview-file][data-path="`+path+`"] [data-preview-text]`) + `)?.textContent ?? '')`
	}
	previewPaths := `[...document.querySelectorAll('[data-preview-file]')].map(f => f.dataset.path).join()`
	setText := func(sel, value string) chromedp.Action {
		v, _ := json.Marshal(value)
		return chromedp.Evaluate(`(() => { const el = document.querySelector(`+quote(sel)+`); el.focus(); el.value = `+string(v)+`; el.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
	}
	tick := func(sel string) chromedp.Action {
		return chromedp.Evaluate(`document.querySelector(`+quote(sel)+`).click()`, nil)
	}
	selectOption := func(sel, value string) chromedp.Action {
		return chromedp.Evaluate(`(() => { const el = document.querySelector(`+quote(sel)+`); el.value = `+quote(value)+`; el.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil)
	}
	opens := func(path string) []chromedp.Action {
		return []chromedp.Action{
			chromedp.Navigate(w.srv.URL + path),
			waitUntil(visible(form) + ` && !document.querySelector('[data-loading]')?.checkVisibility()`),
		}
	}
	opensEditor := func(path string) []chromedp.Action {
		return []chromedp.Action{chromedp.Navigate(w.srv.URL + path), waitUntil(visible(`[data-agent-editor]`))}
	}
	run := func(b *browser, name string, actions ...chromedp.Action) {
		for i, a := range actions {
			b.run(fmt.Sprintf("%s [action %d]", name, i), a)
		}
	}

	const short = "Turns a Figma frame into specs developers can build from."
	const full = short + " Use when a design is ready for implementation. For reviewing a built screen, use `tdd`."

	b := newTLSBrowser(t)
	run(b, "/agents/skills/new opens the full-page form: no tabs, the skills/ prefix, empty fields, Save off and nothing written",
		chromedp.EmulateViewport(1400, 1000),
		chromedp.Navigate(w.srv.URL+"/agents/skills/new"),
		waitUntil(visible(form)),
		waitUntil(`document.querySelector('[data-skill-form] h1').textContent.trim() === 'New skill'`),
		waitUntil(`!document.querySelector('[role=tablist]')?.checkVisibility()`),
		waitUntil(`document.querySelector('.prefixed > span').textContent.trim() === 'skills/'`),
		waitUntil(save+`.disabled === true && `+save+`.textContent.trim() === 'Create skill'`),
		waitUntil(flat(`[data-save-state]`)+` === 'Nothing is written until you create the skill.'`),
		waitUntil(flat(`[data-preview-hint]`)+`.startsWith('Give the skill a name and a description')`),
		waitUntil(flat(`[data-meter]`)+` === '0 / 1024'`),
		waitUntil(coach+` === 'what=no,when=no,neighbour=no'`),
		waitUntil(`[...document.querySelectorAll('[data-sf-bundle-row]')].map(r => r.dataset.name).join() === 'ux' && [...document.querySelectorAll('[data-sf-agent-row]')].map(r => r.dataset.name).join() === 'backend,designer'`),
	)
	run(b, "typing the name and a description shows the meter, the coach and, after a pause, the server's SKILL.md",
		setField(`#sf-name`, "figma-handoff"),
		setField(`#sf-desc`, short),
		waitUntil(flat(`[data-meter]`)+` === '`+fmt.Sprint(len(short))+` / 1024'`),
		waitUntil(coach+` === 'what=ok,when=no,neighbour=no'`),
		waitUntil(previewPaths+` === 'skills/figma-handoff/SKILL.md'`),
		waitUntil(previewOf("skills/figma-handoff/SKILL.md")+`.includes('name: figma-handoff') && `+previewOf("skills/figma-handoff/SKILL.md")+`.includes('Turns a Figma frame into specs')`),
		waitUntil(save+`.disabled === false`),
		waitUntil(flat(`[data-save-state]`)+` === '2 unsaved changes · writes skills/figma-handoff/SKILL.md'`),
	)
	run(b, "the preview follows each change while typing: the description, then the body",
		setField(`#sf-desc`, full),
		waitUntil(coach+` === 'what=ok,when=ok,neighbour=ok'`),
		waitUntil(previewOf("skills/figma-handoff/SKILL.md")+`.includes('Use when a design is ready for implementation')`),
		selectOption(`#sf-start`, "workflow"),
		waitUntil(`document.querySelector('#sf-body').value.includes('## Steps')`),
		setText(`#sf-body`, "## Steps\n1. Read the frame.\n2. Write the spec.\n"),
		waitUntil(previewOf("skills/figma-handoff/SKILL.md")+`.includes('2. Write the spec.')`),
	)
	run(b, "Preview renders the Markdown, and Write brings the text back",
		clickButton(`document.querySelector('.md-bar')`, "Preview"),
		waitUntil(`document.querySelector('[data-body-preview] h2')?.textContent === 'Steps' && document.querySelectorAll('[data-body-preview] li').length === 2`),
		waitUntil(`!`+visible(`#sf-body`)),
		clickButton(`document.querySelector('.md-bar')`, "Write"),
		waitUntil(visible(`#sf-body`)+` && !`+visible(`[data-body-preview]`)),
	)
	run(b, "extra files: a new file is named and written, and an uploaded one joins it",
		chromedp.Click(`[data-new-file]`, chromedp.ByQuery),
		waitUntil(`document.activeElement?.id === 'sf-new-path'`),
		chromedp.SendKeys(`#sf-new-path`, "../oops", chromedp.ByQuery),
		pressKey("Enter", 13),
		waitUntil(visible(`[data-newfile-error]`)+` && `+flat(`[data-newfile-error]`)+`.includes('no “..”')`),
		setField(`#sf-new-path`, "references/checklist.md"),
		pressKey("Enter", 13),
		waitUntil(`[...document.querySelectorAll('[data-file-row]')].map(r => r.dataset.path).join() === 'references/checklist.md'`),
		waitUntil(visible(`[data-file-text]`)),
		setText(`[data-file-text]`, "- every state is covered\n"),
		waitUntil(previewPaths+` === 'skills/figma-handoff/SKILL.md,skills/figma-handoff/references/checklist.md'`),
		waitUntil(previewOf("skills/figma-handoff/references/checklist.md")+` === '- every state is covered\n'`),
		chromedp.SetUploadFiles(`#sf-upload-files`, []string{upload}, chromedp.ByQuery),
		waitUntil(`[...document.querySelectorAll('[data-file-row]')].map(r => r.dataset.path).join() === 'references/checklist.md,notes.md'`),
		waitUntil(flat(`[data-upload-note]`)+` === 'Added 1 file.'`),
		waitUntil(previewOf("skills/figma-handoff/notes.md")+` === 'uploaded notes\n'`),
	)
	run(b, "Who gets it: a bundle and an agent are ticked, the reach says who, and agents.json joins the preview",
		tick(`[data-sf-bundle-row][data-name=ux] input`),
		tick(`[data-sf-agent-row][data-name=backend] input`),
		waitUntil(flat(`[data-sf-reach]`)+` === 'Reaches 2 agents: backend, designer. Leave both empty to keep the skill on the shelf.'`),
		waitUntil(previewPaths+`.includes('agents.json')`),
		waitUntil(`[...document.querySelectorAll('[data-preview-file][data-path="agents.json"] [data-diff-line][data-kind=add]')].some(l => l.textContent.includes('figma-handoff'))`),
		waitUntil(flat(`[data-save-state]`)+`.includes('writes skills/figma-handoff/SKILL.md') && `+flat(`[data-save-state]`)+`.includes('agents.json')`),
		bothSchemes("skill-form-filled"),
	)
	run(b, "Review change shows the diff of every file the save writes",
		clickButton(`document.querySelector('[data-save-footer]')`, "Review change"),
		waitUntil(visible(`[data-review-panel]`)),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-file]')].map(f => f.dataset.path).join().includes('agents.json')`),
		waitUntil(`[...document.querySelectorAll('[data-review-panel] [data-diff-file]')].some(f => f.dataset.path === 'skills/figma-handoff/SKILL.md')`),
	)
	run(b, "Create skill saves, shows the toast, and lands on the skill with its body and files",
		clickButton(`document.querySelector('[data-save-footer]')`, "Create skill"),
		atPath(w.srv.URL+"/agents/skills/figma-handoff"),
		waitUntil(visible(`[data-toast]`)+` && `+flat(`[data-toast]`)+`.includes('Running sessions keep what they started with')`),
		waitUntil(`[...document.querySelectorAll('[data-skill-row]')].map(r => r.dataset.key).includes('figma-handoff')`),
		waitUntil(`document.querySelector('[data-skill-detail] h2').textContent.trim() === 'figma-handoff'`),
		waitUntil(`document.querySelector('[data-skill-detail] [data-skill-body] h2')?.textContent === 'Steps'`),
		waitUntil(`!document.querySelector('[data-skill-form]')`),
		waitUntil(`[...document.querySelectorAll('[data-skill-detail] [data-reach="agents"] a')].map(a => a.textContent.trim()).join() === 'backend'`),
		waitUntil(`[...document.querySelectorAll('[data-skill-detail] [data-reach="bundles"] a')].map(a => a.textContent.trim()).join() === '@ux'`),
	)
	got := catalogFile("skills/figma-handoff/SKILL.md")
	for _, want := range []string{"name: figma-handoff", "Use when a design is ready", "2. Write the spec."} {
		if !strings.Contains(got, want) {
			t.Fatalf("SKILL.md lacks %q:\n%s", want, got)
		}
	}
	if got := catalogFile("skills/figma-handoff/references/checklist.md"); got != "- every state is covered\n" {
		t.Fatalf("checklist.md = %q", got)
	}
	if got := catalogFile("skills/figma-handoff/notes.md"); got != "uploaded notes\n" {
		t.Fatalf("notes.md = %q", got)
	}
	if got := catalogFile("agents.json"); strings.Count(got, `"figma-handoff"`) != 2 {
		t.Fatalf("figma-handoff should be in the ux bundle and in backend:\n%s", got)
	}
	run(b, "the agent has it: its detail lists the skill after bundles",
		chromedp.Navigate(w.srv.URL+"/agents/designer"),
		waitUntil(`document.querySelector('[data-agent-detail]')?.textContent.includes('figma-handoff')`),
	)

	run(b, "a name another skill has shows its link and a name to try, linked to the field; Save stays off",
		append(opens("/agents/skills/new"),
			setField(`#sf-name`, "tdd"),
			setField(`#sf-desc`, full),
			waitUntil(visible(`#sf-name-error`)),
			waitUntil(flat(`#sf-name-error`)+`.includes('A skill named tdd already exists.')`),
			waitUntil(attr(`[data-open-taken]`, "href")+` === '/agents/skills/tdd'`),
			waitUntil(flat(`[data-use-suggestion]`)+` === 'tdd-2'`),
			waitUntil(attr(`#sf-name`, "aria-describedby")+` === 'sf-name-hint sf-name-error' && `+attr(`#sf-name`, "aria-invalid")+` === 'true'`),
			waitUntil(save+`.disabled === true`),
			waitUntil(flat(`[data-save-state]`)+` === 'Fix 1 field to create the skill.'`),
			bothSchemes("skill-form-name-taken"),
			chromedp.Click(`[data-use-suggestion]`, chromedp.ByQuery),
			waitUntil(`document.querySelector('#sf-name').value === 'tdd-2' && !`+visible(`#sf-name-error`)),
			waitUntil(save+`.disabled === false`),
		)...,
	)
	run(b, "the meter blocks a description over 1024 characters, says by how much, and is linked to the field",
		setField(`#sf-desc`, strings.Repeat("x", 1030)),
		waitUntil(flat(`[data-meter]`)+` === '1030 / 1024' && `+attr(`[data-meter]`, "data-over")+` === 'true'`),
		waitUntil(visible(`#sf-desc-error`)+` && `+flat(`#sf-desc-error`)+`.includes('6 over the limit of 1024')`),
		waitUntil(attr(`#sf-desc`, "aria-describedby")+`.includes('sf-desc-error') && `+attr(`#sf-desc`, "aria-invalid")+` === 'true'`),
		waitUntil(save+`.disabled === true`),
		bothSchemes("skill-form-too-long"),
		setField(`#sf-desc`, strings.Repeat("x", 1024)),
		waitUntil(`!`+visible(`#sf-desc-error`)+` && `+save+`.disabled === false`),
	)
	run(b, "Cancel on an edited form asks first; Discard changes leaves for the Skills tab",
		clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
		waitUntil(visible(`[data-leave]`)),
		waitUntil(`document.activeElement === document.querySelector('[data-keep]')`),
		clickButton(`document.querySelector('[data-leave]')`, "Keep editing"),
		waitUntil(`!`+visible(`[data-leave]`)+` && document.querySelector('#sf-name').value === 'tdd-2'`),
		clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
		clickButton(`document.querySelector('[data-leave]')`, "Discard changes"),
		atPath(w.srv.URL+"/agents/skills"),
		waitUntil(`!document.querySelector('[data-skill-form]')`),
	)

	run(b, "editing a skill opens with its text and readers; a rename says what it rewrites",
		append(opens("/agents/skills/figma-handoff/edit"),
			waitUntil(`document.querySelector('#sf-name').value === 'figma-handoff' && document.querySelector('#sf-desc').value.startsWith('Turns a Figma frame')`),
			waitUntil(`document.querySelector('#sf-body').value.includes('2. Write the spec.')`),
			waitUntil(`[...document.querySelectorAll('[data-file-row]')].map(r => r.dataset.path).sort().join() === 'notes.md,references/checklist.md'`),
			waitUntil(`document.querySelector('[data-sf-bundle-row][data-name=ux] input').checked && document.querySelector('[data-sf-agent-row][data-name=backend] input').checked`),
			waitUntil(save+`.disabled === true && `+save+`.textContent.trim() === 'Save skill'`),
			waitUntil(`!`+visible(`[data-sf-rename]`)),
			setField(`#sf-name`, "figma-specs"),
			waitUntil(visible(`[data-sf-rename]`)+` && `+flat(`[data-sf-rename]`)+`.includes('rewrites the 2 places in agents.json that name it')`),
			chromedp.Click(`[data-remove-file][data-path="notes.md"]`, chromedp.ByQuery),
			waitUntil(`[...document.querySelectorAll('[data-file-row]')].map(r => r.dataset.path).join() === 'references/checklist.md'`),
			waitUntil(previewPaths+`.includes('skills/figma-specs/SKILL.md')`),
			bothSchemes("skill-form-edit-rename"),
		)...,
	)
	run(b, "Save skill moves the folder, rewrites the references and removes the file",
		clickButton(`document.querySelector('[data-save-footer]')`, "Save skill"),
		atPath(w.srv.URL+"/agents/skills/figma-specs"),
		waitUntil(flat(`[data-toast]`)+`.includes('Running sessions keep what they started with')`),
		waitUntil(`[...document.querySelectorAll('[data-skill-row]')].map(r => r.dataset.key).join() === 'figma-specs,lint,tdd'`),
	)
	if _, err := os.Stat(filepath.Join(w.root, "agents", "skills", "figma-handoff")); err == nil {
		t.Fatal("the old folder is still there after the rename")
	}
	if got := catalogFile("skills/figma-specs/notes.md"); got != "" {
		t.Fatalf("the removed file is still there: %q", got)
	}
	if got := catalogFile("agents.json"); strings.Contains(got, "figma-handoff") || strings.Count(got, `"figma-specs"`) != 2 {
		t.Fatalf("the references did not follow the rename:\n%s", got)
	}

	run(b, "a conflict: an outside change to agents.json blocks the save and Overwrite writes it",
		append(opens("/agents/skills/figma-specs/edit"),
			tick(`[data-sf-agent-row][data-name=designer] input`),
			waitUntil(save+`.disabled === false`),
			chromedp.ActionFunc(func(context.Context) error {
				writeCatalogFile(t, w, "agents.json", strings.Replace(catalogFile("agents.json"), "Builds the API.", "Builds the API. Changed by hand.", 1))
				return nil
			}),
			clickButton(`document.querySelector('[data-save-footer]')`, "Save skill"),
			waitUntil(visible(`[data-conflict]`)),
			waitUntil(save+`.disabled === true`),
			waitUntil(flat(`[data-save-state]`)+` === 'Saving is off until you choose Reload or Overwrite.'`),
			clickButton(`document.querySelector('[data-conflict]')`, "Overwrite"),
			atPath(w.srv.URL+"/agents/skills/figma-specs"),
		)...,
	)

	run(b, "Create skill… from the agent editor goes to the form with the editor to return to; saving returns with the skill chosen and the edit intact",
		append(opensEditor("/agents/new"),
			setField(`#agent-name`, "trip"),
			setField(`#agent-desc`, "Takes a trip."),
			chromedp.Focus(`#skillPicker-input`, chromedp.ByQuery),
			chromedp.SendKeys(`#skillPicker-input`, "booking", chromedp.ByQuery),
			waitUntil(`document.querySelector('#skillPicker-list [role=option]').dataset.key === '+create'`),
			pressKey("Enter", 13),
			atPath(w.srv.URL+"/agents/skills/new?name=booking&return=%2Fagents%2Fnew%23skills"),
			waitUntil(visible(form)+` && document.querySelector('#sf-name').value === 'booking'`),
			waitUntil(flat(`[data-sf-back]`)+` === '← Back to the agent'`),
			waitUntil(flat(`[data-save-state]`)+` === 'Nothing is written until you create the skill.'`),
			setField(`#sf-desc`, "Books a trip. Use when a person plans travel."),
			waitUntil(save+`.disabled === false`),
			clickButton(`document.querySelector('[data-save-footer]')`, "Create skill"),
			atPath(w.srv.URL+"/agents/new#skills"),
			waitUntil(visible(`[data-agent-editor]`)+` && document.querySelector('#agent-name').value === 'trip' && document.querySelector('#agent-desc').value === 'Takes a trip.'`),
			waitUntil(`[...document.querySelectorAll('[data-agent-editor] [data-chips] [data-chip]')].map(c => c.dataset.chip).join() === 'booking'`),
			waitUntil(`location.search === ''`),
			waitUntil(visible(`[data-toast]`)+` && `+flat(`[data-toast]`)+`.includes('skills/booking')`),
			clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
			clickButton(`document.querySelector('[data-leave]')`, "Discard changes"),
			atPath(w.srv.URL+"/agents"),
		)...,
	)
	run(b, "Cancel from the form goes back to the editor with the edit intact and nothing added",
		append(opensEditor("/agents/new"),
			setField(`#agent-name`, "trip2"),
			setField(`#agent-desc`, "Another trip."),
			chromedp.Focus(`#skillPicker-input`, chromedp.ByQuery),
			chromedp.SendKeys(`#skillPicker-input`, "hotels", chromedp.ByQuery),
			pressKey("Enter", 13),
			atPath(w.srv.URL+"/agents/skills/new?name=hotels&return=%2Fagents%2Fnew%23skills"),
			waitUntil(visible(form)),
			clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
			atPath(w.srv.URL+"/agents/new#skills"),
			waitUntil(`document.querySelector('#agent-name')?.value === 'trip2' && document.querySelector('#agent-desc').value === 'Another trip.'`),
			waitUntil(`document.querySelectorAll('[data-agent-editor] [data-chips] [data-chip]').length === 0`),
			clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
			clickButton(`document.querySelector('[data-leave]')`, "Discard changes"),
			atPath(w.srv.URL+"/agents"),
		)...,
	)
	run(b, "a copy of another skill starts with its text; a skill the catalog lacks has no form",
		append(opens("/agents/skills/new?from=lint"),
			waitUntil(`document.querySelector('#sf-desc').value === 'The lint skill.' && document.querySelector('#sf-body').value.includes('body')`),
			waitUntil(`document.querySelector('#sf-name').value === ''`),
			clickButton(`document.querySelector('[data-save-footer]')`, "Cancel"),
			clickButton(`document.querySelector('[data-leave]')`, "Discard changes"),
			atPath(w.srv.URL+"/agents/skills"),
			chromedp.Navigate(w.srv.URL+"/agents/skills/ghost/edit"),
			waitUntil(visible(`[data-sf-missing]`)+` && `+flat(`[data-sf-missing]`)+`.includes('That skill no longer exists.')`),
		)...,
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
