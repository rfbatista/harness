package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/gitcli"
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

func TestHTTP_FindRepositories(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	deps := catalog.SQLiteDeps(db)
	deps.RepositoryFinder = gitcli.NewRepositoryFinder()
	h := NewHandler(servicesOf(catalog.New(deps)))

	root := t.TempDir()
	for _, dir := range []string{"api", "web"} {
		if out, err := exec.Command("git", "init", "-q", filepath.Join(root, dir)).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
	}

	get := func(dir string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		NewRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/find_repositories?root_dir="+url.QueryEscape(dir), nil))
		return rec
	}

	rec := get(root)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Root         string `json:"root"`
		Repositories []struct {
			Path, Name string
		} `json:"repositories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Root != filepath.Clean(root) || len(out.Repositories) != 2 || out.Repositories[0].Name != "api" || out.Repositories[1].Name != "web" {
		t.Fatalf("got %+v", out)
	}

	if rec := get("relative/dir"); rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("INVALID_ROOT")) {
		t.Fatalf("relative dir: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_RepositoryEnvFiles(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	repos := sqlite.NewRepositoryRepository(db)
	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, ".env"), []byte("FROM_CHECKOUT=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ := projects.Create("proj", checkout)
	repo, _ := repos.Create(p.ID, "api", "", "file://"+checkout, checkout)
	deps := catalog.SQLiteDeps(db)
	deps.Projects, deps.Repositories = projects, repos
	deps.EnvFiles, deps.EnvFileIO = sqlite.NewEnvFileRepository(db), gitcli.NewEnvFiles()
	router := NewRouter(NewHandler(servicesOf(catalog.New(deps))))

	post := func(path string, body map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
		return rec
	}
	list := func() []map[string]any {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/list_repository_env_files?repository_id="+repo.ID, nil))
		var out struct {
			EnvFiles []map[string]any `json:"env_files"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out.EnvFiles
	}

	if rec := post("/api/import_repository_env_file", map[string]any{"repository_id": repo.ID, "path": ".env"}); rec.Code != http.StatusOK ||
		!bytes.Contains(rec.Body.Bytes(), []byte("FROM_CHECKOUT=1")) {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	if rec := post("/api/save_repository_env_file", map[string]any{"repository_id": repo.ID, "path": "./apps/web/.env", "content": "WEB=1"}); rec.Code != http.StatusOK ||
		!bytes.Contains(rec.Body.Bytes(), []byte(`"path":"apps/web/.env"`)) {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if got := list(); len(got) != 2 || got[0]["path"] != ".env" || got[1]["content"] != "WEB=1" {
		t.Fatalf("list = %v", got)
	}

	for _, c := range []struct {
		path, body string
		code       int
		want       string
	}{
		{"/api/save_repository_env_file", `{"repository_id":"` + repo.ID + `","path":"../.env","content":"x"}`, 400, "INVALID_PATH"},
		{"/api/import_repository_env_file", `{"repository_id":"` + repo.ID + `","path":"missing/.env"}`, 404, "ENV_FILE_NOT_FOUND"},
		{"/api/save_repository_env_file", `{"repository_id":"ghost","path":".env","content":"x"}`, 404, "REPOSITORY_NOT_FOUND"},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body)))
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s %s: %d %s, want %d %s", c.path, c.body, rec.Code, rec.Body, c.code, c.want)
		}
	}

	if rec := post("/api/delete_repository_env_file", map[string]any{"repository_id": repo.ID, "path": "apps/web/.env"}); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if got := list(); len(got) != 1 {
		t.Fatalf("after delete: %v", got)
	}
}
