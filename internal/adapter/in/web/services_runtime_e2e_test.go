//go:build !windows

package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/in/web/serversettings"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
)

// fakeServicesSettings stands in for the services settings endpoints
// (contract: Harness server ↔ Web UI — services, section 9): podman is not
// found, docker is.
type fakeServicesSettings struct {
	mu      sync.Mutex
	runtime string
	saved   []string
}

func (f *fakeServicesSettings) answer(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"container_runtime": f.runtime, "available": f.runtime == "docker"})
}

func (f *fakeServicesSettings) get(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answer(w)
}

func (f *fakeServicesSettings) set(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Runtime string `json:"container_runtime"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.Runtime != "docker" && in.Runtime != "podman" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "container_runtime must be docker or podman", "code": "INVALID_INPUT"})
		return
	}
	f.runtime = in.Runtime
	f.saved = append(f.saved, in.Runtime)
	f.answer(w)
}

// The container runtime on the Server page, end to end: the person finds
// docker found, picks podman, is told it is not found, and sees the choice
// kept after a reload.
func TestContainerRuntimeIsChosenOnTheServerPage_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	fake := &fakeServicesSettings{runtime: "docker"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/get_services_settings", fake.get)
	mux.HandleFunc("POST /api/set_services_settings", fake.set)
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects:  cat.Projects,
		Settings:  cat.Settings,
		DataPaths: &serversettings.Paths{DataDir: t.TempDir(), Source: "a test", Database: ":memory:"},
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const sel = `document.querySelector('select[data-container-runtime]')`
	const status = `document.querySelector('[data-runtime-status]')`
	b := newBrowser(t)
	b.run("docker is chosen and found",
		chromedp.Navigate(srv.URL+"/server"),
		waitUntil(sel+` !== null && `+sel+`.value === 'docker' && `+sel+`.disabled === false`),
		waitUntil(status+`.textContent === 'docker was found on this machine.'`),
	)
	b.run("choosing podman saves it and says it is not found",
		chromedp.Evaluate(sel+`.value = 'podman'; `+sel+`.dispatchEvent(new Event('change', { bubbles: true }))`, nil),
		waitUntil(status+`.textContent.startsWith('podman was not found on this machine.')`),
	)
	fake.mu.Lock()
	saved := append([]string(nil), fake.saved...)
	fake.mu.Unlock()
	if len(saved) != 1 || saved[0] != "podman" {
		t.Errorf("saved = %v, want [podman]", saved)
	}
	b.run("it is still podman after a reload",
		chromedp.Reload(),
		waitUntil(sel+`.value === 'podman' && `+status+`.textContent.startsWith('podman was not found')`),
	)
}
