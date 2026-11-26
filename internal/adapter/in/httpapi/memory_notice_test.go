package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"operators-mcp/internal/domain"
	"strings"
	"testing"
	"time"

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

type memoryReader struct {
	ports.Orchestration
	read domain.MemoryRead
	live bool
}

func (m memoryReader) SessionMemory(string) (domain.MemoryRead, bool) { return m.read, m.live }

func readMemory(t *testing.T, r memoryReader) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/s1/memory", nil)
	rec := httptest.NewRecorder()
	NewRouter(&Handler{orchSvc: r}).ServeHTTP(rec, req)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	return rec.Code, got
}

func TestTheInitialMemoryReadIsTheLastSampleWithKillsBudgetSourceAndPrevention(t *testing.T) {
	at := time.Date(2026, 11, 25, 22, 40, 1, 0, time.UTC)
	code, got := readMemory(t, memoryReader{live: true, read: domain.MemoryRead{
		Kind: "session.memory", SessionID: "s1", TaskID: "t1", SampledAt: &at, RSSBytes: 1288490188, Budget: 4 << 30, Processes: 7,
		Heaviest:     &domain.MemoryProcess{PID: 81234, Command: "node jest", RSSBytes: 905969664},
		Kills:        []domain.MemoryKill{{Kind: "session.memory_killed", SessionID: "s1", PID: 81234, Command: "node jest", Signal: "SIGTERM"}},
		BudgetSource: "repository", Prevention: domain.MemoryPrevention{HeapMB: 1365, TestWorkers: 2}}})
	kills, _ := got["kills"].([]any)
	prevention, _ := got["prevention"].(map[string]any)
	if code != http.StatusOK || got["kind"] != "session.memory" || got["sampled_at"] != "2026-11-25T22:40:01Z" || got["rss_bytes"] != float64(1288490188) ||
		len(kills) != 1 || kills[0].(map[string]any)["kind"] != "session.memory_killed" ||
		got["budget_source"] != "repository" || prevention["heap_mb"] != float64(1365) || prevention["test_workers"] != float64(2) {
		t.Errorf("read = %d %v", code, got)
	}
}

// Before the first sample a live session answers 200 with a null sampled_at,
// nothing counted, and still the budget source and the prevention.
func TestTheInitialMemoryReadBeforeTheFirstSampleHasANullSampledAt(t *testing.T) {
	code, got := readMemory(t, memoryReader{live: true, read: domain.MemoryRead{
		Kind: "session.memory", SessionID: "s1", Budget: 4 << 30, Kills: []domain.MemoryKill{},
		BudgetSource: "global", Prevention: domain.MemoryPrevention{HeapMB: 1365, TestWorkers: 2}}})
	if v, present := got["sampled_at"]; code != http.StatusOK || !present || v != nil || got["rss_bytes"] != float64(0) || got["processes"] != float64(0) ||
		got["heaviest"] != nil || got["budget_source"] != "global" || got["kills"] == nil {
		t.Errorf("read = %d %v", code, got)
	}
}

func TestTheInitialMemoryReadIs404ForASessionThatIsNotLive(t *testing.T) {
	if code, _ := readMemory(t, memoryReader{}); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}
