package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// archivedDoneTask creates a done task through the API and archives it.
func archivedDoneTask(t *testing.T, h *Handler, pid, title string) string {
	t.Helper()
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": title, "status": "done"})
	id := ticketIn(t, out)["id"].(string)
	code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": id})
	if code != http.StatusOK {
		t.Fatalf("archive_ticket = %d %v", code, out)
	}
	if tk := ticketIn(t, out); tk["id"] != id || tk["archived_at"] == nil || tk["status"] != "done" {
		t.Fatalf("archive_ticket answered %v", tk)
	}
	return id
}

func listTicketIDs(t *testing.T, h *Handler, query string) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/list_tickets?"+query, nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list_tickets?%s = %d %s", query, rec.Code, rec.Body.String())
	}
	var out struct {
		Tickets []struct {
			ID string `json:"id"`
		} `json:"tickets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, tk := range out.Tickets {
		ids = append(ids, tk.ID)
	}
	return ids
}

func TestHTTP_ArchiveTicketAndTheArchivedList(t *testing.T) {
	h, pid := newPlanningHandler(t)
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Open"})
	open := ticketIn(t, out)["id"].(string)
	archived := archivedDoneTask(t, h, pid, "Shipped")

	if got := listTicketIDs(t, h, "project_id="+pid); len(got) != 1 || got[0] != open {
		t.Fatalf("default list = %v, want only %s", got, open)
	}
	if got := listTicketIDs(t, h, "project_id="+pid+"&archived=true"); len(got) != 1 || got[0] != archived {
		t.Fatalf("archived list = %v, want only %s", got, archived)
	}
}

func TestHTTP_RestoreTicket(t *testing.T) {
	h, pid := newPlanningHandler(t)
	id := archivedDoneTask(t, h, pid, "Shipped")
	code, out := postTicket(t, h, "/api/restore_ticket", map[string]any{"id": id})
	if code != http.StatusOK {
		t.Fatalf("restore_ticket = %d %v", code, out)
	}
	if tk := ticketIn(t, out); tk["id"] != id || tk["archived_at"] != nil || tk["status"] != "done" {
		t.Fatalf("restore_ticket answered %v", tk)
	}
}

func TestHTTP_ArchiveRefusalsAreConflicts(t *testing.T) {
	h, pid := newPlanningHandler(t)
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Waiting", "status": "review"})
	inReview := ticketIn(t, out)["id"].(string)
	if code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": inReview}); code != http.StatusConflict || out["code"] != "TASK_NOT_DONE" {
		t.Fatalf("archiving a task in review = %d %v", code, out)
	}
	archived := archivedDoneTask(t, h, pid, "Shipped")
	if code, out := postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": archived, "status": "todo"}); code != http.StatusConflict || out["code"] != "TASK_ARCHIVED" {
		t.Fatalf("moving an archived task = %d %v", code, out)
	}
}

func TestHTTP_ArchiveDoneTickets(t *testing.T) {
	h, pid := newPlanningHandler(t)
	post := func() (int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/archive_done_tickets", strings.NewReader(`{"project_id":"`+pid+`"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		NewRouter(h).ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	if code, body := post(); code != http.StatusOK || body != `{"archived":0,"ids":[]}` {
		t.Fatalf("archive_done_tickets with nothing done = %d %s", code, body)
	}
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Shipped", "status": "done"})
	id := ticketIn(t, out)["id"].(string)
	if code, body := post(); code != http.StatusOK || body != `{"archived":1,"ids":["`+id+`"]}` {
		t.Fatalf("archive_done_tickets = %d %s", code, body)
	}
}
