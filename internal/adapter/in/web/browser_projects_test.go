package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// waitForPath waits until the tab is at url, riding out the navigation that
// gets it there (a poll inside the page would die with the old document).
func waitForPath(url string, got *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := chromedp.Location(got).Do(ctx); err == nil && *got == url {
				return nil
			}
			time.Sleep(50 * time.Millisecond)
		}
		return fmt.Errorf("still at %q, want %q", *got, url)
	})
}

// fakeProjectsAPI answers the two writes the forms make, the way httpapi does.
type fakeProjectsAPI struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeProjectsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/events" {
		http.NotFound(w, r) // the rail's feed is not under test
		return
	}
	if r.URL.Path == "/api/find_repositories" {
		root := strings.TrimRight(r.URL.Query().Get("root_dir"), "/")
		f.mu.Lock()
		f.calls = append(f.calls, "find "+root)
		f.mu.Unlock()
		found := map[string][]map[string]string{
			"/src/app":  {{"path": "/src/app/api", "name": "api", "remote": "git@github.com:me/api.git"}, {"path": "/src/app/web", "name": "web"}},
			"/src/pool": {{"path": "/src/pool/harness", "name": "harness"}, {"path": "/src/pool/kit", "name": "kit"}},
		}[root]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"root": root, "repositories": found})
		return
	}
	var in map[string]string
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	b, _ := json.Marshal(in)
	f.calls = append(f.calls, r.URL.Path+" "+string(b))
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/create_project":
		_ = json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"id": "p-new", "name": in["name"], "root_dir": in["root_dir"]}})
	case "/api/create_repository":
		_ = json.NewEncoder(w).Encode(map[string]any{"repository": map[string]string{
			"id": "r-new", "project_id": in["project_id"], "name": in["name"], "url": in["url"], "root_dir": in["root_dir"],
		}})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeProjectsAPI) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

func TestProjectFormsInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	api := &fakeProjectsAPI{}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool", RootDir: "/src/pool"}, {ID: "p-new", Name: "app"}}},
		Tasks:        fakeTickets{},
		Sessions:     fakeSessions{},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness", URL: "file:///src/pool/harness", RootDir: "/src/pool/harness"}}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	typeInto := func(sel, text string) chromedp.Action {
		return chromedp.SendKeys(sel, text, chromedp.ByQuery)
	}
	var location, label string
	var found []string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/new"),
		chromedp.WaitVisible(`#project-root`, chromedp.ByQuery),
		typeInto(`#project-root`, "/src/app"),
		chromedp.Poll(`document.querySelector('#project-name').placeholder === 'app'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		clickButton(`main form`, "Find repositories"),
		chromedp.Poll(`document.querySelectorAll('[aria-label="Repositories to add"] .row').length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label="Repositories to add"] .row')].map(r => r.textContent.replace(/\s+/g, ' ').trim())`, &found),
		chromedp.Evaluate(`document.querySelector('main form button[type=submit]').textContent`, &label),
		clickButton(`main form`, "Create project"),
		waitForPath(srv.URL+"/projects/p-new", &location),
	)
	if err != nil {
		t.Fatalf("new project: %v\nAPI calls:\n%s\nJS errors: %v", err, api.log(), errs.all())
	}
	if strings.Join(found, " | ") != "api git@github.com:me/api.git | web local only" || label != "Create project with 2 repositories" {
		t.Errorf("found %q, button %q", found, label)
	}
	wantCalls := []string{
		`find /src/app`,
		`/api/create_project {"name":"app","root_dir":"/src/app"}`,
		`/api/create_repository {"description":"","name":"api","project_id":"p-new","root_dir":"/src/app/api","url":"git@github.com:me/api.git"}`,
		`/api/create_repository {"description":"","name":"web","project_id":"p-new","root_dir":"/src/app/web","url":"file:///src/app/web"}`,
	}
	if api.log() != strings.Join(wantCalls, "\n") {
		t.Errorf("API calls:\n%s\nwant:\n%s", api.log(), strings.Join(wantCalls, "\n"))
	}

	var names []string
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/projects/p1/repositories"),
		chromedp.Poll(`document.querySelector('[data-ssr]') === null`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		// kit was found in the project's directory and is not added yet.
		chromedp.Poll(`document.querySelectorAll('[aria-label="Found in the project\'s directory"] .row').length === 1`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		clickButton(`[aria-label="Found in the project's directory"]`, "Add"),
		chromedp.Poll(`document.querySelectorAll('[aria-label="Repositories"] .row').length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label="Repositories"] .row .weight-medium')].map(e => e.textContent)`, &names),
	)
	if err != nil {
		t.Fatalf("add repository: %v\nJS errors: %v", err, errs.all())
	}
	if strings.Join(names, ",") != "harness,kit" {
		t.Errorf("repositories listed = %v", names)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
