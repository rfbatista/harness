package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
)

func newRepositoryHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	repos := sqlite.NewRepositoryRepository(db)
	p, _ := projects.Create("proj", "/tmp/proj")
	deps := catalog.SQLiteDeps(db)
	deps.Projects, deps.Repositories = projects, repos
	return NewHandler(servicesOf(catalog.New(deps))), p.ID
}

func TestHTTP_CreateAndListRepositories(t *testing.T) {
	h, pid := newRepositoryHandler(t)

	body, _ := json.Marshal(map[string]any{
		"project_id": pid,
		"name":       "api",
		"url":        "https://github.com/o/api",
		"root_dir":   "/tmp/api",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/create_repository", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create_repository status = %d, body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/list_repositories?project_id="+pid, nil)
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list_repositories status = %d", rec.Code)
	}
	var out struct {
		Repositories []map[string]any `json:"repositories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Repositories) != 1 {
		t.Fatalf("want 1 repository, got %d", len(out.Repositories))
	}
}

func TestHTTP_CreateRepository_MissingProject(t *testing.T) {
	h, _ := newRepositoryHandler(t)
	body, _ := json.Marshal(map[string]any{
		"project_id": "nope",
		"name":       "api",
		"url":        "https://github.com/o/api",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/create_repository", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for missing project, got %d", rec.Code)
	}
}
