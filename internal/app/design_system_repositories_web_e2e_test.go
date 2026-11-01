//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A repository's opt-in to the design system, from the two places the person
// sets it and from the question after adding a repository, against the server
// as it runs. The state each control shows for each answer is covered in
// web/src/modules/designsystem; this proves the pages and the endpoint agree.

const (
	dsQuestion   = `[data-design-system-question]`
	dsRepoAddBtn = `form button[type=submit]`
)

func (p *designPage) addRepository(name string) string {
	p.t.Helper()
	out := p.mustPost("create_repository", map[string]any{"name": name, "url": "https://example.com/acme/" + name + ".git"})
	return out["repository"].(map[string]any)["id"].(string)
}

// mustGetJSON calls GET /api/<path> and answers the JSON body.
func (p *designPage) mustGetJSON(path string) map[string]any {
	p.t.Helper()
	res, err := p.srv.Client().Get(p.srv.URL + "/api/" + path)
	if err != nil {
		p.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != http.StatusOK {
		p.t.Fatalf("%s: %d %v", path, res.StatusCode, out)
	}
	return out
}

func (p *designPage) repository(id string) map[string]any {
	p.t.Helper()
	return p.mustGetJSON("get_repository?repository_id=" + id)["repository"].(map[string]any)
}

func (p *designPage) settingsURL(id string) string {
	return p.srv.URL + "/projects/" + p.project + "/repositories/" + id + "/env"
}

const (
	dsSelectBase    = `[data-repository-design-system]`
	dsSelect        = dsSelectBase + ` [data-field=design-system]`
	dsSelectPath    = dsSelectBase + ` [data-field=path]`
	dsSelectSave    = dsSelectBase + ` [data-action=save-path]`
	dsSelectError   = dsSelectBase + ` [data-ds-error]`
	dsSelectMove    = dsSelectBase + ` [data-ds-move]`
	dsSelectConfirm = dsSelectBase + ` [data-action=confirm-move]`
	dsSelectKeep    = dsSelectBase + ` [data-action=keep-current]`
	dsSelectNone    = dsSelectBase + ` [data-no-design-systems]`
)

// chooseDesignSystem picks the option with the value in the settings select, as a person's change does.
func chooseDesignSystem(value string) chromedp.Action {
	return designPoll(`(() => { const s = document.querySelector('` + dsSelect + `'); if (!s || s.disabled) return false; s.value = ` + jsString(value) + `; s.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`)
}

func (p *designPage) createDesignSystem(slug, name string) string {
	p.t.Helper()
	return p.mustPost("create_design_system", map[string]any{"name": name, "slug": slug})["design_system_id"].(string)
}

// shotsDir is where the settings select's screenshots go when SELECT_SHOTS is set.
func selectShot(name string) chromedp.Action {
	dir := os.Getenv("SELECT_SHOTS")
	if dir == "" {
		return chromedp.ActionFunc(func(context.Context) error { return nil })
	}
	var buf []byte
	return chromedp.Tasks{
		chromedp.Sleep(200 * time.Millisecond),
		chromedp.FullScreenshot(&buf, 90),
		chromedp.ActionFunc(func(context.Context) error {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, name+".png"), buf, 0o644)
		}),
	}
}

func TestRepositorySettingsSelectAppliesMovesAndRemovesADesignSystemInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	app := pg.createDesignSystem("app", "App")
	marketing := pg.createDesignSystem("marketing", "Marketing")
	pg.mustPost("save_design_system", map[string]any{"design_system_id": app, "base_version": 0, "record": dsRecord()})
	id := pg.addRepository("spillway")
	me := newPerson(t)
	used := func() any { return pg.repository(id)["design_system_id"] }

	me.run(t, "none by default, no path shown, both design systems offered",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(pg.settingsURL(id)),
		designPoll(`document.querySelector('`+dsSelect+`')?.value === ''`),
		designPoll(`[...document.querySelectorAll('`+dsSelect+` option')].map((o) => o.textContent.trim()).join(',') === 'None,App,Marketing'`),
		designPoll(`document.querySelector('`+dsSelectPath+`')?.checkVisibility() === false`),
		designPoll(`document.querySelector('`+dsSelectNone+`')?.checkVisibility() === false`),
	)

	me.run(t, "choosing App applies it with the default path",
		chooseDesignSystem(app),
		designPoll(`document.querySelector('`+dsSelectPath+`')?.checkVisibility() === true && document.querySelector('`+dsSelectPath+`').value === '.harness/design-system/'`),
		designPoll(`document.querySelector('`+dsSelect+`')?.disabled === false`),
		selectShot("repo-settings-select"),
	)
	if got := used(); got != app {
		t.Fatalf("after choosing App: %v", got)
	}

	me.run(t, "choosing Marketing asks first and changes nothing",
		chooseDesignSystem(marketing),
		designPoll(`document.querySelector('`+dsSelectMove+`')?.checkVisibility() === true`),
		designPoll(`document.querySelector('`+dsSelectMove+`').textContent.includes('from App to Marketing')`),
		selectShot("repo-settings-select-move"),
	)
	if got := used(); got != app {
		t.Fatalf("the question applied something: %v", got)
	}

	me.run(t, "keeping App closes the question and the select goes back",
		designClick(dsSelectKeep),
		designPoll(`document.querySelector('`+dsSelectMove+`')?.checkVisibility() === false`),
		designPoll(`document.querySelector('`+dsSelect+`').value === '`+app+`'`),
	)
	if got := used(); got != app {
		t.Fatalf("after keeping: %v", got)
	}

	me.run(t, "confirming the move applies Marketing",
		chooseDesignSystem(marketing),
		designClick(dsSelectConfirm),
		designPoll(`document.querySelector('`+dsSelectMove+`')?.checkVisibility() === false && document.querySelector('`+dsSelect+`').disabled === false`),
	)
	if got := used(); got != marketing {
		t.Fatalf("after the move: %v", got)
	}

	me.run(t, "a custom path is saved and a refused one shows under the select",
		chromedp.SetValue(dsSelectPath, "ui/tokens/", chromedp.ByQuery),
		designClick(dsSelectSave),
		designPoll(`document.querySelector('`+dsSelectSave+`')?.disabled === true`),
	)
	if r := pg.repository(id); r["design_system_path"] != "ui/tokens/" {
		t.Fatalf("after saving the path: %v", r)
	}
	me.run(t, "a refused path",
		chromedp.SetValue(dsSelectPath, "ui/tokens", chromedp.ByQuery),
		designClick(dsSelectSave),
		designPoll(`document.querySelector('`+dsSelectError+`')?.textContent.includes('must end in /')`),
	)
	if r := pg.repository(id); r["design_system_path"] != "ui/tokens/" || r["design_system_id"] != marketing {
		t.Fatalf("a refused path changed the repository: %v", r)
	}

	me.run(t, "the choice and the path persist after a reload",
		chromedp.Navigate(pg.settingsURL(id)),
		designPoll(`document.querySelector('`+dsSelect+`')?.value === '`+marketing+`'`),
		designPoll(`document.querySelector('`+dsSelectPath+`')?.value === 'ui/tokens/'`),
	)

	me.run(t, "None removes it at once, with no question, and keeps the path",
		chooseDesignSystem(""),
		designPoll(`document.querySelector('`+dsSelectPath+`')?.checkVisibility() === false`),
		designPoll(`document.querySelector('`+dsSelect+`')?.disabled === false`),
		chromedp.Navigate(pg.settingsURL(id)),
		designPoll(`document.querySelector('`+dsSelect+`')?.value === ''`),
	)
	if r := pg.repository(id); r["design_system_id"] != nil || r["design_system_path"] != "ui/tokens/" {
		t.Fatalf("after removing: %v", r)
	}
}

func TestRepositorySettingsSelectIsDisabledWhenTheProjectHasNoDesignSystemInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	id := pg.addRepository("spillway")
	me := newPerson(t)
	me.run(t, "disabled, saying so, with a way to start one",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(pg.settingsURL(id)),
		designPoll(`document.querySelector('`+dsSelect+`')?.disabled === true`),
		designPoll(`document.querySelector('`+dsSelectNone+`')?.textContent.includes('No design systems in this project yet')`),
		designPoll(`document.querySelector('`+dsSelectNone+` a')?.getAttribute('href') === '/projects/`+pg.project+`/design-system'`),
		selectShot("repo-settings-select-empty"),
	)
}

func TestRepositoriesTabAndSettingsAgreeInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	pg.mustSave(0, dsRecord())
	first := pg.designSystemID()
	other := pg.createDesignSystem("brand", "Brand")
	api := pg.addRepository("api")
	web := pg.addRepository("web")
	me := newPerson(t)
	rowOf := func(id string) string {
		return `[data-design-system-optin] [data-optin-row][data-repository-id="` + id + `"]`
	}
	chosen := func(id string) any { return pg.repository(id)["design_system_id"] }

	me.run(t, "the tab lists both repositories, off, with no export state",
		chromedp.Navigate(pg.url+"?tab=repositories"),
		designPoll(`document.querySelectorAll('#design-system-panel-repositories [data-optin-row]').length === 2`),
		designPoll(`document.querySelector('`+rowOf(web)+` [data-field=uses]').checked === false`),
		designPoll(`!document.querySelector('`+rowOf(web)+` [data-export-state]')?.checkVisibility()`),
	)

	me.run(t, "switch web on here",
		designClick(rowOf(web)+` [data-field=uses]`),
		designPoll(`document.querySelector('`+rowOf(web)+`').dataset.phase === 'idle'`),
		designPoll(`document.querySelector('`+rowOf(web)+` [data-export-state]')?.textContent.trim() === 'Not exported yet'`),
		designPoll(`document.querySelector('`+rowOf(web)+` [data-field=path]').value === '.harness/design-system/'`),
		chromedp.EmulateViewport(1280, 900),
		shot("repositories-tab", "light"),
	)
	if got := chosen(web); got != first {
		t.Fatalf("the tab applied %v, want %v", got, first)
	}

	me.run(t, "its settings show the same state, and the other repository is still none",
		chromedp.Navigate(pg.settingsURL(web)),
		designPoll(`document.querySelector('`+dsSelect+`')?.value === '`+first+`'`),
		chromedp.Navigate(pg.settingsURL(api)),
		designPoll(`document.querySelector('`+dsSelect+`')?.value === ''`),
	)

	me.run(t, "a change in settings is seen in the tab after a reload",
		chooseDesignSystem(first),
		designPoll(`document.querySelector('`+dsSelectPath+`')?.checkVisibility() === true && document.querySelector('`+dsSelect+`').disabled === false`),
		chromedp.Navigate(pg.url+"?tab=repositories"),
		designPoll(`document.querySelector('`+rowOf(api)+` [data-field=uses]')?.checked === true`),
	)

	pg.mustPost("set_repository_design_system", map[string]any{"repository_id": api, "design_system_id": other, "confirm_move": true})
	me.run(t, "a repository that uses another design system is moved here after confirming",
		chromedp.Navigate(pg.url+"?tab=repositories"),
		designPoll(`document.querySelector('`+rowOf(api)+` [data-elsewhere]')?.textContent.trim() === 'Uses Brand now.'`),
		designClick(rowOf(api)+` [data-field=uses]`),
		designPoll(`document.querySelector('`+rowOf(api)+` [data-optin-move]')?.checkVisibility() === true`),
		designClick(rowOf(api)+` [data-optin-move] [data-action=move]`),
		designPoll(`document.querySelector('`+rowOf(api)+`').dataset.phase === 'idle' && document.querySelector('`+rowOf(api)+` [data-optin-move]')?.checkVisibility() === false`),
	)
	if got := chosen(api); got != first {
		t.Fatalf("after the move: %v, want %v", got, first)
	}
}

func TestAddingARepositoryToAProjectWithADesignSystemAsksOnceInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	me := newPerson(t)
	reposURL := pg.srv.URL + "/projects/" + pg.project + "/repositories"
	dir := t.TempDir()
	checkout := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	add := func(name string) []chromedp.Action {
		return []chromedp.Action{
			chromedp.SetValue(`#repo-root`, checkout(name), chromedp.ByQuery),
			designClick(dsRepoAddBtn),
			designPoll(`!!document.querySelector('[aria-label=Repositories]') && [...document.querySelectorAll('[aria-label=Repositories] .weight-medium')].some((e) => e.textContent.trim() === '` + name + `')`),
		}
	}
	idOf := func(name string) string {
		for _, r := range pg.mustGetJSON("list_repositories?project_id=" + pg.project)["repositories"].([]any) {
			if m := r.(map[string]any); m["name"] == name {
				return m["id"].(string)
			}
		}
		t.Fatalf("no repository %q", name)
		return ""
	}

	me.run(t, "no design system yet: nothing is asked",
		append([]chromedp.Action{chromedp.Navigate(reposURL), designPoll(`!!document.querySelector('#repo-root')`)}, add("plain")...)...)
	me.run(t, "still nothing asked", designPoll(`document.querySelector('`+dsQuestion+`') === null`))

	pg.mustSave(0, dsRecord())

	me.run(t, "now adding asks, and yes opts in",
		append([]chromedp.Action{chromedp.Navigate(reposURL), designPoll(`!!document.querySelector('#repo-root')`)}, add("api")...)...)
	me.run(t, "the question names the repository",
		designPoll(`document.querySelector('`+dsQuestion+`')?.textContent.includes('Use the project design system here?') && document.querySelector('`+dsQuestion+`').textContent.includes('api')`),
		chromedp.EmulateViewport(1280, 900),
		shot("new-repo-prompt", "light"),
		designClick(dsQuestion+` [data-answer=yes]`),
		designPoll(`document.querySelector('`+dsQuestion+`') === null`),
	)
	if r := pg.repository(idOf("api")); r["design_system_id"] == nil {
		t.Fatalf("yes: %v", r)
	}

	me.run(t, "no leaves the next one off",
		append(add("web"),
			designPoll(`!!document.querySelector('`+dsQuestion+` [data-answer=no]')`),
			designClick(dsQuestion+` [data-answer=no]`),
			designPoll(`document.querySelector('`+dsQuestion+`') === null`),
		)...)
	if r := pg.repository(idOf("web")); r["design_system_id"] != nil {
		t.Fatalf("no: %v", r)
	}

	me.run(t, "the question does not come back after a reload",
		chromedp.Navigate(reposURL),
		designPoll(`!!document.querySelector('#repo-root')`),
		designPoll(`document.querySelector('`+dsQuestion+`') === null`),
	)
}
