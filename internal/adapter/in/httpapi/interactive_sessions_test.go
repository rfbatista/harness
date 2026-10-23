package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/application/agentready"
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

func TestHTTP_StartInteractiveSessionMode(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	status, started := post(t, srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"architect"}`)
	if status != http.StatusCreated {
		t.Fatalf("start = %d (%s), want 201", status, started.Code)
	}
	if started.Session.Mode != domain.SessionModeArchitect || !strings.Contains(started.Agent.Prompt, "task-architecture") {
		t.Fatalf("architect start = %+v / prompt %q", started.Session, started.Agent.Prompt)
	}

	status, design := post(t, srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"design"}`)
	if status != http.StatusCreated || design.Session.Mode != domain.SessionModeDesign || !design.Session.Interactive || !strings.Contains(design.Agent.Prompt, "design-artifacts") {
		t.Fatalf("design start = %d %+v / prompt %q", status, design.Session, design.Agent.Prompt)
	}

	status, implement := post(t, srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"implement"}`)
	if status != http.StatusCreated || implement.Session.Mode != domain.SessionModeImplement || !strings.Contains(implement.Agent.Prompt, "task-implement-spec") {
		t.Fatalf("implement start = %d %+v / prompt %q", status, implement.Session, implement.Agent.Prompt)
	}

	status, product := post(t, srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"product"}`)
	if status != http.StatusCreated || product.Session.Mode != domain.SessionModeProduct || !strings.Contains(product.Agent.Prompt, "task-product") {
		t.Fatalf("product start = %d %+v / prompt %q", status, product.Session, product.Agent.Prompt)
	}

	status, refused := post(t, srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"wizard"}`)
	if status != http.StatusBadRequest || refused.Code != "INVALID_INPUT" {
		t.Fatalf("unknown mode = %d %q, want 400 INVALID_INPUT", status, refused.Code)
	}
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
		{"/api/resume_interactive_session", `{"session_id":"nope","runs_on":"moon"}`, 400, "INVALID_INPUT"},
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

// The session wire shape says whether a resume would be accepted, and why not.
func TestHTTP_SessionsCarryResumability(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	_, started := post(t, srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","prompt":"hi"}`)
	id := started.Session.ID
	if started.Session.Resumable || started.Session.ResumeBlocked != "SESSION_ALREADY_RUNNING" {
		t.Fatalf("running session = resumable %v, blocked %q", started.Session.Resumable, started.Session.ResumeBlocked)
	}
	if status, ended := post(t, srv.URL+"/api/end_interactive_session", `{"session_id":"`+id+`","exit_code":0}`); status != http.StatusOK {
		t.Fatalf("end = %d %s", status, ended.Code)
	}

	resp, err := http.Get(srv.URL + "/api/sessions?ticket_id=tk1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Sessions) != 1 {
		t.Fatalf("sessions = %+v", raw.Sessions)
	}
	got := raw.Sessions[0]
	if got["resumable"] != true || got["resume_blocked"] != "" {
		t.Fatalf("ended session = resumable %v, resume_blocked %#v; want true and \"\" present", got["resumable"], got["resume_blocked"])
	}
}

// A headless session has nothing to resume.
func TestHTTP_ResumeHeadlessSession(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()
	status, created := post(t, srv.URL+"/api/sessions", `{"project_id":"p1","repository_id":"r1","task":"hello"}`)
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, created.Code)
	}
	if created.Session.ResumeBlocked != "SESSION_NOT_INTERACTIVE" {
		t.Errorf("headless resume_blocked = %q", created.Session.ResumeBlocked)
	}
	status, out := post(t, srv.URL+"/api/resume_interactive_session", `{"session_id":"`+created.Session.ID+`"}`)
	if status != http.StatusConflict || out.Code != "SESSION_NOT_INTERACTIVE" {
		t.Fatalf("resume headless = %d %q, want 409 SESSION_NOT_INTERACTIVE", status, out.Code)
	}
}

// A product-mode session starts on the project with no task (ADR 0016): it
// is recorded with an empty ticket_id and listed among the project's
// sessions; any other mode without a ticket is 400 INVALID_INPUT.
func TestHTTP_StartInteractiveSessionWithoutATask(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	status, started := post(t, srv.URL+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","mode":"product","prompt":"Export"}`)
	if status != http.StatusCreated {
		t.Fatalf("product start without a ticket = %d (%s), want 201", status, started.Code)
	}
	if started.Session.TicketID != "" || started.Session.ProjectID != "p1" || started.Session.Mode != domain.SessionModeProduct ||
		!strings.HasPrefix(started.Agent.Prompt, "Use the task-product skill on project ") {
		t.Fatalf("session = %+v / prompt %q", started.Session, started.Agent.Prompt)
	}

	resp, err := http.Get(srv.URL + "/api/sessions?project_id=p1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listed struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range listed.Sessions {
		if s["id"] != started.Session.ID {
			continue
		}
		found = true
		if ticket, _ := s["ticket_id"].(string); ticket != "" {
			t.Fatalf("the project's sessions list the ticket-less session with ticket_id %q", ticket)
		}
	}
	if !found {
		t.Fatalf("the project's sessions do not list %s: %v", started.Session.ID, listed.Sessions)
	}

	for _, mode := range []string{"architect", "implement", "design", ""} {
		status, refused := post(t, srv.URL+"/api/start_interactive_session",
			`{"project_id":"p1","repository_id":"r1","mode":"`+mode+`"}`)
		if status != http.StatusBadRequest || refused.Code != "INVALID_INPUT" {
			t.Errorf("mode %q without a ticket = %d %q, want 400 INVALID_INPUT", mode, status, refused.Code)
		}
	}
}

// A start the gate refuses reaches the HTTP response whole: a status the web
// client can tell from a bug, the code, the message with the page anchor, and
// the list of what is missing in the details. Nothing is spawned.
func TestHTTP_AStartRefusedForMissingPrerequisitesAnswersTheirNamesAndAnchors(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	env.orch.Ready = &agentready.Gate{
		Credential:    func() bool { return false },
		SandboxOn:     func() bool { return true },
		SandboxDriver: func() bool { return false },
	}
	for _, path := range []string{"/api/start_interactive_session", "/api/sessions"} {
		body := `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","task":"hi"}`
		resp, err := http.Post(env.srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Error   string `json:"error"`
			Code    string `json:"code"`
			Details struct {
				Missing []struct{ Code, Anchor string } `json:"missing"`
			} `json:"details"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable || out.Code != "AGENT_CREDENTIAL_MISSING" {
			t.Fatalf("%s = %d %q, want 503 AGENT_CREDENTIAL_MISSING", path, resp.StatusCode, out.Code)
		}
		if !strings.Contains(out.Error, "no agent credential") || !strings.Contains(out.Error, "/agent-isolation#credential") || !strings.Contains(out.Error, "no sandbox driver") {
			t.Errorf("%s message = %q", path, out.Error)
		}
		if len(out.Details.Missing) != 2 || out.Details.Missing[1].Code != "SANDBOX_DRIVER_MISSING" || out.Details.Missing[1].Anchor != "/agent-isolation#sandbox" {
			t.Errorf("%s details = %+v", path, out.Details)
		}
	}
	if got, _ := env.orch.List(context.Background(), ports.SessionFilter{}); len(got) != 0 {
		t.Errorf("a refused start left %d sessions", len(got))
	}
}
