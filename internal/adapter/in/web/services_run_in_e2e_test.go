//go:build !windows

package web_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/application/services"
	"operators-mcp/internal/domain"
)

// A command typed in the Subfolder field is refused when the service is saved,
// under that field, in the server's words (spec "Web UI — services: Run in
// picker and run variables", testing decisions; contract section 11).
func TestACommandTypedInTheSubfolderFieldIsRefusedWhenSaved_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	b := newTLSBrowser(t)
	b.run("open the form on the Services page",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!document.querySelector('[data-services-empty]')`),
		chromedp.Click(`[data-new-service]`, chromedp.ByQuery),
		waitUntil(`document.activeElement.id === 'sf-name'`),
	)
	b.run("fill it with a repository and a command in the subfolder",
		chromedp.SendKeys(`#sf-name`, "web", chromedp.ByQuery),
		chromedp.SendKeys(`#sf-line`, "make air", chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const s = document.querySelector('#sf-repository'); s.value = '`+w.repo.ID+`'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
		waitUntil(`document.querySelector('#sf-subdir').closest('.field').checkVisibility()`),
		waitUntil(`document.getElementById('sf-subdir-hint').textContent.includes('not a command')`),
		chromedp.SendKeys(`#sf-subdir`, "npm run dev", chromedp.ByQuery),
		chromedp.Click(`[data-form-save]`, chromedp.ByQuery),
	)
	b.run("the refusal shows under the Subfolder field and nothing is saved",
		waitUntil(`(() => { const e = document.getElementById('sf-subdir-error'); return !e.hidden && e.textContent.includes('"npm run dev" is not a folder in api') && document.getElementById('sf-subdir').getAttribute('aria-invalid') === 'true'; })()`),
		waitUntil(`!!document.querySelector('[data-service-form]') && !document.querySelector('[data-service]')`),
	)
}

// stubRunWorktrees offers the main checkout and one session's worktree, the
// way the real lookup does (ADR 0039), with directories that exist.
type stubRunWorktrees struct{ session *domain.RunWorktree }

func (s stubRunWorktrees) ListRepositoryRunWorktrees(_ context.Context, _, _ string) ([]*domain.RunWorktree, error) {
	return []*domain.RunWorktree{{Key: "main", Kind: domain.RunWorktreeMain, Branch: "main"}, s.session}, nil
}

// shot saves a screenshot of the page as <name>.png in $SERVICES_SHOTS_DIR, to compare with
// the designs; without the variable it does nothing.
func shot(name string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		dir := os.Getenv("SERVICES_SHOTS_DIR")
		if dir == "" {
			return nil
		}
		var png []byte
		if err := chromedp.CaptureScreenshot(&png).Do(ctx); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name+".png"), png, 0o644)
	})
}

// theme pins the page's theme, light or dark.
func theme(name string) chromedp.Action {
	return chromedp.Evaluate(`document.documentElement.dataset.theme = '`+name+`'`, nil)
}

type fixedBranch string

func (b fixedBranch) CurrentBranch(context.Context, string) (string, error) { return string(b), nil }

// The "Run in" picker against a real server (spec "Web UI — services: Run in
// picker and run variables", testing decisions): the person picks a session's
// worktree beside Start, starts the service there, sees the branch on its row
// and in the log's header, and the pick is remembered after a reload. The
// session's worktree is a stand-in lookup with a real directory, since
// starting a real agent session is too heavy for this test; everything else
// is the real services module, API and page.
func TestAServiceIsStartedInASessionsWorktreeFromTheRunInPicker_EndToEnd(t *testing.T) {
	session := &domain.RunWorktree{
		Key: "session:s2", Kind: domain.RunWorktreeSession, Branch: "agent/other", Dir: t.TempDir(),
		SessionID: "s2", Agent: "go-developer", SessionStatus: domain.SessionStatus("running"),
	}
	w := newServicesWorldWith(t, func(o *services.Options) {
		o.RunWorktrees = stubRunWorktrees{session: session}
		o.Branches = fixedBranch("main")
	})
	svc, err := w.svc.CreateService(context.Background(), w.project.ID, domain.ServiceInput{
		Name: "web", Kind: domain.ServiceKindCommand, RepositoryID: w.repo.ID,
		Env:     "PORT=8080\nTOKEN=s3cret",
		Command: &domain.ServiceCommand{Line: "echo up; sleep 30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	const (
		row     = `document.querySelector('[data-service][data-name=web]')`
		trigger = row + `.querySelector('[data-run-in] .trigger')`
		status  = row + `?.querySelector('[data-service-status]')?.textContent`
	)
	b := newTLSBrowser(t)
	b.run("the picker offers the main checkout and the session's worktree",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!`+trigger+` && `+trigger+`.textContent.includes('main')`),
		chromedp.Evaluate(trigger+`.click()`, nil),
		waitUntil(`[...document.querySelectorAll('[role=option]')].some((o) => o.textContent.includes('agent/other') && o.textContent.includes('go-developer'))`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("picker-open-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("picker-open-dark"),
	)
	b.run("pick the session's worktree: nothing starts",
		chromedp.Evaluate(`[...document.querySelectorAll('[role=option]')].find((o) => o.textContent.includes('agent/other')).click()`, nil),
		waitUntil(trigger+`.textContent.includes('agent/other')`),
		waitUntil(status+` === 'stopped'`),
	)
	b.run("Start runs it there: the row names the branch and the kind of worktree",
		chromedp.Click(`[data-service][data-name=web] [data-action=start]`, chromedp.ByQuery),
		waitUntil(status+` === 'running'`),
		waitUntil(row+`.querySelector('[data-service-where]').textContent === 'session worktree @ agent/other'`),
		waitUntil(`!`+row+`.querySelector('[data-run-in]').checkVisibility()`),
		waitUntil(row+`.querySelector('[data-action=restart]').title === 'Repeats the run: the same worktree and the same overrides'`),
	)
	b.run("the log's header names it too",
		chromedp.Click(`[data-service][data-name=web] [data-action=log]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-service-log] .meta').textContent.includes('session worktree @ agent/other')`),
	)
	const (
		vars  = `document.querySelector('[data-service-variables]')`
		item  = `(name) => ` + vars + `.querySelector('[data-variable][data-name=' + name + ']')`
		value = `(name) => (` + item + `)(name).querySelector('[data-variable-value]').textContent.trim()`
	)
	b.run("the Variables section lists the run's variables with their source, masked",
		waitUntil(vars+`.querySelector('[data-variables-summary]').textContent === '2 variables'`),
		chromedp.Click(`[data-variables-toggle]`, chromedp.ByQuery),
		waitUntil(`!!(`+item+`)('PORT') && (`+item+`)('PORT').checkVisibility()`),
		waitUntil(`(`+item+`)('PORT').textContent.includes('service')`),
		waitUntil(`(`+value+`)('PORT').startsWith('••')`),
		waitUntil(`!document.body.innerText.includes('s3cret') && !document.body.innerText.includes('8080')`),
	)
	b.run("reveal one value: only that one shows",
		chromedp.Click(`[data-variable][data-name=PORT] [data-variable-toggle]`, chromedp.ByQuery),
		waitUntil(`(`+value+`)('PORT').startsWith('8080')`),
		waitUntil(`(`+value+`)('TOKEN').startsWith('••') && !document.body.innerText.includes('s3cret')`),
		waitUntil(`document.querySelector('[data-variable][data-name=PORT] [data-variable-toggle]').getAttribute('aria-label') === 'Hide PORT'`),
	)
	b.run("the screenshots, then Reveal all",
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("running-in-session-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("running-in-session-dark"),
		chromedp.Click(`[data-variables-all]`, chromedp.ByQuery),
		waitUntil(`(`+value+`)('TOKEN').startsWith('s3cret') && document.querySelector('[data-variables-all]').textContent === 'Hide all'`),
		waitUntil(`!JSON.stringify(localStorage).includes('s3cret') && !location.href.includes('s3cret')`),
	)
	b.run("stop it, reload: the pick is remembered",
		chromedp.Click(`[data-service][data-name=web] [data-action=stop]`, chromedp.ByQuery),
		waitUntil(status+` === 'stopped'`),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!`+trigger+` && `+trigger+`.textContent.includes('agent/other')`),
		waitUntil(`localStorage.getItem('harness:service-run-worktree:`+svc.ID+`') === 'session:s2'`),
	)
}
