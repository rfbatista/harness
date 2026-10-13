package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/agents/claudehome"
)

// transcriptGet answers GET /api/sessions/{id}/transcript?query as decoded
// JSON.
func transcriptGet(t *testing.T, base, id, query string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(base + "/api/sessions/" + id + "/transcript?" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

func entryTexts(body map[string]any) []string {
	var out []string
	for _, e := range body["entries"].([]any) {
		out = append(out, e.(map[string]any)["text"].(string))
	}
	return out
}

// The transcript route over the real orchestration and claude home reader:
// the contract's JSON shape, paging, and its errors.
func TestHTTP_SessionTranscript(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	home := t.TempDir()
	env.orch.TranscriptReader = claudehome.NewTranscriptReader(home)

	_, started := post(t, env.srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","prompt":"hi"}`)
	id := started.Session.ID
	if id == "" {
		t.Fatal("no session started")
	}

	status, body := transcriptGet(t, env.srv.URL, id, "")
	if status != http.StatusNotFound || body["code"] != "SESSION_TRANSCRIPT_MISSING" {
		t.Fatalf("no transcript yet = %d %v, want 404 SESSION_TRANSCRIPT_MISSING", status, body)
	}

	dir := env.sessions.Get(id).WorkingDir
	real, _ := filepath.EvalSymlinks(dir)
	proj := filepath.Join(home, "projects", regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(real, "-"))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"user","timestamp":"2026-10-13T12:00:00.000Z","message":{"role":"user","content":"fix the tests"}}`,
		`{"type":"assistant","timestamp":"2026-10-13T12:00:01.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"…"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","timestamp":"2026-10-13T12:00:02.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"FAIL","is_error":true}]},"toolUseResult":{"stdout":"FAIL"}}`,
		`{"type":"ai-title","aiTitle":"Fix tests"}`,
		`{"type":"assistant","timestamp":"2026-10-13T12:00:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Fixed."}]}}`,
	}
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, body = transcriptGet(t, env.srv.URL, id, "")
	if status != http.StatusOK {
		t.Fatalf("transcript = %d %v", status, body)
	}
	for _, k := range []string{"entries", "first_cursor", "last_cursor", "has_older", "has_newer"} {
		if _, ok := body[k]; !ok {
			t.Errorf("page has no %q: %v", k, body)
		}
	}
	entries := body["entries"].([]any)
	if len(entries) != 4 {
		t.Fatalf("entries = %v", entries)
	}
	var keys []string
	for k := range entries[2].(map[string]any) {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"at", "cursor", "is_error", "kind", "text", "tool", "truncated"}; !slices.Equal(keys, want) {
		t.Errorf("entry fields = %v, want %v", keys, want)
	}
	want := []map[string]any{
		{"kind": "user", "text": "fix the tests", "tool": "", "is_error": false, "at": "2026-10-13T12:00:00.000Z"},
		{"kind": "tool_use", "text": "go test ./...", "tool": "Bash", "is_error": false},
		{"kind": "tool_result", "text": "FAIL", "tool": "Bash", "is_error": true},
		{"kind": "assistant", "text": "Fixed.", "tool": "", "is_error": false},
	}
	for i, w := range want {
		for k, v := range w {
			if got := entries[i].(map[string]any)[k]; got != v {
				t.Errorf("entry %d %s = %#v, want %#v", i, k, got, v)
			}
		}
	}
	if body["has_older"] != false || body["has_newer"] != false || body["first_cursor"] != entries[0].(map[string]any)["cursor"] {
		t.Errorf("page flags = %v", body)
	}

	status, page := transcriptGet(t, env.srv.URL, id, "limit=2")
	if status != http.StatusOK || !slices.Equal(entryTexts(page), []string{"FAIL", "Fixed."}) || page["has_older"] != true {
		t.Fatalf("newest two = %d %v", status, page)
	}
	_, older := transcriptGet(t, env.srv.URL, id, "limit=2&before="+url.QueryEscape(page["first_cursor"].(string)))
	if !slices.Equal(entryTexts(older), []string{"fix the tests", "go test ./..."}) || older["has_older"] != false || older["has_newer"] != true {
		t.Fatalf("before = %v", older)
	}
	_, newer := transcriptGet(t, env.srv.URL, id, "after="+url.QueryEscape(page["last_cursor"].(string)))
	if got := newer["entries"].([]any); len(got) != 0 || newer["first_cursor"] != "" || newer["has_newer"] != false {
		t.Fatalf("after the last = %v", newer)
	}

	for _, q := range []string{"limit=abc", "limit=0", "limit=501", "before=1.0&after=2.0", "before=nope", "after=99999.0"} {
		if status, body := transcriptGet(t, env.srv.URL, id, q); status != http.StatusBadRequest || body["code"] != "INVALID_INPUT" {
			t.Errorf("%s = %d %v, want 400 INVALID_INPUT", q, status, body)
		}
	}
	if status, body := transcriptGet(t, env.srv.URL, "nope", ""); status != http.StatusNotFound || body["code"] != "SESSION_NOT_FOUND" {
		t.Errorf("unknown session = %d %v, want 404 SESSION_NOT_FOUND", status, body)
	}
}
