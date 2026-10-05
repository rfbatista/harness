package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// fakeEnvAPI answers the env file writes the way httpapi does.
type fakeEnvAPI struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeEnvAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	b, _ := json.Marshal(in)
	f.calls = append(f.calls, r.URL.Path+" "+string(b))
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/import_repository_env_file":
		_ = json.NewEncoder(w).Encode(map[string]any{"env_file": map[string]string{"path": in["path"], "content": "FROM_DISK=1\n", "updated_at": time.Now().Format(time.RFC3339)}})
	case "/api/save_repository_env_file":
		_ = json.NewEncoder(w).Encode(map[string]any{"env_file": map[string]string{"path": in["path"], "content": in["content"], "updated_at": time.Now().Format(time.RFC3339)}})
	default:
		http.NotFound(w, r)
	}
}

func TestEnvFilesInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	api := &fakeEnvAPI{}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{},
		Sessions:     fakeSessions{},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "api", RootDir: "/src/api"}}},
		EnvFiles:     fakeEnv{},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var content string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/repositories/r1/env"),
		chromedp.Poll(`document.querySelector('#env-path')?.value === '.env'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		clickButton(`main form`, "Import from checkout"),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=".env"] textarea').length === 1`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('section[aria-label=".env"] textarea').value`, &content),
		setField(`section[aria-label=".env"] textarea`, "FROM_DISK=1\nEXTRA=2\n", "input"),
		clickButton(`section[aria-label=".env"]`, "Save"),
		chromedp.Poll(`document.querySelector('[role=status]')?.textContent?.includes('Saved .env') ?? false`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nAPI:\n%s\nJS errors: %v", err, strings.Join(api.calls, "\n"), errs.all())
	}
	if content != "FROM_DISK=1\n" {
		t.Errorf("imported content = %q", content)
	}
	want := []string{
		`/api/import_repository_env_file {"path":".env","repository_id":"r1"}`,
		`/api/save_repository_env_file {"content":"FROM_DISK=1\nEXTRA=2\n","path":".env","repository_id":"r1"}`,
	}
	if got := strings.Join(api.calls, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("API calls:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
