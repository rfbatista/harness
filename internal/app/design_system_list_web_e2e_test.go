//go:build !windows

package app

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The Design systems list and the header menu against the server as it runs,
// with a headless Chrome as the person.

const (
	listRows     = `[data-design-system-row]`
	listLive     = `!document.querySelector('[data-ssr]') && !!document.querySelector('main[x-data]')`
	newForm      = `[data-new-form]`
	menuButton   = `[data-design-system-menu]`
	menuPanel    = `[data-menu-panel]`
	headerName   = `[data-design-system-name]`
	locationPath = `location.pathname`
)

// rowTexts is the list as the person reads it: name, version, repositories, one row per entry.
const rowTexts = `[...document.querySelectorAll('` + listRows + `')].map((r) => [r.querySelector('[data-row-name]').textContent.trim(), r.querySelector('[data-row-version]').textContent.trim()].join(' ')).join(',')`

// typeInto sets a field the way typing does: its input event fires.
func typeInto(sel, text string) chromedp.Action {
	return chromedp.SetValue(sel, text, chromedp.ByQuery)
}

func (p *designPage) listURL() string {
	return p.srv.URL + "/projects/" + p.project + "/design-systems"
}

// designSystemIDs answers the project's design systems by slug.
func (p *designPage) designSystemIDs() map[string]string {
	p.t.Helper()
	out := map[string]string{}
	for _, d := range p.mustPost("list_design_systems", map[string]any{})["design_systems"].([]any) {
		row := d.(map[string]any)
		out[row["slug"].(string)] = row["design_system_id"].(string)
	}
	return out
}

// arrivedWhere waits for the page a click navigates to: a read that lands in the
// middle of the navigation fails with "target navigated", so it is tried again.
func arrivedWhere(js string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(10 * time.Second)
		for {
			var ok bool
			if err := chromedp.Evaluate(js, &ok).Do(ctx); err == nil && ok {
				return nil
			}
			if time.Now().After(deadline) {
				return context.DeadlineExceeded
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
}

func pickMenu(action string) chromedp.Action {
	return chromedp.Tasks{
		designClick(menuButton),
		designClick(`[role=menu] [data-action=` + action + `]`),
	}
}

func TestDesignSystemsListCreatesAndFollowsDesignSystemsInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	me := newPerson(t)

	me.run(t, "an empty project shows the three starts",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(pg.listURL()),
		designPoll(listLive),
		designPoll(`!!document.querySelector('#design-system-starts [data-action=start-blank]:not([disabled])')`),
		designPoll(`document.querySelector('#design-system-starts [data-action=import]')?.disabled === true && document.querySelector('#design-system-starts [data-action=spawn]')?.disabled === true`),
		designPoll(`!document.querySelector('[data-action=new-design-system]')?.checkVisibility()`),
		shot("ds-list-empty", "light"),
	)

	me.run(t, "Start blank asks for the identity, suggests the slug, creates App and opens its page",
		designClick(`#design-system-starts [data-action=start-blank]`),
		designPoll(`!!document.querySelector('`+newForm+` [data-field=name]')`),
		typeInto(newForm+` [data-field=name]`, "App"),
		designPoll(`document.querySelector('`+newForm+` [data-field=slug]').value === 'app'`),
		designPoll(`document.querySelector('`+newForm+` [data-action=continue]').textContent.trim() === 'Start blank'`),
		designClick(newForm+` [data-action=continue]`),
		arrivedWhere(locationPath+` === '/projects/`+pg.project+`/design-systems/app' && document.querySelector('`+headerName+`')?.textContent.trim() === 'App'`),
	)
	if ids := pg.designSystemIDs(); len(ids) != 1 {
		t.Fatalf("design systems = %v", ids)
	}

	me.run(t, "New design system creates Marketing through the starts step",
		chromedp.Navigate(pg.listURL()),
		designPoll(listLive+` && `+rowTexts+` === 'App v0'`),
		designClick(`[data-action=new-design-system]`),
		typeInto(newForm+` [data-field=name]`, "Marketing"),
		typeInto(newForm+` [data-field=description]`, "Public site and campaign pages"),
		shot("ds-new-form", "light"),
		designClick(newForm+` [data-action=continue]`),
		designPoll(`!!document.querySelector('#new-design-system-starts [data-action=start-blank]')`),
		designPoll(`document.querySelector('#new-design-system-starts [data-action=import]').disabled === true && document.querySelector('#new-design-system-starts [data-action=spawn]').disabled === true`),
		designClick(`#new-design-system-starts [data-action=start-blank]`),
		arrivedWhere(locationPath+` === '/projects/`+pg.project+`/design-systems/marketing' && document.querySelector('[data-design-system-description]')?.textContent.trim() === 'Public site and campaign pages'`),
	)

	me.run(t, "the list shows both by name, and follows a save without a reload",
		chromedp.Navigate(pg.listURL()),
		designPoll(listLive+` && `+rowTexts+` === 'App v0,Marketing v0'`),
	)
	time.Sleep(time.Second) // the list's feed follow is open
	marketing := *pg
	marketing.ds = pg.designSystemIDs()["marketing"]
	marketing.mustSave(0, dsRecord())
	me.run(t, "version 1 arrives on the open list",
		designPoll(rowTexts+` === 'App v0,Marketing v1'`),
		shot("ds-list", "light"),
	)

	me.run(t, "a taken slug is refused inline and creates nothing",
		designClick(`[data-action=new-design-system]`),
		typeInto(newForm+` [data-field=name]`, "App"),
		designClick(newForm+` [data-action=continue]`),
		designClick(`#new-design-system-starts [data-action=start-blank]`),
		designPoll(`document.querySelector('`+newForm+` [data-fault]')?.textContent.includes('SLUG_TAKEN') === true`),
		designPoll(`!!document.querySelector('`+newForm+` [data-field=name]')`),
	)
	if ids := pg.designSystemIDs(); len(ids) != 2 {
		t.Fatalf("a taken slug created something: %v", ids)
	}
	me.run(t, "another slug with the same name is a taken name",
		typeInto(newForm+` [data-field=slug]`, "app-two"),
		designClick(newForm+` [data-action=continue]`),
		designClick(`#new-design-system-starts [data-action=start-blank]`),
		designPoll(`document.querySelector('`+newForm+` [data-fault=name]')?.textContent.includes('SLUG_TAKEN') === true`),
	)
	if ids := pg.designSystemIDs(); len(ids) != 2 {
		t.Fatalf("a taken name created something: %v", ids)
	}
	me.run(t, "Cancel closes the form", designClick(newForm+` [data-action=cancel]`), designPoll(`!document.querySelector('`+newForm+`')`))
}

func TestDesignSystemMenuRenamesAndDeletesInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	app := pg.createDesignSystem("app", "App")
	pg.createDesignSystem("marketing", "Marketing")
	appPage := pg.srv.URL + "/projects/" + pg.project + "/design-systems/app"
	me := newPerson(t)

	me.run(t, "Rename changes the name on the header and the list but not the address",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(appPage),
		designPoll(`document.querySelector('`+headerName+`')?.textContent.trim() === 'App'`),
		pickMenu("rename"),
		designPoll(`!!document.querySelector('`+menuPanel+` [data-field=name]')`),
		designPoll(`document.querySelector('`+menuPanel+` [data-field=name]').value === 'App'`),
		typeInto(menuPanel+` [data-field=name]`, "Apps"),
		designClick(menuPanel+` [data-action=save]`),
		designPoll(`document.querySelector('`+headerName+`').textContent.trim() === 'Apps' && !document.querySelector('`+menuPanel+`')`),
		designPoll(locationPath+` === '/projects/`+pg.project+`/design-systems/app'`),
		chromedp.Navigate(pg.listURL()),
		designPoll(listLive+` && `+rowTexts+` === 'Apps v0,Marketing v0'`),
	)

	me.run(t, "a rename to a taken name is refused under the field",
		chromedp.Navigate(appPage),
		designPoll(`document.querySelector('`+headerName+`')?.textContent.trim() === 'Apps'`),
		pickMenu("rename"),
		typeInto(menuPanel+` [data-field=name]`, "marketing"),
		designClick(menuPanel+` [data-action=save]`),
		designPoll(`document.querySelector('`+menuPanel+` [data-fault]')?.textContent.includes('SLUG_TAKEN') === true`),
		designClick(menuPanel+` [data-action=cancel]`),
	)

	me.run(t, "Edit description changes the description",
		pickMenu("description"),
		typeInto(menuPanel+` [data-field=description]`, "Product apps"),
		designClick(menuPanel+` [data-action=save]`),
		designPoll(`document.querySelector('[data-design-system-description]')?.textContent.trim() === 'Product apps'`),
	)

	me.run(t, "Delete with a wrong name does nothing",
		pickMenu("delete"),
		typeInto(menuPanel+` [data-field=confirm]`, "App"),
		designPoll(`document.querySelector('`+menuPanel+` [data-action=delete]').disabled === true`),
	)
	if _, ok := pg.designSystemIDs()["app"]; !ok {
		t.Fatal("a wrong name deleted the design system")
	}

	me.run(t, "Delete with the right name removes it and goes back to the list",
		typeInto(menuPanel+` [data-field=confirm]`, "Apps"),
		designClick(menuPanel+` [data-action=delete]`),
		arrivedWhere(locationPath+` === '/projects/`+pg.project+`/design-systems'`),
		designPoll(listLive+` && `+rowTexts+` === 'Marketing v0'`),
	)
	if _, ok := pg.designSystemIDs()["app"]; ok {
		t.Fatal("the design system is still there")
	}
	_ = app
}

func TestDesignSystemInUseIsRefusedNamingItsRepositoriesInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	app := pg.createDesignSystem("app", "App")
	web, api := pg.addRepository("spillway-web"), pg.addRepository("spillway")
	pg.mustPost("set_repository_design_system", map[string]any{"repository_id": web, "design_system_id": app})
	pg.mustPost("set_repository_design_system", map[string]any{"repository_id": api, "design_system_id": app})
	me := newPerson(t)

	me.run(t, "the list names the repositories that use a design system",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(pg.listURL()),
		designPoll(listLive+` && document.querySelector('[data-row-repositories]')?.textContent.includes('spillway-web') === true`),
	)

	me.run(t, "Delete shows the refusal naming them, and keeps the design system",
		chromedp.Navigate(pg.srv.URL+"/projects/"+pg.project+"/design-systems/app"),
		designPoll(`document.querySelector('`+headerName+`')?.textContent.trim() === 'App'`),
		pickMenu("delete"),
		typeInto(menuPanel+` [data-field=confirm]`, "App"),
		designClick(menuPanel+` [data-action=delete]`),
		designPoll(`document.querySelector('[data-delete-refusal] [data-refusal-repositories]')?.textContent.trim() === 'spillway, spillway-web' || document.querySelector('[data-delete-refusal] [data-refusal-repositories]')?.textContent.trim() === 'spillway-web, spillway'`),
		designPoll(`document.querySelector('[data-delete-refusal]').textContent.includes('DESIGN_SYSTEM_IN_USE')`),
		shot("ds-delete-refusal", "light"),
	)
	if _, ok := pg.designSystemIDs()["app"]; !ok {
		t.Fatal("a design system in use was deleted")
	}
}
