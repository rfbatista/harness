package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const ed = instructionsPanel + ` [data-instruction-editor] `

// typeInto replaces the editor's text the way typing does: the value changes and an input event follows.
func edType(text string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const ta = document.querySelector('`+instructionsPanel+` textarea[data-file-text]'); ta.value = `+edJS(text)+`; ta.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
}

func edJS(s string) string {
	return "`" + strings.NewReplacer("\\", "\\\\", "`", "\\`", "${", "\\${", "\n", "\\n").Replace(s) + "`"
}

func edVisible(sel string) string {
	return `(document.querySelector('` + instructionsPanel + ` ` + sel + `')?.checkVisibility() ?? false)`
}

func edClick(sel string) chromedp.Action {
	return chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` `+sel+`').click()`, nil)
}

func edSet(sel, value, event string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const e = document.querySelector('`+instructionsPanel+` `+sel+`'); e.value = `+edJS(value)+`; e.dispatchEvent(new Event('`+event+`', { bubbles: true })); })()`, nil)
}

func edText(sel string) string {
	return `document.querySelector('` + instructionsPanel + ` ` + sel + `')?.textContent.replace(/\s+/g, ' ').trim()`
}

// Add file opens an empty editor with the path prefilled for the kind; saving creates the file at version 1 and the list shows it.
func TestInstructionsAddFilePrefillsThePathPerKindAndCreatesTheFile(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t, edClick(`[data-add]`), poll(edVisible(`[data-new-file]`)+` && `+edVisible(`textarea[data-file-text]`)))
	for _, c := range []struct{ kind, path string }{
		{"memory", "CLAUDE.md"},
		{"skill", ".claude/skills/<name>/SKILL.md"},
		{"subagent", ".claude/agents/<name>.md"},
	} {
		rig.run(t, edSet(`[data-new-kind]`, c.kind, "change"), poll(`document.querySelector('`+instructionsPanel+` [data-new-path]').value === `+edJS(c.path)))
	}
	var text string
	rig.run(t, chromedp.Evaluate(editorText(), &text))
	if text != "" {
		t.Fatalf("the new file's editor holds %q, want it empty", text)
	}
	rig.run(t,
		edSet(`[data-new-kind]`, "subagent", "change"),
		edSet(`[data-new-path]`, ".claude/agents/planner.md", "input"),
		edType("You plan the work.\n"),
		edClick(`[data-save]`),
		poll(`document.querySelector('`+instructionsPanel+` [data-file-version]')?.textContent.trim() === 'Version 1'`),
		poll(`[...document.querySelectorAll('`+instructionsPanel+` [data-entry] [data-path]')].some(p => p.textContent.trim() === '.claude/agents/planner.md')`),
	)
	if got, ok := rig.files.content(".claude/agents/planner.md"); !ok || got != "You plan the work.\n" {
		t.Fatalf("stored = %q, %v", got, ok)
	}
	body := rig.files.lastBody("save_instruction_file")
	if body["base_version"] != float64(0) || body["kind"] != "subagent" || body["repository_id"] != "r1" {
		t.Fatalf("save body = %v, want base_version 0 and the subagent kind", body)
	}
	var fixed bool
	rig.run(t, chromedp.Evaluate(`!document.querySelector('`+instructionsPanel+` [data-new-path]')?.checkVisibility()`, &fixed))
	if !fixed {
		t.Fatal("the path is still editable after the file exists")
	}
}

// Editing marks the editor dirty, Discard restores the saved text, Save clears the mark and shows the new version, and the page's save bar is never involved.
func TestInstructionsEditDiscardAndSave(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`))
	var bar bool
	rig.run(t, chromedp.Evaluate(edVisible(`[data-dirty]`), &bar))
	if bar {
		t.Fatal("a file just opened is marked dirty")
	}
	rig.run(t, edType("changed\n"), poll(edVisible(`[data-dirty]`)), chromedp.Evaluate(`[...document.querySelectorAll('[data-save-bar]')].some(b => b.checkVisibility())`, &bar))
	if bar {
		t.Fatal("editing a file shows the page's save bar")
	}
	rig.run(t, edClick(`[data-discard]`), poll(editorText()+` === "# Repository rules\n\nBe **kind** to the next reader.\n"`), poll(`!`+edVisible(`[data-dirty]`)))
	rig.run(t, edType("changed\n"), edClick(`[data-save]`),
		poll(`document.querySelector('`+instructionsPanel+` [data-file-version]')?.textContent.trim() === 'Version 2'`),
		poll(`!`+edVisible(`[data-dirty]`)))
	if got, _ := rig.files.content("CLAUDE.md"); got != "changed\n" {
		t.Fatalf("stored = %q", got)
	}
	if body := rig.files.lastBody("save_instruction_file"); body["base_version"] != float64(1) || body["path"] != "CLAUDE.md" {
		t.Fatalf("save body = %v, want the version the text started from", body)
	}
	rig.run(t, chromedp.Evaluate(`[...document.querySelectorAll('[data-save-bar]')].some(b => b.checkVisibility())`, &bar))
	if bar {
		t.Fatal("saving a file shows the page's save bar")
	}
	rig.api.mu.Lock()
	recordVersion := rig.api.version
	rig.api.mu.Unlock()
	if recordVersion != 1 {
		t.Fatalf("saving a file moved the architecture record to version %d", recordVersion)
	}
}

// Refusals show next to the field they are about and keep the person's text.
func TestInstructionsFieldErrorsStayNextToTheirFieldAndKeepTheText(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t, edClick(`[data-add]`), poll(edVisible(`[data-new-file]`)),
		edSet(`[data-new-path]`, "../outside.md", "input"), edType("keep me"), edClick(`[data-save]`),
		poll(edVisible(`[data-error-for="path"]`)))
	var raw string
	rig.run(t, chromedp.Evaluate(edText(`[data-error-for="path"]`), &raw))
	if !strings.Contains(raw, "..") {
		t.Fatalf("path error = %q", raw)
	}
	var text string
	var contentErr bool
	rig.run(t, chromedp.Evaluate(editorText(), &text), chromedp.Evaluate(edVisible(`[data-error-for="content"]`), &contentErr))
	if text != "keep me" || contentErr {
		t.Fatalf("after an invalid path: text = %q, content error shown = %v", text, contentErr)
	}
	var invalid string
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-new-path]').getAttribute('aria-invalid')`, &invalid))
	if invalid != "true" {
		t.Fatalf("path aria-invalid = %q", invalid)
	}

	// Too large: the error is on the text.
	rig.run(t, edSet(`[data-new-path]`, "docs/BIG.md", "input"), edType(strings.Repeat("x", 257*1024)), edClick(`[data-save]`),
		poll(edVisible(`[data-error-for="content"]`)), poll(`!`+edVisible(`[data-error-for="path"]`)))

	// Exists: the error is on the path.
	rig.run(t, edSet(`[data-new-path]`, "CLAUDE.md", "input"), edType("mine"), edClick(`[data-save]`),
		poll(edVisible(`[data-error-for="path"]`)), poll(`!`+edVisible(`[data-error-for="content"]`)))
	rig.run(t, chromedp.Evaluate(edText(`[data-error-for="path"]`), &raw), chromedp.Evaluate(editorText(), &text))
	if !strings.Contains(raw, "already exists") || text != "mine" {
		t.Fatalf("exists: error = %q, text = %q", raw, text)
	}
	if got, _ := rig.files.content("CLAUDE.md"); !strings.HasPrefix(got, "# Repository rules") {
		t.Fatalf("an existing file was overwritten: %q", got)
	}
}

// A save on a stale version keeps the text, offers Reload and a diff against the newest version, and Reload loads it.
func TestInstructionsStaleSaveKeepsTheTextAndOffersReloadAndDiff(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`))
	rig.files.seed("CLAUDE.md", "memory", "# Theirs\nshared\n") // version 2, not announced
	rig.run(t, edType("# Mine\nshared\n"), edClick(`[data-save]`), poll(edVisible(`[data-newer-notice]`)))
	var text string
	rig.run(t, chromedp.Evaluate(editorText(), &text))
	if text != "# Mine\nshared\n" {
		t.Fatalf("text after a stale save = %q", text)
	}
	if !strings.Contains(strings.ToLower(edTextNow(t, rig, `[data-newer-notice]`)), "version 2") {
		t.Fatalf("notice = %q", edTextNow(t, rig, `[data-newer-notice]`))
	}
	rig.run(t, poll(edVisible(`[data-reload]`)+` && `+edVisible(`[data-show-diff]`)), edClick(`[data-show-diff]`), poll(edVisible(`[data-instruction-diff]`)))
	var raw string
	rig.run(t, chromedp.Evaluate(`JSON.stringify([...document.querySelectorAll('`+instructionsPanel+` [data-instruction-diff] [data-diff-line]')].map(l => [l.dataset.diffLine, l.textContent.trim()]))`, &raw))
	if !strings.Contains(raw, `["del","# Mine"]`) || !strings.Contains(raw, `["ins","# Theirs"]`) || !strings.Contains(raw, `["eq","shared"]`) {
		t.Fatalf("diff = %s", raw)
	}
	rig.run(t, edClick(`[data-reload]`), poll(editorText()+` === "# Theirs\nshared\n"`),
		poll(`document.querySelector('`+instructionsPanel+` [data-file-version]')?.textContent.trim() === 'Version 2'`),
		poll(`!`+edVisible(`[data-newer-notice]`)+` && !`+edVisible(`[data-dirty]`)))
}

func edTextNow(t *testing.T, rig *archRig, sel string) string {
	t.Helper()
	var s string
	rig.run(t, chromedp.Evaluate(edText(sel), &s))
	return s
}

// The feed reloads a clean open file; on an edited one it shows a notice and leaves the text.
func TestInstructionsFeedReloadsACleanFileAndOnlyWarnsOnADirtyOne(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`))
	rig.files.saveElsewhere("CLAUDE.md", "from another tab\n")
	rig.run(t, poll(editorText()+` === "from another tab\n"`), poll(`document.querySelector('`+instructionsPanel+` [data-file-version]')?.textContent.trim() === 'Version 2'`))
	var notice bool
	rig.run(t, chromedp.Evaluate(edVisible(`[data-newer-notice]`), &notice))
	if notice {
		t.Fatal("a clean file reloaded and still shows a notice")
	}

	rig.run(t, edType("my edit\n"), poll(edVisible(`[data-dirty]`)))
	rig.files.saveElsewhere("CLAUDE.md", "second change\n")
	rig.run(t, poll(edVisible(`[data-newer-notice]`)))
	var text, version string
	rig.run(t, chromedp.Evaluate(editorText(), &text), chromedp.Evaluate(edText(`[data-file-version]`), &version))
	if text != "my edit\n" || version != "Version 2" {
		t.Fatalf("a dirty file after a feed event: text = %q, version = %q, want its text kept and the version it started from", text, version)
	}
	rig.run(t, edClick(`[data-reload]`), poll(editorText()+` === "second change\n"`), poll(`!`+edVisible(`[data-newer-notice]`)))
	// The list follows too: a file saved elsewhere appears.
	rig.files.saveElsewhere("docs/NEW.md", "n\n")
	rig.run(t, poll(`[...document.querySelectorAll('`+instructionsPanel+` [data-entry] [data-path]')].some(p => p.textContent.trim() === 'docs/NEW.md')`))
}

// A feed event with version 0 shows the file as deleted and keeps any unsaved text; Save puts it back.
func TestInstructionsDeletedElsewhereShowsGoneAndKeepsUnsavedText(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edType("unsaved words\n"))
	rig.files.deleteElsewhere("CLAUDE.md")
	rig.run(t, poll(edVisible(`[data-deleted-notice]`)))
	var text string
	rig.run(t, chromedp.Evaluate(editorText(), &text))
	if text != "unsaved words\n" {
		t.Fatalf("text after the file was deleted = %q", text)
	}
	rig.run(t, edClick(`[data-save]`), poll(`document.querySelector('`+instructionsPanel+` [data-file-version]')?.textContent.trim() === 'Version 1'`), poll(`!`+edVisible(`[data-deleted-notice]`)))
	if got, ok := rig.files.content("CLAUDE.md"); !ok || got != "unsaved words\n" {
		t.Fatalf("stored after saving over a deleted file = %q, %v", got, ok)
	}
	if body := rig.files.lastBody("save_instruction_file"); body["base_version"] != float64(0) {
		t.Fatalf("save body = %v, want base_version 0 to put the file back", body)
	}

	// A clean file that is deleted shows it too.
	rig = instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`))
	rig.files.deleteElsewhere("CLAUDE.md")
	rig.run(t, poll(edVisible(`[data-deleted-notice]`)))
	rig.run(t, chromedp.Evaluate(editorText(), &text))
	if text == "" {
		t.Fatal("the deleted file's text vanished")
	}
}

// Unsaved text survives a trip to another tab and back, and a refusal.
func TestInstructionsUnsavedTextSurvivesATabSwitch(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edType("not saved yet\n"),
		clickTab("Tools"), poll(`document.querySelector('#architecture-panel-tools').checkVisibility()`),
		clickTab("Instructions & rules"), poll(edVisible(`textarea[data-file-text]`)))
	var text string
	rig.run(t, chromedp.Evaluate(editorText(), &text))
	if text != "not saved yet\n" {
		t.Fatalf("text after a tab round trip = %q", text)
	}
	if !strings.Contains(edTextNow(t, rig, `[data-dirty]`), "Unsaved") {
		t.Fatal("the dirty marker is gone after the tab round trip")
	}
}

// The feed echo of the person's own save can arrive before the POST answers; it is not read as someone else's change.
func TestInstructionsOwnSaveEchoIsNotANotice(t *testing.T) {
	rig := instructionRig(t)
	rig.files.saveDelay = 700 * time.Millisecond
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edType("my save\n"), edClick(`[data-save]`), chromedp.Sleep(350*time.Millisecond))
	var notice bool
	rig.run(t, chromedp.Evaluate(edVisible(`[data-newer-notice]`), &notice))
	if notice {
		t.Fatal("the echo of the person's own save showed a notice while the save was in flight")
	}
	rig.run(t, poll(`document.querySelector('`+instructionsPanel+` [data-file-version]')?.textContent.trim() === 'Version 2'`), poll(`!`+edVisible(`[data-dirty]`)))
	var text string
	rig.run(t, chromedp.Evaluate(editorText(), &text), chromedp.Evaluate(edVisible(`[data-newer-notice]`), &notice))
	if text != "my save\n" || notice {
		t.Fatalf("after the answer: text = %q, notice = %v", text, notice)
	}
}

// The editor's controls are labelled and reach by keyboard.
func TestInstructionsEditorControlsAreLabelled(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edType("x"))
	var raw string
	rig.run(t, chromedp.Evaluate(`JSON.stringify(['[data-save]','[data-discard]','textarea[data-file-text]'].map(s => { const e = document.querySelector('`+instructionsPanel+` '+s); return [e.getAttribute('aria-label') ?? e.textContent.trim(), e.disabled === true, e.tabIndex]; }))`, &raw))
	if strings.Contains(raw, `["",`) || strings.Contains(raw, "true,") {
		t.Fatalf("controls = %s, want labelled and enabled once the text is edited", raw)
	}
}
