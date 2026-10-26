package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

const (
	pickFolderBtn = `#architecture-panel-layers [data-pick-folder]`
	folderTreeSel = `#architecture-panel-layers [data-folder-tree]`
)

func folderBox(path string) string {
	return fmt.Sprintf(`%s input[type=checkbox][value=%q]`, folderTreeSel, path)
}

// Pick folder… opens the folder tree read from the repository's checkout;
// checking a folder sets the layer's folder, unchecking clears it.
func TestPickFolderOpensTheTreeAndCheckingAFolderSetsTheLayersFolder(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var closedFirst, expanded, labels, pattern string
	rig.run(t,
		addLayerBy("folder"),
		poll(`!!document.querySelector('`+pickFolderBtn+`')`),
		chromedp.Evaluate(fmt.Sprintf(`String(!!document.querySelector(%q)?.checkVisibility())`, folderTreeSel), &closedFirst),
		archClick(pickFolderBtn),
		poll(`document.querySelectorAll('`+folderTreeSel+` input[type=checkbox]').length > 0`),
		chromedp.Evaluate(`document.querySelector('`+pickFolderBtn+`').getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(`[...document.querySelectorAll('`+folderTreeSel+` label')].map(l => l.textContent.trim()).join('|')`, &labels),
	)
	if closedFirst != "false" {
		t.Fatalf("the tree is visible before Pick folder… is pressed")
	}
	if expanded != "true" {
		t.Fatalf("aria-expanded = %q after opening", expanded)
	}
	if want := "internal|internal/domain|internal/ports|web"; labels != want {
		t.Fatalf("folders = %q, want %q (directories only, no .git, none ignored)", labels, want)
	}
	if got := rig.api.treeRoots; len(got) != 1 || got[0] != "/src/harness" {
		t.Fatalf("list_tree roots = %v, want the repository's checkout once", got)
	}
	rig.run(t,
		archClick(folderBox("internal/domain")),
		poll(`document.querySelector('`+layerSel(0, `[data-field=pattern]`)+`').value === 'internal/domain'`),
		poll(fmt.Sprintf(`document.querySelector(%q).checked`, folderBox("internal/domain"))),
		poll(`document.querySelector('`+layerCountSel+`')?.textContent.includes('2 files')`), // the preview follows the pick
		// another folder replaces it: a folder layer holds one folder
		archClick(folderBox("web")),
		poll(`document.querySelector('`+layerSel(0, `[data-field=pattern]`)+`').value === 'web'`),
		poll(fmt.Sprintf(`!document.querySelector(%q).checked`, folderBox("internal/domain"))),
		// unchecking removes it
		archClick(folderBox("web")),
		poll(`document.querySelector('`+layerSel(0, `[data-field=pattern]`)+`').value === ''`),
		chromedp.Evaluate(`document.querySelector('`+layerSel(0, `[data-field=pattern]`)+`').value`, &pattern),
	)
	if pattern != "" {
		t.Fatalf("pattern after unchecking = %q", pattern)
	}
	// the tree follows what is typed
	rig.run(t,
		setLayerField(0, "pattern", "internal/ports"),
		poll(fmt.Sprintf(`document.querySelector(%q).checked`, folderBox("internal/ports"))),
	)
}

// A folder checked in the tree is saved as the layer's folder.
func TestAFolderPickedInTheTreeIsSaved(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	rig.run(t,
		addLayerBy("folder"),
		poll(`!!document.querySelector('`+pickFolderBtn+`')`),
		archClick(pickFolderBtn),
		poll(`!!document.querySelector('`+folderBox("internal/domain")+`')`),
		archClick(folderBox("internal/domain")),
		poll(`document.querySelector('`+layerSel(0, `[data-field=pattern]`)+`').value === 'internal/domain'`),
		archClick(archSave),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	l := savedLayer(t, rig, 0)
	if m, _ := l["match"].(map[string]any); m["folder"] != "internal/domain" {
		t.Fatalf("saved layer = %v, want folder internal/domain", l)
	}
}

// When the listing fails the tree says so and typing the folder stays possible.
func TestWhenListingFoldersFailsTheTreeShowsTheFailureAndTypingStays(t *testing.T) {
	rig := newArchRig(t)
	rig.api.treeFails = true
	openLayersTab(t, rig)
	var msg string
	var boxes int
	rig.run(t,
		addLayerBy("folder"),
		poll(`!!document.querySelector('`+pickFolderBtn+`')`),
		archClick(pickFolderBtn),
		poll(`!!document.querySelector('`+folderTreeSel+` [data-folder-tree-error]')`),
		chromedp.Evaluate(`document.querySelector('`+folderTreeSel+` [data-folder-tree-error]').textContent.replace(/\s+/g, ' ').trim()`, &msg),
		chromedp.Evaluate(`document.querySelectorAll('`+folderTreeSel+` input[type=checkbox]').length`, &boxes),
		setLayerField(0, "pattern", "internal/domain"),
		poll(`document.querySelector('`+layerCountSel+`')?.textContent.includes('2 files')`),
	)
	if !strings.Contains(msg, "no such file or directory") {
		t.Fatalf("failure = %q, want the server's reason", msg)
	}
	if boxes != 0 {
		t.Fatalf("%d folders shown after a failed listing", boxes)
	}
	// opening it again asks again
	rig.api.mu.Lock()
	rig.api.treeFails = false
	rig.api.mu.Unlock()
	rig.run(t,
		archClick(pickFolderBtn), // closes
		archClick(pickFolderBtn), // opens: asks again
		poll(`!!document.querySelector('`+folderBox("internal/domain")+`')`),
		poll(fmt.Sprintf(`document.querySelector(%q).checked`, folderBox("internal/domain"))),
	)
}

// A regex layer has no folder picker.
func TestARegexLayerHasNoFolderPicker(t *testing.T) {
	rig := newArchRig(t)
	openLayersTab(t, rig)
	var shown bool
	rig.run(t,
		addLayerBy("regex"),
		chromedp.Evaluate(`!document.querySelector('`+pickFolderBtn+`')`, &shown),
	)
	if !shown {
		t.Fatal("a regex layer shows Pick folder…")
	}
}
