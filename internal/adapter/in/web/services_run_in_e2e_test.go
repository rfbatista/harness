//go:build !windows

package web_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/adapter/out/gitcli"
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

// mainOnlyRunWorktrees offers the main checkout alone, as a repository with no live session does.
type mainOnlyRunWorktrees struct{}

func (mainOnlyRunWorktrees) ListRepositoryRunWorktrees(context.Context, string, string) ([]*domain.RunWorktree, error) {
	return []*domain.RunWorktree{{Key: "main", Kind: domain.RunWorktreeMain, Branch: "main"}}, nil
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

// git runs a git command in dir for the test, with an author, failing the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(realGit(t), append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	// A fixture repository in a temp folder: the agent shims in PATH (the branch gate's) are not for it.
	cmd.Env = withoutShims(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// realGit is the git of the PATH without the agent shims (exec resolves the name against the parent's PATH).
func realGit(t *testing.T) string {
	t.Helper()
	for _, kv := range withoutShims([]string{"PATH=" + os.Getenv("PATH")}) {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			for _, dir := range filepath.SplitList(path) {
				if p := filepath.Join(dir, "git"); isExecutable(p) {
					return p
				}
			}
		}
	}
	t.Skip("browser test: git not found")
	return ""
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// withoutShims is env without the harness's agent shims in PATH.
func withoutShims(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			var keep []string
			for _, dir := range filepath.SplitList(path) {
				if !strings.Contains(dir, "harness-shims") {
					keep = append(keep, dir)
				}
			}
			kv = "PATH=" + strings.Join(keep, string(os.PathListSeparator))
		}
		out = append(out, kv)
	}
	return out
}

// A branch is run from the "Run in" picker against a real server and a real git
// repository (spec "Web UI — services: run a branch from the Run in picker",
// testing decisions; contract section 12): the person searches the branches,
// picks a local one that is checked out nowhere, starts the service, sees
// branch @ <name> · <sha> on its row and in the log's header, the program ran
// that branch's files, and a reload remembers the pick.
func TestAServiceRunsABranchPickedFromTheBranchSearch_EndToEnd(t *testing.T) {
	// The server's own git calls are not an agent's: without the agent shims in PATH.
	for _, kv := range withoutShims([]string{"PATH=" + os.Getenv("PATH")}) {
		t.Setenv("PATH", strings.TrimPrefix(kv, "PATH="))
	}
	w := newServicesWorldWith(t, func(o *services.Options) {
		o.RunWorktrees = mainOnlyRunWorktrees{}
		o.BranchGit = gitcli.NewServiceBranches()
		o.WorktreeRoot = t.TempDir()
	})
	repo := w.repo.RootDir
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "WHERE.txt"), []byte("on main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "main work")
	git(t, repo, "checkout", "-q", "-b", "feature/x")
	if err := os.WriteFile(filepath.Join(repo, "WHERE.txt"), []byte("on feature x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-q", "-am", "Add the feature")
	sha := git(t, repo, "rev-parse", "--short=7", "HEAD")
	git(t, repo, "checkout", "-q", "main")
	// A branch that exists only on origin: pushed to a bare origin, then removed locally.
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, repo, "init", "-q", "--bare", "-b", "main", origin)
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "checkout", "-q", "-b", "remote-only")
	if err := os.WriteFile(filepath.Join(repo, "WHERE.txt"), []byte("on origin only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-q", "-am", "Work that lives on origin")
	remoteSha := git(t, repo, "rev-parse", "--short=7", "HEAD")
	git(t, repo, "push", "-q", "origin", "remote-only")
	git(t, repo, "checkout", "-q", "main")
	git(t, repo, "branch", "-q", "-D", "remote-only")
	git(t, repo, "fetch", "-q", "origin")
	svc, err := w.svc.CreateService(context.Background(), w.project.ID, domain.ServiceInput{
		Name: "web", Kind: domain.ServiceKindCommand, RepositoryID: w.repo.ID,
		Command: &domain.ServiceCommand{Line: "cat WHERE.txt; sleep 30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	const (
		row     = `document.querySelector('[data-service][data-name=web]')`
		trigger = row + `.querySelector('[data-run-in] .trigger')`
		status  = row + `?.querySelector('[data-service-status]')?.textContent`
		option  = `[...document.querySelectorAll('[data-branch-search] [role=option]')].find((o) => o.textContent.includes('feature/x'))`
	)
	b := newTLSBrowser(t)
	b.run("open the picker and choose Branch…",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!`+trigger+` && `+trigger+`.textContent.includes('main')`),
		chromedp.Evaluate(trigger+`.click()`, nil),
		waitUntil(`[...document.querySelectorAll('[role=option]')].some((o) => o.textContent.includes('Branch…'))`),
		chromedp.Evaluate(`[...document.querySelectorAll('[role=option]')].find((o) => o.textContent.includes('Branch…')).click()`, nil),
		waitUntil(`!!document.querySelector('[data-branch-search]')`),
	)
	b.run("the search lists the branches with their commit; typing narrows it",
		waitUntil(`!!(`+option+`) && (`+option+`).textContent.includes('`+sha+`') && (`+option+`).textContent.includes('Add the feature')`),
		waitUntil(`document.querySelector('[data-branch-search] .popup').textContent.includes('Local branches')`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("branch-search-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("branch-search-dark"),
		chromedp.SendKeys(`[data-branch-search] input:not([type=hidden])`, "zzz", chromedp.ByQuery),
		waitUntil(`!(`+option+`)`),
	)
	b.run("pick the branch: the picker reads it, nothing starts",
		chromedp.SendKeys(`[data-branch-search] input:not([type=hidden])`, strings.Repeat(kb.Backspace, 3), chromedp.ByQuery),
		waitUntil(`!!(`+option+`)`),
		chromedp.Evaluate(`(`+option+`).click()`, nil),
		waitUntil(`!document.querySelector('[data-branch-search]')`),
		waitUntil(trigger+`.textContent.includes('feature/x')`),
		waitUntil(status+` === 'stopped'`),
	)
	b.run("Start runs it: the row and the log header name the branch and its commit",
		chromedp.Click(`[data-service][data-name=web] [data-action=start]`, chromedp.ByQuery),
		waitUntil(status+` === 'running'`),
		waitUntil(row+`.querySelector('[data-service-where]').textContent === 'branch @ feature/x · `+sha+`'`),
		chromedp.Click(`[data-service][data-name=web] [data-action=log]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-service-log] .meta').textContent.includes('branch @ feature/x · `+sha+`')`),
		waitUntil(`document.querySelector('[data-log]').textContent.includes('on feature x')`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("branch-run-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("branch-run-dark"),
	)
	b.run("stop it and reload: the pick is remembered",
		chromedp.Click(`[data-service][data-name=web] [data-action=stop]`, chromedp.ByQuery),
		waitUntil(status+` === 'stopped'`),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!`+trigger+` && `+trigger+`.textContent.includes('feature/x')`),
		waitUntil(`localStorage.getItem('harness:service-run-worktree:`+svc.ID+`') === 'branch:feature/x'`),
	)
	const remoteOption = `[...document.querySelectorAll('[data-branch-search] [role=option]')].find((o) => o.textContent.includes('origin/remote-only'))`
	b.run("search for a branch that exists only on origin: it is listed under the remote branches",
		chromedp.Evaluate(trigger+`.click()`, nil),
		waitUntil(`[...document.querySelectorAll('[role=option]')].some((o) => o.textContent.includes('Branch…'))`),
		chromedp.Evaluate(`[...document.querySelectorAll('[role=option]')].find((o) => o.textContent.includes('Branch…')).click()`, nil),
		waitUntil(`!!document.querySelector('[data-branch-search]')`),
		waitUntil(`!!(`+remoteOption+`) && document.querySelector('[data-branch-search] .popup').textContent.includes('Remote branches · fetched first')`),
		waitUntil(`(`+remoteOption+`).textContent.includes('`+remoteSha+`')`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("branch-search-remote-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("branch-search-remote-dark"),
		chromedp.Evaluate(`(`+remoteOption+`).click()`, nil),
		waitUntil(`!document.querySelector('[data-branch-search]')`),
		waitUntil(trigger+`.textContent.includes('origin/remote-only')`),
	)
	b.run("Start fetches and runs it: the row and the log read branch @ origin/<name> · <sha>",
		chromedp.Click(`[data-service][data-name=web] [data-action=start]`, chromedp.ByQuery),
		waitUntil(status+` === 'running'`),
		waitUntil(row+`.querySelector('[data-service-where]').textContent === 'branch @ origin/remote-only · `+remoteSha+`'`),
		chromedp.Click(`[data-service][data-name=web] [data-action=log]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-service-log] .meta').textContent.includes('branch @ origin/remote-only · `+remoteSha+`')`),
		waitUntil(`document.querySelector('[data-log]').textContent.includes('on origin only')`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("branch-run-remote-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("branch-run-remote-dark"),
	)
}
