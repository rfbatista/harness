//go:build !windows

package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The Principles and History tabs of the Design system page against the
// server as it runs. The pure parts (the outline, the version rows) and the
// page's handling of each answer are covered in web/src/modules/designsystem.

const (
	dsPrinciples    = `#design-system-panel-principles [data-field=principles]`
	dsHistoryRows   = `document.querySelectorAll('#design-system-panel-history [data-version-row]').length`
	dsHistoryNewest = `document.querySelector('#design-system-panel-history [data-version-row]')`
)

// jsLiteral is s as a JavaScript string literal, newlines included.
func jsLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func dsVersions(pg *designPage) []any {
	return pg.mustPost("list_design_system_versions", map[string]any{})["versions"].([]any)
}

func TestDesignSystemPrinciplesInTheBrowserAgainstTheRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignSystemPage(t)
	pg.mustSave(0, dsRecord())
	me := newPerson(t)
	text := "# Principles\n\n## Colour\n\nUse the signal colour for one action per view.\n\n## Space\n\nBreathe: spacing comes from the scale.\n"

	me.run(t, "write two sections",
		chromedp.Navigate(pg.url+"?tab=principles"),
		designPoll(`!!document.querySelector('`+dsPrinciples+`')`),
		designPoll(`document.querySelector('`+dsPrinciples+`').value.includes('## Calm')`),
		chromedp.SetValue(dsPrinciples, text, chromedp.ByQuery),
		designPoll(`document.querySelectorAll('#design-system-panel-principles [data-outline-item]').length === 2`),
		designPoll(`[...document.querySelectorAll('#design-system-panel-principles [data-outline-item]')].map((e) => e.textContent.trim()).join('|') === 'Colour|Space'`),
		designPoll(`document.querySelector('[data-save-bar] [data-action=save]')?.disabled === false`),
		designPoll(`document.querySelector('[data-save-bar] [data-save-summary]')?.textContent.includes('principles')`),
	)

	// Clicking an outline entry puts the cursor at its section, in Write.
	me.run(t, "the outline moves the cursor",
		designClick(`#design-system-panel-principles [data-outline-item]:nth-of-type(2)`),
		designPoll(`document.querySelector('`+dsPrinciples+`').selectionStart === `+strconv.Itoa(strings.Index(text, "## Space"))),
	)

	me.run(t, "preview renders the sections",
		designClick(`#design-system-panel-principles [data-principles-mode=preview]`),
		designPoll(`document.querySelectorAll('#design-system-panel-principles [data-principles-preview] h2').length === 2`),
		designPoll(`document.querySelector('`+dsPrinciples+`').hidden === true`),
		chromedp.EmulateViewport(1280, 900),
		shot("principles", "light"),
		designClick(`#design-system-panel-principles [data-principles-mode=write]`),
		designPoll(`document.querySelector('`+dsPrinciples+`').hidden === false`),
	)

	me.run(t, "save",
		chromedp.SetValue(`[data-save-bar] [data-field=message]`, "Colour and space", chromedp.ByQuery),
		designClick(dsSaveButton),
		designPoll(`document.querySelector('`+dsSaveVersion+`')?.textContent.trim() === 'Saves as version 3'`),
	)
	if got := pg.get()["record"].(map[string]any)["principles"]; got != text {
		t.Fatalf("stored principles = %q", got)
	}

	me.run(t, "reload", chromedp.Navigate(pg.url+"?tab=principles"),
		designPoll(`document.querySelector('`+dsPrinciples+`')?.value === `+jsLiteral(text)),
	)

	// Editing only principles enables the same Save bar; the version summary says ~1.
	edited := strings.Replace(text, "one action per view", "one action", 1)
	me.run(t, "edit one section",
		chromedp.SetValue(dsPrinciples, edited, chromedp.ByQuery),
		designPoll(`document.querySelector('[data-save-bar] [data-action=save]')?.disabled === false`),
		designClick(dsSaveButton),
		designPoll(`document.querySelector('`+dsSaveVersion+`')?.textContent.trim() === 'Saves as version 4'`),
	)
	newest := dsVersions(pg)[0].(map[string]any)
	if got := newest["summary"].(map[string]any)["principles"]; got != "+0 ~1 -0" {
		t.Errorf("principles summary = %v (version %v)", got, newest["version"])
	}
}

func TestDesignSystemHistoryInTheBrowserAgainstTheRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignSystemPage(t)
	pg.mustSave(0, dsRecord())
	second := dsRecord()
	second["tokens"] = append(second["tokens"].([]any), dsToken("colors", "ink", "color", "#111111"))
	pg.mustPost("save_design_system", map[string]any{"base_version": 1, "record": second, "message": "Add ink"})
	me := newPerson(t)

	me.run(t, "the list, newest first",
		chromedp.Navigate(pg.url+"?tab=history"),
		designPoll(dsHistoryRows+` === 2`),
		designPoll(dsHistoryNewest+`?.dataset.versionRow === '2'`),
		designPoll(dsHistoryNewest+`.textContent.includes('Add ink') && `+dsHistoryNewest+`.textContent.includes('tokens +1')`),
		designPoll(dsHistoryNewest+`.textContent.includes('you')`),
	)

	// A save made elsewhere appears without a reload.
	third := dsRecord()
	third["tokens"] = second["tokens"]
	third["principles"] = "## Calm\n\nQuieter.\n"
	pg.mustPost("save_design_system", map[string]any{"base_version": 2, "record": third, "message": "Quieter calm"})
	me.run(t, "the feed adds a row",
		designPoll(dsHistoryRows+` === 3`),
		designPoll(dsHistoryNewest+`.textContent.includes('Quieter calm') && `+dsHistoryNewest+`.textContent.includes('principles ~1')`),
	)

	// The person's own save adds one more row, with its message and summary.
	me.run(t, "a save here",
		designClick(`[role=tab][id=design-system-tab-principles]`),
		designPoll(`!!document.querySelector('`+dsPrinciples+`')`),
		chromedp.SetValue(dsPrinciples, "## Calm\n\nQuieter still.\n\n## Space\n\nScale.\n", chromedp.ByQuery),
		chromedp.SetValue(`[data-save-bar] [data-field=message]`, "Add space", chromedp.ByQuery),
		designClick(dsSaveButton),
		designPoll(`document.querySelector('`+dsSaveVersion+`')?.textContent.trim() === 'Saves as version 5'`),
		designClick(`[role=tab][id=design-system-tab-history]`),
		designPoll(dsHistoryRows+` === 4`),
		designPoll(dsHistoryNewest+`.textContent.includes('Add space') && `+dsHistoryNewest+`.textContent.includes('principles +1 ~1')`),
		chromedp.EmulateViewport(1280, 900),
		shot("history", "light"),
	)

	// An older version opens read-only, with its values and no way to restore it.
	me.run(t, "open version 1",
		designClick(`#design-system-panel-history [data-version-row="1"] [data-action=open-version]`),
		designPoll(`!!document.querySelector('#design-system-panel-history [data-version-view]')`),
		designPoll(`document.querySelectorAll('#design-system-panel-history [data-version-view] [data-version-token]').length === 4`),
		designPoll(`document.querySelector('#design-system-panel-history [data-version-view]').textContent.includes('oklch(0.5 0.095 182)')`),
		designPoll(`document.querySelector('#design-system-panel-history [data-version-view]').textContent.includes('Quiet surfaces')`),
	)
	var fields int
	var restore bool
	me.run(t, "read only",
		chromedp.Evaluate(`document.querySelectorAll('#design-system-panel-history input, #design-system-panel-history textarea, #design-system-panel-history select').length`, &fields),
		chromedp.Evaluate(`/restore|revert|roll ?back/i.test(document.querySelector('#design-system-panel-history').textContent) && !/no restore/i.test(document.querySelector('#design-system-panel-history').textContent)`, &restore),
	)
	if fields != 0 || restore {
		t.Errorf("the version view is not read only: %d fields, restore offered: %v", fields, restore)
	}
	me.run(t, "back to the list",
		designClick(`#design-system-panel-history [data-action=close-version]`),
		designPoll(dsHistoryRows+` === 4`),
	)
}
