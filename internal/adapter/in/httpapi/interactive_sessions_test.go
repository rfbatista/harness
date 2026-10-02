package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type interactiveResp struct {
	Session domain.Session  `json:"session"`
	Agent   ports.AgentSpec `json:"agent"`
	Code    string          `json:"code"`
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
	if !started.Session.Interactive || started.Agent.SessionID != id || started.Agent.Dir == "" {
		t.Fatalf("start response = %+v", started)
	}
	if started.Agent.Kind != "claude" || started.Agent.Prompt != "hi" || started.Agent.Conversation != (ports.Conversation{ID: id}) {
		t.Fatalf("agent spec = %+v, want claude with the prompt, opening conversation %s", started.Agent, id)
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
	if resumed.Agent.Conversation != (ports.Conversation{ID: "after-clear", Resume: true}) {
		t.Fatalf("resumed conversation = %+v, want after-clear resumed", resumed.Agent.Conversation)
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
