package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/blueprint"
)

func newBoundedContextHandler(t *testing.T) (*Handler, string, *blueprint.Service) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	svc := blueprint.NewService(projects, nil, sqlite.NewZoneRepository(db), nil, nil, nil, nil, nil, nil, nil, "").
		WithBoundedContexts(sqlite.NewBoundedContextRepository(db))
	return &Handler{svc: svc}, p.ID, svc
}

func TestHTTP_CreateAndListBoundedContexts(t *testing.T) {
	h, pid, _ := newBoundedContextHandler(t)

	body, _ := json.Marshal(map[string]any{
		"project_id": pid,
		"name":       "Billing",
		"purpose":    "Handles invoicing",
		"ubiquitous_language": []map[string]any{
			{"term": "Invoice", "definition": "A bill"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/create_bounded_context", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create_bounded_context status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		BoundedContext struct {
			ID                 string `json:"id"`
			Name               string `json:"name"`
			UbiquitousLanguage []struct {
				Term string `json:"term"`
			} `json:"ubiquitous_language"`
		} `json:"bounded_context"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.BoundedContext.Name != "Billing" || len(created.BoundedContext.UbiquitousLanguage) != 1 || created.BoundedContext.UbiquitousLanguage[0].Term != "Invoice" {
		t.Fatalf("bad bounded_context payload: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/list_bounded_contexts?project_id="+pid, nil)
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list_bounded_contexts status = %d", rec.Code)
	}
	var out struct {
		BoundedContexts []map[string]any `json:"bounded_contexts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.BoundedContexts) != 1 {
		t.Fatalf("want 1 bounded context, got %d", len(out.BoundedContexts))
	}
}

func TestHTTP_ListBoundedContexts_RequiresProjectID(t *testing.T) {
	h, _, _ := newBoundedContextHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/list_bounded_contexts", nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestHTTP_GetBoundedContext_NotFound(t *testing.T) {
	h, _, _ := newBoundedContextHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/get_bounded_context?bounded_context_id=missing", nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_DeleteBoundedContext_NotFoundCode(t *testing.T) {
	h, _, _ := newBoundedContextHandler(t)
	body, _ := json.Marshal(map[string]any{"bounded_context_id": "missing"})
	req := httptest.NewRequest(http.MethodPost, "/api/delete_bounded_context", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != "BOUNDED_CONTEXT_NOT_FOUND" {
		t.Fatalf("want BOUNDED_CONTEXT_NOT_FOUND, got %q", out.Code)
	}
}

func TestHTTP_AssignZoneToBoundedContext(t *testing.T) {
	h, pid, svc := newBoundedContextHandler(t)
	bc, err := svc.CreateBoundedContext(pid, "Billing", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	zone, err := svc.Zones.Create(pid, "api", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"zone_id": zone.ID, "bounded_context_id": bc.ID})
	req := httptest.NewRequest(http.MethodPost, "/api/assign_zone_to_bounded_context", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Zone struct {
			BoundedContextID string `json:"bounded_context_id"`
		} `json:"zone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Zone.BoundedContextID != bc.ID {
		t.Fatalf("zone not linked: %s", rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"zone_id": zone.ID})
	req = httptest.NewRequest(http.MethodPost, "/api/unassign_zone_from_bounded_context", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unassign status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := svc.Zones.Get(zone.ID); got.BoundedContextID != "" {
		t.Fatalf("zone should be unlinked: %+v", got)
	}
}

func TestHTTP_DeleteBoundedContext_ClearsZone(t *testing.T) {
	h, pid, svc := newBoundedContextHandler(t)
	bc, err := svc.CreateBoundedContext(pid, "Billing", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	zone, err := svc.Zones.Create(pid, "api", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AssignZoneToBoundedContext(zone.ID, bc.ID); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"bounded_context_id": bc.ID})
	req := httptest.NewRequest(http.MethodPost, "/api/delete_bounded_context", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := svc.Zones.Get(zone.ID); got == nil || got.BoundedContextID != "" {
		t.Fatalf("zone should survive unlinked: %+v", got)
	}
}
