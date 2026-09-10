package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/approval"
	"github.com/rfbatista/llmkit/claude"
	"github.com/rfbatista/llmkit/claude/mcpapprove"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

type stubResolver struct{ proj *domain.Project }

func (s *stubResolver) GetProject(id string) *domain.Project {
	if id == "p1" {
		return s.proj
	}
	return nil
}

func (s *stubResolver) GetRepository(id string) *domain.Repository {
	if id == "r1" {
		return &domain.Repository{ID: "r1", ProjectID: "p1", Name: "api", RootDir: s.proj.RootDir}
	}
	return nil
}
func (s *stubResolver) GetAgent(id string) *domain.Agent      { return nil }
func (s *stubResolver) ResolveAgentRelations(a *domain.Agent) {}
func (s *stubResolver) ListMCPServers() []*domain.MCPServer   { return nil }
func (s *stubResolver) GetZone(id string) *domain.Zone        { return nil }

type stubTickets map[string]*domain.Ticket

func (s stubTickets) Get(id string) *domain.Ticket { return s[id] }

// stubProvisioner stands in for workspaces.Service: it hands out real
// directories so the CLI has somewhere to run, and records what it was asked for.
type stubProvisioner struct {
	dir        string
	createErr  error
	created    []*domain.Workspace
	baseRefs   []string
	deleted    []string
	deleteErr  error
	discarded  []string
	discardErr error
}

func (s *stubProvisioner) Create(repositoryID, name, branch, baseRef string) (*domain.Workspace, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	ws := &domain.Workspace{
		ID:           "ws" + strconv.Itoa(len(s.created)+1),
		RepositoryID: repositoryID,
		Name:         name,
		Branch:       branch,
		Path:         filepath.Join(s.dir, name),
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		return nil, err
	}
	s.created = append(s.created, ws)
	s.baseRefs = append(s.baseRefs, baseRef)
	return ws, nil
}

func (s *stubProvisioner) Delete(id string) error {
	s.deleted = append(s.deleted, id)
	return s.deleteErr
}

func (s *stubProvisioner) Discard(id string) error {
	s.discarded = append(s.discarded, id)
	return s.discardErr
}

func newSessionTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := newSessionTestServerWithBroker(t)
	return srv
}

// newSessionTestServerWithBroker also hands back the permission broker, so a
// test can stand in for the CLI's end of a pending decision.
func newSessionTestServerWithBroker(t *testing.T) (*httptest.Server, *approval.Broker) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	mgr := claude.New(llmkit.Options{
		Bin: exe,
		ApprovalURL: func(sessionID string) string {
			return "http://127.0.0.1:8080" + mcpapprove.PathPrefix + sessionID
		},
		ApprovalTimeout: time.Hour,
	})
	broker := mgr.Approvals()
	hub := orchestration.NewHub(64)
	svc := orchestration.NewService(mgr, broker, hub, sqlite.NewSessionRepository(db),
		&stubResolver{proj: &domain.Project{ID: "p1", RootDir: t.TempDir()}},
		stubTickets{"tk1": {ID: "tk1", ProjectID: "p1", Title: "Ship the thing"}},
		&stubProvisioner{dir: t.TempDir()})
	svc.DefaultEnv = []string{"CLAUDE_FAKE=1"}

	h := &Handler{orchSvc: svc}
	return httptest.NewServer(NewRouter(h)), broker
}

func TestHTTP_CreateAndStreamSession(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"repository_id":"r1","project_id":"p1","task":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	var created struct {
		Session domain.Session `json:"session"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.Session.ID == "" {
		t.Fatal("no session id")
	}

	// Stream events; expect at least one data line within the timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/sessions/"+created.Session.ID+"/events", nil)
	sresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()

	sc := bufio.NewScanner(sresp.Body)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if !sc.Scan() {
			break
		}
		if bytes.HasPrefix([]byte(sc.Text()), []byte("data:")) {
			return // got a streamed event
		}
	}
	t.Fatal("no SSE data event received")
}

func TestHTTP_CreateSessionWithTicketAndFilter(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"repository_id":"r1","project_id":"p1","task":"hello","ticket_id":"tk1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	var created struct {
		Session domain.Session `json:"session"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.Session.TicketID != "tk1" {
		t.Fatalf("session ticket_id = %q, want tk1", created.Session.TicketID)
	}

	// A second, unlinked session must not appear in the ticket filter.
	resp2, err := http.Post(srv.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"repository_id":"r1","project_id":"p1","task":"other"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	lresp, err := http.Get(srv.URL + "/api/sessions?ticket_id=tk1")
	if err != nil {
		t.Fatal(err)
	}
	defer lresp.Body.Close()
	var listed struct {
		Sessions []domain.Session `json:"sessions"`
	}
	json.NewDecoder(lresp.Body).Decode(&listed)
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != created.Session.ID {
		t.Fatalf("ticket filter returned %+v, want only %s", listed.Sessions, created.Session.ID)
	}
}

func TestHTTP_DeleteSession(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"repository_id":"r1","project_id":"p1","task":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Session domain.Session `json:"session"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/sessions/"+created.Session.ID, nil)
	dresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	dresp.Body.Close()
	if dresp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", dresp.StatusCode)
	}

	lresp, err := http.Get(srv.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer lresp.Body.Close()
	var listed struct {
		Sessions []domain.Session `json:"sessions"`
	}
	json.NewDecoder(lresp.Body).Decode(&listed)
	if len(listed.Sessions) != 0 {
		t.Fatalf("session still listed after delete: %+v", listed.Sessions)
	}

	req2, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/sessions/"+created.Session.ID, nil)
	dresp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	dresp2.Body.Close()
	if dresp2.StatusCode != http.StatusNotFound {
		t.Fatalf("deleting a missing session: want 404, got %d", dresp2.StatusCode)
	}
}

func TestHTTP_CreateSessionWithUnknownTicketFails(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"repository_id":"r1","project_id":"p1","task":"hello","ticket_id":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		t.Fatal("expected error for unknown ticket, got 201")
	}
}

func TestHTTP_CreateSessionRequiresRepository(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"project_id":"p1","task":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "INVALID_INPUT" {
		t.Fatalf("code = %q, want INVALID_INPUT", body["code"])
	}
}

// postApproval retries until the broker has registered the pending request:
// Await races the HTTP call, and a decision for a request that has not landed
// yet is rejected rather than queued.
func postApproval(t *testing.T, srv *httptest.Server, sessionID, reqID, body string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		resp, err := http.Post(srv.URL+"/api/sessions/"+sessionID+"/approvals/"+reqID,
			"application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		status = resp.StatusCode
		if status == http.StatusAccepted {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	return status
}

// The approvals route carries both decisions a session can owe: a permission
// for an ordinary tool, and an answer for AskUserQuestion. An `answers` body
// must reach the agent as answers — not as a bare allow, which would tell it
// the question was answered while carrying nothing it can read.
func TestHTTP_ApprovalRouteAnswersQuestions(t *testing.T) {
	srv, broker := newSessionTestServerWithBroker(t)
	defer srv.Close()

	decided := make(chan approval.Decision, 1)
	go func() {
		decided <- broker.Await(context.Background(), "s1", "q1", orchestration.AskUserQuestionTool,
			json.RawMessage(`{"questions":[{"question":"Onde?","options":[{"label":"Raiz"}]}]}`))
	}()

	body := `{"allow":true,"answers":{"Onde?":"Raiz"},"notes":{"Onde?":"melhor lugar"}}`
	if got := postApproval(t, srv, "s1", "q1", body); got != http.StatusAccepted {
		t.Fatalf("want 202, got %d", got)
	}

	select {
	case dec := <-decided:
		if !dec.Allow {
			t.Fatal("answering must allow the tool call")
		}
		if dec.Answers["Onde?"] != "Raiz" {
			t.Errorf("answers did not survive the route: %#v", dec.Answers)
		}
		if dec.Notes["Onde?"] != "melhor lugar" {
			t.Errorf("notes did not survive the route: %#v", dec.Notes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after the answer was posted")
	}
}

// Regression guard on the shared route: a body with no answers is still a plain
// permission decision, and denying is how a question is skipped.
func TestHTTP_ApprovalRouteStillResolvesPlainDecisions(t *testing.T) {
	srv, broker := newSessionTestServerWithBroker(t)
	defer srv.Close()

	decided := make(chan approval.Decision, 1)
	go func() {
		decided <- broker.Await(context.Background(), "s1", "r1", "Bash", json.RawMessage(`{"command":"ls"}`))
	}()

	body := `{"allow":false,"message":"skipped by user"}`
	if got := postApproval(t, srv, "s1", "r1", body); got != http.StatusAccepted {
		t.Fatalf("want 202, got %d", got)
	}

	select {
	case dec := <-decided:
		if dec.Allow {
			t.Fatal("expected a denial")
		}
		if dec.Message != "skipped by user" {
			t.Errorf("message = %q", dec.Message)
		}
		if dec.Answers != nil {
			t.Errorf("a plain decision must carry no answers, got %#v", dec.Answers)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after the decision was posted")
	}
}

// createSession starts one through the real route and returns its id.
func createSession(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"repository_id":"r1","project_id":"p1","task":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: want 201, got %d", resp.StatusCode)
	}
	var created struct {
		Session domain.Session `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Session.AutoRun {
		t.Fatal("a new session must start with auto-run off")
	}
	return created.Session.ID
}

func postAutoRun(t *testing.T, srv *httptest.Server, sessionID string, enabled bool) int {
	t.Helper()
	body := `{"enabled":` + strconv.FormatBool(enabled) + `}`
	resp, err := http.Post(srv.URL+"/api/sessions/"+sessionID+"/auto-run",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func getSession(t *testing.T, srv *httptest.Server, sessionID string) domain.Session {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Session domain.Session `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got.Session
}

// The toggle is readable back off the session, both ways — a client that only
// fetched the session must not have to replay the event log to render it.
func TestHTTP_AutoRunRouteRoundTrips(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()
	id := createSession(t, srv)

	if got := postAutoRun(t, srv, id, true); got != http.StatusAccepted {
		t.Fatalf("enable: want 202, got %d", got)
	}
	if !getSession(t, srv, id).AutoRun {
		t.Fatal("auto_run did not survive the route")
	}

	if got := postAutoRun(t, srv, id, false); got != http.StatusAccepted {
		t.Fatalf("disable: want 202, got %d", got)
	}
	if getSession(t, srv, id).AutoRun {
		t.Fatal("auto_run was not cleared")
	}
}

func TestHTTP_AutoRunUnknownSession(t *testing.T) {
	srv := newSessionTestServer(t)
	defer srv.Close()

	if got := postAutoRun(t, srv, "nope", true); got != http.StatusNotFound {
		t.Fatalf("want 404 for an unknown session, got %d", got)
	}
}

// The whole point of the toggle, end to end: once it is on, a tool call the CLI
// asks about is answered by the broker and never reaches the user.
func TestHTTP_AutoRunAnswersToolCalls(t *testing.T) {
	srv, broker := newSessionTestServerWithBroker(t)
	defer srv.Close()
	id := createSession(t, srv)

	if got := postAutoRun(t, srv, id, true); got != http.StatusAccepted {
		t.Fatalf("enable: want 202, got %d", got)
	}

	dec := broker.Await(context.Background(), id, "r1", "Bash", json.RawMessage(`{"command":"ls"}`))
	if !dec.Allow {
		t.Fatalf("auto-run did not allow the tool call: %+v", dec)
	}
}

// Flipping it on while a decision is on screen releases it, so the agent is not
// left blocked on a question the user just said they no longer want to answer.
func TestHTTP_AutoRunFlushesPendingApproval(t *testing.T) {
	srv, broker := newSessionTestServerWithBroker(t)
	defer srv.Close()
	id := createSession(t, srv)

	decided := make(chan approval.Decision, 1)
	go func() {
		decided <- broker.Await(context.Background(), id, "r1", "Bash", json.RawMessage(`{"command":"ls"}`))
	}()
	waitForPendingCount(t, srv, id, 1)

	if got := postAutoRun(t, srv, id, true); got != http.StatusAccepted {
		t.Fatalf("enable: want 202, got %d", got)
	}

	select {
	case dec := <-decided:
		if !dec.Allow {
			t.Fatalf("pending approval was not flushed as allowed: %+v", dec)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending approval was never released")
	}

	// A flushed request is a decided one: the count has to come back down, or
	// the session shows a decision the user can never be asked to make.
	waitForPendingCount(t, srv, id, 0)
}

// waitForPendingCount polls the session until it reports the expected number of
// outstanding decisions. Await races the HTTP call in both directions — a flush
// cannot release what has not landed yet, and the release itself is applied by
// the orchestration goroutine rather than the request handler.
func waitForPendingCount(t *testing.T, srv *httptest.Server, sessionID string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var got int
	for time.Now().Before(deadline) {
		got = getSession(t, srv, sessionID).PendingApprovals
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pending_approvals = %d, want %d", got, want)
}
