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

// post sends body to path through the real router and decodes the reply.
func postTicket(t *testing.T, h *Handler, path string, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func ticketIn(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	tk, ok := out["ticket"].(map[string]any)
	if !ok {
		t.Fatalf("no ticket in %v", out)
	}
	return tk
}

func TestHTTP_UpdateTicket_IsPartial(t *testing.T) {
	h, pid := newPlanningHandler(t)
	code, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Ship it", "description": "with care", "status": "todo"})
	if code != http.StatusOK {
		t.Fatalf("create = %d %v", code, out)
	}
	id := ticketIn(t, out)["id"].(string)

	// The board's move: status only, text untouched.
	code, out = postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": id, "status": "review"})
	if code != http.StatusOK {
		t.Fatalf("status-only = %d %v", code, out)
	}
	if tk := ticketIn(t, out); tk["status"] != "review" || tk["title"] != "Ship it" || tk["description"] != "with care" {
		t.Fatalf("status-only update touched the text: %v", tk)
	}

	// The editor's save: text only, card stays put; empty description clears.
	code, out = postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": id, "title": "Ship it now", "description": ""})
	if code != http.StatusOK {
		t.Fatalf("text-only = %d %v", code, out)
	}
	if tk := ticketIn(t, out); tk["status"] != "review" || tk["title"] != "Ship it now" || tk["description"] != nil {
		t.Fatalf("text-only update moved the card or kept the description: %v", tk)
	}

	// null is "untouched", not "blank".
	code, out = postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": id, "title": nil, "status": nil})
	if code != http.StatusOK {
		t.Fatalf("nulls = %d %v", code, out)
	}
	if tk := ticketIn(t, out); tk["status"] != "review" || tk["title"] != "Ship it now" {
		t.Fatalf("null fields changed the ticket: %v", tk)
	}

	// Today's callers send all four fields and see no difference.
	code, out = postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": id, "title": "Ship it", "description": "again", "status": "done"})
	if code != http.StatusOK {
		t.Fatalf("full = %d %v", code, out)
	}
	if tk := ticketIn(t, out); tk["status"] != "done" || tk["title"] != "Ship it" || tk["description"] != "again" {
		t.Fatalf("full update = %v", tk)
	}
}

func TestHTTP_UpdateTicket_Errors(t *testing.T) {
	h, pid := newPlanningHandler(t)
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "T"})
	id := ticketIn(t, out)["id"].(string)

	tests := []struct {
		name string
		body map[string]any
		code int
		err  string
	}{
		{"missing ticket_id", map[string]any{"status": "done"}, http.StatusBadRequest, "INVALID_INPUT"},
		{"blank title", map[string]any{"ticket_id": id, "title": ""}, http.StatusBadRequest, "INVALID_INPUT"},
		{"bad status", map[string]any{"ticket_id": id, "status": "shipped"}, http.StatusBadRequest, "INVALID_INPUT"},
		{"unknown ticket", map[string]any{"ticket_id": "nope", "status": "done"}, http.StatusNotFound, "TICKET_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := postTicket(t, h, "/api/update_ticket", tt.body)
			if code != tt.code || out["code"] != tt.err {
				t.Fatalf("got %d %v, want %d %s", code, out, tt.code, tt.err)
			}
		})
	}
}
