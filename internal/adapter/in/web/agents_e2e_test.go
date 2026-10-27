//go:build !windows

package web_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestAgentsPageShowsTheCatalogInTheBrowser_EndToEnd is W1 against a real
// server and a temporary catalog (spec "Web UI — the Agents page"): the page
// shows the catalog folder the read names and what it lists, from the first
// paint and from the browser's own read.
func TestAgentsPageShowsTheCatalogInTheBrowser_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
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
		// A long absolute root is clipped to its last two folders in a chip, the full
		// path is its title, and the header never covers the tab bar.
		waitUntil(`document.querySelector('main[x-data="agentsPage"] code.catalog-root')?.textContent === `+quote("…/"+filepath.Base(w.root)+"/agents/")),
		waitUntil(`document.querySelector('code.catalog-root').title === `+quote(catalogDir)),
		waitUntil(`(() => { const tabs = document.querySelector('[role=tablist]'); const head = document.querySelector('main[x-data="agentsPage"] > header'); if (!tabs || !tabs.checkVisibility()) return false; const t = tabs.getBoundingClientRect(); const h = head.getBoundingClientRect(); const hit = document.elementFromPoint(t.left + 8, t.top + t.height / 2); return head.scrollHeight <= head.clientHeight + 1 && tabs.scrollHeight <= tabs.clientHeight + 1 && h.bottom <= t.top + 1 && tabs.contains(hit) && document.documentElement.scrollWidth <= innerWidth; })()`),
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

// A catalog with many agents and a long absolute root keeps the header and the
// tab bar whole: the list's height never squeezes them over each other.
func TestAgentsHeaderAndTabsStayWholeWithManyAgentsAndALongRoot_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	catalogDir := filepath.Join(w.root, "agents")
	agents := ""
	for i := 0; i < 60; i++ {
		if i > 0 {
			agents += ","
		}
		agents += fmt.Sprintf(`"agent-%02d":{"description":"Agent number %d.","skills":[]}`, i, i)
	}
	if err := os.MkdirAll(catalogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalogDir, "agents.json"), []byte(`{"agents":{`+agents+`}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	b := newTLSBrowser(t)
	b.run("the header holds its text and the tab bar is visible and not covered",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.srv.URL+"/agents"),
		waitUntil(`document.querySelector('[data-count=agents]')?.textContent.trim() === '60'`),
		waitUntil(`(() => { const tabs = document.querySelector('[role=tablist]'); const head = document.querySelector('main[x-data="agentsPage"] > header'); if (!tabs || !tabs.checkVisibility()) return false; const t = tabs.getBoundingClientRect(); const h = head.getBoundingClientRect(); const hit = document.elementFromPoint(t.left + 8, t.top + t.height / 2); return head.scrollHeight <= head.clientHeight + 1 && tabs.scrollHeight <= tabs.clientHeight + 1 && h.bottom <= t.top + 1 && tabs.contains(hit) && document.documentElement.scrollWidth <= innerWidth; })()`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors: %v", e)
	}
}
