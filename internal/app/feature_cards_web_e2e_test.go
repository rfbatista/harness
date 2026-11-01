//go:build !windows

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/domain"
)

// Feature cards, end to end, over the real planning service and the real
// HTTP API (no stubs): a feature and its children are one card; archiving a
// feature with an open child is refused on its card, with the server's words;
// "Archive all" takes what Done shows and says how many features stayed; a
// child of an archived feature is refused its restore; a feature restored
// while the board is open brings back the children archived on their own.
func TestFeatureCardsInTheWebUI_EndToEnd(t *testing.T) {
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
	task := func(title string, status domain.TicketStatus, parent string) *domain.Ticket {
		tk, err := f.plan.CreateTicket(ctx, f.project.ID, title, "", status, parent)
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	open := task("Files tab", domain.TicketStatusDone, "")
	openDone := task("Tree and viewer", domain.TicketStatusDone, open.ID)
	task("Narrow layout", domain.TicketStatusTodo, open.ID)
	alone := task("Notes pane", domain.TicketStatusDone, open.ID)
	whole := task("Code views", domain.TicketStatusDone, "")
	wholeKid := task("Diff view", domain.TicketStatusDone, whole.ID)
	wholeAlone := task("Code map", domain.TicketStatusDone, whole.ID)
	plain := task("Fix stale rail count", domain.TicketStatusDone, "")
	for _, id := range []string{alone.ID, wholeAlone.ID} {
		if _, err := f.plan.ArchiveTicket(ctx, id); err != nil { // archived on its own, while its feature is on the board
			t.Fatal(err)
		}
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
		srv.CloseClientConnections()
		srv.Close()
	})

	const live = `document.querySelector('[x-data=streamStatus]')?.dataset.state === 'live'`
	card := func(id string) string { return `document.querySelector('.board .card[data-task-id="` + id + `"]')` }
	board := srv.URL + "/projects/" + f.project.ID
	ready := waitFor(live + ` && document.querySelector('[data-ssr]') === null`)
	p := newPerson(t)

	p.run(t, "the person opens the board: the features are one card each, in Done",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(board),
		ready,
		waitFor(`!!`+card(open.ID)),
		both("feature-cards-board"),
	)
	ids := []string{open.ID, whole.ID, plain.ID}
	slices.Sort(ids)
	if got := p.text(t, `[...document.querySelectorAll('.board [data-column="done"] .card')].map(c => c.dataset.taskId).sort().join()`); got != strings.Join(ids, ",") {
		t.Errorf("Done shows %q: want the two features and the plain task, and no child", got)
	}
	if got := p.text(t, card(open.ID)+`.querySelector('.sum b').innerText`); got != "2 of 3 done" {
		t.Errorf("summary = %q: the child archived on its own counts as done on its feature's card", got)
	}

	p.run(t, "the person opens the card and archives the feature with an open child: the server refuses on the card",
		chromedp.Evaluate(card(open.ID)+`.querySelector('.disclosure').click()`, nil),
		waitFor(card(open.ID)+`.querySelector('.children').checkVisibility()`),
		chromedp.Evaluate(card(open.ID)+`.querySelector('.move-to').focus()`, nil),
		chromedp.KeyEvent(kb.ArrowDown),
		waitFor(`document.activeElement?.getAttribute('role') === 'menuitemradio'`),
		chromedp.KeyEvent("a"),
		waitFor(`!!`+card(open.ID)+`.querySelector('.refusal')`),
		both("feature-cards-refusal"),
	)
	if got := p.text(t, card(open.ID)+`.querySelector('.refusal').innerText.replace(/\s+/g, ' ')`); !strings.Contains(got, "Not archived. 1 of its tasks is still open: Narrow layout (Todo).") || !strings.Contains(got, "FEATURE_HAS_OPEN_CHILDREN") {
		t.Errorf("refusal = %q", got)
	}
	if got := p.text(t, `[...`+card(open.ID)+`.querySelectorAll('.child')].map(c => c.querySelector('.t').textContent + (c.hasAttribute('data-archived') ? ':archived' : '')).join()`); got != "Narrow layout,Notes pane:archived,Tree and viewer" && got != "Narrow layout,Tree and viewer,Notes pane:archived" { // equal moves: either order
		t.Errorf("children = %q: open first, then Done by latest move, the one archived on its own marked", got)
	}

	p.run(t, "the person archives all: the open feature stays, the whole one goes with its children",
		chromedp.Evaluate(`document.querySelector('[data-archive-all]').click()`, nil),
		waitFor(`!!document.querySelector('[data-confirm="archive-all"]')?.checkVisibility()`),
	)
	if got := p.text(t, `document.querySelector('[data-confirm="archive-all"]').innerText.replace(/\s+/g, ' ')`); !strings.Contains(got, "Archive all 2 done tasks?") || !strings.Contains(got, "1 feature with open children stays on the board.") {
		t.Errorf("confirmation = %q", got)
	}
	p.run(t, "the person confirms",
		chromedp.Evaluate(`document.querySelector('[data-confirm="archive-all"] [data-variant=primary]').click()`, nil),
		waitFor(`!`+card(whole.ID)+` && !`+card(plain.ID)),
	)
	if p.text(t, `String(!!`+card(open.ID)+`)`) != "true" {
		t.Error("the skipped feature left the board")
	}

	p.run(t, "in Archived, restoring a child of the archived feature is refused",
		chromedp.Navigate(srv.URL+"/projects/"+f.project.ID+"/archived"),
		waitFor(`!!document.querySelector('[data-archived-list] [data-task-id="`+wholeKid.ID+`"] [data-restore]')`),
		chromedp.Evaluate(`document.querySelector('[data-archived-list] [data-task-id="`+wholeKid.ID+`"] [data-restore]').click()`, nil),
		waitFor(`!!document.querySelector('[data-archived-list] [data-task-id="`+wholeKid.ID+`"] .row-error')`),
		both("feature-cards-archived-refusal"),
	)
	if got := p.text(t, `document.querySelector('[data-archived-list] [data-task-id="`+wholeKid.ID+`"] .row-error').innerText.replace(/\s+/g, ' ')`); !strings.Contains(got, "Restore the feature “Code views” first.") || !strings.Contains(got, "FEATURE_ARCHIVED") {
		t.Errorf("restore refusal = %q", got)
	}

	p.run(t, "the board is open again; the feature is restored from elsewhere",
		chromedp.Navigate(board),
		ready,
		waitFor(`!!`+card(open.ID)),
	)
	if _, err := f.plan.RestoreTicket(ctx, whole.ID); err != nil {
		t.Fatal(err)
	}
	p.run(t, "the restored feature is back with the child archived on its own, without a reload",
		waitFor(`!!`+card(whole.ID)+`?.querySelector('.child[data-archived]')`),
	)
	if got := p.text(t, `[...`+card(whole.ID)+`.querySelectorAll('.child')].map(c => c.querySelector('.t').textContent + (c.hasAttribute('data-archived') ? ':archived' : '')).join()`); got != "Code map:archived,Diff view" && got != "Diff view,Code map:archived" { // equal moves: either order
		t.Errorf("restored children = %q", got)
	}
	if p.text(t, `String(!!`+card(wholeAlone.ID)+` || !!`+card(openDone.ID)+`)`) != "false" {
		t.Error("a child has a card of its own")
	}

	p.run(t, "a child's page, wide and narrow: the feature's link in the title row, the rail nested",
		chromedp.Navigate(srv.URL+"/projects/"+f.project.ID+"/tasks/"+openDone.ID),
		waitFor(live+` && !!document.querySelector('.toolbar .crumbline')`),
		both("feature-cards-child-page"),
		chromedp.EmulateViewport(390, 844),
		both("feature-cards-child-page-narrow"),
		chromedp.Navigate(board),
		ready,
		// The card is still open, as the browser remembers. Show the Done column alone, so a screenshot has the narrow card in it.
		chromedp.Evaluate(`document.querySelectorAll('.board > .column:not([data-column="done"])').forEach(c => c.remove())`, nil),
		both("feature-cards-board-narrow"),
	)
}
