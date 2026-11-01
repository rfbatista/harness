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


func TestHTTP_ArchiveTicketAndTheArchivedList(t *testing.T) {
	h, pid := newPlanningHandler(t)
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Open"})
	open := ticketIn(t, out)["id"].(string)
	archived := archivedDoneTask(t, h, pid, "Shipped")

	if got := listTicketIDs(t, h, pid, ""); len(got) != 1 || got[0] != open {
		t.Fatalf("default list = %v, want only %s", got, open)
	}
	if got := listTicketIDs(t, h, pid, "&archived=true"); len(got) != 1 || got[0] != archived {
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
	if code, body := post(); code != http.StatusOK || body != `{"archived":0,"ids":[],"skipped":0}` {
		t.Fatalf("archive_done_tickets with nothing done = %d %s", code, body)
	}
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Shipped", "status": "done"})
	id := ticketIn(t, out)["id"].(string)
	if code, body := post(); code != http.StatusOK || body != `{"archived":1,"ids":["`+id+`"],"skipped":0}` {
		t.Fatalf("archive_done_tickets = %d %s", code, body)
	}
}

// featureWithChildren creates a feature task with one child per status, all
// through the API, and answers their ids.
func featureWithChildren(t *testing.T, h *Handler, pid, featureStatus string, childStatuses ...string) (string, []string) {
	t.Helper()
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Feature", "status": featureStatus})
	feature := ticketIn(t, out)["id"].(string)
	var children []string
	for i, st := range childStatuses {
		_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Child " + string(rune('A'+i)), "status": st, "parent_id": feature})
		children = append(children, ticketIn(t, out)["id"].(string))
	}
	return feature, children
}

func ticketOf(t *testing.T, h *Handler, id string) map[string]any {
	t.Helper()
	rec := getJSON(t, h, "/get_ticket?ticket_id="+id)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return ticketIn(t, out)
}

func TestHTTP_ArchivingAFeatureArchivesItsChildrenTogether(t *testing.T) {
	h, pid := newPlanningHandler(t)
	feature, children := featureWithChildren(t, h, pid, "done", "done", "done")

	code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": feature})
	if code != http.StatusOK || ticketIn(t, out)["id"] != feature {
		t.Fatalf("archive feature = %d %v", code, out)
	}
	at := ticketOf(t, h, feature)["archived_at"]
	if at == nil {
		t.Fatal("the feature is not archived")
	}
	for _, id := range children {
		if got := ticketOf(t, h, id)["archived_at"]; got != at {
			t.Errorf("child %s archived_at = %v, want the feature's %v", id, got, at)
		}
	}
	if got := listTicketIDs(t, h, pid, ""); len(got) != 0 {
		t.Fatalf("board = %v, want empty", got)
	}
}

func TestHTTP_ArchivingAFeatureWithOpenChildrenIsRefusedAndNamesThem(t *testing.T) {
	h, pid := newPlanningHandler(t)
	feature, children := featureWithChildren(t, h, pid, "done", "done", "in_progress")

	code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": feature})
	if code != http.StatusConflict || out["code"] != "FEATURE_HAS_OPEN_CHILDREN" {
		t.Fatalf("archive feature = %d %v", code, out)
	}
	if msg, _ := out["error"].(string); msg != "1 task is not done: Child B" {
		t.Errorf("message = %q", msg)
	}
	open, _ := out["open_children"].([]any)
	if len(open) != 1 {
		t.Fatalf("open_children = %v", out["open_children"])
	}
	if c := open[0].(map[string]any); c["id"] != children[1] || c["title"] != "Child B" || c["status"] != "in_progress" {
		t.Errorf("open_children[0] = %v", c)
	}
	for _, id := range append([]string{feature}, children...) {
		if ticketOf(t, h, id)["archived_at"] != nil {
			t.Errorf("%s was archived by a refused archive", id)
		}
	}
}

func TestHTTP_ArchivingAFeatureLeavesAChildArchivedOnItsOwnAlone(t *testing.T) {
	h, pid := newPlanningHandler(t)
	feature, children := featureWithChildren(t, h, pid, "done", "done", "done")
	if code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": children[0]}); code != http.StatusOK {
		t.Fatalf("archive child = %d %v", code, out)
	}
	before := ticketOf(t, h, children[0])["archived_at"]

	if code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": feature}); code != http.StatusOK {
		t.Fatalf("archive feature = %d %v", code, out)
	}
	if got := ticketOf(t, h, children[0])["archived_at"]; got != before {
		t.Errorf("child archived on its own was re-stamped: %v -> %v", before, got)
	}
	if got := ticketOf(t, h, children[1])["archived_at"]; got != ticketOf(t, h, feature)["archived_at"] {
		t.Errorf("other child archived_at = %v", got)
	}
}

func TestHTTP_ArchivingAFeatureThatIsNotDoneIsTaskNotDone(t *testing.T) {
	h, pid := newPlanningHandler(t)
	feature, _ := featureWithChildren(t, h, pid, "in_progress", "done")
	if code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": feature}); code != http.StatusConflict || out["code"] != "TASK_NOT_DONE" {
		t.Fatalf("archive = %d %v", code, out)
	}
}

func TestHTTP_RestoringAFeatureBringsBackOnlyTheChildrenArchivedWithIt(t *testing.T) {
	h, pid := newPlanningHandler(t)
	feature, children := featureWithChildren(t, h, pid, "done", "done", "done")
	if code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": children[0]}); code != http.StatusOK {
		t.Fatalf("archive child = %d %v", code, out)
	}
	if code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": feature}); code != http.StatusOK {
		t.Fatalf("archive feature = %d %v", code, out)
	}

	code, out := postTicket(t, h, "/api/restore_ticket", map[string]any{"id": feature})
	if code != http.StatusOK || ticketIn(t, out)["archived_at"] != nil {
		t.Fatalf("restore feature = %d %v", code, out)
	}
	if ticketOf(t, h, children[1])["archived_at"] != nil || ticketOf(t, h, children[1])["status"] != "done" {
		t.Errorf("the child archived with the feature is not back: %v", ticketOf(t, h, children[1]))
	}
	if ticketOf(t, h, children[0])["archived_at"] == nil {
		t.Error("the child archived on its own came back with the feature")
	}
}

func TestHTTP_RestoringAChildOfAnArchivedFeatureIsRefusedAndNamesTheFeature(t *testing.T) {
	h, pid := newPlanningHandler(t)
	feature, children := featureWithChildren(t, h, pid, "done", "done")
	if code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": feature}); code != http.StatusOK {
		t.Fatalf("archive feature = %d %v", code, out)
	}

	code, out := postTicket(t, h, "/api/restore_ticket", map[string]any{"id": children[0]})
	if code != http.StatusConflict || out["code"] != "FEATURE_ARCHIVED" {
		t.Fatalf("restore child = %d %v", code, out)
	}
	if out["error"] != "Restore the feature “Feature” first." {
		t.Errorf("message = %q", out["error"])
	}
	if f, _ := out["feature"].(map[string]any); f["id"] != feature || f["title"] != "Feature" {
		t.Errorf("feature = %v", out["feature"])
	}
	if ticketOf(t, h, children[0])["archived_at"] == nil {
		t.Error("the refused restore restored the child")
	}
}

func TestHTTP_RestoringAChildArchivedOnItsOwnWorksWhileItsFeatureIsOnTheBoard(t *testing.T) {
	h, pid := newPlanningHandler(t)
	_, children := featureWithChildren(t, h, pid, "in_progress", "done")
	if code, out := postTicket(t, h, "/api/archive_ticket", map[string]any{"id": children[0]}); code != http.StatusOK {
		t.Fatalf("archive child = %d %v", code, out)
	}
	if code, out := postTicket(t, h, "/api/restore_ticket", map[string]any{"id": children[0]}); code != http.StatusOK || ticketIn(t, out)["archived_at"] != nil {
		t.Fatalf("restore child = %d %v", code, out)
	}
}

func archiveAll(t *testing.T, h *Handler, pid string) (archived int, ids []string, skipped int) {
	t.Helper()
	code, out := postTicket(t, h, "/api/archive_done_tickets", map[string]any{"project_id": pid})
	if code != http.StatusOK {
		t.Fatalf("archive_done_tickets = %d %v", code, out)
	}
	for _, id := range out["ids"].([]any) {
		ids = append(ids, id.(string))
	}
	return int(out["archived"].(float64)), ids, int(out["skipped"].(float64))
}

func TestHTTP_ArchiveAllSkipsAFeatureWithOpenChildrenAndSaysSo(t *testing.T) {
	h, pid := newPlanningHandler(t)
	feature, children := featureWithChildren(t, h, pid, "done", "done", "in_progress")
	_, out := postTicket(t, h, "/api/create_ticket", map[string]any{"project_id": pid, "title": "Plain", "status": "done"})
	plain := ticketIn(t, out)["id"].(string)

	archived, ids, skipped := archiveAll(t, h, pid)
	if archived != 1 || len(ids) != 1 || ids[0] != plain || skipped != 1 {
		t.Fatalf("archived %d %v skipped %d, want only %s and 1 skipped", archived, ids, skipped, plain)
	}
	for _, id := range append([]string{feature}, children...) {
		if ticketOf(t, h, id)["archived_at"] != nil {
			t.Errorf("%s was archived though its feature has an open child", id)
		}
	}
}

func TestHTTP_ArchiveAllArchivesAFeatureWithItsChildren(t *testing.T) {
	h, pid := newPlanningHandler(t)
	feature, children := featureWithChildren(t, h, pid, "done", "done", "done")

	archived, ids, skipped := archiveAll(t, h, pid)
	if archived != 3 || skipped != 0 || len(ids) != 3 || ids[0] != feature {
		t.Fatalf("archived %d %v skipped %d, want the feature and its 2 children", archived, ids, skipped)
	}
	for _, id := range children {
		if ticketOf(t, h, id)["archived_at"] != ticketOf(t, h, feature)["archived_at"] {
			t.Errorf("child %s was not archived with the feature", id)
		}
	}
}

func TestHTTP_ArchiveAllLeavesADoneChildOfAFeatureOnTheBoardAlone(t *testing.T) {
	h, pid := newPlanningHandler(t)
	_, children := featureWithChildren(t, h, pid, "in_progress", "done")

	archived, ids, skipped := archiveAll(t, h, pid)
	if archived != 0 || len(ids) != 0 || skipped != 0 {
		t.Fatalf("archived %d %v skipped %d, want nothing: the child is inside its feature's card", archived, ids, skipped)
	}
	if ticketOf(t, h, children[0])["archived_at"] != nil {
		t.Error("the child was archived")
	}
}
