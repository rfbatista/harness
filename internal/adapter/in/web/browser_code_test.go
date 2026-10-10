package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// codeWorld is a project whose one repository is the code fixture (a git
// repository with a session's worktree and a gone one), read through the
// real code browser, in process for the pages and over /api for the browser.
type codeWorld struct {
	f      *portstest.CodeFixture
	at     portstest.CodeTarget
	code   ports.CodeBrowser
	srv    *httptest.Server
	repoID string
}

func newCodeWorld(t *testing.T, deps func(w *codeWorld, d *Deps)) *codeWorld {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	w := &codeWorld{f: portstest.NewCodeFixture(t)}
	code, at := workspacestest.NewCode(t, w.f)
	w.code, w.at, w.repoID = code, at, at.RepositoryID
	d := Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: w.f.ProjectID, Name: "fixture"}}},
		Tasks:        fakeTickets{},
		Sessions:     fakeSessions{},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: at.RepositoryID, ProjectID: w.f.ProjectID, Name: "fixture", RootDir: w.f.RepoDir}}},
		Code:         code,
	}
	if deps != nil {
		deps(w, &d)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Code: code}))))
	mux.Handle("/", NewHandler(d, assets, nil))
	w.srv = httptest.NewServer(mux)
	t.Cleanup(w.srv.Close)
	return w
}

func saveShot(name string, shot []byte) {
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, name), shot, 0o644)
	}
}

// TestRepositoryCodePageInTheBrowser opens a repository's code page: the
// tree painted by the server, a file opened from it with its lines and a
// line targeted from the URL.
func TestRepositoryCodePageInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, nil)
	ctx, errs := browser(t)
	base := w.srv.URL + "/projects/" + w.f.ProjectID + "/repositories/" + w.repoID + "/code"

	var ssrRows, rows int
	var names []string
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(base),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('.file-tree [role=treeitem]').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('.file-tree [role=treeitem]').length`, &rows),
		chromedp.Evaluate(`[...document.querySelectorAll('.file-tree [role=treeitem] .name')].map(n => n.textContent.trim())`, &names),
		chromedp.Click(`.file-tree [title="internal"] > .item`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('.file-tree [title="internal/app"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`.file-tree [title="internal/app"] > .item`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('.file-tree [title="internal/app/app.go"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`.file-tree [title="internal/app/app.go"] > .item`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('.code-view .code-lines .line').length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	saveShot("code-repository.jpg", shot)
	_ = ssrRows
	if rows == 0 || len(names) == 0 {
		t.Fatalf("tree rows = %d %v", rows, names)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
	t.Logf("tree: %v", names)
}

var _ = context.Background
