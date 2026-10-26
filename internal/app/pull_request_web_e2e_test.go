//go:build !windows

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/githubapi/githubapitest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Opening a pull request from the web UI, end to end: the server as it runs
// (workspaces, the pull requests use case, the project feed, the HTTP API and
// the pages) with real git, origin a local bare repository that reads as
// GitHub, and only the host faked. Two people, each in their own headless
// Chrome at phone width, one in light and one in dark: one opens the PR on a
// session's history page, the other sees it arrive on the task page; the host
// merges it; then the ways a create cannot go ahead or is refused. CI never
// reaches GitHub. Screenshots land in ARCHITECT_SHOTS when it names a directory.
func TestPullRequestsInTheWebUI_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx := context.Background()
	dir := t.TempDir()
	widgets := domain.GitRemote{Host: "github.com", Owner: "acme", Repo: "widgets"}

	repoDir := filepath.Join(dir, "widgets")
	gitInit(t, repoDir)
	bare := filepath.Join(dir, "origin.git")
	gitE2E(t, dir, "init", "-q", "--bare", bare)
	gitE2E(t, repoDir, "remote", "add", "origin", "git@github.com:acme/widgets.git")
	gitE2E(t, repoDir, "config", "url."+bare+".insteadOf", "git@github.com:acme/widgets.git")
	gitE2E(t, repoDir, "push", "-q", "origin", "main")
	gitE2E(t, repoDir, "fetch", "-q", "origin")
	localDir := filepath.Join(dir, "local-only")
	gitInit(t, localDir)

	// Every host call waits at the gate until the first create has been
	// seen in progress; the gate is set before the server starts.
	host := githubapitest.New()
	gate := make(chan struct{})
	host.Gate = gate
	released := false
	release := func() {
		if !released {
			released = true
			close(gate)
		}
	}
	t.Cleanup(release)

	s := startPRServer(t, dir, filepath.Join(dir, "harness.db"), host)
	t.Cleanup(func() { _ = s.app.Stop(context.Background()) })
	s.storeCredentials(t)
	if _, err := s.cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := s.cat.Projects.CreateProject(ctx, "widgets", dir)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := s.cat.Projects.CreateRepository(ctx, p.ID, "widgets", "", "git@github.com:acme/widgets.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.cat.Projects.CreateRepository(ctx, p.ID, "local-only", "", "file://"+localDir, localDir) // no origin remote in the checkout
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.plan.CreateTicket(ctx, p.ID, "Create pull request", "Open a PR from the UI", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}

	// A session per case, each in its own worktree of the task.
	session := func(id string, r *domain.Repository, base string, commits int, uncommitted bool) *domain.Workspace {
		t.Helper()
		ws, err := s.ws.Create(r.ID, id, "agent/"+id, base)
		if err != nil {
			t.Fatal(err)
		}
		for i := range commits {
			name := filepath.Join(ws.Path, id+string(rune('a'+i))+".txt")
			if err := os.WriteFile(name, []byte(id+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitE2E(t, ws.Path, "add", ".")
			gitE2E(t, ws.Path, "commit", "-q", "-m", "Add "+filepath.Base(name))
		}
		if uncommitted {
			if err := os.WriteFile(filepath.Join(ws.Path, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.sess.Create(&domain.Session{ID: id, ProjectID: p.ID, RepositoryID: r.ID, TicketID: task.ID, Task: id,
			WorkingDir: ws.Path, WorkspaceID: ws.ID, Branch: ws.Branch, Status: domain.SessionDone}); err != nil {
			t.Fatal(err)
		}
		return ws
	}
	feature := session("s-feature", repo, "origin/main", 2, true)
	session("s-empty", repo, "origin/main", 0, false)
	session("s-refusals", repo, "origin/main", 1, false)
	adopted := session("s-adopted", repo, "origin/main", 1, false)
	session("s-local", local, "main", 1, false)

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: s.cat.Projects, Agents: s.cat.Agents, Sessions: s.orch, Planning: s.plan, PullRequests: s.prs,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: s.cat.Projects, Tasks: s.plan, Sessions: s.orch, Agents: s.cat.Agents, Repositories: s.cat.Projects,
		EnvFiles: s.cat.Projects, Documents: s.plan, History: s.ws, PullRequests: s.prs,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the pages' event streams never end on their own
		srv.Close()
	})
	history := func(sessionID string) string {
		return srv.URL + "/projects/" + p.ID + "/tasks/" + task.ID + "/sessions/" + sessionID + "/history"
	}

	author, reviewer := newPerson(t), newPerson(t)
	phone := func(scheme string) chromedp.Action {
		return chromedp.Tasks{
			chromedp.EmulateViewport(390, 844),
			emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}}),
		}
	}
	const (
		panel      = `document.querySelector('main[x-data="pullRequestPanel"]')`
		createBtn  = `document.querySelector('main header [x-on\\:click="openForm"]')`
		submit     = `document.querySelector('#pr-form button[type=submit]')`
		chipText   = `[...document.querySelectorAll('main header .pr-chip span')].map(s => s.textContent).join(' ')`
		badgeOf    = `(id) => [...document.querySelectorAll('[data-session-id="' + id + '"] .pr-badge')].find(b => !b.closest('[data-ssr]'))`
		formOpen   = `document.getElementById('pr-form').checkVisibility()`
		chipShown  = `document.querySelector('main header .pr-chip').checkVisibility()`
		btnShown   = createBtn + `.checkVisibility()`
		refusal    = `document.querySelector('#pr-form [x-ref=refusal]')`
		typeTitleJ = `(t) => { const i = document.getElementById('pr-title'); i.value = t; i.dispatchEvent(new Event('input')) }`
	)
	shown := func(js string) string {
		return `(() => { const el = ` + js + `; return !!el && el.checkVisibility() })()`
	}

	// The reviewer watches the task page; the feature's row has no PR yet.
	reviewer.run(t, "the reviewer opens the task page",
		phone("dark"),
		chromedp.Navigate(srv.URL+"/projects/"+p.ID+"/tasks/"+task.ID),
		waitFor(`[...document.querySelectorAll('[data-session-id="s-feature"]')].some(r => !r.closest('[data-ssr]'))`),
		waitFor(`!(`+shown(`(`+badgeOf+`)('s-feature')`)+`)`),
	)

	// The author opens the feature's history: commits ahead, one file left uncommitted.
	author.run(t, "the author opens the session's history",
		phone("light"),
		chromedp.Navigate(history("s-feature")),
		waitFor(panel+` && `+btnShown),
		chromedp.Evaluate(createBtn+`.click()`, nil),
		waitFor(formOpen),
		shot("pr-form-light", "light"),
	)
	if got := author.text(t, `document.getElementById('pr-title').value`); got != "Create pull request" {
		t.Errorf("the title starts from the task's: %q", got)
	}
	if got := author.text(t, `document.querySelector('#pr-form [x-text=uncommitted]').textContent`); got != "1 uncommitted change in the worktree will not be in the pull request." {
		t.Errorf("uncommitted warning = %q", got)
	}
	if got := author.text(t, `document.querySelector('#pr-form [x-text=summary]').textContent`); got != "2 commits from agent/s-feature into main on acme/widgets" {
		t.Errorf("summary = %q", got)
	}

	// A double submit while the host holds the create: one push, one PR, progress meanwhile.
	author.run(t, "the author submits twice",
		chromedp.Evaluate(`(`+typeTitleJ+`)('Follow the widget feed'); document.getElementById('pr-draft').click(); `+submit+`.click(); `+submit+`.click()`, nil),
		waitFor(shown(`document.querySelector('#pr-form [x-text=progress]')`)),
		shot("pr-creating-light", "light"),
	)
	if got := author.text(t, `document.querySelector('#pr-form [x-text=progress]').textContent`); got != "Pushing agent/s-feature and opening the pull request on GitHub…" {
		t.Errorf("progress = %q", got)
	}
	release()
	author.run(t, "the PR shows as a chip",
		waitFor(chipShown+` && !(`+formOpen+`)`),
		shot("pr-open-light", "light"),
	)
	if got := author.text(t, chipText); got != "draft #1 Follow the widget feed" {
		t.Errorf("chip = %q", got)
	}
	if got := author.text(t, `document.querySelector('.pr-strip [x-text=notice]').textContent`); got != "" {
		t.Errorf("the author's own PR is not news from elsewhere: %q", got)
	}
	if opened := host.Opened(); len(opened) != 1 || opened[0].Title != "Follow the widget feed" || !opened[0].Draft || opened[0].Head != "agent/s-feature" || opened[0].Base != "main" {
		t.Fatalf("opened on the host = %+v: one, as the form said", opened)
	}
	if head, pushed := gitE2E(t, feature.Path, "rev-parse", "HEAD"), gitE2E(t, bare, "rev-parse", "refs/heads/agent/s-feature"); head != pushed {
		t.Errorf("origin has %s, the branch is at %s", pushed, head)
	}

	// The reviewer's row gets its badge without a reload.
	reviewer.run(t, "the badge arrives on the task page",
		waitFor(shown(`(`+badgeOf+`)('s-feature')`)),
		shot("pr-task-badge-dark", "dark"),
	)
	if got := reviewer.text(t, `(`+badgeOf+`)('s-feature').textContent + ' ' + (`+badgeOf+`)('s-feature').getAttribute('href')`); got != "PR #1 https://github.com/acme/widgets/pull/1" {
		t.Errorf("badge = %q", got)
	}

	// GitHub merges it; the server's refresh reaches both pages live.
	host.SetState(widgets, 1, domain.PullRequestMerged)
	if err := s.prs.RefreshNow(ctx, feature.ID); err != nil {
		t.Fatal(err)
	}
	author.run(t, "the merge shows on the history page, and create is offered again",
		waitFor(`(`+chipText+`).startsWith('merged #1') && `+btnShown),
		shot("pr-merged-light", "light"),
	)
	reviewer.run(t, "the merge shows on the badge",
		waitFor(`(`+badgeOf+`)('s-feature').dataset.tone === 'signal'`),
	)
	reviewer.run(t, "the reviewer opens the same history",
		chromedp.Navigate(history("s-feature")),
		waitFor(panel+` && `+chipShown+` && `+btnShown),
		shot("pr-merged-dark", "dark"),
	)

	// Nothing to propose: the button is disabled and says why.
	author.run(t, "a session with no commits",
		chromedp.Navigate(history("s-empty")),
		waitFor(panel+` && `+btnShown),
		shot("pr-blocked-light", "light"),
	)
	if got := author.text(t, `[...document.querySelectorAll('#pr-reasons li')].map(l => l.textContent).join(' | ') + ' ' + `+createBtn+`.getAttribute('aria-disabled')`); got != "Nothing to propose yet: agent/s-empty has no commits ahead of main. true" {
		t.Errorf("no commits: %q", got)
	}

	// No GitHub origin: the reason names it.
	author.run(t, "a repository without origin",
		chromedp.Navigate(history("s-local")),
		waitFor(panel+` && `+btnShown),
	)
	if got := author.text(t, `[...document.querySelectorAll('#pr-reasons li')].map(l => l.textContent).join(' | ')`); got != "This repository has no GitHub origin remote, so there is nowhere to open a pull request." {
		t.Errorf("no remote: %q", got)
	}

	// GitHub refuses the stored token after the page was read: the server's
	// message with its code, the input kept.
	author.run(t, "open the form while the token is good",
		chromedp.Navigate(history("s-refusals")),
		waitFor(panel+` && `+btnShown),
		chromedp.Evaluate(createBtn+`.click()`, nil),
		waitFor(formOpen),
	)
	host.Reject(e2eToken)
	author.run(t, "create with a revoked token",
		chromedp.Evaluate(`(`+typeTitleJ+`)('Keep this title'); `+submit+`.click()`, nil),
		waitFor(shown(refusal)),
		shot("pr-refused-light", "light"),
	)
	if got := author.text(t, refusal+`.innerText`); !contains(got, "GIT_CREDENTIAL_REJECTED", "Bad credentials") {
		t.Errorf("revoked token: %q", got)
	}
	if got := author.text(t, `document.getElementById('pr-title').value`); got != "Keep this title" {
		t.Errorf("the title is kept: %q", got)
	}

	// Origin lost the base after the picker listed it (the list is as last fetched): said on the field.
	if _, err := s.creds.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "GitHub, new", Kind: domain.GitHTTPSToken, Host: "github.com", Token: "ghp_e2eStoredToken0003", IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	author.run(t, "create into a base origin does not have",
		chromedp.Evaluate(`document.querySelector('[x-data=pullRequestPanel]')._x_dataStack[0].form.baseBranch = 'release-9'; `+submit+`.click()`, nil),
		waitFor(shown(`document.getElementById('pr-base-error')`)),
	)
	if got := author.text(t, `document.getElementById('pr-base-error').textContent + ' ' + document.getElementById('pr-base').getAttribute('aria-invalid')`); got != "release-9 is not a branch on origin. true" {
		t.Errorf("base not on origin: %q", got)
	}
	if n := len(host.Opened()); n != 1 {
		t.Errorf("refused creates opened nothing on the host: %d opened", n)
	}

	// A PR opened on GitHub by hand for the branch is adopted, not an error.
	gitE2E(t, adopted.Path, "push", "-q", "origin", adopted.Branch)
	byHand := host.Add(widgets, ports.HostPullRequest{Head: adopted.Branch, Base: "main", Title: "Opened by hand"})
	author.run(t, "create finds the one opened by hand",
		chromedp.Navigate(history("s-adopted")),
		waitFor(panel+` && (`+btnShown+` || `+chipShown+`)`),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// The page's read may already have adopted it in the background.
			var chip bool
			if err := chromedp.Evaluate(chipShown, &chip).Do(ctx); err != nil || chip {
				return err
			}
			return chromedp.Tasks{
				chromedp.Evaluate(createBtn+`.click()`, nil),
				waitFor(formOpen + ` || ` + chipShown),
				chromedp.Evaluate(formOpen+` && `+submit+`.click()`, nil),
			}.Do(ctx)
		}),
		waitFor(chipShown+` && !(`+formOpen+`)`),
	)
	if got := author.text(t, chipText); got != "open #"+itoa(byHand.Number)+" Opened by hand" {
		t.Errorf("adopted chip = %q", got)
	}
	if got := author.text(t, `document.querySelector('.pr-strip [role=alert]')?.offsetParent ? 'error shown' : ''`); got != "" {
		t.Error("adopting is not an error")
	}
	if n := len(host.Opened()); n != 1 {
		t.Errorf("adopting opened nothing new on the host: %d opened", n)
	}

	for _, who := range []*person{author, reviewer} {
		who.mu.Lock()
		if len(who.errors) > 0 {
			t.Errorf("JS errors: %v", who.errors)
		}
		who.mu.Unlock()
	}
}

func contains(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func itoa(n int) string { return strconv.Itoa(n) }
