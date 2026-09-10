package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
)

func newPlanningHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, _ := projects.Create("proj", "/tmp/proj")
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	return &Handler{planningSvc: svc}, p.ID
}

func TestHTTP_CreateAndListTickets(t *testing.T) {
	h, pid := newPlanningHandler(t)

	body, _ := json.Marshal(map[string]any{"project_id": pid, "title": "T1"})
	req := httptest.NewRequest(http.MethodPost, "/api/create_ticket", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create_ticket status = %d, body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/list_tickets?project_id="+pid, nil)
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list_tickets status = %d", rec.Code)
	}
	var out struct {
		Tickets []map[string]any `json:"tickets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Tickets) != 1 {
		t.Fatalf("want 1 ticket, got %d", len(out.Tickets))
	}
}

func TestHTTP_CreateTicket_MissingProject(t *testing.T) {
	h, _ := newPlanningHandler(t)
	body, _ := json.Marshal(map[string]any{"project_id": "nope", "title": "T1"})
	req := httptest.NewRequest(http.MethodPost, "/api/create_ticket", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for missing project, got %d", rec.Code)
	}
}
