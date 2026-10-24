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
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/domain"
)

// The task board, end to end: the server as it runs, real git behind the
// merge badge. The person drags a Review card to Done, moves a Todo card to
// Done with its "Move to…" menu, clears Done with "Archive all" through its
// in-page confirmation, opens "Archived (N)" and restores one task, which is
// back in Done on the board.
func TestTheTaskBoardInTheWebUI_EndToEnd(t *testing.T) {
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

	// Check: in Review, its session's branch has a commit main does not have.
	check, err := f.plan.CreateTicket(ctx, f.project.ID, "Check the export", "", domain.TicketStatusReview, "")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := f.ws.Create(f.repo.ID, "s-check", "agent/s-check", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "export.txt"), []byte("export\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, ws.Path, "add", ".")
	gitE2E(t, ws.Path, "commit", "-q", "-m", "Add the export")
	if _, err := f.sess.Create(&domain.Session{ID: "s-check", ProjectID: f.project.ID, RepositoryID: f.repo.ID, TicketID: check.ID, Task: "s-check",
		WorkingDir: ws.Path, WorkspaceID: ws.ID, Branch: ws.Branch, Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}
	docs, err := f.plan.CreateTicket(ctx, f.project.ID, "Write the docs", "", domain.TicketStatusTodo, "")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: f.cat.Projects, Agents: f.cat.Agents, Sessions: f.orch, Planning: f.plan, Merges: f.merge, TaskArchive: f.plan,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: f.cat.Projects, Tasks: f.plan, Sessions: f.orch, Agents: f.cat.Agents, Repositories: f.cat.Projects,
		EnvFiles: f.cat.Projects, Documents: f.plan, History: f.ws, ArchivedTasks: f.plan, ArchivedList: f.plan,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the pages' event streams never end on their own
		srv.Close()
	})

	const live = `document.querySelector('[x-data=streamStatus]')?.dataset.state === 'live'`
	card := func(id string) string { return `document.querySelector('.board .card[data-task-id="` + id + `"]')` }
	inColumn := func(id, status string) string {
		return `!!document.querySelector('.board [data-column="` + status + `"] .card[data-task-id="` + id + `"]:not([data-pending])')`
	}
	// Native drag and drop, as the browser fires it: chromedp's mouse does not
	// start an HTML drag in headless Chrome, so the test dispatches the events.
	drag := func(id, to string) string {
		return `(() => {
			const card = ` + card(id) + `, target = document.querySelector('.board [data-column="` + to + `"] .cards');
			const dt = new DataTransfer();
			card.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
			card.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }));
			target.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt }));
			target.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt }));
			card.dispatchEvent(new DragEvent('dragend', { bubbles: true, dataTransfer: dt }));
		})()`
	}
	board := srv.URL + "/projects/" + f.project.ID

	p := newPerson(t)
	p.run(t, "the person opens the board",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(board),
		waitFor(live+` && document.querySelector('[data-ssr]') === null`),
		waitFor(`!!`+card(check.ID)+`?.querySelector('[data-merge-state="not_merged"]')`),
		shot("task-board", "light"),
	)
	if got := p.text(t, `document.querySelector('.archived-link').textContent.trim()`); got != "Archived (0)" {
		t.Errorf("archived link = %q", got)
	}

	p.run(t, "the person drags the Review card to Done",
		chromedp.Evaluate(drag(check.ID, "done"), nil),
		waitFor(inColumn(check.ID, "done")),
	)
	if got := p.text(t, card(check.ID)+`.querySelector('[data-last-move]').textContent`); !contains(got, "Moved here by", "you") {
		t.Errorf("last move after the drop = %q", got)
	}
	if got := p.text(t, `[...`+card(check.ID)+`.querySelectorAll('[data-merge-state]')].map(b => b.dataset.mergeState).join()`); got != "not_merged" {
		t.Errorf("merge badge on Done = %q, want not_merged (Done is never blocked)", got)
	}

	p.run(t, "the person moves the Todo card to Done with Move to…",
		chromedp.Evaluate(card(docs.ID)+`.querySelector('.move-to').focus()`, nil),
		chromedp.KeyEvent(kb.ArrowDown),
		waitFor(`document.activeElement?.getAttribute('role') === 'menuitemradio'`),
		chromedp.KeyEvent("5"),
		waitFor(inColumn(docs.ID, "done")),
	)

	p.run(t, "the person archives all of Done",
		chromedp.Evaluate(`document.querySelector('[data-archive-all]').click()`, nil),
		waitFor(`!!document.querySelector('[data-confirm="archive-all"]')?.checkVisibility()`),
	)
	if got := p.text(t, `document.querySelector('[data-confirm="archive-all"] [data-variant=primary]').textContent.trim()`); got != "Archive 2" {
		t.Errorf("confirm button = %q", got)
	}
	p.run(t, "the person confirms",
		chromedp.Evaluate(`document.querySelector('[data-confirm="archive-all"] [data-variant=primary]').click()`, nil),
		waitFor(`!document.querySelector('.board [data-column="done"] .card')`),
		waitFor(`document.querySelector('.archived-link').textContent.trim() === 'Archived (2)'`),
	)

	// The link, followed as a navigation: a click would race the next poll against the page unloading.
	archived := p.text(t, `document.querySelector('.archived-link').href`)
	p.run(t, "the person opens Archived and restores the checked task",
		chromedp.Navigate(archived),
		waitFor(`location.pathname.endsWith('/archived') && !!document.querySelector('[data-archived-list] [data-task-id="`+check.ID+`"] [data-restore]')`),
		chromedp.Evaluate(`document.querySelector('[data-archived-list] [data-task-id="`+check.ID+`"] [data-restore]').click()`, nil),
		waitFor(`(document.querySelector('[data-archived-list] [data-task-id="`+check.ID+`"]')?.innerText ?? '').includes('restored to Done')`),
	)
	p.run(t, "back on the board, the restored task is in Done",
		chromedp.Navigate(board),
		waitFor(live+` && document.querySelector('[data-ssr]') === null`),
		waitFor(inColumn(check.ID, "done")),
	)
	if got := p.text(t, `document.querySelector('.archived-link').textContent.trim()`); got != "Archived (1)" {
		t.Errorf("archived link after the restore = %q", got)
	}
	if p.text(t, `String(!!`+card(docs.ID)+`)`) != "false" {
		t.Error("the task still archived is back on the board")
	}
}
