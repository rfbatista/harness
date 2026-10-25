//go:build !windows

package web_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestAgentsPageShowsTheCatalogInTheBrowser_EndToEnd is W1 against a real
// server and a temporary catalog (spec "Web UI — the Agents page"): the page
// shows the catalog folder the read names and what it lists, from the first
// paint and from the browser's own read.
//
// It needs the server's catalog endpoints. On a branch without them
// /api/get_agent_catalog answers 404 and the test skips. Once the branch is
// rebased onto the server's, pass the catalog service to httpapi.Services in
// newTokenSaverWorld (and web.Deps.AgentCatalog for the seed) and it runs.
func TestAgentsPageShowsTheCatalogInTheBrowser_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	res, err := w.srv.Client().Get(w.srv.URL + "/api/get_agent_catalog")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		t.Skip("the server's catalog endpoints are not on this branch yet: /api/get_agent_catalog answers 404")
	}

	catalogDir := filepath.Join(w.root, "agents")
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(catalogDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("agents.json", `{"agents":{"ux-designer":{"description":"Designs screens.","skills":["tdd"]},"backend":{"description":"APIs.","skills":["tdd"]}},"bundles":{"ux":["tdd"]}}`)
	write("skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nWrite the test first.\n")

	b := newTLSBrowser(t)
	b.run("the page shows the catalog root and what each tab lists",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/skills/tdd"),
		waitUntil(`document.querySelector('main[x-data="agentsPage"] code')?.textContent === `+quote(catalogDir)),
		waitUntil(`document.querySelector('[data-count=agents]').textContent.trim() === '2'`),
		waitUntil(`document.querySelector('[data-count=skills]').textContent.trim() === '1'`),
		waitUntil(`document.querySelector('[data-count=bundles]').textContent.trim() === '1'`),
		waitUntil(`document.querySelector('[role=tab][aria-selected=true]').dataset.tab === 'skills'`),
		waitUntil(`document.querySelector('[data-new]').textContent.trim() === 'New skill'`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
