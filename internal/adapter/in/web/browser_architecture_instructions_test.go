package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

const instructionsPanel = `#architecture-panel-instructions`

// instructionRig is a rig whose harness holds three instruction files: CLAUDE.md
// and a skill that the saved record describes, and a sub-agent prompt it does not.
func instructionRig(t *testing.T) *archRig {
	t.Helper()
	rig := newArchRig(t)
	rig.api.seed(descRecord())
	rig.files.seed("CLAUDE.md", "memory", "# Repository rules\n\nBe **kind** to the next reader.\n")
	rig.files.seed(".claude/skills/go/SKILL.md", "skill", "---\nname: go\n---\nWrite Go.\n")
	rig.files.seed(".claude/agents/reviewer.md", "subagent", "You review diffs.\n")
	return rig
}

// openInstructions loads the tab (optionally on a deep link's query) and waits for its rows.
func openInstructions(t *testing.T, rig *archRig, query string, width int64) {
	t.Helper()
	rig.run(t,
		chromedp.EmulateViewport(width, 900),
		chromedp.Navigate(rig.url("/instructions"+query)),
		poll(`document.querySelectorAll('[data-count]').length === 6`),
		poll(`document.querySelectorAll('`+instructionsPanel+` [data-entry]').length >= 3 && document.querySelector('`+instructionsPanel+`').checkVisibility()`),
	)
	// An event the test sends before the page's feed is connected is lost, so every test starts once it is.
	waitFor(t, "the page's feed", func() bool { return rig.feed.followers() >= 1 })
}

// openFile clicks the row's open control, as a person does, by the path it names.
func openFile(t *testing.T, rig *archRig, path string) {
	t.Helper()
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-open-file][aria-label="Open `+path+`"]').click()`, nil))
}

func editorText() string {
	return `document.querySelector('` + instructionsPanel + ` textarea[data-file-text]')?.value`
}

// The list shows the harness's files with the mockup's row look (a file the
// record does not describe gets the defaults), and opening one puts its text in
// an editor beside the list.
func TestInstructionsListTheHarnessFilesAndOpenOneBesideTheList(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "", 1440)
	d := readDesc(t, rig, "instructions", "data-path", "data-kind", "data-note", "data-meta=applies", "data-meta=loaded")
	if len(d.Entries) != 3 {
		t.Fatalf("rows = %d, want the 2 described files and the sub-agent prompt", len(d.Entries))
	}
	wantPart(t, d, 0, "data-path", "CLAUDE.md")
	wantPart(t, d, 0, "data-note", "Repository rules.")
	wantPart(t, d, 1, "data-meta=applies", "Applies to go-developer, devops")
	wantPart(t, d, 2, "data-path", ".claude/agents/reviewer.md")
	wantPart(t, d, 2, "data-kind", "Sub-agent prompt")
	wantPart(t, d, 2, "data-note", "<none>")
	wantPart(t, d, 2, "data-meta=applies", "Applies to all")
	wantPart(t, d, 2, "data-meta=loaded", "Loaded Always")

	var shut bool
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-instruction-editor]').checkVisibility() === false`, &shut))
	if !shut {
		t.Fatal("the editor is open before any file was")
	}
	openFile(t, rig, "CLAUDE.md")
	rig.run(t, poll(editorText()+` === "# Repository rules\n\nBe **kind** to the next reader.\n"`),
		poll(`document.querySelector('`+instructionsPanel+` textarea[data-file-text]').checkVisibility()`))
	var raw string
	rig.run(t, chromedp.Evaluate(`(() => {
		const panel = document.querySelector('`+instructionsPanel+`');
		const list = panel.querySelector('[data-entries]').getBoundingClientRect();
		const ta = panel.querySelector('textarea[data-file-text]');
		const r = ta.getBoundingClientRect();
		return JSON.stringify({ beside: r.left >= list.right - 1 && r.top < list.bottom, mono: getComputedStyle(ta).fontFamily, path: panel.querySelector('[data-instruction-editor] [data-editor-path]').textContent.trim() });
	})()`, &raw))
	var got struct {
		Beside bool   `json:"beside"`
		Mono   string `json:"mono"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Beside || !containsFold(got.Mono, "mono") || got.Path != "CLAUDE.md" {
		t.Fatalf("editor = %+v, want a monospace text area beside the list, titled with the path", got)
	}
	if body := rig.files.lastBody("get_instruction_file"); body["repository_id"] != "r1" || body["path"] != "CLAUDE.md" {
		t.Fatalf("get body = %v", body)
	}
	// Rows still open the metadata drawer as before.
	rig.run(t, chromedp.Evaluate(`document.querySelectorAll('`+instructionsPanel+` [data-entry]')[0].querySelector('button[data-edit]').click()`, nil),
		poll(`document.querySelector('`+instructionsPanel+` [data-drawer]').checkVisibility()`))
}

// A row the record describes but the harness holds no file for offers no editor.
func TestInstructionsRowWithoutAFileOffersNoEditor(t *testing.T) {
	rig := instructionRig(t)
	rig.api.seed(func() map[string]any {
		rec := descRecord()
		rec["instructions"] = append(rec["instructions"].([]any), map[string]any{"path": "docs/orphan.md", "kind": "rules", "applies_to": []any{"all"}, "loaded": "always"})
		return rec
	}())
	openInstructions(t, rig, "", 1440)
	var n int
	rig.run(t, chromedp.Evaluate(`document.querySelectorAll('`+instructionsPanel+` [data-open-file][aria-label="Open docs/orphan.md"]').length`, &n))
	if n != 0 {
		t.Fatalf("open controls for a path with no file = %d", n)
	}
}

// The preview shows rendered Markdown, escapes embedded HTML, and goes back to the text.
func TestInstructionsPreviewRendersMarkdownAndEscapesHTML(t *testing.T) {
	rig := instructionRig(t)
	rig.files.seed("AGENTS.md", "memory", "# Title\n\nSome **bold** and <img src=x onerror=\"window.pwned=1\"> text.\n")
	openInstructions(t, rig, "?path=AGENTS.md", 1440)
	rig.run(t, poll(editorText()+` !== undefined && `+editorText()+` !== ""`),
		chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-view="preview"]').click()`, nil),
		poll(`!!document.querySelector('`+instructionsPanel+` [data-file-preview] h1')`))
	var raw string
	rig.run(t, chromedp.Evaluate(`(() => {
		const p = document.querySelector('`+instructionsPanel+` [data-file-preview]');
		const ta = document.querySelector('`+instructionsPanel+` textarea[data-file-text]');
		return JSON.stringify({ h1: p.querySelector('h1').textContent, bold: p.querySelector('strong')?.textContent, imgs: p.querySelectorAll('img').length, text: p.textContent.includes('<img src=x'), pwned: window.pwned === 1, previewShown: p.checkVisibility(), textShown: ta.checkVisibility() });
	})()`, &raw))
	var got struct {
		H1           string `json:"h1"`
		Bold         string `json:"bold"`
		Imgs         int    `json:"imgs"`
		Text         bool   `json:"text"`
		Pwned        bool   `json:"pwned"`
		PreviewShown bool   `json:"previewShown"`
		TextShown    bool   `json:"textShown"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.H1 != "Title" || got.Bold != "bold" || got.Imgs != 0 || !got.Text || got.Pwned || !got.PreviewShown || got.TextShown {
		t.Fatalf("preview = %+v, want rendered Markdown with the embedded HTML shown as text", got)
	}
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-view="text"]').click()`, nil),
		poll(`document.querySelector('`+instructionsPanel+` textarea[data-file-text]').checkVisibility() && !document.querySelector('`+instructionsPanel+` [data-file-preview]').checkVisibility()`))
}

// A deep link with ?path= opens that file; an unknown path says so.
func TestInstructionsDeepLinkOpensTheFileOrSaysItIsNotFound(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=.claude/agents/reviewer.md", 1440)
	rig.run(t, poll(editorText()+` === "You review diffs.\n"`))

	rig = instructionRig(t)
	openInstructions(t, rig, "?path=nope/MISSING.md", 1440)
	rig.run(t, poll(`!!document.querySelector('`+instructionsPanel+` [data-not-found]') && document.querySelector('`+instructionsPanel+` [data-not-found]').checkVisibility()`))
	var msg string
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-not-found]').textContent.replace(/\s+/g, ' ').trim()`, &msg))
	if !strings.Contains(msg, "nope/MISSING.md") || !strings.Contains(msg, "no instruction file") {
		t.Fatalf("not-found message = %q", msg)
	}
	var shown bool
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` textarea[data-file-text]')?.checkVisibility() ?? false`, &shown))
	if shown {
		t.Fatal("an unknown path shows an editor")
	}
}

// The tab says when agents get the files; the editor opens and toggles with the keyboard.
func TestInstructionsDeliveryNoteAndKeyboard(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "", 1440)
	var note string
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-delivery-note]').textContent.replace(/\s+/g, ' ').trim()`, &note))
	if !strings.Contains(note, "when their session starts") || !strings.Contains(note, "running sessions keep") {
		t.Fatalf("delivery note = %q", note)
	}
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-open-file][aria-label="Open CLAUDE.md"]').focus()`, nil),
		chromedp.KeyEvent("\r"),
		poll(editorText()+` !== undefined && `+editorText()+` !== ""`),
		chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-view="preview"]').focus()`, nil),
		chromedp.KeyEvent(" "),
		poll(`!!document.querySelector('`+instructionsPanel+` [data-file-preview] strong')`),
	)
	var raw string
	rig.run(t, chromedp.Evaluate(`(() => {
		const p = document.querySelector('`+instructionsPanel+`');
		const name = (e) => e.getAttribute('aria-label') ?? e.textContent.trim();
		return JSON.stringify({
			pressed: p.querySelector('[data-view="preview"]').getAttribute('aria-pressed'),
			textPressed: p.querySelector('[data-view="text"]').getAttribute('aria-pressed'),
			area: p.querySelector('textarea[data-file-text]').getAttribute('aria-label'),
			close: name(p.querySelector('[data-editor-close]')),
			group: p.querySelector('[data-instruction-editor] [role=group]').getAttribute('aria-label'),
		});
	})()`, &raw))
	var got map[string]string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got["pressed"] != "true" || got["textPressed"] != "false" || got["area"] == "" || got["close"] == "" || got["group"] == "" {
		t.Fatalf("editor controls = %v, want a pressed preview toggle and labelled controls", got)
	}
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-editor-close]').focus()`, nil),
		chromedp.KeyEvent("\r"),
		poll(`!document.querySelector('`+instructionsPanel+` [data-instruction-editor]').checkVisibility()`))
}

// At a narrow width the editor stacks below the list.
func TestInstructionsEditorStacksBelowTheListWhenNarrow(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "?path=CLAUDE.md", 600)
	rig.run(t, poll(editorText()+` !== undefined && `+editorText()+` !== ""`))
	var below bool
	rig.run(t, chromedp.Evaluate(`(() => {
		const panel = document.querySelector('`+instructionsPanel+`');
		const list = panel.querySelector('[data-entries]').getBoundingClientRect();
		const ed = panel.querySelector('[data-instruction-editor]').getBoundingClientRect();
		return ed.top >= list.bottom - 1 && ed.right <= innerWidth + 1;
	})()`, &below))
	if !below {
		t.Fatal("the editor does not stack below the list at 600px")
	}
}

// Edit on a file the record does not describe opens the metadata drawer with the
// defaults; Cancel leaves the page's draft as it was.
func TestInstructionsEditOnAnUndescribedFileOpensTheDrawerAndCancelLeavesNoDraft(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t,
		chromedp.Evaluate(`document.querySelectorAll('`+instructionsPanel+` [data-entry]')[2].querySelector('button[data-edit]').click()`, nil),
		poll(`document.querySelector('`+instructionsPanel+` [data-drawer]').checkVisibility()`))
	var raw string
	rig.run(t, chromedp.Evaluate(`JSON.stringify({ path: document.querySelector('`+instructionsPanel+` [data-drawer] [data-field=path]').value, loaded: document.querySelector('`+instructionsPanel+` [data-drawer] [data-field=loaded]').value, kind: document.querySelector('`+instructionsPanel+` [data-drawer] [data-field=kind]').value })`, &raw))
	if raw != `{"path":".claude/agents/reviewer.md","loaded":"always","kind":"subagent_prompt"}` {
		t.Fatalf("drawer fields = %s", raw)
	}
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-drawer] [data-cancel]').click()`, nil),
		poll(`!document.querySelector('`+instructionsPanel+` [data-drawer]').checkVisibility()`))
	var rows int
	var barShown bool
	rig.run(t, chromedp.Evaluate(`document.querySelectorAll('`+instructionsPanel+` [data-entry]').length`, &rows),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-save-bar]')].some(b => b.checkVisibility())`, &barShown))
	if rows != 3 || barShown {
		t.Fatalf("after Cancel: rows = %d, save bar shown = %v, want 3 rows and no draft", rows, barShown)
	}
}
