//go:build !windows

package app

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Importing a design system from a repository, with a headless Chrome as the
// person, against the server as it runs and a git fixture holding a copy of
// the harness's own DESIGN.md and design-system/tokens.json.

const (
	importCard    = `#new-design-system-starts [data-action=import]`
	importPreview = `[data-import-preview]`
)

func TestDesignImportFromARepositoryInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	checkout := gitFixture(t, "harness", harnessFiles(t))
	repo := pg.mustPost("create_repository", map[string]any{"name": "harness", "url": "file://" + checkout, "root_dir": checkout})["repository"].(map[string]any)["id"].(string)
	me := newPerson(t)

	me.run(t, "an empty project ranks Import first, as the primary start, once a repository holds the files",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(pg.listURL()),
		designPoll(listLive),
		designPoll(`document.querySelector('#design-system-starts [data-action=import]')?.disabled === false`),
		designPoll(`document.querySelector('#design-system-starts .entry-card h2').textContent.includes('Import')`),
		designPoll(`document.querySelector('#design-system-starts [data-action=import]').dataset.variant === 'primary' && document.querySelector('#design-system-starts [data-action=start-blank]').dataset.variant !== 'primary'`),
		shot("ds-import-start", "light"),
	)

	me.run(t, "Import asks for the name, then offers the repository with the paths found",
		designClick(`#design-system-starts [data-action=import]`),
		designPoll(`!!document.querySelector('`+newForm+` [data-field=name]')`),
		typeInto(newForm+` [data-field=name]`, "Harness look"),
		designPoll(`document.querySelector('`+newForm+` [data-action=continue]').textContent.trim() === 'Continue'`),
		designClick(newForm+` [data-action=continue]`),
		designPoll(`document.querySelector('`+importCard+`')?.textContent.trim() === 'Preview import'`),
		designPoll(`document.querySelector('[data-field=import-repository]').value === `+jsString(repo)),
		designPoll(`document.querySelector('[data-field=tokens-path]').value === 'design-system/tokens.json' && document.querySelector('[data-field=design-md-path]').value === 'DESIGN.md'`),
		shot("ds-import-card", "light"),
	)

	me.run(t, "the preview counts the tokens by group and lists what was skipped, and stores nothing",
		designClick(importCard),
		designPoll(`!!document.querySelector('`+importPreview+`')`),
		designPoll(`document.querySelector('`+importPreview+` [data-count=colors]')?.textContent.includes('23')`),
		designPoll(`[...document.querySelectorAll('`+importPreview+` [data-skipped]')].some((e) => e.textContent.includes('themes.dark'))`),
		designPoll(`!!document.querySelector('`+importPreview+` [data-section]')`),
		shot("ds-import-preview", "light"),
	)
	if ids := pg.designSystemIDs(); len(ids) != 0 {
		t.Fatalf("a preview stored a design system: %v", ids)
	}

	me.run(t, "Cancel leaves the preview and stores nothing",
		designClick(importPreview+` [data-action=cancel-preview]`),
		designPoll(`!document.querySelector('`+importPreview+`') && !!document.querySelector('`+importCard+`')`),
	)
	if ids := pg.designSystemIDs(); len(ids) != 0 {
		t.Fatalf("cancel stored a design system: %v", ids)
	}

	me.run(t, "Import creates the design system and opens it at version 1 with its tokens",
		designClick(importCard),
		designPoll(`!!document.querySelector('`+importPreview+`')`),
		designClick(importPreview+` [data-action=confirm-import]`),
		arrivedWhere(locationPath+` === '/projects/`+pg.project+`/design-systems/harness-look' && document.querySelector('`+headerName+`')?.textContent.trim() === 'Harness look'`),
		designPoll(`document.querySelectorAll('#design-system-panel-tokens [data-token-row]').length > 20`),
		shot("ds-import-imported", "light"),
	)
	ids := pg.designSystemIDs()
	if len(ids) != 1 {
		t.Fatalf("design systems = %v", ids)
	}
	got := pg.mustPost("get_design_system", map[string]any{"design_system_id": ids["harness-look"]})
	if got["version"] != float64(1) || got["updated_by"] != "import" {
		t.Errorf("imported design system = version %v by %v", got["version"], got["updated_by"])
	}
}
