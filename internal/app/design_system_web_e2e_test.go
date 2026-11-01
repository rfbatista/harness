//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
)

// The Design system page against the server as it runs: the real record,
// validation, version check and project feed, with a headless Chrome as the
// person. What the page does with each answer is covered in detail in
// web/src/modules/designsystem; this proves the two sides agree.

type designPage struct {
	t       *testing.T
	srv     *httptest.Server
	project string
	url     string
}

// mustPost calls POST /api/<verb> for the project and answers the JSON body.
func (p *designPage) mustPost(verb string, body map[string]any) map[string]any {
	p.t.Helper()
	body["project_id"] = p.project
	raw, _ := json.Marshal(body)
	res, err := p.srv.Client().Post(p.srv.URL+"/api/"+verb, "application/json", bytes.NewReader(raw))
	if err != nil {
		p.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != http.StatusOK {
		p.t.Fatalf("%s: %d %v", verb, res.StatusCode, out)
	}
	return out
}

func (p *designPage) get() map[string]any { return p.mustPost("get_design_system", map[string]any{}) }

func (p *designPage) mustSave(base int, record map[string]any) {
	p.t.Helper()
	p.mustPost("save_design_system", map[string]any{"base_version": base, "record": record})
}

func startDesignPage(t *testing.T) *designPage {
	t.Helper()
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	dir := t.TempDir()
	var (
		cat  catalog.Catalog
		orch *orchestration.Service
		plan *planning.Service
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&cat, &orch, &plan),
		fx.NopLogger,
	)
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	p, err := cat.Projects.CreateProject(context.Background(), "widgets", dir)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, Sessions: orch, Planning: plan, DesignSystems: cat.DesignSystems,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, EnvFiles: cat.Projects,
		DesignSystems: cat.DesignSystems,
	}, assets, nil))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true // each page holds an event stream open
	srv.StartTLS()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return &designPage{t: t, srv: srv, project: p.ID, url: srv.URL + "/projects/" + p.ID + "/design-system"}
}

func designPoll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(10*time.Second))
}

func designClick(sel string) chromedp.Action {
	return designPoll(`(() => { const e = document.querySelector(` + jsString(sel) + `); if (!e || e.disabled) return false; e.click(); return true; })()`)
}

// tokenRow is the selector of a part of the n-th token row.
func tokenRow(n int, part string) string {
	return `#design-system-panel-tokens [data-token-row]:nth-of-type(` + strconv.Itoa(n) + `) ` + part
}

const (
	dsSaveVersion = `[data-save-bar] [data-save-version]`
	dsSaveButton  = `[data-save-bar] [data-action=save]`
	dsBackground  = `getComputedStyle(document.querySelector('#design-system-panel-tokens [data-token-row] [data-sample]')).backgroundColor`
)

func TestDesignSystemPageInTheBrowserAgainstTheRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	me := newPerson(t)

	// A project with no design system: the first-start screen, and nothing is stored by Start blank.
	me.run(t, "the first-start screen",
		chromedp.Navigate(pg.url),
		designPoll(`!!document.querySelector('#design-system-start [data-action=start-blank]')`),
		designPoll(`document.querySelector('#design-system-start [data-action=import]')?.disabled === true && document.querySelector('#design-system-start [data-action=spawn]')?.disabled === true`),
		designClick(`#design-system-start [data-action=start-blank]`),
		designPoll(`!document.querySelector('#design-system-start') && !!document.querySelector('[data-save-bar]')`),
	)
	if got := pg.get(); got["exists"] != false {
		t.Fatalf("Start blank stored something: %v", got)
	}
	me.run(t, "the save bar", designPoll(`document.querySelector('`+dsSaveVersion+`')?.textContent.trim() === 'Saves as version 1'`))

	// A colour token: the swatch follows what is typed.
	var first, second string
	me.run(t, "add a colour token",
		designClick(`[data-add-token]`),
		designPoll(`!!document.querySelector('#design-system-panel-tokens [data-token-row]')`),
		chromedp.SetValue(tokenRow(1, `[data-field=group]`), "colors", chromedp.ByQuery),
		chromedp.SetValue(tokenRow(1, `[data-field=name]`), "signal", chromedp.ByQuery),
		chromedp.SetValue(tokenRow(1, `[data-field=value]`), "oklch(0.5 0.095 182)", chromedp.ByQuery),
		designPoll(`document.querySelector('[data-token-row] [data-sample]')?.dataset.sampleKind === 'swatch' && `+dsBackground+` !== 'rgba(0, 0, 0, 0)'`),
		chromedp.Evaluate(dsBackground, &first),
		chromedp.SetValue(tokenRow(1, `[data-field=value]`), "#ff0000", chromedp.ByQuery),
		designPoll(dsBackground+` === 'rgb(255, 0, 0)'`),
		chromedp.Evaluate(dsBackground, &second),
	)
	if first == second {
		t.Fatalf("the swatch did not change while typing: %q", first)
	}

	// Save: the version in the bar advances.
	me.run(t, "save",
		chromedp.SetValue(`[data-save-bar] [data-field=message]`, "First token", chromedp.ByQuery),
		designClick(dsSaveButton),
		designPoll(`document.querySelector('`+dsSaveVersion+`')?.textContent.trim() === 'Saves as version 2'`),
	)
	if got := pg.get(); got["version"] != float64(1) || len(got["record"].(map[string]any)["tokens"].([]any)) != 1 {
		t.Fatalf("after the first save: %v", got)
	}
	versions := pg.mustPost("list_design_system_versions", map[string]any{})["versions"].([]any)
	if versions[0].(map[string]any)["message"] != "First token" {
		t.Errorf("version message = %v", versions[0])
	}

	// A duplicate name and a bad value show their codes inline; nothing is saved.
	me.run(t, "a duplicate and a bad value",
		designClick(`[data-add-token]`),
		designPoll(`document.querySelectorAll('#design-system-panel-tokens [data-token-row]').length === 2`),
		chromedp.SetValue(tokenRow(2, `[data-field=group]`), "colors", chromedp.ByQuery),
		chromedp.SetValue(tokenRow(2, `[data-field=name]`), "signal", chromedp.ByQuery),
		chromedp.SetValue(tokenRow(2, `[data-field=value]`), "#fff", chromedp.ByQuery),
		designClick(`[data-add-token]`),
		designPoll(`document.querySelectorAll('#design-system-panel-tokens [data-token-row]').length === 3`),
		chromedp.SetValue(tokenRow(3, `[data-field=group]`), "colors", chromedp.ByQuery),
		chromedp.SetValue(tokenRow(3, `[data-field=name]`), "ink", chromedp.ByQuery),
		chromedp.SetValue(tokenRow(3, `[data-field=value]`), "oklch(0.2; 1)", chromedp.ByQuery),
		designClick(dsSaveButton),
		designPoll(`!!document.querySelector('[data-token-row]:nth-of-type(2) [data-error-for][data-code=DUPLICATE_TOKEN]')`),
		designPoll(`!!document.querySelector('[data-token-row]:nth-of-type(3) [data-error-for][data-code=BAD_VALUE]')`),
		designPoll(`!!document.querySelector('[data-problems]')`),
	)
	if got := pg.get(); got["version"] != float64(1) {
		t.Fatalf("an invalid save stored version %v", got["version"])
	}
	me.run(t, "fixing a row clears its error",
		chromedp.SetValue(tokenRow(3, `[data-field=value]`), "#111", chromedp.ByQuery),
		designPoll(`!document.querySelector('[data-token-row]:nth-of-type(3) [data-error-for]')`),
		designClick(tokenRow(2, `[data-remove]`)),
		designClick(dsSaveButton),
		designPoll(`document.querySelector('`+dsSaveVersion+`')?.textContent.trim() === 'Saves as version 3'`),
	)
	if got := pg.get(); got["version"] != float64(2) || len(got["record"].(map[string]any)["tokens"].([]any)) != 2 {
		t.Fatalf("after the second save: %v", got)
	}

	// Rename is its own action, saved as its own version.
	me.run(t, "rename a token",
		designClick(tokenRow(1, `[data-rename]`)),
		designPoll(`!!document.querySelector('[data-rename-form]')`),
		chromedp.SetValue(`[data-rename-form] [data-field=new-name]`, "brand", chromedp.ByQuery),
		designClick(`[data-rename-form] [data-action=apply-rename]`),
		designPoll(`document.querySelector('[data-token-row] [data-field=name]')?.value === 'brand' && document.querySelector('`+dsSaveVersion+`')?.textContent.trim() === 'Saves as version 4'`),
	)
	if got := pg.get(); got["version"] != float64(3) {
		t.Fatalf("after the rename: %v", got)
	}

	// The page is server-rendered: a reload shows the tokens at once, on the tab asked for.
	me.run(t, "reload", chromedp.Navigate(pg.url+"?tab=tokens"),
		designPoll(`document.querySelectorAll('#design-system-panel-tokens [data-token-row]').length === 2`),
		designPoll(`document.querySelector('[role=tab][aria-selected=true]')?.textContent.includes('Tokens')`),
	)
}

// A save made in another tab: a quiet reload when nothing is edited, a notice
// over unsaved edits that keeps them until the person reloads.
func TestDesignSystemPageFollowsASaveMadeElsewhere(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignPage(t)
	pg.mustSave(0, dsRecord())
	here, there := newPerson(t), newPerson(t)
	rows := `document.querySelectorAll('#design-system-panel-tokens [data-token-row]').length`
	for _, p := range []*person{here, there} {
		p.run(t, "open", chromedp.Navigate(pg.url), designPoll(rows+` === 4`))
		time.Sleep(time.Second) // the page's feed follow is open
	}

	// Nothing edited here: the other tab's save appears without a notice.
	record := pg.get()["record"].(map[string]any)
	record["tokens"] = append(record["tokens"].([]any), dsToken("colors", "ink", "color", "#111"))
	pg.mustSave(1, record)
	here.run(t, "a quiet reload",
		designPoll(rows+` === 5`),
		designPoll(`document.querySelector('[data-save-bar] [data-save-version]')?.textContent.trim() === 'Saves as version 3'`),
	)
	if here.text(t, `String(!!document.querySelector('[data-conflict]'))`) != "false" {
		t.Error("an unedited page showed the notice")
	}

	// Edits here: the notice names the version and the edit stays.
	here.run(t, "edit",
		chromedp.SetValue(`#design-system-panel-tokens [data-token-row]:nth-of-type(1) [data-field=note]`, "mine", chromedp.ByQuery),
		designPoll(`document.querySelector('[data-save-bar] [data-action=save]')?.disabled === false`),
	)
	record = pg.get()["record"].(map[string]any)
	record["principles"] = "# Theirs\n"
	pg.mustSave(2, record)
	here.run(t, "the notice",
		designPoll(`document.querySelector('[data-conflict]')?.textContent.includes('Version 3 was saved elsewhere')`),
	)
	if got := here.text(t, `document.querySelector('#design-system-panel-tokens [data-token-row]:nth-of-type(1) [data-field=note]').value`); got != "mine" {
		t.Fatalf("the edit was replaced by %q", got)
	}
	// Saving over it is refused, not applied.
	here.run(t, "reload on request",
		designClick(`[data-conflict] [data-action=reload]`),
		designPoll(`!document.querySelector('[data-conflict]') && document.querySelector('#design-system-panel-tokens [data-token-row]:nth-of-type(1) [data-field=note]').value === ''`),
		designPoll(`document.querySelector('[data-save-bar] [data-save-version]')?.textContent.trim() === 'Saves as version 4'`),
	)
}
