package web

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

const banner = `[data-repository-banner]`

// copiesRig is an instructionRig whose checkout also holds: AGENTS.md (not in
// the harness), CLAUDE.md as the harness has it (same, imported), and a
// sub-agent prompt that differs from the harness's (imported).
func copiesRig(t *testing.T) *archRig {
	t.Helper()
	rig := instructionRig(t)
	rig.files.setCopies(
		fakeRepositoryCopy{Path: "AGENTS.md", Kind: "memory", Content: "agents\n"},
		fakeRepositoryCopy{Path: "CLAUDE.md", Kind: "memory", Content: "# Repository rules\n\nBe **kind** to the next reader.\n"},
		fakeRepositoryCopy{Path: ".claude/agents/reviewer.md", Kind: "subagent", Content: "You review diffs differently.\n"},
	)
	return rig
}

func copyRow(path string) string {
	return banner + ` [data-repository-copy][data-copy-path="` + path + `"]`
}

func copyField(path, sel string) string {
	return edText(copyRow(path) + ` ` + sel)
}

func bannerCopies() string {
	return `JSON.stringify([...document.querySelectorAll('` + instructionsPanel + ` ` + banner + ` [data-repository-copy]')].map(r => r.dataset.copyPath))`
}

func evalStr(t *testing.T, rig *archRig, js string) string {
	t.Helper()
	var out string
	rig.run(t, chromedp.Evaluate(js, &out))
	return out
}

// The banner lists what the repository still holds, each marked differs or same in words, says those copies stay in git, and warns that an imported copy is read twice.
func TestInstructionsBannerListsTheRepositoryCopiesWithDiffersAndSame(t *testing.T) {
	rig := copiesRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(edVisible(banner)), poll(`document.querySelectorAll('`+instructionsPanel+` `+banner+` [data-repository-copy]').length === 3`))
	if got := evalStr(t, rig, bannerCopies()); got != `["AGENTS.md","CLAUDE.md",".claude/agents/reviewer.md"]` {
		t.Fatalf("banner copies = %s", got)
	}
	for path, want := range map[string]string{"AGENTS.md": "differs", "CLAUDE.md": "same", ".claude/agents/reviewer.md": "differs"} {
		if got := evalStr(t, rig, copyField(path, `[data-copy-mark]`)); got != want {
			t.Fatalf("%s mark = %q, want %q", path, got, want)
		}
	}
	// The mark is a word and a tone, never a colour alone.
	tones := evalStr(t, rig, `JSON.stringify(['AGENTS.md','CLAUDE.md'].map(p => document.querySelector('`+instructionsPanel+` `+banner+` [data-copy-path="'+p+'"] [data-copy-mark]').dataset.tone))`)
	if tones != `["attention","neutral"]` {
		t.Fatalf("mark tones = %s", tones)
	}
	if got := evalStr(t, rig, edText(banner+` [data-repository-note]`)); !strings.Contains(got, "stay in git until") {
		t.Fatalf("banner note = %q", got)
	}
	// Imported copies say so; a copy the harness lacks does not.
	imported := evalStr(t, rig, `JSON.stringify([...document.querySelectorAll('`+instructionsPanel+` `+banner+` [data-repository-copy]')].map(r => !!r.querySelector('[data-copy-imported]')?.checkVisibility()))`)
	if imported != `[false,true,true]` {
		t.Fatalf("imported marks = %s", imported)
	}
	// Two copies are in the repository and in the harness: the page warns both reach the agent.
	if got := evalStr(t, rig, edText(banner+` [data-both-copies]`)); !strings.Contains(got, "2 files") || !strings.Contains(got, "both") {
		t.Fatalf("both-copies warning = %q", got)
	}
}

// With nothing left in the repository there is no banner.
func TestInstructionsNoBannerWithoutRepositoryCopies(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(`document.querySelector('`+instructionsPanel+` [data-entry]') !== null`))
	var present bool
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` `+banner+`')?.checkVisibility() ?? false`, &present))
	if present {
		t.Fatal("a banner shows with no repository copies")
	}
	if !hasString(rig.files.called(), "list_instruction_files") {
		t.Fatal("the list was never asked")
	}
}

// Import on one file adds it to the list and the banner marks it imported and same; Import all takes what is left to import.
func TestInstructionsImportOneFileAndImportAll(t *testing.T) {
	rig := copiesRig(t)
	rig.files.setCopies(append(rig.files.copiesNow(), fakeRepositoryCopy{Path: "docs/RULES.md", Kind: "memory", Content: "docs rules\n"})...)
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(`document.querySelectorAll('`+instructionsPanel+` `+banner+` [data-repository-copy]').length === 4`))
	if got := evalStr(t, rig, `JSON.parse(`+listedPaths()+`).includes('AGENTS.md').toString()`); got != "false" {
		t.Fatal("AGENTS.md is listed before it was imported")
	}
	// The same, imported CLAUDE.md has no Import.
	var none bool
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` `+banner+` [aria-label="Import CLAUDE.md"]') === null`, &none))
	if !none {
		t.Fatal("an imported copy that is the same still offers Import")
	}
	rig.run(t, edClick(banner+` [aria-label="Import AGENTS.md"]`),
		poll(`JSON.parse(`+listedPaths()+`).includes('AGENTS.md')`),
		poll(edText(copyRow("AGENTS.md")+` [data-copy-mark]`)+` === 'same'`),
		poll(edVisible(copyRow("AGENTS.md")+` [data-copy-imported]`)))
	if body := rig.files.lastBody("import_instruction_files"); body["repository_id"] != "r1" || strings.Join(stringList(body["paths"]), ",") != "AGENTS.md" {
		t.Fatalf("import body = %v", body)
	}
	if got, _ := rig.files.content("AGENTS.md"); got != "agents\n" {
		t.Fatalf("imported text = %q", got)
	}
	// Import all: the two that still differ or are not in the harness.
	rig.run(t, edClick(banner+` [data-import-all]`),
		poll(`JSON.parse(`+listedPaths()+`).includes('docs/RULES.md')`),
		poll(`[...document.querySelectorAll('`+instructionsPanel+` `+banner+` [data-copy-mark]')].every(m => m.textContent.trim() === 'same')`))
	if body := rig.files.lastBody("import_instruction_files"); strings.Join(stringList(body["paths"]), ",") != ".claude/agents/reviewer.md,docs/RULES.md" {
		t.Fatalf("import all body = %v", body)
	}
	if got := evalStr(t, rig, bannerCopies()); strings.Count(got, ",") != 3 {
		t.Fatalf("copies stay listed until removed: %s", got)
	}
	rig.run(t, poll(`!`+edVisible(banner+` [data-import-all]`)))
}

// An import that reaches the file open in the editor leaves unsaved text alone and says a newer version is there; a clean file reloads.
func TestInstructionsImportKeepsUnsavedEditorText(t *testing.T) {
	rig := copiesRig(t)
	rig.files.setCopies(fakeRepositoryCopy{Path: "CLAUDE.md", Kind: "memory", Content: "from git\n"})
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edType("my unsaved words\n"), poll(edVisible(`[data-dirty]`)))
	rig.run(t, edClick(banner+` [aria-label="Import CLAUDE.md"]`), poll(edVisible(`[data-newer-notice]`)))
	if got := evalStr(t, rig, editorText()); got != "my unsaved words\n" {
		t.Fatalf("editor text after an import = %q", got)
	}
	if got := rig.files.version("CLAUDE.md"); got != 2 {
		t.Fatalf("version after import = %d", got)
	}
	// Reload takes the imported text.
	rig.run(t, edClick(`[data-reload]`), poll(editorText()+` === "from git\n"`))
}

func TestInstructionsImportOnACleanOpenFileReloadsIt(t *testing.T) {
	rig := copiesRig(t)
	rig.files.setCopies(fakeRepositoryCopy{Path: "CLAUDE.md", Kind: "memory", Content: "from git\n"})
	openInstructions(t, rig, "?path=CLAUDE.md", 1440)
	rig.run(t, poll(editorText()+` !== ""`), edClick(banner+` [aria-label="Import CLAUDE.md"]`), poll(editorText()+` === "from git\n"`))
}

// A refused import says so and changes nothing.
func TestInstructionsImportRefusalIsExplained(t *testing.T) {
	rig := copiesRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(edVisible(banner)))
	rig.files.refuseNext("import_instruction_files", 404, "REPOSITORY_NOT_FOUND", nil)
	rig.run(t, edClick(banner+` [aria-label="Import AGENTS.md"]`), poll(edVisible(banner+` [data-repository-error]`)))
	if got := evalStr(t, rig, edText(banner+` [data-repository-error]`)); !strings.Contains(got, "not in the project") {
		t.Fatalf("refusal = %q", got)
	}
	if _, ok := rig.files.content("AGENTS.md"); ok {
		t.Fatal("a refused import stored the file")
	}
}

// Remove from repository asks in the page first, then makes one commit and shows it; the removed copy leaves the banner and the warning goes with it.
func TestInstructionsRemoveFromRepositoryAsksThenShowsTheCommit(t *testing.T) {
	rig := copiesRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(edVisible(banner+` [data-both-copies]`)))
	// A copy the harness lacks cannot be removed yet.
	var absent bool
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` `+banner+` [aria-label="Remove AGENTS.md from the repository"]') === null`, &absent))
	if !absent {
		t.Fatal("a copy that is not imported offers Remove")
	}
	rig.run(t, edClick(banner+` [aria-label="Remove CLAUDE.md from the repository"]`), poll(edVisible(banner+` [data-remove-confirm]`)))
	ask := evalStr(t, rig, edText(banner+` [data-remove-confirm]`))
	for _, want := range []string{"CLAUDE.md", "one commit", "default branch", "no push"} {
		if !strings.Contains(ask, want) {
			t.Fatalf("confirmation %q lacks %q", ask, want)
		}
	}
	if rig.files.lastBody("remove_instruction_files_from_repository") != nil {
		t.Fatal("the removal was sent before the person confirmed")
	}
	// Cancel changes nothing.
	rig.run(t, edClick(banner+` [data-cancel-remove]`), poll(`!`+edVisible(banner+` [data-remove-confirm]`)))
	if rig.files.lastBody("remove_instruction_files_from_repository") != nil {
		t.Fatal("Cancel sent the removal")
	}
	rig.run(t, edClick(banner+` [aria-label="Remove CLAUDE.md from the repository"]`), poll(edVisible(banner+` [data-remove-confirm]`)), edClick(banner+` [data-confirm-remove]`),
		poll(edVisible(banner+` [data-removed-commit]`)))
	if got := evalStr(t, rig, edText(banner+` [data-removed-commit]`)); !strings.Contains(got, "abc1234") || strings.Contains(got, "abc1234def") {
		t.Fatalf("commit note = %q, want the short hash", got)
	}
	rig.run(t, poll(`!document.querySelector('`+instructionsPanel+` `+copyRow("CLAUDE.md")+`')`), poll(`!`+edVisible(banner+` [data-remove-confirm]`)))
	if body := rig.files.lastBody("remove_instruction_files_from_repository"); strings.Join(stringList(body["paths"]), ",") != "CLAUDE.md" {
		t.Fatalf("remove body = %v", body)
	}
	if n := len(rig.files.madeCommits()); n != 1 {
		t.Fatalf("commits = %d, want one", n)
	}
	// The file stays in the harness and in the list.
	if _, ok := rig.files.content("CLAUDE.md"); !ok {
		t.Fatal("the harness lost the file")
	}
	if got := evalStr(t, rig, `JSON.parse(`+listedPaths()+`).includes('CLAUDE.md').toString()`); got != "true" {
		t.Fatal("the list lost the file")
	}
	// Remove all imported: the sub-agent prompt's repository copy differs from the harness's, so the server refuses; Import it again, then it goes.
	rig.run(t, edClick(banner+` [data-remove-all]`), poll(edVisible(banner+` [data-remove-confirm]`)), edClick(banner+` [data-confirm-remove]`),
		poll(edVisible(banner+` [data-repository-error]`)))
	if got := evalStr(t, rig, edText(banner+` [data-repository-error]`)); !strings.Contains(got, "changed since you imported it") || !strings.Contains(got, "Import it again first") {
		t.Fatalf("differs refusal = %q", got)
	}
	if n := len(rig.files.madeCommits()); n != 1 {
		t.Fatalf("commits after a refused removal = %d, want still one", n)
	}
	rig.run(t, edClick(banner+` [data-repository-error] [aria-label="Import .claude/agents/reviewer.md"]`),
		poll(edText(copyRow(".claude/agents/reviewer.md")+` [data-copy-mark]`)+` === 'same'`), poll(`!`+edVisible(banner+` [data-repository-error]`)))
	rig.run(t, edClick(banner+` [data-remove-all]`), poll(edVisible(banner+` [data-remove-confirm]`)), edClick(banner+` [data-confirm-remove]`),
		poll(`!document.querySelector('`+instructionsPanel+` `+copyRow(".claude/agents/reviewer.md")+`')`),
		poll(`!`+edVisible(banner+` [data-both-copies]`)))
	if body := rig.files.lastBody("remove_instruction_files_from_repository"); strings.Join(stringList(body["paths"]), ",") != ".claude/agents/reviewer.md" {
		t.Fatalf("remove all body = %v", body)
	}
	if n := len(rig.files.madeCommits()); n != 2 {
		t.Fatalf("commits = %d, want two", n)
	}
	// AGENTS.md is the copy left, still in the banner, not imported.
	if got := evalStr(t, rig, bannerCopies()); got != `["AGENTS.md"]` {
		t.Fatalf("copies after removal = %s", got)
	}
}

// The repository copy changed after the import: the server refuses the removal, the page says so in words and offers Import for that file.
func TestInstructionsRemoveRefusedWhenTheRepositoryCopyChanged(t *testing.T) {
	rig := copiesRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(edVisible(banner+` [data-repository-copy]`)))
	rig.files.changeCopy("CLAUDE.md", "edited in git\n")
	rig.run(t, edClick(banner+` [aria-label="Remove CLAUDE.md from the repository"]`), poll(edVisible(banner+` [data-remove-confirm]`)), edClick(banner+` [data-confirm-remove]`),
		poll(edVisible(banner+` [data-repository-error] [data-refusal-import]`)),
		poll(edText(copyRow("CLAUDE.md")+` [data-copy-mark]`)+` === 'differs'`))
	if got := evalStr(t, rig, edText(banner+` [data-repository-error]`)); !strings.Contains(got, "Import it again first") {
		t.Fatalf("refusal = %q", got)
	}
	if n := len(rig.files.madeCommits()); n != 0 {
		t.Fatalf("commits = %d", n)
	}
	rig.run(t, edClick(banner+` [data-refusal-import]`), poll(edText(copyRow("CLAUDE.md")+` [data-copy-mark]`)+` === 'same'`))
	if got, _ := rig.files.content("CLAUDE.md"); got != "edited in git\n" {
		t.Fatalf("imported text = %q", got)
	}
	rig.run(t, edClick(banner+` [aria-label="Remove CLAUDE.md from the repository"]`), poll(edVisible(banner+` [data-remove-confirm]`)), edClick(banner+` [data-confirm-remove]`),
		poll(edVisible(banner+` [data-removed-commit]`)))
}

// A copy that left the default branch since the list was read is refused in words, and the list then drops it.
func TestInstructionsRemoveRefusedWhenTheCopyLeftTheDefaultBranch(t *testing.T) {
	rig := copiesRig(t)
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(edVisible(banner+` [data-repository-copy]`)))
	rig.files.dropCopy("CLAUDE.md")
	rig.run(t, edClick(banner+` [aria-label="Remove CLAUDE.md from the repository"]`), poll(edVisible(banner+` [data-remove-confirm]`)), edClick(banner+` [data-confirm-remove]`),
		poll(edVisible(banner+` [data-repository-error]`)))
	if got := evalStr(t, rig, edText(banner+` [data-repository-error]`)); got != "That file is no longer on the repository's default branch." {
		t.Fatalf("refusal = %q", got)
	}
	rig.run(t, poll(`!document.querySelector('`+instructionsPanel+` `+copyRow("CLAUDE.md")+`')`))
}

// When the checkout cannot be read the banner says so with the server's message, instead of nothing.
func TestInstructionsBannerSaysWhenTheRepositoryCannotBeRead(t *testing.T) {
	rig := instructionRig(t)
	rig.files.setUnreadable("the checkout is not a git repository")
	openInstructions(t, rig, "", 1440)
	rig.run(t, poll(edVisible(banner)))
	got := evalStr(t, rig, edText(banner+` [data-repository-unreadable]`))
	if !strings.Contains(got, "could not be read") || !strings.Contains(got, "the checkout is not a git repository") {
		t.Fatalf("unreadable note = %q", got)
	}
	if n := evalStr(t, rig, `String(document.querySelectorAll('`+instructionsPanel+` `+banner+` [data-repository-copy]').length)`); n != "0" {
		t.Fatalf("copies listed for an unreadable repository: %s", n)
	}
	// Readable again: the banner goes with the next list.
	rig.files.setUnreadable("")
	rig.files.saveElsewhere("docs/NEW.md", "n\n")
	rig.run(t, poll(`!`+edVisible(banner)))
}

// Each refusal of the removal has its own sentence and changes nothing.
func TestInstructionsRemoveRefusalsEachHaveTheirOwnSentence(t *testing.T) {
	want := map[string]string{
		"CHECKOUT_DIRTY":           "uncommitted changes",
		"WRONG_BRANCH":             "not on its default branch",
		"INSTRUCTION_NOT_IMPORTED": "not in the harness yet",
		"INSTRUCTION_DIFFERS":      "changed since you imported it",
		"INSTRUCTION_NOT_FOUND":    "no longer on the repository's default branch",
	}
	said := map[string]bool{}
	for code, part := range want {
		rig := copiesRig(t)
		openInstructions(t, rig, "", 1440)
		rig.run(t, poll(edVisible(banner+` [data-repository-copy]`)))
		rig.files.refuseNext("remove_instruction_files_from_repository", 409, code, nil)
		rig.run(t, edClick(banner+` [aria-label="Remove CLAUDE.md from the repository"]`), poll(edVisible(banner+` [data-remove-confirm]`)), edClick(banner+` [data-confirm-remove]`),
			poll(edVisible(banner+` [data-repository-error]`)))
		got := evalStr(t, rig, edText(banner+` [data-repository-error]`))
		if !strings.Contains(got, part) {
			t.Fatalf("%s: sentence = %q, want it to say %q", code, got, part)
		}
		if said[got] {
			t.Fatalf("%s repeats another refusal's sentence: %q", code, got)
		}
		said[got] = true
		if n := len(rig.files.madeCommits()); n != 0 {
			t.Fatalf("%s: a refused removal made %d commits", code, n)
		}
		if got := evalStr(t, rig, bannerCopies()); got != `["AGENTS.md","CLAUDE.md",".claude/agents/reviewer.md"]` {
			t.Fatalf("%s: copies = %s", code, got)
		}
		if got := evalStr(t, rig, `String(`+edVisible(banner+` [data-removed-commit]`)+`)`); got != "false" {
			t.Fatalf("%s: a commit shows after a refusal", code)
		}
		if got := evalStr(t, rig, `String(`+edVisible(banner+` [data-both-copies]`)+`)`); got != "true" {
			t.Fatalf("%s: the both-copies warning went", code)
		}
	}
}

// The banner's controls are named, reachable by keyboard and fit a narrow screen.
func TestInstructionsBannerControlsAreLabelledAndFitNarrow(t *testing.T) {
	rig := copiesRig(t)
	openInstructions(t, rig, "", 400)
	rig.run(t, poll(edVisible(banner+` [data-repository-copy]`)))
	got := evalStr(t, rig, `JSON.stringify([...document.querySelectorAll('`+instructionsPanel+` `+banner+` button')].filter(b => b.checkVisibility()).map(b => (b.getAttribute('aria-label') || b.textContent).trim()))`)
	for _, want := range []string{"Import AGENTS.md", "Import all", "Remove CLAUDE.md from the repository", "Remove all imported"} {
		if !strings.Contains(got, want) {
			t.Fatalf("banner buttons = %s, lack %q", got, want)
		}
	}
	if over := evalStr(t, rig, `String(document.documentElement.scrollWidth > document.documentElement.clientWidth)`); over != "false" {
		t.Fatal("the banner makes the page scroll sideways")
	}
	// A confirmation is a labelled dialog .
	rig.run(t, edClick(banner+` [aria-label="Remove CLAUDE.md from the repository"]`), poll(edVisible(banner+` [data-remove-confirm]`)))
	if role := evalStr(t, rig, `document.querySelector('`+instructionsPanel+` `+banner+` [data-remove-confirm]').getAttribute('role')+':'+document.querySelector('`+instructionsPanel+` `+banner+` [data-remove-confirm]').getAttribute('aria-label')`); role != "alertdialog:Remove from the repository" {
		t.Fatalf("confirmation role:label = %q", role)
	}
}
