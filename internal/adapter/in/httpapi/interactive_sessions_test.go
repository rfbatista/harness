package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

type interactiveResp struct {
	Session domain.Session       `json:"session"`
	Launch  orchestration.Launch `json:"launch"`
	Code    string               `json:"code"`
}

func post(t *testing.T, url, body string) (int, interactiveResp) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out interactiveResp
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestHTTP_InteractiveSessionLifecycle(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	status, started := post(t, srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","prompt":"hi"}`)
	if status != http.StatusCreated {
		t.Fatalf("start = %d (%s), want 201", status, started.Code)
	}
	id := started.Session.ID
	if !started.Session.Interactive || started.Launch.SessionID != id || started.Launch.Dir == "" {
		t.Fatalf("start response = %+v", started)
	}
	if len(started.Launch.Args) == 0 || started.Launch.Args[len(started.Launch.Args)-1] != "hi" {
		t.Fatalf("launch args do not end with the prompt: %q", started.Launch.Args)
	}

	// Headless operations are refused with a code the client can branch on.
	status, refused := post(t, srv.URL+"/api/sessions/"+id+"/messages", `{"text":"x"}`)
	if status != http.StatusConflict || refused.Code != "SESSION_INTERACTIVE" {
		t.Fatalf("send to interactive = %d %q, want 409 SESSION_INTERACTIVE", status, refused.Code)
	}
	status, refused = post(t, srv.URL+"/api/resume_interactive_session", `{"session_id":"`+id+`"}`)
	if status != http.StatusConflict || refused.Code != "SESSION_ALREADY_RUNNING" {
		t.Fatalf("resume while running = %d %q, want 409 SESSION_ALREADY_RUNNING", status, refused.Code)
	}

	// The CLI's SessionStart hook reports a new conversation after /clear.
	resp, err := http.Post(srv.URL+InteractiveSessionStartedPath+"?session_id="+id, "application/json",
		strings.NewReader(`{"session_id":"after-clear","source":"clear","hook_event_name":"SessionStart"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hook = %d, want 200", resp.StatusCode)
	}

	status, ended := post(t, srv.URL+"/api/end_interactive_session", `{"session_id":"`+id+`","exit_code":0,"closed":true}`)
	if status != http.StatusOK || ended.Session.Status != domain.SessionStopped {
		t.Fatalf("end = %d %+v, want 200 stopped", status, ended.Session)
	}
	if ended.Session.ClaudeSessionID != "after-clear" {
		t.Fatalf("claude session id = %q, want after-clear", ended.Session.ClaudeSessionID)
	}

	status, resumed := post(t, srv.URL+"/api/resume_interactive_session", `{"session_id":"`+id+`"}`)
	if status != http.StatusOK || resumed.Session.Status != domain.SessionRunning {
		t.Fatalf("resume = %d (%s) %+v", status, resumed.Code, resumed.Session)
	}
	i := indexOf(resumed.Launch.Args, "--resume")
	if i < 0 || resumed.Launch.Args[i+1] != "after-clear" {
		t.Fatalf("resume args = %q, want --resume after-clear", resumed.Launch.Args)
	}
}

func TestHTTP_InteractiveSessionErrors(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	cases := []struct {
		path, body string
		status     int
		code       string
	}{
		{"/api/start_interactive_session", `{"project_id":"p1","repository_id":"r1"}`, 400, "INVALID_INPUT"},
		{"/api/start_interactive_session", `{"project_id":"p1","repository_id":"r1","ticket_id":"nope"}`, 404, "TICKET_NOT_FOUND"},
		{"/api/end_interactive_session", `{"session_id":"nope"}`, 404, "SESSION_NOT_FOUND"},
		{"/api/resume_interactive_session", `{"session_id":"nope"}`, 404, "SESSION_NOT_FOUND"},
	}
	for _, tc := range cases {
		status, out := post(t, srv.URL+tc.path, tc.body)
		if status != tc.status || out.Code != tc.code {
			t.Errorf("%s %s = %d %q, want %d %q", tc.path, tc.body, status, out.Code, tc.status, tc.code)
		}
	}

	// A hook for an unknown session is still answered 200: it runs inside a
	// live claude session and must never fail there.
	resp, err := http.Post(srv.URL+InteractiveSessionStartedPath+"?session_id=nope", "application/json",
		strings.NewReader(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("hook with garbage = %d, want 200", resp.StatusCode)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
