package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
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
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, Documents: docReader{w.docs, w.tickets},
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var path, heading, count string
	var shot []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(`!!document.querySelector('a[href$="/tasks/t-feed/documents"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('a[href$="/tasks/t-feed/documents"] .badge').textContent`, &count),
		chromedp.Evaluate(`document.querySelector('a[href$="/tasks/t-feed/documents"]').click()`, nil),
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
// sandboxed frame whose script runs (it reports in by postMessage) but cannot
// touch the page, the frame has an accessible name, and Tab leaves it for
// the bar's link.
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
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, Documents: docReader{w.docs, w.tickets},
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var sandbox, frameTitle, pageTitle, afterTab string
	var inlineHeading, tabLeftFrame bool
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		// Installed before any document loads, so the page hears the framed
		// script report in; the frame's own copy of it is harmless.
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`window.__framed = []; addEventListener('message', e => window.__framed.push(e.data))`).Do(ctx)
			return err
		}),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-feed/documents/d-page"),
		chromedp.Poll(`(window.__framed || []).includes('ran')`, nil, chromedp.WithPollingTimeout(10*time.Second)), // the framed script ran
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

// scopeServer serves the documents pages over a world and answers
// POST /api/set_document_scope by flipping the document's scope in the world
// and echoing it, recording every body. The rest of /api is not under test.
func scopeServer(t *testing.T, w world, assets *Assets) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/set_document_scope", func(rw http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var in struct {
			DocumentID string `json:"document_id"`
			Scope      string `json:"scope"`
		}
		_ = json.Unmarshal(b, &in)
		mu.Lock()
		bodies = append(bodies, string(b))
		for _, docs := range w.docs {
			for _, d := range docs {
				if d.ID == in.DocumentID {
					d.Scope = domain.DocumentScope(in.Scope)
				}
			}
		}
		mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(rw, `{"document":{"id":"`+in.DocumentID+`","project_id":"p1","title":"x","format":"markdown","scope":"`+in.Scope+`","updated_at":"2026-10-02T14:00:01Z"}}`)
	})
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, Documents: docReader{w.docs, w.tickets},
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), bodies...) }
}

// TestDocumentScopeMovesInPlaceInTheBrowser: on the task's documents page the
// move button sends one set_document_scope for the open document and the
// page follows without reloading: the word, the list's mark and the label
// change, and the document stays listed on the task.
func TestDocumentScopeMovesInPlaceInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	srv, bodies := scopeServer(t, documentsBoard(), assets)

	var word, label, aria, mark string
	var stayed, stillListed bool
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-feed/documents/d-plan"),
		chromedp.Poll(`document.querySelector('[x-data="tasksDocumentScope"] button[type=submit]')?.textContent === 'Move to project'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__stayed = true; true`, nil),
		chromedp.Click(`[x-data="tasksDocumentScope"] button[type=submit]`),
		chromedp.Poll(`document.querySelector('[x-data="tasksDocumentScope"] button[type=submit]')?.textContent === 'Move back to task'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-scope-word]').textContent`, &word),
		chromedp.Evaluate(`document.querySelector('[x-data="tasksDocumentScope"] button[type=submit]').textContent`, &label),
		chromedp.Evaluate(`document.querySelector('[x-data="tasksDocumentScope"] button[type=submit]').getAttribute('aria-label')`, &aria),
		chromedp.Evaluate(`(m => m && getComputedStyle(m).display !== 'none' ? m.textContent : '')(document.querySelector('a[aria-current="page"] [data-scope-mark]'))`, &mark),
		chromedp.Evaluate(`window.__stayed === true`, &stayed),
		chromedp.Evaluate(`!!document.querySelector('nav[aria-label="Documents of this task"] a[href$="/documents/d-plan"]')`, &stillListed),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if got := bodies(); len(got) != 1 || !strings.Contains(got[0], `"document_id":"d-plan"`) || !strings.Contains(got[0], `"scope":"project"`) {
		t.Errorf("set_document_scope calls: %q", got)
	}
	if word != "Project document" || label != "Move back to task" || aria != "Move Plan back to task" || mark != "project" {
		t.Errorf("after the move: word %q, label %q, aria %q, mark %q", word, label, aria, mark)
	}
	if !stayed || !stillListed {
		t.Errorf("the page reloaded (%v) or dropped the document (%v)", !stayed, !stillListed)
	}
}

// TestProjectDocumentsLibraryInTheBrowser: from a task page, the shell's
// Documents link opens the library; it lists the project documents, renders
// the open one, names its task with a link back, and moves it back in place.
func TestProjectDocumentsLibraryInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	srv, bodies := scopeServer(t, libraryBoard(), assets)

	var path, heading, fromTask, label string
	var shot []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(`!!document.querySelector('nav[aria-label="Tasks"] a[href="/projects/p1/documents"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] a[href="/projects/p1/documents"]').click()`, nil),
		waitForPath(srv.URL+"/projects/p1/documents", &path),
		chromedp.Poll(`!!document.querySelector('article .prose h1')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('article .prose h1').textContent`, &heading),
		chromedp.Evaluate(`[...document.querySelectorAll('nav[aria-label="Documents of this project"] a')].find(a => a.textContent.includes('Plan page')).click()`, nil),
		waitForPath(srv.URL+"/projects/p1/documents/d-page", &path),
		chromedp.Poll(`!!document.querySelector('article iframe')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('article a[href="/projects/p1/tasks/t-feed/documents/d-page"]').textContent`, &fromTask),
		chromedp.Click(`[x-data="tasksDocumentScope"] button[type=submit]`),
		chromedp.Poll(`document.querySelector('[x-data="tasksDocumentScope"] button[type=submit]')?.textContent === 'Move to project'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[x-data="tasksDocumentScope"] button[type=submit]').textContent`, &label),
		chromedp.FullScreenshot(&shot, 80),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if heading != "Boundaries" || fromTask != "Add SSE feed" || label != "Move to project" {
		t.Errorf("heading %q, from task %q, label %q", heading, fromTask, label)
	}
	if got := bodies(); len(got) != 1 || !strings.Contains(got[0], `"document_id":"d-page"`) || !strings.Contains(got[0], `"scope":"task"`) {
		t.Errorf("set_document_scope calls: %q", got)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/project-documents.jpg", shot, 0o644)
	}
}
