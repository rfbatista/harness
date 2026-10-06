package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestDocumentsPageInTheBrowser opens a task's documents from its toolbar,
// reads the newest, and switches to another.
func TestDocumentsPageInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	w := documentsBoard()
	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler()) // the watch's polls and terminals are not under test
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, Documents: w.docs,
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var path, heading, count string
	var shot []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(`!!document.querySelector('a[href$="/documents"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('a[href$="/documents"] .badge').textContent`, &count),
		chromedp.Evaluate(`document.querySelector('a[href$="/documents"]').click()`, nil),
		waitForPath(srv.URL+"/projects/p1/tasks/t-feed/documents", &path),
		chromedp.Poll(`!!document.querySelector('article .prose code')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('nav[aria-label="Documents of this task"] a')].find(a => a.textContent.includes('Plan')).click()`, nil),
		waitForPath(srv.URL+"/projects/p1/tasks/t-feed/documents/d-plan", &path),
		chromedp.Poll(`!!document.querySelector('article .prose table')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('article .prose h1').textContent`, &heading),
		chromedp.FullScreenshot(&shot, 80),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if count != "3" || heading != "The plan" {
		t.Errorf("count %q, heading %q", count, heading)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/documents.jpg", shot, 0o644)
	}
}

// TestDocumentsPageFramesHTMLInTheBrowser: an HTML document renders in a
// sandboxed frame whose script cannot touch the page, the frame has an
// accessible name, and Tab leaves it for the bar's link.
func TestDocumentsPageFramesHTMLInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	w := documentsBoard()
	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, Documents: w.docs,
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var sandbox, frameTitle, pageTitle, afterTab string
	var inlineHeading, tabLeftFrame bool
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-feed/documents/d-page"),
		chromedp.Poll(`!!document.querySelector('article iframe')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(500*time.Millisecond), // let the framed script run
		chromedp.Evaluate(`document.querySelector('article iframe').getAttribute('sandbox')`, &sandbox),
		chromedp.Evaluate(`document.querySelector('article iframe').getAttribute('title')`, &frameTitle),
		chromedp.Evaluate(`document.title`, &pageTitle),
		chromedp.Evaluate(`!!document.querySelector('article h1')`, &inlineHeading),
		chromedp.Evaluate(`document.querySelector('article iframe').focus(); true`, nil),
		chromedp.KeyEvent("\t"),
		chromedp.Evaluate(`document.activeElement.tagName + ':' + (document.activeElement.textContent || '').trim()`, &afterTab),
		chromedp.Evaluate(`document.activeElement.tagName !== 'IFRAME' && document.activeElement.closest('.bar') !== null`, &tabLeftFrame),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if sandbox != "allow-scripts" {
		t.Errorf("iframe sandbox = %q, want allow-scripts only", sandbox)
	}
	if frameTitle != "Plan page" {
		t.Errorf("iframe title = %q", frameTitle)
	}
	if pageTitle == "pwned" || !strings.HasPrefix(pageTitle, "Plan page · ") {
		t.Errorf("the framed script reached the page title: %q", pageTitle)
	}
	if inlineHeading {
		t.Error("the document's <h1> was inlined into the page")
	}
	if !tabLeftFrame {
		t.Errorf("Tab from the frame landed on %q, want the bar's link", afterTab)
	}
}
