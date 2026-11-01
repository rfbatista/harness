//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
)

// A repository's opt-in to the design system, from the two places the person
// sets it and from the question after adding a repository, against the server
// as it runs. The state each control shows for each answer is covered in
// web/src/modules/designsystem; this proves the pages and the endpoint agree.

const (
	optInRow     = `[data-design-system-optin] [data-optin-row]`
	optInSwitch  = optInRow + ` [data-field=uses]`
	optInPath    = optInRow + ` [data-field=path]`
	optInSave    = optInRow + ` [data-action=save-path]`
	optInError   = optInRow + ` [data-optin-error]`
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

func TestRepositorySettingsSwitchAndPathPersistInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	id := pg.addRepository("spillway")
	me := newPerson(t)

	me.run(t, "off by default, no path shown",
		chromedp.Navigate(pg.settingsURL(id)),
		designPoll(`document.querySelector('`+optInSwitch+`')?.checked === false`),
		designPoll(`document.querySelector('`+optInPath+`')?.checkVisibility() === false`),
	)

	me.run(t, "switch on shows the default path",
		designClick(optInSwitch),
		designPoll(`document.querySelector('`+optInPath+`')?.checkVisibility() === true`),
		designPoll(`document.querySelector('`+optInPath+`').value === '.harness/design-system/'`),
		designPoll(`document.querySelector('`+optInRow+`').dataset.phase === 'idle'`),
	)
	if r := pg.repository(id); r["uses_design_system"] != true {
		t.Fatalf("after switching on: %v", r)
	}

	me.run(t, "a custom path is saved",
		chromedp.SetValue(optInPath, "ui/tokens/", chromedp.ByQuery),
		designClick(optInSave),
		designPoll(`document.querySelector('`+optInSave+`')?.disabled === true`),
		chromedp.EmulateViewport(1280, 900),
		shot("repo-settings", "light"),
	)
	if r := pg.repository(id); r["design_system_path"] != "ui/tokens/" {
		t.Fatalf("after saving the path: %v", r)
	}

	me.run(t, "both persist after a reload",
		chromedp.Navigate(pg.settingsURL(id)),
		designPoll(`document.querySelector('`+optInSwitch+`')?.checked === true`),
		designPoll(`document.querySelector('`+optInPath+`')?.value === 'ui/tokens/'`),
	)

	me.run(t, "the server's refusal shows under the row and the saved path stays",
		chromedp.SetValue(optInPath, "ui/tokens", chromedp.ByQuery),
		designClick(optInSave),
		designPoll(`document.querySelector('`+optInError+`')?.textContent.includes('must end in /')`),
	)
	if r := pg.repository(id); r["design_system_path"] != "ui/tokens/" {
		t.Fatalf("a refused path changed the repository: %v", r)
	}

	me.run(t, "switch off persists and keeps the path",
		designClick(optInSwitch),
		designPoll(`document.querySelector('`+optInRow+`').dataset.phase === 'idle' && document.querySelector('`+optInSwitch+`').checked === false`),
		chromedp.Navigate(pg.settingsURL(id)),
		designPoll(`document.querySelector('`+optInSwitch+`')?.checked === false`),
	)
	if r := pg.repository(id); r["uses_design_system"] != false || r["design_system_path"] != "ui/tokens/" {
		t.Fatalf("after switching off: %v", r)
	}
}

func TestRepositoriesTabAndSettingsAgreeInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	pg.mustSave(0, dsRecord())
	api := pg.addRepository("api")
	web := pg.addRepository("web")
	me := newPerson(t)
	rowOf := func(id string) string {
		return `[data-design-system-optin] [data-optin-row][data-repository-id="` + id + `"]`
	}

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

	me.run(t, "its settings show the same state, and the other repository is still off",
		chromedp.Navigate(pg.settingsURL(web)),
		designPoll(`document.querySelector('`+optInSwitch+`')?.checked === true`),
		chromedp.Navigate(pg.settingsURL(api)),
		designPoll(`document.querySelector('`+optInSwitch+`')?.checked === false`),
	)

	me.run(t, "a change in settings is seen in the tab after a reload",
		chromedp.Navigate(pg.settingsURL(api)),
		designPoll(`document.querySelector('`+optInSwitch+`')?.checked === false`),
		designClick(optInSwitch),
		designPoll(`document.querySelector('`+optInRow+`').dataset.phase === 'idle' && document.querySelector('`+optInSwitch+`').checked === true`),
		chromedp.Navigate(pg.url+"?tab=repositories"),
		designPoll(`document.querySelector('`+rowOf(api)+` [data-field=uses]')?.checked === true`),
	)
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
	if r := pg.repository(idOf("api")); r["uses_design_system"] != true {
		t.Fatalf("yes: %v", r)
	}

	me.run(t, "no leaves the next one off",
		append(add("web"),
			designPoll(`!!document.querySelector('`+dsQuestion+` [data-answer=no]')`),
			designClick(dsQuestion+` [data-answer=no]`),
			designPoll(`document.querySelector('`+dsQuestion+`') === null`),
		)...)
	if r := pg.repository(idOf("web")); r["uses_design_system"] != false {
		t.Fatalf("no: %v", r)
	}

	me.run(t, "the question does not come back after a reload",
		chromedp.Navigate(reposURL),
		designPoll(`!!document.querySelector('#repo-root')`),
		designPoll(`document.querySelector('`+dsQuestion+`') === null`),
	)
}
