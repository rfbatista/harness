//go:build !windows

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
)

// poWeb is the product owner's server (poE2E: the real services and a stand-in claude that
// answers "answer to: <text>" in two deltas) with the web UI in front of it, on one origin as
// the harness serves it.
type poWeb struct {
	*poE2E
	url string
}

func newPOWeb(t *testing.T) *poWeb {
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
	e := newPOE2E(t)
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Sessions: e.orch, Planning: e.plan, Projects: e.cat.Projects, Settings: e.cat.Settings, ProductOwner: e.po, AgentBoard: e.board, Usage: e.usage,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: e.cat.Projects, Tasks: e.plan, Sessions: e.orch, Agents: e.cat.Agents, Repositories: e.cat.Projects,
		EnvFiles: e.cat.Projects, Documents: e.plan, AgentBoard: e.board,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the page's event stream never ends on its own
		srv.Close()
	})
	return &poWeb{poE2E: e, url: srv.URL}
}

const (
	poLive    = `document.querySelector('[x-data=streamStatus]')?.dataset.state === 'live'`
	poPanel   = `document.querySelector('.owner-panel')`
	poToggle  = `document.querySelector('.po-toggle')`
	poMessage = `[...document.querySelectorAll('.owner-panel .msg, .owner-page .msg')]`
)

// The panel in the browser, against the real server: the toggle opens it, the composer has
// focus, a message gets its streamed answer, the conversation stays across a click to another
// page of the project, and Esc closes it and gives the focus back to the toggle.
func TestTheProductOwnerPanelAnswersAndStaysAcrossPagesInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	p := newPerson(t)
	board := w.url + "/projects/" + w.project

	p.run(t, "the person opens the board, and the toggle is there, closed",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(board),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		waitFor(`!!`+poToggle+` && `+poToggle+`.getAttribute('aria-pressed') === 'false'`),
	)
	if p.truth(t, `!!`+poPanel) {
		t.Fatal("the panel is open before it was asked for")
	}

	p.run(t, "the toggle opens the panel and the composer takes focus",
		chromedp.Click(`.po-toggle`, chromedp.ByQuery),
		waitFor(`!!`+poPanel+` && `+poToggle+`.getAttribute('aria-pressed') === 'true'`),
		waitFor(`document.activeElement?.id === 'po-composer'`),
	)
	if !p.truth(t, `document.querySelector('#app-shell').hasAttribute('data-owner')`) {
		t.Error("the app shell has no column for the panel")
	}

	p.run(t, "a message is sent and its answer streams in",
		chromedp.SendKeys(`#po-composer`, "What was done this week?", chromedp.ByQuery),
		pressEnter(),
		waitFor(poMessage+`.some(m => m.dataset.from === 'person' && m.textContent.includes('What was done this week?'))`),
		waitFor(poMessage+`.some(m => m.dataset.from === 'product_owner' && !m.hasAttribute('data-streaming') && m.textContent.includes('answer to: What was done this week?'))`),
		waitFor(`!document.querySelector('.owner-panel .owner-live') && document.querySelector('.composer .send')?.getAttribute('aria-label') === 'Send'`),
	)
	if got := p.text(t, `document.querySelector('.owner-panel [role=status]').textContent`); got != "Answer finished" {
		t.Errorf("the status line says %q, want it to say the answer finished", got)
	}

	p.run(t, "a click to another page of the project keeps the panel and its conversation",
		chromedp.Click(`#rail a[href$="/documents"]`, chromedp.ByQuery),
		waitFor(`location.pathname.endsWith('/documents')`),
		waitFor(`!!`+poPanel+` && `+poMessage+`.length === 2`),
	)

	p.run(t, "Escape inside the panel closes it and the focus returns to the toggle",
		chromedp.Focus(`#po-composer`, chromedp.ByQuery),
		chromedp.KeyEvent("\u001b"),
		waitFor(`!`+poPanel),
		waitFor(`document.activeElement === `+poToggle),
	)
	if p.truth(t, `document.querySelector('#app-shell').hasAttribute('data-owner')`) {
		t.Error("the app shell keeps the panel's column after it closed")
	}

	p.run(t, "the toggle opens it again on the same thread, from what the browser remembered",
		chromedp.Click(`.po-toggle`, chromedp.ByQuery),
		waitFor(`!!`+poPanel+` && `+poMessage+`.length === 2`),
	)

	// A second tab of the same browser follows the same thread live.
	tab, cancel := chromedp.NewContext(p.ctx)
	defer cancel()
	if err := chromedp.Run(tab,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(board),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		waitFor(`!!`+poPanel+` && `+poMessage+`.length === 2`),
	); err != nil {
		t.Fatalf("the second tab: %v", err)
	}
	p.run(t, "a message sent in the first tab",
		chromedp.SendKeys(`#po-composer`, "And what is next?", chromedp.ByQuery),
		pressEnter(),
		waitFor(poMessage+`.some(m => m.textContent.includes('answer to: And what is next?') && !m.hasAttribute('data-streaming'))`),
	)
	if err := chromedp.Run(tab,
		waitFor(poMessage+`.some(m => m.dataset.from === 'person' && m.hasAttribute('data-fresh') && m.textContent.includes('from another tab'))`),
		waitFor(poMessage+`.some(m => m.textContent.includes('answer to: And what is next?') && !m.hasAttribute('data-streaming'))`),
	); err != nil {
		t.Fatalf("the second tab does not show the same thread live: %v", err)
	}
}

// The full page in the browser: the rail row opens it, the thread list shows the threads, a
// click opens one at its own URL, and the thread moves to the docked panel beside the board.
func TestTheProductOwnerPageListsOpensAndDocksAThreadInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	older := w.seedThread("Roadmap ideas", time.Now().Add(-48*time.Hour), 2)
	recent := w.seedThread("What blocks the release", time.Now().Add(-time.Hour), 2)
	p := newPerson(t)
	board := w.url + "/projects/" + w.project

	p.run(t, "the rail's Product owner row opens the page in place",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(board),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		chromedp.Click(`#rail a[href$="/product-owner"]`, chromedp.ByQuery),
		waitFor(`location.pathname === '/projects/`+w.project+`/product-owner' && !!document.querySelector('.owner-page .owner-split')`),
		waitFor(`document.querySelector('#rail a[href$="/product-owner"]').getAttribute('aria-current') === 'page'`),
	)
	if got := p.text(t, `[...document.querySelectorAll('.owner-split > .threads .thread-row .t')].map(e => e.textContent.trim()).join('|')`); got != "What blocks the release|Roadmap ideas" {
		t.Errorf("the thread list = %q, want the most recently active first", got)
	}
	if p.truth(t, `!!document.querySelector('#rail a[href*="`+older+`"], #rail a[href*="`+recent+`"]')`) {
		t.Error("a thread shows in the rail")
	}

	p.run(t, "a click on a thread opens it at its own URL",
		chromedp.Click(`.owner-split > .threads .thread-row`, chromedp.ByQuery),
		waitFor(`decodeURIComponent(location.pathname) === '/projects/`+w.project+`/product-owner/threads/`+recent+`'`),
		waitFor(poMessage+`.length === 2 && `+poMessage+`.some(m => m.textContent.includes('message 1'))`),
		waitFor(`document.querySelector('.owner-page .owner-head h2')?.textContent === 'What blocks the release'`),
	)
	if p.truth(t, `!!document.querySelector('.owner-panel')`) {
		t.Error("the panel shows beside the page")
	}

	p.run(t, "the page's address opens the same thread on a fresh load",
		chromedp.Navigate(w.url+"/projects/"+w.project+"/product-owner/threads/"+older),
		waitFor(`!!document.querySelector('.owner-page .owner-split')`),
		waitFor(`document.querySelector('.owner-page .owner-head h2')?.textContent === 'Roadmap ideas'`),
		waitFor(poMessage+`.length === 2`),
	)

	p.run(t, "Dock beside the page moves the thread to the panel next to the board",
		chromedp.Click(`[aria-label="Dock beside the page"]`, chromedp.ByQuery),
		waitFor(`location.pathname === '/projects/`+w.project+`'`),
		waitFor(`!!`+poPanel+` && `+poMessage+`.length === 2 && document.querySelector('.owner-panel .owner-head')?.textContent.includes('Roadmap ideas')`),
	)

	p.run(t, "Open as a page moves it back",
		chromedp.Click(`[aria-label="Open as a page"]`, chromedp.ByQuery),
		waitFor(`decodeURIComponent(location.pathname) === '/projects/`+w.project+`/product-owner/threads/`+older+`'`),
		waitFor(`!document.querySelector('.owner-panel') && `+poMessage+`.length === 2`),
	)
}

// The Settings section in the browser: the switch turns the product owner off and on, the server
// holds it, and the toggle and the open panel follow.
func TestTheProductOwnerSwitchOnTheSettingsPageDrivesTheToggleAndPanelInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	p := newPerson(t)
	sw := `document.querySelector('#product-owner [role=switch]')`

	p.run(t, "the person opens the panel on the board",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		chromedp.Click(`.po-toggle`, chromedp.ByQuery),
		waitFor(`!!`+poPanel),
	)
	p.run(t, "Settings (a page of the same frame) has the section, on",
		chromedp.Click(`main a[href$="/settings"]`, chromedp.ByQuery),
		waitFor(`location.pathname.endsWith('/settings') && !!`+sw+` && `+sw+`.getAttribute('aria-checked') === 'true'`),
	)
	if got := p.text(t, sw+`.textContent.trim()`); got != "Product owner is on" {
		t.Errorf("the switch says %q", got)
	}

	p.run(t, "the switch turns it off: the server holds it, the toggle is struck, the panel says so",
		chromedp.Click(`#product-owner [role=switch]`, chromedp.ByQuery),
		waitFor(sw+`.getAttribute('aria-checked') === 'false' && `+sw+`.textContent.trim() === 'Product owner is off'`),
		waitFor(poToggle+`.getAttribute('aria-label') === 'Product owner (off)' && `+poToggle+`.hasAttribute('data-off')`),
		waitFor(`!!document.querySelector('.owner-panel .banner') && document.querySelector('#po-composer').disabled`),
	)
	if w.settings().Enabled {
		t.Error("the server still has the product owner on")
	}

	p.run(t, "Turn it on, from the panel, flips the section's switch too",
		chromedp.Click(`.owner-panel .banner .button[data-variant=primary]`, chromedp.ByQuery),
		waitFor(sw+`.getAttribute('aria-checked') === 'true'`),
		waitFor(`!`+poToggle+`.hasAttribute('data-off') && !document.querySelector('#po-composer').disabled`),
	)
	if !w.settings().Enabled {
		t.Error("the server has the product owner off after Turn it on")
	}
}
