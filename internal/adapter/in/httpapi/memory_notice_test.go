package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// notices answers a reason for one Bash call only.
type notices struct {
	ports.Orchestration
	started []string
	reason  map[string]string
}

func (n *notices) BashStarted(session, call string) { n.started = append(n.started, session+"/"+call) }
func (n *notices) BashEnded(session, call string) string {
	return n.reason[session+"/"+call]
}

func hook(t *testing.T, h *Handler, event, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/memory/hook?hook="+event, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5000"
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	return rec
}

// A Bash call's PostToolUse answer carries the reason of an encerramento por
// memória that happened during it, in claude's hook output format.
func TestThePostToolUseAnswerForABashCallCarriesTheMemoryReason(t *testing.T) {
	n := &notices{reason: map[string]string{"s1/call-1": "[memory budget] The harness ended `node jest` (3.5 GB)"}}
	h := &Handler{orchSvc: n}

	if rec := hook(t, h, "PreToolUse", `{"tool_name":"Bash","tool_use_id":"call-1"}`); rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("PreToolUse = %d %q, want an empty answer", rec.Code, rec.Body)
	}
	if len(n.started) != 1 || n.started[0] != "s1/call-1" {
		t.Errorf("started = %v", n.started)
	}
	rec := hook(t, h, "PostToolUse", `{"tool_name":"Bash","tool_use_id":"call-1"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hookEventName":"PostToolUse"`) || !strings.Contains(rec.Body.String(), "node jest") ||
		!strings.Contains(rec.Body.String(), "additionalContext") {
		t.Errorf("answer = %d %s", rec.Code, rec.Body)
	}
	if rec := hook(t, h, "PostToolUse", `{"tool_name":"Bash","tool_use_id":"call-2"}`); rec.Body.Len() != 0 {
		t.Errorf("a call with nothing ended says %q", rec.Body)
	}
}

func TestTheMemoryHookIsLoopbackOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/memory/hook?hook=PostToolUse", strings.NewReader(`{}`))
	req.RemoteAddr = "203.0.113.9:5000"
	rec := httptest.NewRecorder()
	NewRouter(&Handler{orchSvc: &notices{}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
