package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/approval"
	"github.com/rfbatista/llmkit/claude"
	"github.com/rfbatista/llmkit/claude/mcpapprove"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
)

type fakeResolver struct {
	proj *domain.Project
	repo *domain.Repository
}

func (f *fakeResolver) GetProject(id string) *domain.Project {
	if id == "p1" {
		return f.proj
	}
	return nil
}

func (f *fakeResolver) GetRepository(id string) *domain.Repository {
	if id == "r1" {
		return f.repo
	}
	return nil
}
func (f *fakeResolver) GetAgent(id string) *domain.Agent      { return nil }
func (f *fakeResolver) ResolveAgentRelations(a *domain.Agent) {}
func (f *fakeResolver) ListMCPServers() []*domain.MCPServer   { return nil }
func (f *fakeResolver) GetZone(id string) *domain.Zone        { return nil }

type fakeTickets map[string]*domain.Ticket

func (f fakeTickets) Get(id string) *domain.Ticket { return f[id] }

// fakeProvisioner stands in for workspaces.Service: it hands out real
// directories so the CLI has somewhere to run, and records what it was asked for.
type fakeProvisioner struct {
	dir        string
	createErr  error
	created    []*domain.Workspace
	baseRefs   []string
	deleted    []string
	deleteErr  error
	discarded  []string
	discardErr error
	// omitMkdir skips materializing the returned workspace's directory, so a
	// caller that goes on to chdir into it (runtime.Start) fails — used to
	// exercise the rollback path for a failure that happens after
	// provisioning, without needing the real claude CLI to be absent.
	omitMkdir bool
}

func (f *fakeProvisioner) Create(repositoryID, name, branch, baseRef string) (*domain.Workspace, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	ws := &domain.Workspace{
		ID:           "ws" + strconv.Itoa(len(f.created)+1),
		RepositoryID: repositoryID,
		Name:         name,
		Branch:       branch,
		Path:         filepath.Join(f.dir, name),
	}
	if !f.omitMkdir {
		if err := os.MkdirAll(ws.Path, 0o755); err != nil {
			return nil, err
		}
	}
	f.created = append(f.created, ws)
	f.baseRefs = append(f.baseRefs, baseRef)
	return ws, nil
}

func (f *fakeProvisioner) Delete(id string) error {
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}

func (f *fakeProvisioner) Discard(id string) error {
	f.discarded = append(f.discarded, id)
	return f.discardErr
}

// testTaskServerBase stands in for the host's per-session task endpoint. The
// route belongs to an inbound adapter, which the application layer (and so this
// test) deliberately does not import; the URL is injected instead.
const testTaskServerBase = "http://127.0.0.1:8080/mcp/task/"

func newTestService(t *testing.T) (*Service, *approval.Broker, *fakeProvisioner) {
	t.Helper()
	exe, _ := os.Executable()
	return newTestServiceWithBin(t, exe)
}

// newTestServiceWithBin builds the service around a named agent binary, so a
// test can also exercise the machine that has no CLI installed.
func newTestServiceWithBin(t *testing.T, bin string) (*Service, *approval.Broker, *fakeProvisioner) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewSessionRepository(db)
	mgr := claude.New(llmkit.Options{
		Bin: bin,
		ApprovalURL: func(sessionID string) string {
			return "http://127.0.0.1:8080" + mcpapprove.PathPrefix + sessionID
		},
		ApprovalTimeout: time.Hour,
	})
	broker := mgr.Approvals()
	hub := NewHub(64)
	root := t.TempDir()
	res := &fakeResolver{
		proj: &domain.Project{ID: "p1", RootDir: root},
		repo: &domain.Repository{ID: "r1", ProjectID: "p1", Name: "api", RootDir: root},
	}
	tickets := fakeTickets{
		"tk1": {ID: "tk1", ProjectID: "p1", Title: "Ship the thing"},
		"tk2": {ID: "tk2", ProjectID: "other", Title: "Elsewhere"},
	}
	prov := &fakeProvisioner{dir: t.TempDir()}
	svc := NewService(mgr, broker, hub, repo, res, tickets, prov)
	svc.DefaultEnv = []string{"CLAUDE_FAKE=1"}
	svc.TaskServerURL = func(sessionID string) string {
		return testTaskServerBase + sessionID
	}
	return svc, broker, prov
}

func TestService_StartStreamsOutput(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == "" || d.Status != domain.SessionStarting {
		t.Fatalf("bad session: %+v", d)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	ch, replay, cancel := svc.Subscribe(d.ID)
	defer cancel()
	// The "working" event may already be in the replay buffer (published before
	// Subscribe) or arrive live afterwards — check both.
	for _, ev := range replay {
		if ev.Type == "output" && ev.Text == "working" {
			return
		}
	}
	deadline := time.After(4 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "output" && ev.Text == "working" {
				return
			}
		case <-deadline:
			t.Fatal("did not stream output event")
		}
	}
}

// waitForStatus polls the persisted session until it reaches want. Statuses are
// written from the pump goroutine, so the test cannot read them synchronously.
func waitForStatus(t *testing.T, svc *Service, id string, want domain.SessionStatus) {
	t.Helper()
	deadline := time.After(4 * time.Second)
	for {
		if got := svc.Get(id); got != nil && got.Status == want {
			return
		}
		select {
		case <-deadline:
			var got domain.SessionStatus
			if s := svc.Get(id); s != nil {
				got = s.Status
			}
			t.Fatalf("session status = %q, want %q", got, want)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestService_TurnLifecycle covers the interactive loop: the CLI's "result"
// line ends a turn and hands control back to the user (idle), a follow-up
// message starts a new turn (thinking), and that message is persisted as its
// own user_message event — not as "output", which is Claude's own text.
func TestService_TurnLifecycle(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	// The initial task is a turn like any other: it ends at idle.
	waitForStatus(t, svc, d.ID, domain.SessionIdle)

	// And it opens the timeline as a user message, so the feed shows what was
	// actually asked rather than starting mid-conversation.
	first := svc.History(d.ID, 0)
	if len(first) == 0 || first[0].Type != "user_message" || first[0].Text != "hello" {
		t.Fatalf("first event = %+v, want the task as a user_message", first)
	}

	if err := svc.Send(context.Background(), d.ID, "follow up"); err != nil {
		t.Fatalf("send: %v", err)
	}

	var user *SessionEvent
	for _, ev := range svc.History(d.ID, 0) {
		if ev.Type == "user_message" {
			e := ev
			user = &e
		}
	}
	if user == nil {
		t.Fatal("follow-up was not persisted as a user_message event")
	}
	if user.Text != "follow up" {
		t.Fatalf("user_message text = %q, want %q", user.Text, "follow up")
	}
	if user.Status != domain.SessionThinking {
		t.Fatalf("user_message status = %q, want thinking", user.Status)
	}

	// The second turn runs and lands back at idle, in the same process.
	waitForStatus(t, svc, d.ID, domain.SessionIdle)
}

// TestService_TurnBoundaryIgnoresLateInit pins the ordering hazard that makes
// this worth a busy flag at all: the CLI emits "system"/init once it has
// initialized, which is routinely *after* the initial task was written to its
// stdin. Reporting idle there would tell the user it is their turn while
// Claude is working on the first one.
func TestService_TurnBoundaryIgnoresLateInit(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.setBusy("s1", true)

	if _, ok := svc.turnBoundary("s1", llmkit.Event{Type: llmkit.EventSystem, Subtype: "init"}); ok {
		t.Fatal("init reported a status while a turn was in flight")
	}

	st, ok := svc.turnBoundary("s1", llmkit.Event{Type: llmkit.EventResult})
	if !ok || st != domain.SessionIdle {
		t.Fatalf("result boundary = %q %v, want idle true", st, ok)
	}

	// The result cleared the turn, so a subsequent init may report ready again.
	st, ok = svc.turnBoundary("s1", llmkit.Event{Type: llmkit.EventSystem, Subtype: "init"})
	if !ok || st != domain.SessionIdle {
		t.Fatalf("init boundary after result = %q %v, want idle true", st, ok)
	}
}

func TestService_SendToUnknownSessionFails(t *testing.T) {
	svc, _, _ := newTestService(t)
	var se *domain.StructuredError
	if err := svc.Send(context.Background(), "nope", "hi"); !errors.As(err, &se) || se.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("err = %v, want SESSION_NOT_FOUND", err)
	}
}

func TestService_StartWithTicketPersistsLink(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello", TicketID: "tk1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	if d.TicketID != "tk1" {
		t.Fatalf("returned session ticket_id = %q, want tk1", d.TicketID)
	}
	if got := svc.Get(d.ID); got == nil || got.TicketID != "tk1" {
		t.Fatalf("persisted session ticket_id = %+v, want tk1", got)
	}
}

// Spawning into a task must reach the process: the task MCP server attached, its
// tools allow-listed, and the task named in the system prompt.
func TestService_StartWithTicketConfiguresTaskAccess(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello", TicketID: "tk1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	run, ok := svc.runtime.Get(d.ID)
	if !ok {
		t.Fatal("session not running")
	}
	cfg := run.Config()
	task := findMCPServer(cfg, "task")
	if task == nil {
		t.Fatalf("task server not attached: %+v", cfg.MCPServers)
	}
	// The session id is minted before the URL is built, so the endpoint the CLI
	// is handed is this session's own.
	if want := testTaskServerBase + d.ID; task.URL != want {
		t.Fatalf("task server url = %q, want %q", task.URL, want)
	}
	if !slices.Contains(cfg.AllowedTools, "mcp__task__get_task") {
		t.Fatalf("task tools not allow-listed: %v", cfg.AllowedTools)
	}
	if !strings.Contains(cfg.AppendSystem, "Ship the thing") {
		t.Fatalf("task brief missing from system prompt:\n%s", cfg.AppendSystem)
	}
}

// A session without a task gets none of it.
func TestService_StartWithoutTicketLeavesTaskAccessOff(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	run, ok := svc.runtime.Get(d.ID)
	if !ok {
		t.Fatal("session not running")
	}
	cfg := run.Config()
	if len(cfg.MCPServers) != 0 || len(cfg.AllowedTools) != 0 || cfg.AppendSystem != "" {
		t.Fatalf("untasked session was given task access: %+v", cfg)
	}
}

// findMCPServer returns the named MCP server of a spawned session's config, or
// nil. MCPServerSpec carries slices and maps, so it cannot be compared with ==.
func findMCPServer(cfg llmkit.SessionConfig, name string) *llmkit.MCPServerSpec {
	for i := range cfg.MCPServers {
		if cfg.MCPServers[i].Name == name {
			return &cfg.MCPServers[i]
		}
	}
	return nil
}

// A machine without the CLI installed is an expected condition, not an opaque
// 500: the library reports it as a typed error and the service translates it
// into the code clients branch on (mapped to 503 in httpapi/errors.go).
func TestService_StartWithMissingBinaryIsStructured(t *testing.T) {
	svc, _, _ := newTestServiceWithBin(t, "claude-not-installed-here")
	_, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "CLAUDE_CLI_NOT_FOUND" {
		t.Fatalf("err = %v, want CLAUDE_CLI_NOT_FOUND", err)
	}
	// The full string, not a substring: the client renders this message verbatim
	// to the user (lib/core/api/api_client.dart throws it as the exception text),
	// and it has to read exactly like claudetext's message for the same
	// condition — no library namespace leaking through.
	want := `Claude CLI not found ("claude-not-installed-here"). Install it with: npm install -g @anthropic-ai/claude-code`
	if se.Message != want {
		t.Fatalf("message = %q, want %q", se.Message, want)
	}
}

func TestService_StartWithUnknownTicketFails(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello", TicketID: "nope"})
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "TICKET_NOT_FOUND" {
		t.Fatalf("err = %v, want TICKET_NOT_FOUND", err)
	}
}

func TestService_StartWithCrossProjectTicketFails(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello", TicketID: "tk2"})
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "CROSS_PROJECT_ACCESS" {
		t.Fatalf("err = %v, want CROSS_PROJECT_ACCESS", err)
	}
}

func TestService_DeleteRemovesSessionAndEvents(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(context.Background(), d.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := svc.Get(d.ID); got != nil {
		t.Fatalf("session still present after delete: %+v", got)
	}
	if hist := svc.History(d.ID, 0); len(hist) != 0 {
		t.Fatalf("event log still present after delete: %d events", len(hist))
	}

	var se *domain.StructuredError
	if err := svc.Delete(context.Background(), d.ID); !errors.As(err, &se) || se.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("second delete err = %v, want SESSION_NOT_FOUND", err)
	}
}

func TestService_DeleteRemovesWorkspace(t *testing.T) {
	svc, _, prov := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if len(prov.deleted) != 1 || prov.deleted[0] != prov.created[0].ID {
		t.Fatalf("worktree not torn down: %v", prov.deleted)
	}
	// Session deletion must use the branch-preserving Delete, never Discard.
	if len(prov.discarded) != 0 {
		t.Fatalf("session delete must not call Discard: %v", prov.discarded)
	}
}

func TestService_DeleteSucceedsWhenWorktreeRemovalFails(t *testing.T) {
	svc, _, prov := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	prov.deleteErr = errors.New("worktree is locked")

	// A locked worktree must not make the session undeletable: the workspace row
	// survives and stays retryable through delete_workspace.
	if err := svc.Delete(context.Background(), d.ID); err != nil {
		t.Fatalf("Delete = %v, want nil", err)
	}
	if svc.Get(d.ID) != nil {
		t.Fatal("session should be gone")
	}
}

func TestService_DeltaStreamsButIsNotPersisted(t *testing.T) {
	svc, _, _ := newTestService(t)

	// Subscribe first so the ephemeral delta is delivered live.
	ch, _, cancel := svc.Subscribe("s1")
	defer cancel()

	svc.publishDelta("s1", SessionEvent{Type: "output_delta", DeltaKind: "text", Text: "Hi"})

	select {
	case ev := <-ch:
		if ev.Type != "output_delta" || ev.Text != "Hi" {
			t.Fatalf("live delta wrong: %+v", ev)
		}
		if ev.Seq != 0 {
			t.Fatalf("delta must not carry a seq, got %d", ev.Seq)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive live delta")
	}

	if hist := svc.History("s1", 0); len(hist) != 0 {
		t.Fatalf("delta must not be persisted, history has %d events", len(hist))
	}
}

func TestService_ApprovalRoundTrip(t *testing.T) {
	svc, broker, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()

	// Simulate the MCP approve tool calling the broker.
	decided := make(chan approval.Decision, 1)
	go func() {
		decided <- broker.Await(context.Background(), d.ID, "req1", "Bash", []byte(`{"command":"ls"}`))
	}()

	// Expect an approval_needed event.
	var sawNeeded bool
	deadline := time.After(4 * time.Second)
	for !sawNeeded {
		select {
		case ev := <-ch:
			if ev.Type == "approval_needed" && ev.Approval != nil && ev.Approval.ReqID == "req1" {
				sawNeeded = true
			}
		case <-deadline:
			t.Fatal("no approval_needed event")
		}
	}

	if err := svc.Resolve(context.Background(), d.ID, "req1", true, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case dec := <-decided:
		if !dec.Allow {
			t.Fatal("expected allow decision")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after Resolve")
	}

	// The resolution must identify which request it answered: several
	// approvals can be pending at once, and the client correlates by req_id.
	var sawResolved bool
	resolveDeadline := time.After(4 * time.Second)
	for !sawResolved {
		select {
		case ev := <-ch:
			if ev.Type != "approval_resolved" {
				continue
			}
			if ev.Approval == nil {
				t.Fatal("approval_resolved carried no approval block")
			}
			if ev.Approval.ReqID != "req1" {
				t.Fatalf("approval_resolved req_id = %q, want req1", ev.Approval.ReqID)
			}
			sawResolved = true
		case <-resolveDeadline:
			t.Fatal("no approval_resolved event")
		}
	}
}

// TestService_ResolveOnePendingLeavesOthersPending proves that several
// approvals can be outstanding at once and resolving one does not touch the
// others: this is the backend half of the correctness guarantee behind the
// client's pendingApprovals map (keyed by req_id so an out-of-order decision
// removes exactly the right entry, never a sibling's).
func TestService_ResolveOnePendingLeavesOthersPending(t *testing.T) {
	svc, broker, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()

	decided1 := make(chan approval.Decision, 1)
	decided2 := make(chan approval.Decision, 1)
	go func() {
		decided1 <- broker.Await(context.Background(), d.ID, "req1", "Bash", []byte(`{"command":"ls"}`))
	}()
	go func() {
		decided2 <- broker.Await(context.Background(), d.ID, "req2", "Bash", []byte(`{"command":"pwd"}`))
	}()

	// Wait for both registrations to reach the broker before resolving
	// either — approval_needed events confirm Await has registered.
	seen := map[string]bool{}
	deadline := time.After(4 * time.Second)
	for len(seen) < 2 {
		select {
		case ev := <-ch:
			if ev.Type == "approval_needed" && ev.Approval != nil {
				seen[ev.Approval.ReqID] = true
			}
		case <-deadline:
			t.Fatalf("did not see both approval_needed events, saw: %v", seen)
		}
	}

	if err := svc.Resolve(context.Background(), d.ID, "req2", true, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case dec := <-decided2:
		if !dec.Allow {
			t.Fatal("expected allow decision for req2")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await for req2 did not return after resolving req2")
	}

	// req1 must still be pending: resolving req2 must not have answered it.
	select {
	case <-decided1:
		t.Fatal("req1's Await returned before req1 was resolved")
	case <-time.After(200 * time.Millisecond):
		// Still pending, as expected.
	}

	if err := svc.Resolve(context.Background(), d.ID, "req1", true, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case dec := <-decided1:
		if !dec.Allow {
			t.Fatal("expected allow decision for req1")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await for req1 did not return after resolving req1")
	}
}

// An AskUserQuestion call rides the same broker as any other approval, but the
// user owes it an answer rather than a permission: the questions must reach the
// client on the approval_needed event, and the selections must reach the agent
// on the decision.
func TestService_AnswerQuestionRoundTrip(t *testing.T) {
	svc, broker, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()

	decided := make(chan approval.Decision, 1)
	go func() {
		decided <- broker.Await(context.Background(), d.ID, "q1", AskUserQuestionTool, json.RawMessage(askPayload))
	}()

	var needed SessionEvent
	deadline := time.After(4 * time.Second)
	for needed.Type == "" {
		select {
		case ev := <-ch:
			if ev.Type == "approval_needed" && ev.Approval != nil && ev.Approval.ReqID == "q1" {
				needed = ev
			}
		case <-deadline:
			t.Fatal("no approval_needed event")
		}
	}
	if len(needed.Approval.Questions) != 2 {
		t.Fatalf("got %d questions on the event, want 2", len(needed.Approval.Questions))
	}

	answers := map[string]string{
		"Onde devo criar o arquivo de versão?": "Raiz do repo git",
		"O que mais devo incluir?":             "CHANGELOG, Tag git",
	}
	if err := svc.Answer(context.Background(), d.ID, "q1", answers, map[string]string{"O que mais devo incluir?": "sem pressa"}); err != nil {
		t.Fatal(err)
	}

	select {
	case dec := <-decided:
		if !dec.Allow {
			t.Fatal("answering must allow the tool call")
		}
		if dec.Answers["Onde devo criar o arquivo de versão?"] != "Raiz do repo git" {
			t.Errorf("answers did not reach the decision: %#v", dec.Answers)
		}
		if dec.Notes["O que mais devo incluir?"] != "sem pressa" {
			t.Errorf("notes did not reach the decision: %#v", dec.Notes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after Answer")
	}

	// The resolved row shows what was chosen, in a stable order — the same
	// event is rendered live and again on replay from the log.
	var resolved SessionEvent
	resolveDeadline := time.After(4 * time.Second)
	for resolved.Type == "" {
		select {
		case ev := <-ch:
			if ev.Type == "approval_resolved" {
				resolved = ev
			}
		case <-resolveDeadline:
			t.Fatal("no approval_resolved event")
		}
	}
	if resolved.Approval == nil || resolved.Approval.ReqID != "q1" {
		t.Fatalf("approval_resolved did not identify the request: %#v", resolved.Approval)
	}
	if want := "answered: CHANGELOG, Tag git; Raiz do repo git"; resolved.Text != want {
		t.Errorf("resolved text = %q, want %q", resolved.Text, want)
	}
}

// A request the agent stopped waiting on has to be retracted end to end: the
// timeline says so, the pending count comes back down, and the session leaves
// waiting_approval. Otherwise the UI keeps showing a question that can no
// longer be answered — which is exactly what the 60s CLI tool timeout used to
// leave behind.
func TestService_ExpiredApprovalIsRetracted(t *testing.T) {
	svc, broker, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()

	// The CLI calls the approve tool, then hangs up on it — its request context
	// is cancelled, which is what a tool-call timeout looks like from here.
	ctx, hangUp := context.WithCancel(context.Background())
	go func() { broker.Await(ctx, d.ID, "req1", "AskUserQuestion", []byte(`{}`)) }()

	waitForEvent(t, ch, func(ev SessionEvent) bool {
		return ev.Type == "approval_needed" && ev.Approval != nil && ev.Approval.ReqID == "req1"
	}, "approval_needed")
	waitForStatus(t, svc, d.ID, domain.SessionWaitingApproval)
	if got := svc.Get(d.ID); got == nil || got.PendingApprovals != 1 {
		t.Fatalf("pending approvals = %v, want 1", got)
	}

	hangUp()

	expired := waitForEvent(t, ch, func(ev SessionEvent) bool {
		return ev.Type == "approval_expired"
	}, "approval_expired")
	if expired.Approval == nil || expired.Approval.ReqID != "req1" {
		t.Fatalf("approval_expired did not identify the request: %#v", expired.Approval)
	}
	if expired.ToolName != "AskUserQuestion" {
		t.Errorf("approval_expired tool = %q, want AskUserQuestion", expired.ToolName)
	}
	if expired.Text == "" {
		t.Error("approval_expired carries no reason for the user")
	}

	waitForStatus(t, svc, d.ID, domain.SessionThinking)
	if got := svc.Get(d.ID); got == nil || got.PendingApprovals != 0 {
		t.Fatalf("pending approvals = %v, want 0", got)
	}

	// And the decision surface is genuinely dead, rather than reporting success
	// into a request nobody is waiting on.
	err = svc.Answer(context.Background(), d.ID, "req1", map[string]string{"q": "a"}, nil)
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "NO_PENDING_APPROVAL" {
		t.Fatalf("err = %v, want a NO_PENDING_APPROVAL structured error", err)
	}
}

// A session that died owes the user nothing. The count used to survive the
// process, leaving finished sessions listed as still waiting on a decision.
func TestService_ExitClearsPendingApprovals(t *testing.T) {
	svc, broker, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}

	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()
	go func() { broker.Await(context.Background(), d.ID, "req1", "Bash", []byte(`{}`)) }()
	waitForEvent(t, ch, func(ev SessionEvent) bool { return ev.Type == "approval_needed" }, "approval_needed")

	if err := svc.Stop(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}

	// Which terminal status wins is a race between the stop and the fake CLI
	// finishing on its own; either way the count must be back to zero.
	deadline := time.After(4 * time.Second)
	for {
		got := svc.Get(d.ID)
		if got != nil && got.Status.IsTerminal() {
			if got.PendingApprovals != 0 {
				t.Fatalf("pending approvals after exit = %d, want 0", got.PendingApprovals)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("session never reached a terminal status: %+v", got)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// waitForEvent drains the subscription until pred matches, so a test can assert
// on one event without caring how much unrelated traffic the CLI produced.
func waitForEvent(t *testing.T, ch <-chan SessionEvent, pred func(SessionEvent) bool, what string) SessionEvent {
	t.Helper()
	deadline := time.After(4 * time.Second)
	for {
		select {
		case ev := <-ch:
			if pred(ev) {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s event", what)
		}
	}
}

// Skipping is a denial, not an empty answer: an allow with nothing chosen would
// tell the agent its question was answered when it was not.
func TestService_AnswerWithoutAnswersIsRejected(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	err = svc.Answer(context.Background(), d.ID, "q1", nil, nil)
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "NO_ANSWERS" {
		t.Fatalf("err = %v, want a NO_ANSWERS structured error", err)
	}
}

func TestService_StartRequiresRepository(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", Task: "hello"})
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "INVALID_INPUT" {
		t.Fatalf("Start without a repository = %v, want INVALID_INPUT", err)
	}
}

func TestService_StartProvisionsWorktree(t *testing.T) {
	svc, _, prov := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{
		ProjectID: "p1", RepositoryID: "r1", Task: "hello",
		Branch: "agent/hello-9f2c", BaseBranch: "origin/main",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	if len(prov.created) != 1 {
		t.Fatalf("provisioned %d workspaces, want 1", len(prov.created))
	}
	ws := prov.created[0]
	if d.WorkspaceID != ws.ID || d.Branch != "agent/hello-9f2c" || d.WorkingDir != ws.Path {
		t.Fatalf("session not bound to its worktree: %+v (ws %+v)", d, ws)
	}
	if prov.baseRefs[0] != "origin/main" {
		t.Fatalf("base ref = %q, want origin/main", prov.baseRefs[0])
	}
}

func TestService_StartDerivesBranchFromTask(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{
		ProjectID: "p1", RepositoryID: "r1", Task: "Ship the thing",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	if d.Branch != "agent/ship-the-thing" {
		t.Fatalf("branch = %q, want agent/ship-the-thing", d.Branch)
	}
}

// TestService_StartRejectsInvalidBranch proves a user-supplied branch that
// would otherwise reach git raw (or reach workspaces.Create's slug check,
// which speaks of "workspace name" rather than "branch") is rejected up front
// with a branch-shaped INVALID_NAME, and that no worktree is created for it.
func TestService_StartRejectsInvalidBranch(t *testing.T) {
	cases := []string{
		"my branch",  // whitespace
		"feature.",   // trailing dot
		"feature..x", // double dot
		"/leading",   // leading slash
		"trailing/",  // trailing slash
		"-leading",   // leading dash
		"trailing-",  // trailing dash
		"___",        // slugifies to "" downstream
	}
	for _, branch := range cases {
		t.Run(branch, func(t *testing.T) {
			svc, _, prov := newTestService(t)
			_, err := svc.Start(context.Background(), StartRequest{
				ProjectID: "p1", RepositoryID: "r1", Task: "hello", Branch: branch,
			})
			var se *domain.StructuredError
			if !errors.As(err, &se) || se.Code != "INVALID_NAME" {
				t.Fatalf("Start with branch %q = %v, want INVALID_NAME", branch, err)
			}
			if !strings.Contains(se.Message, "branch") {
				t.Fatalf("message should name the branch field, got %q", se.Message)
			}
			if len(prov.created) != 0 {
				t.Fatalf("no workspace should be provisioned for an invalid branch: %v", prov.created)
			}
		})
	}
}

func TestService_StartAcceptsAWellFormedBranch(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{
		ProjectID: "p1", RepositoryID: "r1", Task: "hello", Branch: "agent/foo-1a2b",
	})
	if err != nil {
		t.Fatalf("Start with a well-formed branch failed: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	if d.Branch != "agent/foo-1a2b" {
		t.Fatalf("branch = %q, want agent/foo-1a2b", d.Branch)
	}
}

// TestService_StartValidatesAgentBeforeProvisioning proves the cheap agent
// check runs before the slow, side-effecting git worktree add: a bad agent_id
// must never create (and then have to destroy) a worktree.
func TestService_StartValidatesAgentBeforeProvisioning(t *testing.T) {
	svc, _, prov := newTestService(t)
	_, err := svc.Start(context.Background(), StartRequest{
		ProjectID: "p1", RepositoryID: "r1", Task: "hello", AgentID: "missing",
	})
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "AGENT_NOT_FOUND" {
		t.Fatalf("Start with a missing agent = %v, want AGENT_NOT_FOUND", err)
	}
	if len(prov.created) != 0 {
		t.Fatalf("no workspace should be provisioned for a bad agent_id: %v", prov.created)
	}
}

// TestService_StartDiscardsWorkspaceOnFailureAfterProvisioning covers the
// failures that remain after provisioning (here, the CLI failing to start
// because its working directory does not exist): the rollback must call
// Discard, which also removes the just-cut branch, not Delete, which would
// leave the branch behind to poison a retry with a stale BRANCH_EXISTS.
func TestService_StartDiscardsWorkspaceOnFailureAfterProvisioning(t *testing.T) {
	svc, _, prov := newTestService(t)
	prov.omitMkdir = true

	_, err := svc.Start(context.Background(), StartRequest{
		ProjectID: "p1", RepositoryID: "r1", Task: "hello",
	})
	if err == nil {
		t.Fatal("Start should fail when the worktree directory does not exist")
	}
	if len(prov.created) != 1 {
		t.Fatalf("workspace should have been provisioned: %v", prov.created)
	}
	if len(prov.discarded) != 1 || prov.discarded[0] != prov.created[0].ID {
		t.Fatalf("rollback should call Discard: discarded=%v created=%v", prov.discarded, prov.created)
	}
	if len(prov.deleted) != 0 {
		t.Fatalf("rollback must not call Delete (that preserves the branch): deleted=%v", prov.deleted)
	}
}

// The event is what the client folds to render the toggle, so it has to carry
// the new state explicitly — and must not claim a lifecycle transition, which
// would put a bogus divider in the timeline.
func TestService_SetAutoRunPublishesTheNewState(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()

	if err := svc.SetAutoRun(context.Background(), d.ID, true); err != nil {
		t.Fatal(err)
	}

	ev := awaitEvent(t, ch, "auto_run")
	if ev.AutoRun == nil || !*ev.AutoRun {
		t.Fatalf("auto_run event did not carry the new state: %+v", ev)
	}
	if ev.Status != "" {
		t.Fatalf("the gate is not a lifecycle state, got status %q", ev.Status)
	}
	if got := svc.Get(d.ID); got == nil || !got.AutoRun {
		t.Fatalf("session does not report auto-run: %+v", got)
	}

	// Switching off must publish too, carrying false rather than being elided.
	if err := svc.SetAutoRun(context.Background(), d.ID, false); err != nil {
		t.Fatal(err)
	}
	off := awaitEvent(t, ch, "auto_run")
	if off.AutoRun == nil || *off.AutoRun {
		t.Fatalf("switching off did not publish false: %+v", off)
	}
	if got := svc.Get(d.ID); got == nil || got.AutoRun {
		t.Fatalf("session still reports auto-run: %+v", got)
	}
}

// A flushed request was already published, so it owes the client a resolution —
// otherwise the decision surface stays on screen for a request nobody is
// waiting on, and the pending count never comes back down.
func TestService_SetAutoRunResolvesFlushedApprovals(t *testing.T) {
	svc, broker, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()

	decided := make(chan approval.Decision, 1)
	go func() {
		decided <- broker.Await(context.Background(), d.ID, "req1", "Bash", []byte(`{"command":"ls"}`))
	}()
	awaitEvent(t, ch, "approval_needed")

	if err := svc.SetAutoRun(context.Background(), d.ID, true); err != nil {
		t.Fatal(err)
	}

	select {
	case dec := <-decided:
		if !dec.Allow {
			t.Fatalf("flushed approval was not allowed: %+v", dec)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after the gate opened")
	}

	ev := awaitEvent(t, ch, "approval_resolved")
	if ev.Approval == nil || ev.Approval.ReqID != "req1" {
		t.Fatalf("resolution does not identify the request it answered: %+v", ev)
	}
	if got := svc.Get(d.ID); got == nil || got.PendingApprovals != 0 {
		t.Fatalf("pending approvals not released: %+v", got)
	}
}

// Auto-run grants permissions; it cannot answer a question. A queued
// AskUserQuestion has to survive the flush and stay on screen.
func TestService_SetAutoRunLeavesQuestionsPending(t *testing.T) {
	svc, broker, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })

	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()

	decided := make(chan approval.Decision, 1)
	go func() {
		decided <- broker.Await(context.Background(), d.ID, "q1", AskUserQuestionTool,
			[]byte(`{"questions":[{"question":"Onde?","options":[{"label":"Raiz"}]}]}`))
	}()
	awaitEvent(t, ch, "approval_needed")

	if err := svc.SetAutoRun(context.Background(), d.ID, true); err != nil {
		t.Fatal(err)
	}

	select {
	case dec := <-decided:
		t.Fatalf("auto-run answered a question: %+v", dec)
	case <-time.After(200 * time.Millisecond):
	}

	// Still the user's to answer, and answering it still works.
	if err := svc.Answer(context.Background(), d.ID, "q1", map[string]string{"Onde?": "Raiz"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case dec := <-decided:
		if dec.Answers["Onde?"] != "Raiz" {
			t.Fatalf("answers lost: %#v", dec.Answers)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after the answer")
	}
}

func TestService_SetAutoRunUnknownSession(t *testing.T) {
	svc, _, _ := newTestService(t)
	err := svc.SetAutoRun(context.Background(), "nope", true)
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("want SESSION_NOT_FOUND, got %v", err)
	}
}

// awaitEvent drains the subscription until an event of the given type arrives.
func awaitEvent(t *testing.T, ch <-chan SessionEvent, typ string) SessionEvent {
	t.Helper()
	deadline := time.After(4 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s event", typ)
		}
	}
}
