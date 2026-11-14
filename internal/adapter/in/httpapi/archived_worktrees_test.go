package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeArchivedWorktrees struct {
	scope ports.ArchivedWorktreesScope
	res   *ports.ClearResult
	err   error
}

func (f *fakeArchivedWorktrees) Clear(_ context.Context, s ports.ArchivedWorktreesScope) (*ports.ClearResult, error) {
	f.scope = s
	return f.res, f.err
}

func clearRequest(h *Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/clear_archived_worktrees", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	return rec
}

func TestHTTP_ClearArchivedWorktreesAnswersTheContractsShape(t *testing.T) {
	f := &fakeArchivedWorktrees{res: &ports.ClearResult{Cleared: 2, Skipped: []ports.ClearSkip{
		{SessionID: "s1", TaskID: "t1", TaskTitle: "T", Reason: "running"},
		{SessionID: "s2", TaskID: "t1", TaskTitle: "T", Reason: "failed", Message: "busy"},
	}}}
	rec := clearRequest(&Handler{archivedTrees: f}, `{"task_id":"t1"}`)
	if rec.Code != http.StatusOK || f.scope.TaskID != "t1" || f.scope.ProjectID != "" {
		t.Fatalf("%d %s scope=%+v", rec.Code, rec.Body, f.scope)
	}
	var out struct {
		Cleared int              `json:"cleared"`
		Skipped []map[string]any `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Cleared != 2 || len(out.Skipped) != 2 {
		t.Fatalf("body = %s", rec.Body)
	}
	if _, ok := out.Skipped[0]["message"]; ok || out.Skipped[1]["message"] != "busy" || out.Skipped[0]["task_title"] != "T" {
		t.Fatalf("skipped = %v", out.Skipped)
	}
	if rec := clearRequest(&Handler{archivedTrees: f}, `{"project_id":"p"}`); rec.Code != http.StatusOK || f.scope.ProjectID != "p" {
		t.Fatalf("project scope: %d %+v", rec.Code, f.scope)
	}
}

func TestHTTP_ClearArchivedWorktreesRefusals(t *testing.T) {
	for code, want := range map[string]int{
		"TASK_NOT_ARCHIVED": http.StatusConflict, "TICKET_NOT_FOUND": http.StatusNotFound,
		"PROJECT_NOT_FOUND": http.StatusNotFound, "INVALID_INPUT": http.StatusBadRequest,
	} {
		rec := clearRequest(&Handler{archivedTrees: &fakeArchivedWorktrees{err: &domain.StructuredError{Code: code, Message: "m"}}}, `{"task_id":"t"}`)
		if rec.Code != want || !strings.Contains(rec.Body.String(), `"code":"`+code+`"`) {
			t.Errorf("%s = %d %s, want %d", code, rec.Code, rec.Body, want)
		}
	}
	if rec := clearRequest(&Handler{}, `{"task_id":"t"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured = %d", rec.Code)
	}
}
