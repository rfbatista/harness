//go:build !windows

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/domain"
)

// The Agent board in the browser, against the server as it runs: a session
// starts on a task and its card arrives without a reload, its status changes
// in place, it asks the person for approval and the card takes the Needs you
// accent, and when the session ends the card leaves.
func TestTheAgentBoardFollowsTheSessionsInTheWebUI_EndToEnd(t *testing.T) {
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
	e := newBoardE2E(t)
	project := e.project("harness")
	busy := e.task(project, "Wire the board", "")
	e.start(project, busy, fakeSession{status: domain.SessionRunning})
	watched := e.task(project, "Add the feed", "")

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{AgentBoard: e.board})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: e.cat.Projects, Tasks: e.plan, Sessions: e.orch, Agents: e.cat.Agents, Repositories: e.cat.Projects,
		EnvFiles: e.cat.Projects, Documents: e.plan, AgentBoard: e.board,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the page's event stream never ends on its own
		srv.Close()
	})

	const live = `document.querySelector('[x-data=streamStatus]')?.dataset.state === 'live'`
	card := func(id string) string { return `document.querySelector('.agent-card[data-task="` + id + `"]')` }
	dot := func(id string) string {
		return card(id) + `?.querySelector('.session-line .status .visually-hidden')?.textContent`
	}

	p := newPerson(t)
	p.run(t, "the person opens the home page, which is the board",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(srv.URL+"/"),
		waitFor(live+` && document.querySelector('[data-ssr]') === null`),
		waitFor(`!!`+card(busy)),
		shot("agent-board-populated", "light"),
	)
	if p.truth(t, `!!`+card(watched)) {
		t.Fatal("a task with no live session is on the board")
	}

	s := e.start(project, watched, fakeSession{status: domain.SessionStarting})
	p.run(t, "a session starts and its card arrives",
		waitFor(`!!`+card(watched)),
		waitFor(dot(watched)+` === 'starting'`),
	)
	e.setStatus(s, domain.SessionThinking)
	p.run(t, "its status changes in place", waitFor(dot(watched)+` === 'thinking'`))
	if p.truth(t, card(watched)+`.hasAttribute('data-needs-you')`) && p.text(t, card(watched)+`.dataset.needsYou`) == "true" {
		t.Error("a thinking session needs the person")
	}

	e.setStatus(s, domain.SessionIdle)
	p.run(t, "it waits on the person and its card takes the Needs you accent",
		waitFor(card(watched)+`?.dataset.needsYou === 'true'`),
		shot("agent-board-needs-you", "light"),
	)

	e.setStatus(s, domain.SessionDone)
	p.run(t, "the session ends and its card leaves",
		waitFor(`!`+card(watched)),
	)
	if !p.truth(t, `!!`+card(busy)) {
		t.Error("the other card left the board")
	}
}
