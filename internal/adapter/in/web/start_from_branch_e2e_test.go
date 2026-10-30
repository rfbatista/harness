//go:build !windows

package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// branchWorld is the server as it runs, over a real git checkout whose origin
// is a bare repository on disk, and a colleague's clone that pushes to it.
type branchWorld struct {
	t       *testing.T
	srv     *httptest.Server
	project *domain.Project
	task    *domain.Ticket
	repo    string // the checkout
	origin  string // the bare remote
	other   string // a colleague's clone
}

func (w *branchWorld) git(dir string, args ...string) string {
	w.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// colleaguePushes puts a new branch on origin from the colleague's clone.
func (w *branchWorld) colleaguePushes(branch string) {
	w.t.Helper()
	w.git(w.other, "checkout", "-q", "-b", branch, "origin/main")
	w.git(w.other, "commit", "-q", "--allow-empty", "-m", "work on "+branch)
	w.git(w.other, "push", "-q", "origin", branch)
}

// waitForSession returns once the server has a session on the project.
func (w *branchWorld) waitForSession() {
	w.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		res, err := w.srv.Client().Get(w.srv.URL + "/api/sessions?project_id=" + w.project.ID)
		if err == nil {
			var body struct {
				Sessions []json.RawMessage `json:"sessions"`
			}
			_ = json.NewDecoder(res.Body).Decode(&body)
			res.Body.Close()
			if len(body.Sessions) > 0 {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	w.t.Fatal("the session was not started")
}

func (w *branchWorld) taskURL() string {
	return w.srv.URL + "/projects/" + w.project.ID + "/tasks/" + w.task.ID
}

func newBranchWorld(t *testing.T) *branchWorld {
	t.Helper()
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
	if out, err := exec.Command("git", "--exec-path").Output(); err == nil {
		t.Setenv("PATH", strings.TrimSpace(string(out))+":"+os.Getenv("PATH"))
	}
	dir := t.TempDir()
	w := &branchWorld{t: t, repo: filepath.Join(dir, "repo"), origin: filepath.Join(dir, "origin.git"), other: filepath.Join(dir, "other")}
	w.git(dir, "init", "-q", "--bare", w.origin)
	w.git(dir, "init", "-q", w.repo)
	w.git(w.repo, "commit", "-q", "--allow-empty", "-m", "init")
	w.git(w.repo, "remote", "add", "origin", w.origin)
	w.git(w.repo, "push", "-q", "origin", "main")
	w.git(w.repo, "fetch", "-q", "origin")
	w.git(dir, "clone", "-q", w.origin, w.other)

	var (
		cat         catalog.Catalog
		orch        *orchestration.Service
		plan        *planning.Service
		ws          *workspaces.Service
		branchNames *workspaces.BranchNames
		wtGit       ports.WorktreeGit
		code        ports.CodeBrowser
	)
	ctx := context.Background()
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule, app.GitCredentialsModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		fx.Populate(&cat, &orch, &plan, &ws, &branchNames, &wtGit, &code),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })
	orch.Ready = nil // no agent credential or managed claude here: /bin/true stands in for claude

	if _, err = cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	if w.project, err = cat.Projects.CreateProject(ctx, "harness", w.repo); err != nil {
		t.Fatal(err)
	}
	if _, err = cat.Projects.CreateRepository(ctx, w.project.ID, "api", "", "file://"+w.origin, w.repo); err != nil {
		t.Fatal(err)
	}
	if w.task, err = plan.CreateTicket(ctx, w.project.ID, "Start from a branch", "", domain.TicketStatusInProgress, ""); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan,
		Workspaces: ws, BranchNames: branchNames, WorktreeGit: wtGit,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects,
		Workspaces: ws, WorktreeGit: wtGit, Code: code,
	}, assets, nil))
	// Over HTTP/2: each page keeps an event stream open.
	w.srv = httptest.NewUnstartedServer(mux)
	w.srv.EnableHTTP2 = true
	w.srv.StartTLS()
	t.Cleanup(func() {
		w.srv.CloseClientConnections()
		w.srv.Close()
	})
	return w
}

const (
	sbForm    = `form[x-data^=sessionsNewSession]`
	sbOptions = `[...document.querySelectorAll('#new-session-base option')].map(o => o.value)`
	sbHint    = `(document.querySelector('#new-session-base-hint')?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	sbStatus  = `(document.querySelector('[data-branch-base-status]')?.checkVisibility() ? document.querySelector('[data-branch-base-status]').textContent : '').replace(/\s+/g, ' ').trim()`
)

// sbPick chooses a branch in the Branch off field as the person does.
func sbPick(name string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const el = document.querySelector('#new-session-base'); el.value = `+quote(name)+`; el.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil)
}

// sbOpenForm opens the task page's New session form once the branches are listed.
func sbOpenForm(w *branchWorld) []chromedp.Action {
	return []chromedp.Action{
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelector('#new-session-base') !== null && !document.querySelector('#new-session-base').disabled && ` + sbOptions + `.includes('main')`),
		waitUntil(nsLive),
	}
}

// TestStartingASessionFromAColleaguesBranch_EndToEnd is W1 for the spec "Web
// UI — start a session from a branch", against a real server, a real
// checkout and a bare remote: opening the form fetches nothing; a branch a
// colleague pushed appears after Fetch from origin, is picked, is explained
// under the field and starts a session cut from origin/<name>; and a branch
// deleted on origin after the fetch is refused under Branch off with the
// rest of the form as filled.
func TestStartingASessionFromAColleaguesBranch_EndToEnd(t *testing.T) {
	w := newBranchWorld(t)
	w.colleaguePushes("feature-x")
	b := newTLSBrowser(t)

	// Pushed after the checkout's last fetch: the form lists what the checkout has.
	b.run("opening the form fetches nothing: the pushed branch is not listed yet", append(sbOpenForm(w),
		waitUntil(sbOptions+`.includes('origin/main')`),
		waitUntil(`!`+sbOptions+`.includes('origin/feature-x')`),
		waitUntil(`document.querySelector("[data-fetch-origin]") !== null`),
		bothSchemes("start-from-branch-form-opened"),
	)...)
	if got := w.git(w.repo, "branch", "-r"); strings.Contains(got, "feature-x") {
		t.Fatalf("opening the form fetched: %s", got)
	}

	b.run("Fetch from origin lists the pushed branch; picking it explains the cut",
		chromedp.Evaluate(`document.querySelector('[data-fetch-origin]').focus()`, nil),
		chromedp.KeyEvent("\r"),
		waitUntil(sbOptions+`.includes('origin/feature-x')`),
		// The busy button stays focusable: a keyboard user is not dropped out of the field.
		waitUntil(`document.activeElement === document.querySelector('[data-fetch-origin]')`),
		sbPick("origin/feature-x"),
		waitUntil(sbHint+` === 'The session gets its own branch, cut from origin/feature-x; it is fetched from origin when the session starts.'`),
		bothSchemes("start-from-branch-remote-base"),
	)

	b.run("starting creates the session on the colleague's branch",
		waitUntil(`document.querySelector(`+quote(sbForm+` button[type=submit]`)+`)?.disabled === false`),
		chromedp.Evaluate(`document.querySelector(`+quote(sbForm+` button[type=submit]`)+`).click()`, nil),
	)
	w.waitForSession()
	b.run("the session's base is origin/feature-x on the page seed",
		chromedp.Navigate(w.taskURL()),
		waitUntil(`(() => { const s = document.getElementById('sessions-seed'); if (!s) return false; return Object.values(JSON.parse(s.textContent).base_refs ?? {}).includes('origin/feature-x'); })()`),
	)

	// A second branch is pushed, fetched and picked; then it is deleted on origin.
	w.colleaguePushes("doomed")
	b.run("a branch deleted on origin after the fetch is refused under Branch off, with the form as filled", append(sbOpenForm(w),
		chromedp.Click(`[data-fetch-origin]`, chromedp.ByQuery),
		waitUntil(sbOptions+`.includes('origin/doomed')`),
		sbPick("origin/doomed"),
		chromedp.SendKeys(`textarea`, "keep me", chromedp.ByQuery),
	)...)
	w.git(w.other, "push", "-q", "origin", "--delete", "doomed")
	b.run("the refusal",
		waitUntil(`document.querySelector(`+quote(sbForm+` button[type=submit]`)+`)?.disabled === false`),
		chromedp.Evaluate(`document.querySelector(`+quote(sbForm+` button[type=submit]`)+`).click()`, nil),
		waitUntil(sbStatus+`.includes('doomed')`),
		waitUntil(`document.querySelector('#new-session-base').getAttribute('aria-invalid') === 'true'`),
		waitUntil(`document.querySelector(`+quote(sbForm+` textarea`)+`).value === 'keep me'`),
		waitUntil(`document.querySelector('#new-session-base').value === 'origin/doomed'`),
		waitUntil(`document.querySelector(`+quote(sbForm+` button[type=submit]`)+`).disabled === false`),
		bothSchemes("start-from-branch-refused"),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
