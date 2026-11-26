package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// Tester additions at the httpapi seam.

// A hook the harness cannot make sense of never breaks the agent's call: an
// unknown hook name is refused, a body without a call id answers nothing.
func TestTheMemoryHookRefusesAnUnknownEventAndAnswersNothingWithoutACallID(t *testing.T) {
	h := &Handler{orchSvc: &notices{reason: map[string]string{"s1/": "must never be told"}}}
	if rec := hook(t, h, "Stop", `{"tool_use_id":"c"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown hook = %d, want 400", rec.Code)
	}
	for _, body := range []string{`{}`, `{"tool_name":"Bash"}`} {
		if rec := hook(t, h, "PostToolUse", body); rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Errorf("PostToolUse %s = %d %q, want 200 and an empty answer", body, rec.Code, rec.Body)
		}
	}
}

// An orchestration that does not keep memory notices answers an empty 200:
// the hooks fail open.
func TestTheMemoryHookFailsOpenWhenNothingKeepsNotices(t *testing.T) {
	rec := hook(t, &Handler{orchSvc: struct{ ports.Orchestration }{}}, "PostToolUse", `{"tool_use_id":"c"}`)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("answer = %d %q", rec.Code, rec.Body)
	}
}

// The reason in the PostToolUse answer is claude's additionalContext for that event.
func TestThePostToolUseAnswerIsClaudesHookOutputForPostToolUse(t *testing.T) {
	n := &notices{reason: map[string]string{"s1/c": "[memory budget] ended `node jest`"}}
	h := &Handler{orchSvc: n}
	hook(t, h, "PreToolUse", `{"tool_use_id":"c"}`)
	rec := hook(t, h, "PostToolUse", `{"tool_use_id":"c"}`)
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("content type = %q", ct)
	}
	for _, want := range []string{`"hookSpecificOutput"`, `"hookEventName":"PostToolUse"`, `"additionalContext":"[memory budget] ended`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("answer %s lacks %s", rec.Body, want)
		}
	}
}

// Reading the memory of a session on an orchestration that cannot answer is 503, not a panic.
func TestTheInitialMemoryReadIs503WithoutAReader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/s1/memory", nil)
	rec := httptest.NewRecorder()
	NewRouter(&Handler{orchSvc: struct{ ports.Orchestration }{}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
