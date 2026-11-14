//go:build !windows

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/domain"
)

// Clear worktrees in the Archived view, end to end (ADR 0089): the server as
// it runs, real git worktrees. The header button and a row action each open
// the confirm with its counts; confirming clears, keeps every branch and
// session, skips the running session and says so.
func TestClearWorktreesInTheArchivedViewInTheBrowser(t *testing.T) {
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
	f := startMergeFixture(t)

	archived := func(title string) *domain.Ticket {
		tk, err := f.plan.CreateTicket(ctx, f.project.ID, title, "", domain.TicketStatusDone, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.plan.ArchiveTicket(ctx, tk.ID); err != nil {
			t.Fatal(err)
		}
		return tk
	}
	session := func(task *domain.Ticket, id string, status domain.SessionStatus) *domain.Workspace {
		ws, err := f.ws.Create(f.repo.ID, id, "agent/"+id, "main")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.sess.Create(&domain.Session{ID: id, ProjectID: f.project.ID, RepositoryID: f.repo.ID, TicketID: task.ID, Task: id,
			WorkingDir: ws.Path, WorkspaceID: ws.ID, Branch: ws.Branch, Status: status}); err != nil {
			t.Fatal(err)
		}
		return ws
	}
	export, docs, feed := archived("Ship the export"), archived("Write the docs"), archived("Follow the feed")
	dirty := session(export, "s-dirty", domain.SessionDone)
	if err := os.WriteFile(filepath.Join(dirty.Path, "scratch.txt"), []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean := session(export, "s-clean", domain.SessionDone)
	running := session(docs, "s-running", domain.SessionRunning)
	single := session(feed, "s-single", domain.SessionDone)

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: f.cat.Projects, Agents: f.cat.Agents, Sessions: f.orch, Planning: f.plan, TaskArchive: f.plan, ArchivedWorktrees: f.cleared,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: f.cat.Projects, Tasks: f.plan, Sessions: f.orch, Agents: f.cat.Agents, Repositories: f.cat.Projects,
		EnvFiles: f.cat.Projects, Documents: f.plan, History: f.ws, ArchivedTasks: f.plan, ArchivedList: f.plan,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})

	const strip = `[data-confirm="clear-worktrees"]`
	stripText := `document.querySelector('` + strip + `').innerText`
	row := func(id string) string { return `[data-archived-list] [data-task-id="` + id + `"]` }
	click := func(sel string) chromedp.Action {
		return chromedp.Evaluate(`document.querySelector('`+sel+`').click()`, nil)
	}
	stripSays := func(want string) chromedp.Action {
		return waitFor(`!!document.querySelector('` + strip + `')?.checkVisibility() && ` + stripText + `.includes(` + jsString(want) + `)`)
	}

	p := newPerson(t)
	p.run(t, "the person opens Archived",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(srv.URL+"/projects/"+f.project.ID+"/archived"),
		waitFor(`document.querySelector('[data-ssr]') === null && !!document.querySelector('`+row(export.ID)+` [data-clear]')`),
		shot("archived-worktrees", "light"),
	)

	p.run(t, "the header button counts every archived task's worktrees before it asks",
		click(`[data-clear-all]`),
		stripSays("Clear 3 worktrees of all archived tasks?"),
		shot("archived-worktrees-confirm", "light"),
	)
	for _, want := range []string{"1 holds uncommitted changes that will be lost.", "1 running session will be skipped."} {
		if got := p.text(t, stripText); !contains(got, want) {
			t.Errorf("the confirm says %q, want %q", got, want)
		}
	}
	p.run(t, "the person closes it", click(strip+` [data-cancel]`), waitFor(`!document.querySelector('`+strip+`').checkVisibility()`))

	p.run(t, "a row's action asks about that task alone and clears it",
		click(row(feed.ID)+` [data-clear]`),
		stripSays("Clear 1 worktree of Follow the feed?"),
		stripSays("None hold uncommitted changes."),
		click(strip+` [data-confirm-clear]`),
		stripSays("Cleared 1 worktree"),
		waitFor(`document.querySelector('`+row(feed.ID)+` [data-clear]').disabled`),
	)
	if _, err := os.Stat(single.Path); !os.IsNotExist(err) {
		t.Errorf("the row's worktree is still on disk: %v", err)
	}
	p.run(t, "the person closes it", click(strip+` [data-cancel]`), waitFor(`!document.querySelector('`+strip+`').checkVisibility()`))

	p.run(t, "the header clears the rest, skipping the running session and saying so",
		click(`[data-clear-all]`),
		stripSays("Clear 2 worktrees of all archived tasks?"),
		click(strip+` [data-confirm-clear]`),
		stripSays("Cleared 2 worktrees"),
		stripSays("Write the docs: still running, so its worktree stays."),
		waitFor(`document.querySelector('`+row(export.ID)+` [data-clear]').disabled`),
	)
	for name, path := range map[string]string{"dirty": dirty.Path, "clean": clean.Path, "single": single.Path} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("the %s worktree is still on disk: %v", name, err)
		}
	}
	if _, err := os.Stat(running.Path); err != nil {
		t.Errorf("the running session's worktree went: %v", err)
	}
	for _, branch := range []string{"s-dirty", "s-clean", "s-running", "s-single"} {
		if gitE2E(t, f.checkout, "branch", "--list", "agent/"+branch) == "" {
			t.Errorf("branch agent/%s was deleted", branch)
		}
	}
	for id, cleared := range map[string]bool{"s-dirty": true, "s-clean": true, "s-single": true, "s-running": false} {
		s := f.sess.Get(id)
		if s == nil || (s.WorktreeClearedAt != nil) != cleared || s.Branch != "agent/"+id {
			t.Errorf("session %s after the clear: %+v", id, s)
		}
	}

	p.run(t, "a row with only a running session has nothing to remove and says so",
		click(`[data-clear-all]`),
		stripSays("Nothing to clear in all archived tasks"),
		stripSays("1 running session keeps its worktree until it ends."),
	)
	if got := p.text(t, `String(document.querySelector('`+strip+` [data-confirm-clear]').disabled)`); got != "true" {
		t.Errorf("the empty confirm can be confirmed (disabled = %s)", got)
	}
}
