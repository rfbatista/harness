package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func edValue(sel string) string {
	return `document.querySelector('` + instructionsPanel + ` ` + sel + `')?.value`
}

func listedPaths() string {
	return `JSON.stringify([...document.querySelectorAll('` + instructionsPanel + ` [data-entry] [data-path]')].map(p => p.textContent.trim()))`
}

func editorTitle() string {
	return `document.querySelector('` + instructionsPanel + ` [data-editor-path]')?.textContent.trim()`
}

func addressPath() string {
	return `(new URLSearchParams(location.search).get('path') ?? '')`
}

// savableRig is an instructionRig whose record the fake accepts on a save (the shared one holds a tool that needs usage prose).
func savableRig(t *testing.T) *archRig {
	t.Helper()
	rig := instructionRig(t)
	rec := descRecord()
	rec["tools"] = []any{}
	rig.api.seed(rec)
	return rig
}

func renameTo(path string) []chromedp.Action {
	return []chromedp.Action{
		edClick(`[data-rename]`), poll(edVisible(`[data-rename-form]`)),
		edSet(`[data-rename-path]`, path, "input"),
		edClick(`[data-confirm-rename]`),
	}
}

// Rename moves the file, keeps the editor open on the new path and the address follows it.
func TestInstructionsRenameKeepsTheEditorOpenOnTheNewPath(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`))
	rig.run(t, renameTo("docs/GUIDE.md")...)
	rig.run(t, poll(editorTitle()+` === 'docs/GUIDE.md'`), poll(addressPath()+` === 'docs/GUIDE.md'`),
		poll(`!`+edVisible(`[data-rename-form]`)),
		poll(`JSON.parse(`+listedPaths()+`).includes('docs/GUIDE.md')`),
		poll(`!JSON.parse(`+listedPaths()+`).includes('CLAUDE.md')`))
	var text string
	rig.run(t, chromedp.Evaluate(editorText(), &text))
	if text != "# Repository rules\n\nBe **kind** to the next reader.\n" {
		t.Fatalf("text after rename = %q", text)
	}
	body := rig.files.lastBody("rename_instruction_file")
	if body["path"] != "CLAUDE.md" || body["new_path"] != "docs/GUIDE.md" || body["base_version"] != float64(1) || body["repository_id"] != "r1" {
		t.Fatalf("rename body = %v", body)
	}
	if _, ok := rig.files.content("CLAUDE.md"); ok {
		t.Fatal("the old path still holds a file")
	}
	// The editor still saves: against the new path and its version.
	rig.run(t, edType("after rename\n"), edClick(`[data-save]`), poll(`!`+edVisible(`[data-dirty]`)))
	if got, _ := rig.files.content("docs/GUIDE.md"); got != "after rename\n" {
		t.Fatalf("stored after renaming and saving = %q", got)
	}
	var notice bool
	rig.run(t, chromedp.Evaluate(edVisible(`[data-newer-notice]`), &notice))
	if notice {
		t.Fatal("the rename's own feed echo showed a notice")
	}
}

// A rename onto an existing path says so, keeps the file and the form.
func TestInstructionsRenameClashShowsTheRefusalAndKeepsTheFile(t *testing.T) {
	rig := instructionRig(t)
	rig.files.seed("AGENTS.md", "memory", "other\n")
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`))
	rig.run(t, renameTo("AGENTS.md")...)
	rig.run(t, poll(edVisible(`[data-error-for="path"]`)))
	if got := edTextNow(t, rig, `[data-error-for="path"]`); !strings.Contains(got, "already exists") {
		t.Fatalf("refusal = %q", got)
	}
	var title, form bool
	rig.run(t, chromedp.Evaluate(editorTitle()+` === 'CLAUDE.md'`, &title), chromedp.Evaluate(edVisible(`[data-rename-form]`), &form))
	if !title || !form {
		t.Fatalf("after a clash: title kept = %v, form open = %v", title, form)
	}
	if _, ok := rig.files.content("CLAUDE.md"); !ok {
		t.Fatal("the file moved despite the refusal")
	}
	// An invalid path says why.
	rig.run(t, edSet(`[data-rename-path]`, "../out.md", "input"), edClick(`[data-confirm-rename]`),
		poll(edVisible(`[data-error-for="path"]`)+` && `+edText(`[data-error-for="path"]`)+`.includes('..')`))
	// Cancel closes the form. Alpine shows an x-show element from a timer, so an element shown and hidden within one tick can stay shown: let the error's timer fire before Cancel, as a person's click would.
	rig.run(t, chromedp.Sleep(100*time.Millisecond), edClick(`[data-cancel-rename]`), poll(`!`+edVisible(`[data-rename-form]`)+` && !`+edVisible(`[data-error-for="path"]`)))
}

// Renaming with unsaved text says what happens to it, and the text stays.
func TestInstructionsRenameWhileDirtyAsksFirstAndKeepsTheText(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edType("unsaved\n"), edClick(`[data-rename]`), poll(edVisible(`[data-rename-form]`)+` && `+edVisible(`[data-rename-dirty]`)))
	if rig.files.lastBody("rename_instruction_file") != nil {
		t.Fatal("the rename was sent before the person confirmed")
	}
	rig.run(t, edSet(`[data-rename-path]`, "docs/GUIDE.md", "input"), edClick(`[data-confirm-rename]`), poll(editorTitle()+` === 'docs/GUIDE.md'`))
	var text string
	rig.run(t, chromedp.Evaluate(editorText(), &text))
	if text != "unsaved\n" {
		t.Fatalf("unsaved text after rename = %q", text)
	}
	rig.run(t, poll(edVisible(`[data-dirty]`)))
	if got, _ := rig.files.content("docs/GUIDE.md"); !strings.HasPrefix(got, "# Repository rules") {
		t.Fatalf("stored = %q, want the saved text moved, not the unsaved one", got)
	}
}

// A stale rename or delete shows the stale message and keeps the file.
func TestInstructionsStaleRenameAndDeleteKeepTheFile(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`))
	rig.files.seed("CLAUDE.md", "memory", "# Theirs\n") // version 2, not announced
	rig.run(t, renameTo("docs/GUIDE.md")...)
	rig.run(t, poll(edVisible(`[data-newer-notice]`)))
	if _, ok := rig.files.content("CLAUDE.md"); !ok {
		t.Fatal("a stale rename moved the file")
	}
	if got := edTextNow(t, rig, `[data-newer-notice]`); !strings.Contains(got, "Someone saved") {
		t.Fatalf("notice = %q", got)
	}
	var title string
	rig.run(t, chromedp.Evaluate(editorTitle(), &title))
	if title != "CLAUDE.md" {
		t.Fatalf("title after a stale rename = %q", title)
	}

	rig = instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`))
	rig.files.seed("CLAUDE.md", "memory", "# Theirs\n")
	rig.run(t, edClick(`[data-delete]`), poll(edVisible(`[data-delete-confirm]`)), edClick(`[data-confirm-delete]`), poll(edVisible(`[data-newer-notice]`)))
	if _, ok := rig.files.content("CLAUDE.md"); !ok {
		t.Fatal("a stale delete removed the file")
	}
	var open bool
	rig.run(t, chromedp.Evaluate(edVisible(`textarea[data-file-text]`), &open))
	if !open {
		t.Fatal("the editor closed after a stale delete")
	}
}

// Delete asks first, naming the file: Cancel keeps it, confirming removes it from the list and closes the editor.
func TestInstructionsDeleteAsksFirstAndRemovesTheFile(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edClick(`[data-delete]`), poll(edVisible(`[data-delete-confirm]`)))
	if got := edTextNow(t, rig, `[data-delete-confirm]`); !strings.Contains(got, "CLAUDE.md") {
		t.Fatalf("confirmation = %q, want it to name the file", got)
	}
	rig.run(t, edClick(`[data-cancel-delete]`), poll(`!`+edVisible(`[data-delete-confirm]`)))
	if _, ok := rig.files.content("CLAUDE.md"); !ok {
		t.Fatal("cancel deleted the file")
	}
	for _, c := range rig.files.called() {
		if c == "delete_instruction_file" {
			t.Fatal("cancel sent a delete")
		}
	}
	rig.run(t, edClick(`[data-delete]`), poll(edVisible(`[data-delete-confirm]`)), edClick(`[data-confirm-delete]`),
		poll(`!JSON.parse(`+listedPaths()+`).includes('CLAUDE.md')`), poll(`!`+edVisible(`textarea[data-file-text]`)), poll(addressPath()+` === ''`))
	if _, ok := rig.files.content("CLAUDE.md"); ok {
		t.Fatal("the file is still stored")
	}
	if body := rig.files.lastBody("delete_instruction_file"); body["path"] != "CLAUDE.md" || body["base_version"] != float64(1) {
		t.Fatalf("delete body = %v", body)
	}
}

// Deleting with unsaved text says it will be lost.
func TestInstructionsDeleteWhileDirtyWarnsAboutTheText(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edType("unsaved\n"), edClick(`[data-delete]`), poll(edVisible(`[data-delete-confirm]`)))
	if got := edTextNow(t, rig, `[data-delete-confirm]`); !strings.Contains(strings.ToLower(got), "unsaved") {
		t.Fatalf("confirmation = %q, want the unsaved text named", got)
	}
}

// Deleting a file drops its row from the page's record at once: the page is not dirty, and the next metadata save does not bring the row back.
func TestInstructionsDeleteDropsTheRecordRowAndKeepsOtherUnsavedMetadataEdits(t *testing.T) {
	rig := savableRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`),
		// an unrelated, unsaved metadata edit on the skill row
		openDrawerOf("instructions", "instruction", 1),
		setDrawerField("instructions", "instruction", "note", "Kept edit."),
		archClick(drawerSel("instructions", "instruction", "[data-apply]")),
		poll(`document.querySelectorAll('`+instructionsPanel+` [data-entry][data-edited=true]').length === 1`),
		edClick(`[data-delete]`), poll(edVisible(`[data-delete-confirm]`)), edClick(`[data-confirm-delete]`),
		poll(`!JSON.parse(`+listedPaths()+`).includes('CLAUDE.md')`),
		poll(`document.querySelectorAll('`+instructionsPanel+` [data-entry][data-edited=true]').length === 1`),
		poll(`[...document.querySelectorAll('[data-save-bar]')].some(b => b.checkVisibility())`),
	)
	// No conflict banner: the page followed the record's new version.
	var conflict bool
	rig.run(t, chromedp.Sleep(400*time.Millisecond), chromedp.Evaluate(`document.body.textContent.includes('saved elsewhere')`, &conflict))
	if conflict {
		t.Fatal("the page reports a conflict after the file's own delete")
	}
	rig.run(t, clickButton(saveBar, "Save"), poll(`!`+barVisible()))
	saved, _ := json.Marshal(rig.api.lastSaved()["instructions"])
	if strings.Contains(string(saved), "CLAUDE.md") || !strings.Contains(string(saved), "Kept edit.") {
		t.Fatalf("saved instructions = %s, want the unrelated edit and no row for the deleted file", saved)
	}
}

// With nothing unsaved the record simply follows: the next save is not stale.
func TestInstructionsDeleteWithACleanPageLeavesNoStaleRow(t *testing.T) {
	rig := savableRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edClick(`[data-delete]`), poll(edVisible(`[data-delete-confirm]`)), edClick(`[data-confirm-delete]`),
		poll(`!JSON.parse(`+listedPaths()+`).includes('CLAUDE.md')`), chromedp.Sleep(400*time.Millisecond),
		openDrawerOf("instructions", "instruction", 0),
		setDrawerField("instructions", "instruction", "note", "Fresh."),
		archClick(drawerSel("instructions", "instruction", "[data-apply]")),
		poll(barVisible()), clickButton(saveBar, "Save"), poll(`!`+barVisible()))
	saved, _ := json.Marshal(rig.api.lastSaved()["instructions"])
	if strings.Contains(string(saved), "CLAUDE.md") || !strings.Contains(string(saved), "Fresh.") {
		t.Fatalf("saved instructions = %s", saved)
	}
}

// A skill is a folder with its files; "Add file to skill" opens a new file under the folder, and saving lists it there.
func TestInstructionsSkillShowsAsAFolderAndTakesANewFile(t *testing.T) {
	rig := instructionRig(t)
	rig.files.seed(".claude/skills/go/notes.md", "skill", "Notes.\n")
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(`document.querySelectorAll('`+instructionsPanel+` [data-skill-folder]').length === 1`))
	var raw string
	rig.run(t, chromedp.Evaluate(`(() => {
		const li = [...document.querySelectorAll('`+instructionsPanel+` [data-entries] > li')].filter(l => l.checkVisibility());
		return JSON.stringify(li.map(l => l.hasAttribute('data-skill-folder') ? 'folder ' + l.querySelector('[data-folder-name]').textContent.trim() : l.querySelector('[data-path]').textContent.trim()));
	})()`, &raw))
	want := `["CLAUDE.md","folder .claude/skills/go","`+".claude/skills/go/SKILL.md"+`","`+".claude/skills/go/notes.md"+`",".claude/agents/reviewer.md"]`
	if raw != want {
		t.Fatalf("list = %s, want %s", raw, want)
	}
	rig.run(t, edClick(`[data-add-skill-file]`), poll(edVisible(`[data-new-file]`)))
	var kind, path string
	rig.run(t, chromedp.Evaluate(edValue(`[data-new-kind]`), &kind), chromedp.Evaluate(edValue(`[data-new-path]`), &path))
	if kind != "skill" || path != ".claude/skills/go/" {
		t.Fatalf("new file kind = %q, path = %q, want a skill under the folder", kind, path)
	}
	rig.run(t, edSet(`[data-new-path]`, ".claude/skills/go/extra.md", "input"), edType("Extra.\n"), edClick(`[data-save]`),
		poll(`document.querySelector('`+instructionsPanel+` [data-file-version]')?.textContent.trim() === 'Version 1'`),
		poll(`JSON.parse(`+listedPaths()+`).includes('.claude/skills/go/extra.md')`))
	if got, ok := rig.files.content(".claude/skills/go/extra.md"); !ok || got != "Extra.\n" {
		t.Fatalf("stored = %q, %v", got, ok)
	}
	if body := rig.files.lastBody("save_instruction_file"); body["kind"] != "skill" || body["base_version"] != float64(0) {
		t.Fatalf("save body = %v", body)
	}
	var folders int
	rig.run(t, chromedp.Evaluate(`document.querySelectorAll('`+instructionsPanel+` [data-skill-folder]').length`, &folders))
	if folders != 1 {
		t.Fatalf("folders = %d, want the new file in the same folder", folders)
	}
}

// The new controls are labelled.
func TestInstructionsRenameAndDeleteControlsAreLabelled(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edClick(`[data-rename]`), poll(edVisible(`[data-rename-form]`)))
	var raw string
	rig.run(t, chromedp.Evaluate(`JSON.stringify(['[data-rename]','[data-rename-path]','[data-confirm-rename]','[data-cancel-rename]','[data-delete]'].map(s => { const e = document.querySelector('`+instructionsPanel+` '+s); return (e.getAttribute('aria-label') ?? e.labels?.[0]?.textContent ?? e.textContent).trim(); }))`, &raw))
	if strings.Contains(raw, `""`) {
		t.Fatalf("labels = %s", raw)
	}
}

// A skill folder with no SKILL.md is not delivered to agents: the folder says so in words and offers Add SKILL.md, which clears the tag once saved.
func TestInstructionsSkillFolderWithoutSkillMdIsTaggedAndOffersIt(t *testing.T) {
	rig := instructionRig(t)
	rig.files.seed(".claude/skills/solo/notes.md", "skill", "Notes.\n")
	openInstructions(t, rig, "", 1440)
	folder := func(name string) string {
		return `[...document.querySelectorAll('` + instructionsPanel + ` [data-skill-folder]')].find(l => l.querySelector('[data-folder-name]').textContent.trim() === '.claude/skills/` + name + `')`
	}
	rig.run(t, poll(`!!`+folder("solo")))
	var raw string
	rig.run(t, chromedp.Evaluate(`JSON.stringify({
		solo: ` + folder("solo") + `.querySelector('[data-flag="not-delivered"]')?.textContent.trim() ?? null,
		go: ` + folder("go") + `.querySelector('[data-flag="not-delivered"]')?.textContent.trim() ?? null,
		soloAction: !!` + folder("solo") + `.querySelector('[data-add-skill-md]'),
		goAction: !!` + folder("go") + `.querySelector('[data-add-skill-md]'),
		tone: ` + folder("solo") + `.querySelector('[data-flag="not-delivered"]')?.dataset.tone,
	})`, &raw))
	if raw != `{"solo":"not delivered: add SKILL.md","go":null,"soloAction":true,"goAction":false,"tone":"attention"}` {
		t.Fatalf("folders = %s", raw)
	}
	rig.run(t, chromedp.Evaluate(folder("solo")+`.querySelector('[data-add-skill-md]').click()`, nil), poll(edVisible(`[data-new-file]`)))
	var kind, path string
	rig.run(t, chromedp.Evaluate(edValue(`[data-new-kind]`), &kind), chromedp.Evaluate(edValue(`[data-new-path]`), &path))
	if kind != "skill" || path != ".claude/skills/solo/SKILL.md" {
		t.Fatalf("new file kind = %q, path = %q", kind, path)
	}
	rig.run(t, edType("---\nname: solo\n---\n"), edClick(`[data-save]`),
		poll(`document.querySelector('`+instructionsPanel+` [data-file-version]')?.textContent.trim() === 'Version 1'`),
		poll(`!`+folder("solo")+`.querySelector('[data-flag="not-delivered"]') && !`+folder("solo")+`.querySelector('[data-add-skill-md]')`))
}

// Renaming a file moves its record entry to the new path, with its metadata: the row follows, no row stays on the old path, and an unrelated unsaved metadata edit survives.
func TestInstructionsRenameMovesTheRecordRowWithItsMetadata(t *testing.T) {
	rig := savableRig(t)
	openInstructions(t, rig, "?path=.claude/skills/go/SKILL.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`),
		// an unrelated, unsaved metadata edit on the CLAUDE.md row
		openDrawerOf("instructions", "instruction", 0),
		setDrawerField("instructions", "instruction", "note", "Kept edit."),
		archClick(drawerSel("instructions", "instruction", "[data-apply]")),
		poll(`document.querySelectorAll('`+instructionsPanel+` [data-entry][data-edited=true]').length === 1`),
	)
	rig.run(t, renameTo(".claude/skills/golang/SKILL.md")...)
	rig.run(t, poll(editorTitle()+` === '.claude/skills/golang/SKILL.md'`),
		poll(`JSON.parse(`+listedPaths()+`).includes('.claude/skills/golang/SKILL.md')`),
		poll(`!JSON.parse(`+listedPaths()+`).includes('.claude/skills/go/SKILL.md')`),
		poll(`document.querySelectorAll('`+instructionsPanel+` [data-entry][data-edited=true]').length === 1`))
	d := readDesc(t, rig, "instructions", "data-path", "data-note", "data-meta=applies", "data-meta=loaded")
	var moved = -1
	for i, e := range d.Entries {
		path := ""
		if e["data-path"] != nil {
			path = *e["data-path"]
		}
		if strings.Contains(path, ".claude/skills/golang/SKILL.md") {
			moved = i
		}
		if strings.Contains(path, ".claude/skills/go/SKILL.md") {
			t.Fatalf("a row stays on the old path: %v", d.Entries)
		}
	}
	if moved < 0 {
		t.Fatalf("no row on the new path: %v", d.Entries)
	}
	wantPart(t, d, moved, "data-meta=applies", "Applies to go-developer, devops")
	wantPart(t, d, moved, "data-meta=loaded", "Loaded On demand")
	// No row is marked missing, and the page took the record's new version without a conflict.
	var missing, conflict bool
	rig.run(t, chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('`+instructionsPanel+` [data-flag="missing"]')`, &missing),
		chromedp.Evaluate(`document.body.textContent.includes('saved elsewhere')`, &conflict))
	if missing || conflict {
		t.Fatalf("after the rename: missing flag = %v, conflict = %v", missing, conflict)
	}
	rig.run(t, poll(`[...document.querySelectorAll('[data-save-bar]')].some(b => b.checkVisibility())`), clickButton(saveBar, "Save"), poll(`!`+barVisible()))
	saved, _ := json.Marshal(rig.api.lastSaved()["instructions"])
	if strings.Contains(string(saved), `"path":".claude/skills/go/SKILL.md"`) || !strings.Contains(string(saved), ".claude/skills/golang/SKILL.md") || !strings.Contains(string(saved), "Kept edit.") {
		t.Fatalf("saved instructions = %s, want the moved row, the unrelated edit and no old row", saved)
	}
}
