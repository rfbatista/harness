//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The Repositories tab's export, against the server as it runs, on real
// checkouts: one happy path to in sync, and the edited-outside confirmation
// with its diff. The states and refusals are proved at the HTTP seam in
// design_system_export_e2e_test.go.

// exportCheckout is a repository of the page's project on a real checkout
// with an origin, using the design system.
func (p *designPage) exportCheckout(name string) *exportRepo {
	p.t.Helper()
	dir := p.t.TempDir()
	r := &exportRepo{checkout: filepath.Join(dir, name), bare: filepath.Join(dir, "origin.git")}
	gitInit(p.t, r.checkout)
	gitE2E(p.t, dir, "init", "-q", "--bare", r.bare)
	gitE2E(p.t, r.checkout, "remote", "add", "origin", r.bare)
	gitE2E(p.t, r.checkout, "push", "-q", "origin", "main")
	out := p.mustPost("create_repository", map[string]any{"name": name, "url": "file://" + r.checkout, "root_dir": r.checkout})
	r.id = out["repository"].(map[string]any)["id"].(string)
	p.mustPost("set_repository_design_system", map[string]any{"repository_id": r.id, "design_system_id": p.designSystemID()})
	return r
}

func exportRow(id string) string {
	return `[data-design-system-optin] [data-optin-row][data-repository-id="` + id + `"]`
}

func exportBadgeIs(id, label string) chromedp.Action {
	return designPoll(`document.querySelector('` + exportRow(id) + ` [data-export-badge]')?.textContent.trim() === ` + jsString(label))
}

func TestExportingARepositoryFromTheRepositoriesTabGoesToInSyncInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	page := startDesignSystemPage(t)
	page.mustSave(0, dsRecord(buttonComponent()))
	r := page.exportCheckout("spillway")
	plain := page.addRepository("infra")
	me := newPerson(t)
	before := gitE2E(t, r.checkout, "rev-parse", "HEAD")

	me.run(t, "a repository never exported shows its state and Export; one with no design system has none",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(page.url+"?tab=repositories"),
		exportBadgeIs(r.id, "Never exported"),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-action=export]')?.checkVisibility() === true`),
		designPoll(`document.querySelector('`+exportRow(plain)+`') !== null && document.querySelector('`+exportRow(plain)+` [data-action=export]') === null`),
		selectShot("impl-export-never"),
	)

	me.run(t, "Export commits, and the row reads in sync with the commit",
		designClick(exportRow(r.id)+` [data-action=export]`),
		exportBadgeIs(r.id, "In sync"),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-done]')?.textContent.includes('Exported version 1') === true`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-state]').textContent.includes('version 1 exported')`),
		selectShot("impl-export-in-sync"),
	)
	head := gitE2E(t, r.checkout, "rev-parse", "HEAD")
	if head == before || gitE2E(t, r.checkout, "log", "-1", "--format=%s") != "design system: export version 1" {
		t.Errorf("head %s before %s", head, before)
	}
	if gitE2E(t, r.bare, "rev-parse", "main") != before {
		t.Error("the export pushed")
	}

	other := newPerson(t)
	other.run(t, "a second page follows a save without reload, and the header's Export… chooses the repository",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(page.url+"?tab=repositories"),
		exportBadgeIs(r.id, "In sync"),
	)
	page.mustSave(1, dsRecord(buttonComponent()))
	other.run(t, "behind",
		exportBadgeIs(r.id, "Behind"),
		designClick(`[data-action=export-open]`),
		designPoll(`document.querySelector('`+exportRow(r.id)+`').dataset.chosen === 'true' && document.activeElement?.matches('`+exportRow(r.id)+` [data-action=export]')`),
		selectShot("impl-export-behind"),
	)
	me.run(t, "the first page follows the save too",
		exportBadgeIs(r.id, "Behind"),
	)
	other.run(t, "exporting the new version",
		designClick(exportRow(r.id)+` [data-action=export]`),
		exportBadgeIs(r.id, "In sync"),
	)
	me.run(t, "and the first page follows the export",
		exportBadgeIs(r.id, "In sync"),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-state]').textContent.includes('version 2 exported')`),
	)

	gitE2E(t, r.checkout, "checkout", "-q", "-b", "feature")
	me.run(t, "a wrong branch is refused with its reason in the panel",
		chromedp.Navigate(page.url+"?tab=repositories"),
		exportBadgeIs(r.id, "In sync"),
		designClick(exportRow(r.id)+` [data-action=export]`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-error]')?.textContent.includes('feature') === true`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-error]').textContent.includes('WRONG_BRANCH')`),
		selectShot("impl-export-refused"),
	)
}

func TestExportOverEditedOutsideFilesShowsTheDiffAndAsksFirstInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	page := startDesignSystemPage(t)
	page.mustSave(0, dsRecord(buttonComponent()))
	r := page.exportCheckout("spillway")
	page.mustPost("export_design_system", map[string]any{"repository_id": r.id})
	design := filepath.Join(r.checkout, ".harness/design-system/DESIGN.md")
	original, _ := os.ReadFile(design)
	if err := os.WriteFile(design, append(original, []byte("hand written line\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, r.checkout, "commit", "-q", "-am", "hand edit")
	head := gitE2E(t, r.checkout, "rev-parse", "HEAD")
	me := newPerson(t)

	me.run(t, "the row says edited outside and names the file",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(page.url+"?tab=repositories"),
		exportBadgeIs(r.id, "Edited outside the harness"),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-state]').textContent.includes('DESIGN.md differ')`),
	)

	me.run(t, "Export lists the changed file and its differing line, and writes nothing",
		designClick(exportRow(r.id)+` [data-action=export]`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-review]')?.checkVisibility() === true`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-file]')?.textContent.includes('DESIGN.md') === true`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-diff]')?.textContent.includes('-hand written line') === true`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-review]').textContent.includes('lost unless an agent proposes them')`),
		selectShot("impl-export-edited-outside"),
	)
	if gitE2E(t, r.checkout, "rev-parse", "HEAD") != head {
		t.Fatal("the confirmation committed")
	}

	me.run(t, "Cancel leaves everything as it was",
		designClick(exportRow(r.id)+` [data-action=export-cancel]`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-review]')?.checkVisibility() === false`),
		exportBadgeIs(r.id, "Edited outside the harness"),
	)
	if gitE2E(t, r.checkout, "rev-parse", "HEAD") != head {
		t.Fatal("cancelling committed")
	}

	me.run(t, "confirming replaces the edit and the row goes to in sync",
		designClick(exportRow(r.id)+` [data-action=export]`),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-review]')?.checkVisibility() === true`),
		designClick(exportRow(r.id)+` [data-action=export-confirm]`),
		exportBadgeIs(r.id, "In sync"),
		designPoll(`document.querySelector('`+exportRow(r.id)+` [data-export-review]')?.checkVisibility() === false`),
	)
	if now, _ := os.ReadFile(design); !strings.EqualFold(string(now), string(original)) {
		t.Error("the hand edit was not replaced")
	}
	if gitE2E(t, r.checkout, "rev-parse", "HEAD") == head {
		t.Error("the confirmed export made no commit")
	}
}
