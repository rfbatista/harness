package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
)

// nopWorktree satisfies ports.WorktreeManager without touching disk.
type nopWorktree struct{}

func (nopWorktree) Add(repoRoot, path, branch, baseRef string) error { return nil }
func (nopWorktree) Remove(repoRoot, path string) error               { return nil }
func (nopWorktree) DeleteBranch(repoRoot, branch string) error       { return nil }
func (nopWorktree) ListBranches(repoRoot string) ([]domain.GitBranch, error) {
	return []domain.GitBranch{
		{Name: "main", IsHead: true},
		{Name: "origin/main", Remote: true},
	}, nil
}

func newWorkspaceHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	repo, err := repos.Create("p1", "api", "", "https://example.com/api.git", "/tmp/api")
	if err != nil {
		t.Fatal(err)
	}
	svc := workspaces.NewService(sqlite.NewWorkspaceRepository(db), repos, nopWorktree{}, sqlite.NewSettingsRepository(db))
	return &Handler{workspacesSvc: svc}, repo.ID
}

func TestHTTP_CreateAndListWorkspaces(t *testing.T) {
	h, rid := newWorkspaceHandler(t)

	body, _ := json.Marshal(map[string]any{
		"repository_id": rid,
		"name":          "Feature X",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/create_workspace", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create_workspace status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Workspace map[string]any `json:"workspace"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Workspace["name"] != "feature-x" || created.Workspace["branch"] != "feature-x" {
		t.Fatalf("bad workspace payload: %v", created.Workspace)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/list_workspaces?repository_id="+rid, nil)
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list_workspaces status = %d", rec.Code)
	}
	var out struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Workspaces) != 1 {
		t.Fatalf("want 1 workspace, got %d", len(out.Workspaces))
	}
}

func TestHTTP_ListWorkspaces_RequiresRepositoryID(t *testing.T) {
	h, _ := newWorkspaceHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/list_workspaces", nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestHTTP_CreateWorkspace_MissingRepository(t *testing.T) {
	h, _ := newWorkspaceHandler(t)
	body, _ := json.Marshal(map[string]any{"repository_id": "nope", "name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/create_workspace", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for missing repository, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_CreateWorkspace_DuplicateIsConflict(t *testing.T) {
	h, rid := newWorkspaceHandler(t)
	body, _ := json.Marshal(map[string]any{"repository_id": rid, "name": "dup"})
	for i, want := range []int{http.StatusOK, http.StatusConflict} {
		req := httptest.NewRequest(http.MethodPost, "/api/create_workspace", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		NewRouter(h).ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("attempt %d: want %d got %d, body=%s", i, want, rec.Code, rec.Body.String())
		}
		body, _ = json.Marshal(map[string]any{"repository_id": rid, "name": "dup"})
	}
}

func TestHTTP_DeleteWorkspace(t *testing.T) {
	h, rid := newWorkspaceHandler(t)
	body, _ := json.Marshal(map[string]any{"repository_id": rid, "name": "gone"})
	req := httptest.NewRequest(http.MethodPost, "/api/create_workspace", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	var created struct {
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	body, _ = json.Marshal(map[string]any{"workspace_id": created.Workspace.ID})
	req = httptest.NewRequest(http.MethodPost, "/api/delete_workspace", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete_workspace status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Deleting again is a 404 with the domain code.
	body, _ = json.Marshal(map[string]any{"workspace_id": created.Workspace.ID})
	req = httptest.NewRequest(http.MethodPost, "/api/delete_workspace", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d", rec.Code)
	}
}

func TestHTTP_ListBranches(t *testing.T) {
	h, rid := newWorkspaceHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/list_branches?repository_id="+rid, nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Branches []struct {
			Name   string `json:"name"`
			Remote bool   `json:"remote"`
			IsHead bool   `json:"is_head"`
		} `json:"branches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Branches) != 2 || out.Branches[0].Name != "main" || !out.Branches[0].IsHead {
		t.Fatalf("bad branches payload: %+v", out.Branches)
	}
	if !out.Branches[1].Remote {
		t.Fatalf("origin/main should be marked remote: %+v", out.Branches[1])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/list_branches", nil)
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing repository_id status = %d, want 400", rec.Code)
	}
}
