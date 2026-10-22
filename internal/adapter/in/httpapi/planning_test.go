package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestHTTP_DocumentFormatOnTheWire(t *testing.T) {
	h, pid := newPlanningHandler(t)
	router := NewRouter(h)
	call := func(method, path string, body map[string]any) (int, map[string]any) {
		t.Helper()
		var req *http.Request
		if body != nil {
			b, _ := json.Marshal(body)
			req = httptest.NewRequest(method, path, bytes.NewReader(b))
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	docOf := func(out map[string]any) map[string]any { return out["document"].(map[string]any) }

	// Omitted format is markdown: existing callers are unaffected.
	code, out := call(http.MethodPost, "/api/create_document", map[string]any{"project_id": pid, "title": "Notes", "content": "# n"})
	if code != 200 || docOf(out)["format"] != "markdown" {
		t.Fatalf("create without format: %d %v", code, out)
	}
	notesID := docOf(out)["id"].(string)

	code, out = call(http.MethodPost, "/api/create_document", map[string]any{"project_id": pid, "title": "Page", "content": "<!doctype html><html><body>p</body></html>", "format": "html"})
	if code != 200 || docOf(out)["format"] != "html" {
		t.Fatalf("create html: %d %v", code, out)
	}
	pageID := docOf(out)["id"].(string)

	code, out = call(http.MethodPost, "/api/create_document", map[string]any{"project_id": pid, "title": "X", "format": "pdf"})
	if code != 400 || out["code"] != "INVALID_INPUT" {
		t.Fatalf("unknown format: %d %v", code, out)
	}

	// update without format keeps html; with one, applies it.
	code, out = call(http.MethodPost, "/api/update_document", map[string]any{"document_id": pageID, "title": "Page 2", "content": "<!doctype html><html><body>p2</body></html>"})
	if code != 200 || docOf(out)["format"] != "html" {
		t.Fatalf("update kept format: %d %v", code, out)
	}
	code, out = call(http.MethodPost, "/api/update_document", map[string]any{"document_id": notesID, "title": "Notes", "content": "<!doctype html><html><body>n</body></html>", "format": "html"})
	if code != 200 || docOf(out)["format"] != "html" {
		t.Fatalf("update flipped format: %d %v", code, out)
	}

	// Every read carries it, listings included.
	code, out = call(http.MethodGet, "/api/get_document?document_id="+pageID, nil)
	if code != 200 || docOf(out)["format"] != "html" {
		t.Fatalf("get: %d %v", code, out)
	}
	code, out = call(http.MethodGet, "/api/list_documents?project_id="+pid, nil)
	if code != 200 {
		t.Fatalf("list_documents: %d %v", code, out)
	}
	for _, d := range out["documents"].([]any) {
		if f := d.(map[string]any)["format"]; f != "markdown" && f != "html" {
			t.Fatalf("list_documents without format: %v", d)
		}
	}
	_, out = call(http.MethodPost, "/api/create_ticket", map[string]any{"project_id": pid, "title": "T"})
	ticketID := out["ticket"].(map[string]any)["id"].(string)
	call(http.MethodPost, "/api/link_document_to_ticket", map[string]any{"ticket_id": ticketID, "document_id": pageID})
	code, out = call(http.MethodGet, "/api/list_ticket_documents?ticket_id="+ticketID, nil)
	list := out["documents"].([]any)
	if code != 200 || len(list) != 1 || list[0].(map[string]any)["format"] != "html" || list[0].(map[string]any)["updated_at"] == nil {
		t.Fatalf("list_ticket_documents: %d %v", code, out)
	}
}

func TestHTTP_DocumentScopeOnTheWire(t *testing.T) {
	h, pid := newPlanningHandler(t)
	router := NewRouter(h)
	call := func(method, path string, body map[string]any) (int, map[string]any) {
		t.Helper()
		var req *http.Request
		if body != nil {
			b, _ := json.Marshal(body)
			req = httptest.NewRequest(method, path, bytes.NewReader(b))
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	docOf := func(out map[string]any) map[string]any { return out["document"].(map[string]any) }

	// A document created at the project is a project document; scope is explicit when sent.
	code, out := call(http.MethodPost, "/api/create_document", map[string]any{"project_id": pid, "title": "Architecture", "content": "# a"})
	if code != 200 || docOf(out)["scope"] != "project" {
		t.Fatalf("create without scope: %d %v", code, out)
	}
	archID := docOf(out)["id"].(string)
	code, out = call(http.MethodPost, "/api/create_document", map[string]any{"project_id": pid, "title": "Plan", "content": "# p", "scope": "task"})
	if code != 200 || docOf(out)["scope"] != "task" || docOf(out)["format"] != "markdown" {
		t.Fatalf("create with scope: %d %v", code, out)
	}
	planID := docOf(out)["id"].(string)
	code, out = call(http.MethodPost, "/api/create_document", map[string]any{"project_id": pid, "title": "X", "scope": "global"})
	if code != 400 || out["code"] != "INVALID_INPUT" {
		t.Fatalf("unknown scope on create: %d %v", code, out)
	}

	// The filter: none lists every document, as today; unknown is INVALID_INPUT.
	code, out = call(http.MethodGet, "/api/list_documents?project_id="+pid, nil)
	if code != 200 || len(out["documents"].([]any)) != 2 {
		t.Fatalf("unfiltered list: %d %v", code, out)
	}
	code, out = call(http.MethodGet, "/api/list_documents?project_id="+pid+"&scope=project", nil)
	list := out["documents"].([]any)
	if code != 200 || len(list) != 1 || list[0].(map[string]any)["id"] != archID {
		t.Fatalf("project filter: %d %v", code, out)
	}
	code, out = call(http.MethodGet, "/api/list_documents?project_id="+pid+"&scope=global", nil)
	if code != 400 || out["code"] != "INVALID_INPUT" {
		t.Fatalf("unknown scope filter: %d %v", code, out)
	}

	// A move is a new version the task page's watch notices; the link stays;
	// the same scope again is not a new version.
	_, out = call(http.MethodPost, "/api/create_ticket", map[string]any{"project_id": pid, "title": "T"})
	ticketID := out["ticket"].(map[string]any)["id"].(string)
	call(http.MethodPost, "/api/link_document_to_ticket", map[string]any{"ticket_id": ticketID, "document_id": planID})
	_, out = call(http.MethodGet, "/api/get_document?document_id="+planID, nil)
	before := docOf(out)["updated_at"]
	time.Sleep(2 * time.Millisecond)
	code, out = call(http.MethodPost, "/api/set_document_scope", map[string]any{"document_id": planID, "scope": "project"})
	if code != 200 || docOf(out)["scope"] != "project" || docOf(out)["updated_at"] == before {
		t.Fatalf("set_document_scope: %d %v (before %v)", code, out, before)
	}
	after := docOf(out)["updated_at"]
	code, out = call(http.MethodPost, "/api/set_document_scope", map[string]any{"document_id": planID, "scope": "project"})
	if code != 200 || docOf(out)["updated_at"] != after {
		t.Fatalf("idempotent move wrote: %d %v", code, out)
	}
	code, out = call(http.MethodGet, "/api/list_ticket_documents?ticket_id="+ticketID, nil)
	list = out["documents"].([]any)
	if code != 200 || len(list) != 1 || list[0].(map[string]any)["scope"] != "project" {
		t.Fatalf("the task lost the moved document: %d %v", code, out)
	}
	code, out = call(http.MethodPost, "/api/set_document_scope", map[string]any{"document_id": "missing", "scope": "task"})
	if code != 404 || out["code"] != "DOCUMENT_NOT_FOUND" {
		t.Fatalf("missing document: %d %v", code, out)
	}
	code, out = call(http.MethodPost, "/api/set_document_scope", map[string]any{"document_id": planID, "scope": "global"})
	if code != 400 || out["code"] != "INVALID_INPUT" {
		t.Fatalf("unknown scope: %d %v", code, out)
	}
}

// listTicketIDs lists a project's tickets over the router, with the raw
// query string appended (e.g. "&parent_id=x"), and returns their ids.
func listTicketIDs(t *testing.T, h *Handler, pid, query string) []string {
	t.Helper()
	rec := getJSON(t, h, "/list_tickets?project_id="+pid+query)
	if rec.Code != http.StatusOK {
		t.Fatalf("list_tickets%s = %d %s", query, rec.Code, rec.Body.String())
	}
	var out struct {
		Tickets []struct {
			ID string `json:"id"`
		} `json:"tickets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(out.Tickets))
	for _, tk := range out.Tickets {
		ids = append(ids, tk.ID)
	}
	return ids
}

func TestHTTP_TicketParent_OnTheWire(t *testing.T) {
	h, pid := newPlanningHandler(t)
	code, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Feature"})
	if code != http.StatusOK {
		t.Fatalf("create feature = %d %v", code, out)
	}
	feature := ticketIn(t, out)
	if v, ok := feature["parent_id"]; !ok || v != "" {
		t.Fatalf("a top-level ticket carries parent_id %v (present %v), want \"\"", v, ok)
	}
	featureID := feature["id"].(string)

	// create_ticket with parent_id, read back by get_ticket.
	code, out = postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Child", "parent_id": featureID})
	if code != http.StatusOK {
		t.Fatalf("create child = %d %v", code, out)
	}
	childID := ticketIn(t, out)["id"].(string)
	rec := getJSON(t, h, "/get_ticket?ticket_id="+childID)
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if tk := ticketIn(t, got); tk["parent_id"] != featureID {
		t.Fatalf("get_ticket parent_id = %v, want %s", tk["parent_id"], featureID)
	}

	// update_ticket: absent keeps, a parent moves, "" clears.
	code, out = postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Loner"})
	if code != http.StatusOK {
		t.Fatalf("create loner = %d %v", code, out)
	}
	lonerID := ticketIn(t, out)["id"].(string)
	code, out = postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": lonerID, "parent_id": featureID})
	if code != http.StatusOK || ticketIn(t, out)["parent_id"] != featureID {
		t.Fatalf("update to a parent = %d %v", code, out)
	}
	code, out = postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": lonerID, "title": "Loner renamed"})
	if code != http.StatusOK || ticketIn(t, out)["parent_id"] != featureID {
		t.Fatalf("update without parent_id moved the ticket = %d %v", code, out)
	}
	code, out = postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": lonerID, "parent_id": ""})
	if code != http.StatusOK || ticketIn(t, out)["parent_id"] != "" {
		t.Fatalf("update with an empty parent_id kept the parent = %d %v", code, out)
	}

	// list_tickets: no filter lists every task, children included; parent_id
	// lists one feature's children.
	if ids := listTicketIDs(t, h, pid, ""); len(ids) != 3 {
		t.Fatalf("list_tickets without a filter = %v, want the feature, its child and the loner", ids)
	}
	if ids := listTicketIDs(t, h, pid, "&parent_id="+featureID); len(ids) != 1 || ids[0] != childID {
		t.Fatalf("list_tickets?parent_id = %v, want only %s", ids, childID)
	}
	if ids := listTicketIDs(t, h, pid, "&parent_id="); len(ids) != 2 {
		t.Fatalf("list_tickets with an empty parent_id = %v, want the two top-level tickets", ids)
	}

	// An invalid parent is TICKET_PARENT_INVALID, 400.
	code, out = postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Grandchild", "parent_id": childID})
	if code != http.StatusBadRequest || out["code"] != "TICKET_PARENT_INVALID" {
		t.Fatalf("create under a child = %d %v, want 400 TICKET_PARENT_INVALID", code, out)
	}
	code, out = postTicket(t, h, "/api/update_ticket", map[string]any{"ticket_id": lonerID, "parent_id": "missing"})
	if code != http.StatusBadRequest || out["code"] != "TICKET_PARENT_INVALID" {
		t.Fatalf("update to a missing parent = %d %v, want 400 TICKET_PARENT_INVALID", code, out)
	}
}

func TestHTTP_DeleteTicket_RefusedWhileItHasChildren(t *testing.T) {
	h, pid := newPlanningHandler(t)
	code, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Feature"})
	if code != http.StatusOK {
		t.Fatalf("create feature = %d %v", code, out)
	}
	featureID := ticketIn(t, out)["id"].(string)
	code, out = postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Child", "parent_id": featureID})
	if code != http.StatusOK {
		t.Fatalf("create child = %d %v", code, out)
	}
	childID := ticketIn(t, out)["id"].(string)

	// get_ticket and every list_tickets item carry children_count.
	rec := getJSON(t, h, "/get_ticket?ticket_id="+featureID)
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if n, ok := ticketIn(t, got)["children_count"]; !ok || n != float64(1) {
		t.Fatalf("get_ticket children_count = %v (present %v), want 1", n, ok)
	}
	rec = getJSON(t, h, "/list_tickets?project_id="+pid)
	var listed struct {
		Tickets []map[string]any `json:"tickets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	counts := map[string]any{}
	for _, tk := range listed.Tickets {
		n, ok := tk["children_count"]
		if !ok {
			t.Fatalf("list_tickets item %v has no children_count", tk["id"])
		}
		counts[tk["id"].(string)] = n
	}
	if counts[featureID] != float64(1) || counts[childID] != float64(0) {
		t.Fatalf("list_tickets children counts = %v, want 1 on the feature and 0 on the child", counts)
	}

	// The feature cannot go while its child exists; the child, then the feature, can.
	code, out = postTicket(t, h, "/api/delete_ticket", map[string]any{"ticket_id": featureID})
	if code != http.StatusConflict || out["code"] != "TICKET_HAS_CHILDREN" {
		t.Fatalf("delete a feature with a child = %d %v, want 409 TICKET_HAS_CHILDREN", code, out)
	}
	if code, out = postTicket(t, h, "/api/delete_ticket", map[string]any{"ticket_id": childID}); code != http.StatusNoContent {
		t.Fatalf("delete the child = %d %v", code, out)
	}
	if code, out = postTicket(t, h, "/api/delete_ticket", map[string]any{"ticket_id": featureID}); code != http.StatusNoContent {
		t.Fatalf("delete the feature after its child = %d %v", code, out)
	}
	if rec := getJSON(t, h, "/get_ticket?ticket_id="+featureID); rec.Code != http.StatusNotFound {
		t.Fatalf("the deleted feature is still there: %d", rec.Code)
	}
}
