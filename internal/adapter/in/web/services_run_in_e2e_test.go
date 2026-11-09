//go:build !windows

package web_test

import (
	"context"
	"testing"

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
	)
	b.run("the log's header names it too",
		chromedp.Click(`[data-service][data-name=web] [data-action=log]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-service-log] .meta').textContent.includes('session worktree @ agent/other')`),
	)
	b.run("stop it, reload: the pick is remembered",
		chromedp.Click(`[data-service][data-name=web] [data-action=stop]`, chromedp.ByQuery),
		waitUntil(status+` === 'stopped'`),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!`+trigger+` && `+trigger+`.textContent.includes('agent/other')`),
		waitUntil(`localStorage.getItem('harness:service-run-worktree:`+svc.ID+`') === 'session:s2'`),
	)
}
