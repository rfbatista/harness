//go:build !windows

package web_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service requests on a branch (spec "Web UI — service requests on a branch", testing decisions;
// contract "Harness server ↔ Web UI — services", revision 9, sections 12 and 13). A real server and
// a real git repository: an agent's start or restart on a branch is filed as the services tool
// files it, with the branch and its tip, and the person answers it on the page.

// requestBranchWorld is a services world whose repository has the local branch feature/x and the branch
// feature/y that lives only on origin, and one command service in it.
type requestBranchWorld struct {
	*servicesWorld
	web     *domain.Service
	xSha    string // feature/x's tip when the world was made
	remoteY string // feature/y's tip on origin
}

func newRequestBranchWorld(t *testing.T) *requestBranchWorld {
	t.Helper()
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
	commit := func(file, message string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, "WHERE.txt"), []byte(file), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, repo, "add", ".")
		git(t, repo, "commit", "-q", "-m", message)
	}
	git(t, repo, "init", "-q", "-b", "main")
	commit("on main\n", "main work")
	git(t, repo, "checkout", "-q", "-b", "feature/x")
	commit("on feature x\n", "Add the feature")
	xSha := git(t, repo, "rev-parse", "--short=7", "HEAD")
	git(t, repo, "checkout", "-q", "main")
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, repo, "init", "-q", "--bare", "-b", "main", origin)
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "checkout", "-q", "-b", "feature/y")
	commit("on origin only\n", "Work that lives on origin")
	remoteY := git(t, repo, "rev-parse", "--short=7", "HEAD")
	git(t, repo, "push", "-q", "origin", "feature/y")
	git(t, repo, "checkout", "-q", "main")
	git(t, repo, "branch", "-q", "-D", "feature/y")
	git(t, repo, "fetch", "-q", "origin")
	web, err := w.svc.CreateService(context.Background(), w.project.ID, domain.ServiceInput{
		Name: "web", Kind: domain.ServiceKindCommand, RepositoryID: w.repo.ID,
		Command: &domain.ServiceCommand{Line: "cat WHERE.txt; sleep 30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &requestBranchWorld{servicesWorld: w, web: web, xSha: xSha, remoteY: remoteY}
}

// asks files the request an agent's start_service or restart_service makes with a worktree key:
// the key, and for a branch the branch with its tip as the server resolves it now.
func (w *requestBranchWorld) asks(t *testing.T, action domain.ServiceRequestAction, worktree string) *agentCall {
	t.Helper()
	in := ports.ServiceRequestInput{Action: action, ServiceID: w.web.ID, ServiceName: "web", Worktree: worktree}
	if strings.HasPrefix(worktree, "branch:") {
		branch, err := w.svc.ResolveServiceBranch(context.Background(), w.web.ID, worktree)
		if err != nil {
			t.Fatal(err)
		}
		in.Branch = branch
	}
	return w.agentAsks(t, in)
}

// A start or restart request on a branch arrives live and says which branch at which commit, as a
// run on a branch does; a request on a remote branch says so; a request on main shows no branch.
func TestAServiceRequestOnABranchShowsTheBranchAndItsCommitOnItsCard_EndToEnd(t *testing.T) {
	w := newRequestBranchWorld(t)
	const cards = `[...document.querySelectorAll('[data-service-request]')].map((c) => c.querySelector('h3').textContent.replace(/^Service\s+/, '').trim() + ' | ' + ([...c.querySelectorAll('[data-request-branch]')].filter((e) => e.checkVisibility()).map((e) => e.textContent.trim()).join('') || 'no branch')).sort().join('\n')`
	b := newTLSBrowser(t)
	b.run("the page is open with nothing waiting",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!document.querySelector('[data-service][data-name=web]')`),
		waitUntil(`!document.querySelector('[data-requests-badge]').checkVisibility()`),
	)
	w.asks(t, domain.ServiceRequestStart, "branch:feature/x")
	waitForRequests(t, w.servicesWorld, 1)
	b.run("the request arrives by the feed, and its card shows the branch and its commit",
		waitUntil(`document.querySelector('[data-requests-badge]').checkVisibility() && document.querySelector('[data-requests-badge]').textContent.includes('1')`),
		chromedp.Click(`[data-requests-badge]`, chromedp.ByQuery),
		waitUntil(`(`+cards+`) === 'Start web | branch @ feature/x · `+w.xSha+`'`),
	)
	w.asks(t, domain.ServiceRequestRestart, "branch:origin/feature/y")
	waitForRequests(t, w.servicesWorld, 2)
	w.asks(t, domain.ServiceRequestRestart, "main")
	waitForRequests(t, w.servicesWorld, 3)
	b.run("a restart on a remote branch says origin/, and a request on main shows no branch",
		waitUntil(`(`+cards+`) === 'Restart web | branch @ origin/feature/y · `+w.remoteY+`\nRestart web | no branch\nStart web | branch @ feature/x · `+w.xSha+`'`),
		waitUntil(`[...document.querySelectorAll('[data-request-worktree]')].filter((e) => e.checkVisibility()).map((e) => e.textContent).join() === 'Runs in the main checkout.'`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-branch-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-branch-dark"),
	)
}

// Approving a start on a branch runs it, and the row's run line then reads as a run on a branch
// with the commit the run started at, which is newer than the one the card showed.
func TestAnApprovedServiceRequestOnABranchShowsAsARunOnTheBranch_EndToEnd(t *testing.T) {
	w := newRequestBranchWorld(t)
	call := w.asks(t, domain.ServiceRequestStart, "branch:feature/x")
	waitForRequests(t, w.servicesWorld, 1)
	// The branch moves on after the request was filed.
	repo := w.repo.RootDir
	git(t, repo, "checkout", "-q", "feature/x")
	if err := os.WriteFile(filepath.Join(repo, "WHERE.txt"), []byte("on feature x, later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-q", "-am", "More work")
	newSha := git(t, repo, "rev-parse", "--short=7", "HEAD")
	git(t, repo, "checkout", "-q", "main")
	const where = `document.querySelector('[data-service][data-name=web] [data-service-where]')?.textContent`
	b := newTLSBrowser(t)
	b.run("the card shows the commit it was filed at",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`document.querySelector('[data-requests-badge]')?.checkVisibility()`),
		chromedp.Click(`[data-requests-badge]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-branch]')?.textContent === 'branch @ feature/x · `+w.xSha+`'`),
	)
	b.run("Approve: the card says done and the run line shows the run on the branch, at the commit it started at",
		chromedp.Click(`[data-service-request] [data-action=approve]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-outcome]')?.textContent.includes('Approved and done.')`),
		waitUntil(`(`+where+`) === 'branch @ feature/x · `+newSha+`'`),
	)
	call.answer(t)
}
