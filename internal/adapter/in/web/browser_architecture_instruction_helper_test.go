package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/web/shell"
)

// The writing helper in the instruction file editor (ticket 08): the pen sits
// on the editor's text area, reads the file's Path and Kind, and changes the
// text only when the person accepts a draft.

const (
	edPen      = instructionsPanel + ` textarea[data-file-text] ~ .pen`
	edPenPanel = `#pen-instruction-file-panel`
)

func helperRig(t *testing.T, helper shell.Helper) (*archRig, *fakeHelperAPI) {
	t.Helper()
	api := newFakeHelperAPI()
	rig := newArchRigWithHelper(t, &helper, api)
	rig.api.seed(descRecord())
	rig.files.seed("CLAUDE.md", "memory", "# Repository rules\n\nBe kind.\n")
	rig.files.seed(".claude/agents/reviewer.md", "subagent", "You review diffs.\n")
	return rig, api
}

// Accepting a draft replaces the text the way typing does: the editor is dirty, Undo (the toast and ⌘Z) restores it, and the request told the server what the file is.
func TestInstructionEditorPenDraftsReplacesTheTextAndUndoRestoresIt(t *testing.T) {
	rig, api := helperRig(t, whAvailable)
	openInstructions(t, rig, "?path=.claude/agents/reviewer.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), whPoll(`!!document.querySelector('[data-panel-id=pen-instruction-file]')?._x_dataStack`))

	var label string
	var disabled bool
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('`+edPen+`').getAttribute('aria-label')`, &label),
		chromedp.Evaluate(`document.querySelector('`+edPen+`').disabled`, &disabled),
	)
	if label != "Write the instruction file with the helper" || disabled {
		t.Fatalf("pen: label %q, disabled %v", label, disabled)
	}
	var dirty bool
	rig.run(t, chromedp.Evaluate(edVisible(`[data-dirty]`), &dirty))
	if dirty {
		t.Fatal("a file just opened is dirty")
	}

	rig.run(t, chromedp.Evaluate(`document.querySelector('`+edPen+`').click()`, nil), whPoll(whPhase("Which fields get the pen first?")))
	steps := api.requests()
	if len(steps) != 1 || steps[0]["step"] != "analyze" {
		t.Fatalf("steps = %v, want one analyze", steps)
	}
	field, _ := steps[0]["field"].(map[string]any)
	if field["kind"] != "instruction_file" || field["text"] != "You review diffs.\n" {
		t.Errorf("field = %v", field)
	}
	if steps[0]["project_id"] != "p1" {
		t.Errorf("project_id = %v", steps[0]["project_id"])
	}
	siblings, _ := steps[0]["siblings"].([]any)
	got := ""
	for _, s := range siblings {
		m, _ := s.(map[string]any)
		got += m["label"].(string) + "=" + m["text"].(string) + ";"
	}
	if got != "Path=.claude/agents/reviewer.md;Kind=subagent;" {
		t.Errorf("siblings = %q", got)
	}
	var reads string
	rig.run(t, chromedp.Evaluate(`[...document.querySelectorAll('`+edPenPanel+` .reads > span')].map((e) => e.textContent).join(' ')`, &reads))
	if reads != "Reads Instruction file Path Kind project coding_pool" {
		t.Errorf("reads = %q", reads)
	}

	// Nothing changes until Use this.
	var text string
	rig.run(t, whClick(edPenPanel, "Skip questions"), whPoll(whPhase("Proposed instruction file")), chromedp.Evaluate(editorText(), &text), chromedp.Evaluate(edVisible(`[data-dirty]`), &dirty))
	if text != "You review diffs.\n" || dirty {
		t.Fatalf("before Use this: text %q, dirty %v", text, dirty)
	}
	rig.run(t, whClick(edPenPanel, "Use this"), whPoll(`!`+whPanelShown),
		poll(editorText()+`.startsWith('Add a magic pen')`),
		poll(edVisible(`[data-dirty]`)),
		poll(`!document.querySelector('`+instructionsPanel+` [data-save]').disabled`),
	)
	var active bool
	rig.run(t, chromedp.Evaluate(`document.activeElement.matches('textarea[data-file-text]')`, &active))
	if !active {
		t.Errorf("focus is not back on the text area")
	}

	// Undo from the toast puts the old text back and the editor is clean again.
	rig.run(t, whClick(edPenPanel+` ~ .toast-region`, "Undo"),
		poll(editorText()+` === 'You review diffs.\n'`),
		poll(`!`+edVisible(`[data-dirty]`)),
	)

	// ⌘Z after accepting restores it too (the write is the browser's own edit).
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+edPen+`').click()`, nil), whPoll(whPhase("Which fields get the pen first?")),
		whClick(edPenPanel, "Skip questions"), whPoll(whPhase("Proposed instruction file")),
		whClick(edPenPanel, "Use this"), poll(editorText()+`.startsWith('Add a magic pen')`),
		chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` textarea[data-file-text]').focus()`, nil),
		chromedp.Evaluate(`document.execCommand('undo')`, nil),
		poll(editorText()+` === 'You review diffs.\n'`),
		poll(`!`+edVisible(`[data-dirty]`)),
	)
	if e := rig.errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A new file's Path and Kind are the ones on the form right now, not the ones it opened with.
func TestInstructionEditorPenReadsTheNewFilesPathAndKind(t *testing.T) {
	rig, api := helperRig(t, whAvailable)
	openInstructions(t, rig, "", 1440)
	rig.run(t, edClick(`[data-add]`), poll(edVisible(`textarea[data-file-text]`)),
		whPoll(`!!document.querySelector('[data-panel-id=pen-instruction-file]')?._x_dataStack`),
		edSet(`[data-new-kind]`, "skill", "change"),
		edSet(`[data-new-path]`, ".claude/skills/review/SKILL.md", "input"),
		chromedp.Evaluate(`document.querySelector('`+edPen+`').click()`, nil),
		whPoll(whPhase("The instruction file is empty")),
		whPoll(whPhase("What should this file make agents do?")),
	)
	var lead string
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+edPenPanel+` .lead').textContent`, &lead))
	if !strings.Contains(lead, "or start from the path") {
		t.Errorf("lead = %q", lead)
	}
	rig.run(t, chromedp.Evaluate(`(() => { const i = document.querySelector('#pen-instruction-file-start'); i.value = 'plan the work'; i.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil),
		whClick(edPenPanel, "Continue"))
	waitFor(t, "the analyze request", func() bool { return len(api.requests()) == 1 })
	steps := api.requests()
	siblings, _ := steps[0]["siblings"].([]any)
	got := ""
	for _, s := range siblings {
		m, _ := s.(map[string]any)
		got += m["label"].(string) + "=" + m["text"].(string) + ";"
	}
	if got != "Path=.claude/skills/review/SKILL.md;Kind=skill;" {
		t.Errorf("siblings = %q", got)
	}
	field, _ := steps[0]["field"].(map[string]any)
	if field["kind"] != "instruction_file" {
		t.Errorf("field = %v", field)
	}
}

// With no helper the pen is there, disabled, and says why; the text area is unaffected.
func TestInstructionEditorPenIsDisabledWithItsReasonWhenTheHelperIsUnavailable(t *testing.T) {
	rig, api := helperRig(t, shell.Helper{Reason: "Claude is not signed in", FixURL: "/agent-isolation#credential"})
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), whPoll(`!!document.querySelector('[data-panel-id=pen-instruction-file]')?._x_dataStack`))
	var disabled bool
	var label, title string
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('`+edPen+`').disabled`, &disabled),
		chromedp.Evaluate(`document.querySelector('`+edPen+`').getAttribute('aria-label')`, &label),
		chromedp.Evaluate(`document.querySelector('`+edPen+`').title`, &title),
		edType("typed\n"),
		whKey(instructionsPanel+` textarea[data-file-text]`, "j", whCtrl),
		chromedp.Sleep(200*time.Millisecond),
	)
	if !disabled || label != "Writing helper unavailable" || !strings.Contains(title, "Claude is not signed in") {
		t.Errorf("disabled %v, label %q, title %q", disabled, label, title)
	}
	var shown bool
	rig.run(t, chromedp.Evaluate(whPanelShown, &shown), poll(edVisible(`[data-dirty]`)))
	if shown || len(api.requests()) != 0 {
		t.Errorf("panel shown %v, steps %v", shown, api.requests())
	}
}

// The Write/Preview toggle keeps working with the pen in the text area's place: Preview hides the pen with the text, Write brings both back.
func TestInstructionEditorPenFollowsTheWritePreviewToggle(t *testing.T) {
	rig, _ := helperRig(t, whAvailable)
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), poll(edVisible(`textarea[data-file-text] ~ .pen`)),
		edClick(`[data-view="preview"]`), poll(edVisible(`[data-file-preview]`)+` && !`+edVisible(`textarea[data-file-text] ~ .pen`)),
		edClick(`[data-view="text"]`), poll(edVisible(`textarea[data-file-text]`)+` && `+edVisible(`textarea[data-file-text] ~ .pen`)),
	)
}
