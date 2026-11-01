package archassistant_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/application/archassistant"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeTurn is the claude turn, scripted: each call is recorded, and script
// answers it.
type fakeTurn struct {
	mu      sync.Mutex
	calls   []archassistant.TurnCall
	script  func(n int, call archassistant.TurnCall, cb archassistant.TurnCallbacks) (archassistant.TurnResult, error)
	deleted []string
}

func (f *fakeTurn) Run(ctx context.Context, call archassistant.TurnCall, cb archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	n := len(f.calls)
	f.mu.Unlock()
	return f.script(n, call, cb)
}

func (f *fakeTurn) DeleteTranscript(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, sessionID)
}

func (f *fakeTurn) callsMade() []archassistant.TurnCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]archassistant.TurnCall(nil), f.calls...)
}

// answering is a script that answers every turn with text under session "s1".
func answering(text string) func(int, archassistant.TurnCall, archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
	return func(_ int, _ archassistant.TurnCall, cb archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		if cb.OnText != nil {
			cb.OnText(text)
		}
		return archassistant.TurnResult{SessionID: "s1", Text: text}, nil
	}
}

type env struct {
	svc       *archassistant.Service
	turn      *fakeTurn
	settings  map[string]string
	repos     map[string]*domain.Repository
	events    []domain.ArchitectureRuleEvent
	peekErr   error
	agentsErr error
	now       time.Time
}

type settingsMap map[string]string

func (s settingsMap) Setting(k string) string { return s[k] }

type fakeRepos struct{ e *env }

func (f fakeRepos) GetRepository(_ context.Context, id string) (*domain.Repository, error) {
	if r, ok := f.e.repos[id]; ok {
		return r, nil
	}
	return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "no such repository"}
}

type fakeArch struct{ e *env }

func (f fakeArch) GetRepositoryArchitecture(_ context.Context, id string) (*domain.RepositoryArchitecture, error) {
	rec := domain.ArchitectureRecord{Repository: "harness", Pattern: "SAVED-PATTERN"}
	rec.Normalize()
	return &domain.RepositoryArchitecture{RepositoryID: id, ProjectID: "p1", Exists: true, Version: 7, Record: rec}, nil
}

func (f fakeArch) DiscoverRepositoryArchitecture(context.Context, string) (*domain.ArchitectureDiscovery, error) {
	return &domain.ArchitectureDiscovery{
		Instructions: []domain.ArchitectureCandidate{{Name: "CLAUDE.md", Path: "CLAUDE.md"}},
		Agents:       []string{"DISCOVERED-AGENT"},
	}, nil
}

func (f fakeArch) ListArchitectureRuleEvents(_ context.Context, _ string, _ time.Time, limit int) ([]domain.ArchitectureRuleEvent, error) {
	if len(f.e.events) > limit {
		return f.e.events[:limit], nil
	}
	return f.e.events, nil
}

type fakeAgents struct{ e *env }

func (f fakeAgents) ListAgents(context.Context) ([]*domain.Agent, error) {
	if f.e != nil && f.e.agentsErr != nil {
		return nil, f.e.agentsErr
	}
	return []*domain.Agent{{ID: "a1", Name: "go-developer", Description: "writes Go"}, {ID: "a2", Name: "software-architect"}}, nil
}
func (fakeAgents) GetAgent(context.Context, string) (*domain.Agent, error) { return nil, nil }

type fakeMCP struct{}

func (fakeMCP) ListMCPServers() []*domain.MCPServer {
	return []*domain.MCPServer{{Name: "postgres", Transport: "stdio", Env: map[string]string{"PASSWORD": "hunter2"}}}
}

type fakeSandbox struct{}

func (fakeSandbox) Enabled() bool { return true }
func (fakeSandbox) Paths() []ports.SandboxPath {
	return []ports.SandboxPath{{Path: "/opt/tools", Write: false}}
}

type fakeDocs struct{}

func (fakeDocs) ListDocuments(_ string, scope domain.DocumentScope) []*domain.Document {
	if scope != domain.DocumentScopeProject {
		return nil
	}
	return []*domain.Document{
		{Title: "Glossary: harness", Content: "GLOSSARY-BODY layer is a zone"},
		{Title: "ADR: 0032 A repository architecture is a structured record", Content: "ADR-BODY-NOT-SENT"},
		{Title: "Spec: something", Content: "SPEC-BODY-NOT-SENT"},
	}
}

type fakeContexts struct{}

func (fakeContexts) ListBoundedContexts(string) []*domain.BoundedContext { return nil }

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		settings: map[string]string{},
		repos:    map[string]*domain.Repository{"r1": {ID: "r1", ProjectID: "p1", Name: "harness", RootDir: "/work/harness"}, "r2": {ID: "r2", ProjectID: "p1", Name: "other", RootDir: "/work/other"}},
		turn:     &fakeTurn{script: answering("plain answer")},
		now:      time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
	}
	e.svc = archassistant.New(archassistant.Deps{
		Turn: e.turn, Settings: settingsMap(e.settings), Repos: fakeRepos{e}, Architectures: fakeArch{e},
		Agents: fakeAgents{e}, MCP: fakeMCP{}, Sandbox: fakeSandbox{}, Documents: fakeDocs{}, Contexts: fakeContexts{},
		Peek: func(context.Context) error { return e.peekErr },
		Now:  func() time.Time { return e.now },
	})
	return e
}

func req(message string) ports.ArchitectureAssistantRequest {
	return ports.ArchitectureAssistantRequest{
		RepositoryID: "r1", Message: message,
		Draft: json.RawMessage(`{"version":1,"repository":"harness","pattern":"DRAFT-PATTERN"}`),
	}
}

// run runs a turn and collects its events.
func (e *env) run(ctx context.Context, r ports.ArchitectureAssistantRequest) ([]ports.ArchitectureAssistantEvent, error) {
	var evs []ports.ArchitectureAssistantEvent
	err := e.svc.Run(ctx, r, func(ev ports.ArchitectureAssistantEvent) { evs = append(evs, ev) })
	return evs, err
}

func codeOf(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func conversationID(t *testing.T, evs []ports.ArchitectureAssistantEvent) string {
	t.Helper()
	if len(evs) == 0 || evs[0].Name != ports.ArchitectureAssistantEventConversation {
		t.Fatalf("the first event is not the conversation: %+v", evs)
	}
	return evs[0].Data.(ports.ArchitectureAssistantConversation).ConversationID
}

func TestAFirstTurnIssuesAConversationAndAPlainAnswerHasNoProposal(t *testing.T) {
	e := newEnv(t)
	evs, err := e.run(context.Background(), req("hello"))
	if err != nil {
		t.Fatal(err)
	}
	id := conversationID(t, evs)
	if id == "" {
		t.Fatal("empty conversation id")
	}
	last := evs[len(evs)-1]
	res, ok := last.Data.(ports.ArchitectureAssistantResult)
	if last.Name != ports.ArchitectureAssistantEventResult || !ok || res.Text != "plain answer" || res.Proposal != nil {
		t.Fatalf("last event = %+v", last)
	}
	if calls := e.turn.callsMade(); len(calls) != 1 || calls[0].ResumeID != "" {
		t.Fatalf("calls = %+v, want one that starts a conversation", calls)
	}
}

func TestALaterTurnResumesTheConversationAndRepeatsItsID(t *testing.T) {
	e := newEnv(t)
	evs, _ := e.run(context.Background(), req("one"))
	id := conversationID(t, evs)
	r := req("two")
	r.ConversationID = id
	evs, err := e.run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if got := conversationID(t, evs); got != id {
		t.Errorf("conversation id = %q, want %q", got, id)
	}
	calls := e.turn.callsMade()
	if len(calls) != 2 || calls[1].ResumeID != "s1" {
		t.Fatalf("calls = %+v, want the second to resume session s1", calls)
	}
}

func TestTextAndToolUseArePassedThroughInOrder(t *testing.T) {
	e := newEnv(t)
	e.turn.script = func(_ int, _ archassistant.TurnCall, cb archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		cb.OnTool("Read", "internal/domain/zone.go")
		cb.OnText("Look")
		cb.OnText("ing.")
		return archassistant.TurnResult{SessionID: "s1", Text: "Looking."}, nil
	}
	evs, err := e.run(context.Background(), req("go"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ev := range evs {
		names = append(names, ev.Name)
	}
	if strings.Join(names, ",") != "conversation,activity,delta,delta,result" {
		t.Fatalf("events = %v", names)
	}
	if a := evs[1].Data.(ports.ArchitectureAssistantActivity); a.Tool != "Read" || a.Target != "internal/domain/zone.go" {
		t.Errorf("activity = %+v", a)
	}
	if d := evs[2].Data.(ports.ArchitectureAssistantDelta); d.Text != "Look" {
		t.Errorf("delta = %+v", d)
	}
}

func TestTheTurnRunsOnTheModelSettingAndTheProjectAttributionInTheCheckout(t *testing.T) {
	e := newEnv(t)
	_, _ = e.run(context.Background(), req("a"))
	e.settings[domain.SettingModelArchitectureAssistant] = "opus"
	_, _ = e.run(context.Background(), req("b"))
	calls := e.turn.callsMade()
	if calls[0].Model != "sonnet" || calls[1].Model != "opus" {
		t.Errorf("models = %q, %q; want sonnet then opus", calls[0].Model, calls[1].Model)
	}
	if calls[0].Attribution != "harness:architecture-assistant:p1" {
		t.Errorf("attribution = %q", calls[0].Attribution)
	}
	if calls[0].Dir != "/work/harness" {
		t.Errorf("dir = %q", calls[0].Dir)
	}
}

func TestTheContextHoldsEverythingTheAssistantNeedsAndTheMessageComesLast(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 60; i++ {
		e.events = append(e.events, domain.ArchitectureRuleEvent{ID: fmt.Sprintf("ev%02d", i), RuleID: "stay-in-your-layer", Path: fmt.Sprintf("file%02d.go", i), Detail: "detail-" + fmt.Sprint(i)})
	}
	r := req("MY-QUESTION")
	r.RuleEventID = "ev55"
	if _, err := e.run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	in := e.turn.callsMade()[0].Input
	for _, want := range []string{
		"SAVED-PATTERN", "DRAFT-PATTERN", "go-developer", "writes Go", "postgres", "/opt/tools",
		"DISCOVERED-AGENT", "CLAUDE.md", `"$schema"`, "GLOSSARY-BODY", "ADR: 0032 A repository architecture is a structured record",
		"ev00", "ev49", "ev55", "detail-55",
	} {
		if !strings.Contains(in, want) {
			t.Errorf("the input lacks %q", want)
		}
	}
	for _, not := range []string{"hunter2", "ADR-BODY-NOT-SENT", "SPEC-BODY-NOT-SENT", "file50.go"} {
		if strings.Contains(in, not) {
			t.Errorf("the input has %q", not)
		}
	}
	if strings.LastIndex(in, "MY-QUESTION") < strings.LastIndex(in, "detail-55") {
		t.Errorf("the message does not come last")
	}
}

func TestTheContextIsRebuiltFromEachTurnsDraft(t *testing.T) {
	e := newEnv(t)
	evs, _ := e.run(context.Background(), req("one"))
	r := req("two")
	r.ConversationID = conversationID(t, evs)
	r.Draft = json.RawMessage(`{"version":1,"repository":"harness","pattern":"SECOND-DRAFT"}`)
	if _, err := e.run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if in := e.turn.callsMade()[1].Input; !strings.Contains(in, "SECOND-DRAFT") || strings.Contains(in, "DRAFT-PATTERN\"") {
		t.Errorf("second input = %s", in)
	}
}

func TestTheSystemPromptSaysItOnlyProposesAndHowToWriteAProposal(t *testing.T) {
	e := newEnv(t)
	_, _ = e.run(context.Background(), req("x"))
	sys := e.turn.callsMade()[0].System
	for _, want := range []string{"only propose", "warning", "reason", "layer_access", "unlayered_access", "handoff", "```json"} {
		if !strings.Contains(sys, want) {
			t.Errorf("the system prompt lacks %q", want)
		}
	}
}

func TestInvalidInputIsRefusedBeforeAnything(t *testing.T) {
	e := newEnv(t)
	big := func(r ports.ArchitectureAssistantRequest) ports.ArchitectureAssistantRequest { return r }
	cases := map[string]ports.ArchitectureAssistantRequest{}
	r := req("x")
	r.RepositoryID = ""
	cases["no repository"] = r
	r = req("")
	cases["no message"] = r
	r = req("x")
	r.Draft = nil
	cases["no draft"] = r
	r = req(strings.Repeat("é", 20001))
	cases["long message"] = r
	r = req("x")
	r.Draft = json.RawMessage(`{"pattern":"` + strings.Repeat("a", 1<<20) + `"}`)
	cases["big draft"] = r
	r = req("x")
	r.Draft = json.RawMessage(`not json`)
	cases["draft not json"] = r
	for name, c := range cases {
		_, err := e.run(context.Background(), big(c))
		if codeOf(err) != "INVALID_INPUT" {
			t.Errorf("%s: err = %v, want INVALID_INPUT", name, err)
		}
	}
	if n := len(e.turn.callsMade()); n != 0 {
		t.Errorf("%d turns ran", n)
	}
	if _, err := e.run(context.Background(), req(strings.Repeat("é", 20000))); err != nil {
		t.Errorf("a message of exactly 20,000 characters: %v", err)
	}
}

func TestAnUnknownRepositoryIsRefused(t *testing.T) {
	e := newEnv(t)
	r := req("x")
	r.RepositoryID = "nope"
	if _, err := e.run(context.Background(), r); codeOf(err) != "REPOSITORY_NOT_FOUND" {
		t.Errorf("err = %v", err)
	}
}

func TestAConversationIsNotFoundWhenUnknownForeignOrEnded(t *testing.T) {
	e := newEnv(t)
	evs, _ := e.run(context.Background(), req("one"))
	id := conversationID(t, evs)

	r := req("two")
	r.ConversationID = "never-issued"
	if _, err := e.run(context.Background(), r); codeOf(err) != "CONVERSATION_NOT_FOUND" {
		t.Errorf("unknown: err = %v", err)
	}
	r.ConversationID, r.RepositoryID = id, "r2"
	if _, err := e.run(context.Background(), r); codeOf(err) != "CONVERSATION_NOT_FOUND" {
		t.Errorf("another repository's: err = %v", err)
	}
	e.svc.End(id)
	r.RepositoryID = "r1"
	if _, err := e.run(context.Background(), r); codeOf(err) != "CONVERSATION_NOT_FOUND" {
		t.Errorf("ended: err = %v", err)
	}
}

func TestASecondTurnWhileOneRunsIsBusyAndTheFirstIsNotDisturbed(t *testing.T) {
	e := newEnv(t)
	evs, _ := e.run(context.Background(), req("one"))
	id := conversationID(t, evs)

	started, release := make(chan struct{}), make(chan struct{})
	e.turn.script = func(_ int, _ archassistant.TurnCall, _ archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		close(started)
		<-release
		return archassistant.TurnResult{SessionID: "s1", Text: "done"}, nil
	}
	r := req("two")
	r.ConversationID = id
	done := make(chan error, 1)
	go func() { _, err := e.run(context.Background(), r); done <- err }()
	<-started
	if _, err := e.run(context.Background(), r); codeOf(err) != "CONVERSATION_BUSY" {
		t.Errorf("err = %v, want CONVERSATION_BUSY", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.turn.script = answering("x")
	if _, err := e.run(context.Background(), r); err != nil {
		t.Errorf("after the turn ended: %v", err)
	}
}

func TestAnUnavailableAssistantIsRefusedWithAReasonAndAFixURL(t *testing.T) {
	e := newEnv(t)
	e.peekErr = &domain.StructuredError{Code: "CREDENTIAL_MISSING", Message: "no agent credential"}
	_, err := e.run(context.Background(), req("x"))
	var de *domain.DetailedError
	if codeOf(err) != "ARCHITECTURE_ASSISTANT_UNAVAILABLE" || !errors.As(err, &de) || de.Details.(map[string]string)["fix_url"] == "" {
		t.Fatalf("err = %v", err)
	}
	if a := e.svc.Availability(context.Background(), "r1"); a.Available || a.Reason == "" || a.FixURL == "" {
		t.Errorf("availability = %+v", a)
	}

	e.peekErr = nil
	e.repos["r1"].RootDir = ""
	_, err = e.run(context.Background(), req("x"))
	if codeOf(err) != "ARCHITECTURE_ASSISTANT_UNAVAILABLE" || !strings.Contains(err.Error(), "checkout") {
		t.Errorf("no checkout: err = %v", err)
	}
	if a := e.svc.Availability(context.Background(), "r1"); a.Available || a.FixURL != "/projects/p1/repositories" {
		t.Errorf("availability = %+v", a)
	}
	if a := e.svc.Availability(context.Background(), "r2"); !a.Available {
		t.Errorf("r2 availability = %+v", a)
	}
}

func TestAFailedTurnOrAnEmptyAnswerFailsButTheConversationStaysUsable(t *testing.T) {
	e := newEnv(t)
	evs, _ := e.run(context.Background(), req("one"))
	id := conversationID(t, evs)
	r := req("two")
	r.ConversationID = id

	e.turn.script = func(int, archassistant.TurnCall, archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		return archassistant.TurnResult{}, errors.New("claude exploded")
	}
	if _, err := e.run(context.Background(), r); codeOf(err) != "ARCHITECTURE_ASSISTANT_FAILED" {
		t.Errorf("failed turn: err = %v", err)
	}
	e.turn.script = answering("   ")
	if _, err := e.run(context.Background(), r); codeOf(err) != "ARCHITECTURE_ASSISTANT_FAILED" {
		t.Errorf("empty answer: err = %v", err)
	}
	e.turn.script = answering("fine")
	if _, err := e.run(context.Background(), r); err != nil {
		t.Errorf("the conversation is not usable: %v", err)
	}
}

func TestACancelledTurnEndsWithTheContextErrorAndKeepsTheConversation(t *testing.T) {
	e := newEnv(t)
	evs, _ := e.run(context.Background(), req("one"))
	id := conversationID(t, evs)
	ctx, cancel := context.WithCancel(context.Background())
	e.turn.script = func(_ int, _ archassistant.TurnCall, _ archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		cancel()
		return archassistant.TurnResult{}, ctx.Err()
	}
	r := req("two")
	r.ConversationID = id
	if _, err := e.run(ctx, r); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	e.turn.script = answering("again")
	if _, err := e.run(context.Background(), r); err != nil {
		t.Errorf("the conversation was lost: %v", err)
	}
}

func TestEndingAConversationForgetsItAndDeletesItsTranscriptAndUnknownIDsEndQuietly(t *testing.T) {
	e := newEnv(t)
	evs, _ := e.run(context.Background(), req("one"))
	id := conversationID(t, evs)
	e.svc.End(id)
	e.svc.End(id)
	e.svc.End("never-issued")
	if got := e.turn.deleted; len(got) != 1 || got[0] != "s1" {
		t.Errorf("deleted transcripts = %v, want [s1]", got)
	}
}

func TestSweepingForgetsConversationsIdleForADayAndDeletesTheirTranscripts(t *testing.T) {
	e := newEnv(t)
	old, _ := e.run(context.Background(), req("old"))
	oldID := conversationID(t, old)
	e.now = e.now.Add(23 * time.Hour)
	fresh, _ := e.run(context.Background(), req("fresh"))
	freshID := conversationID(t, fresh)
	e.turn.script = answering("x")

	e.now = e.now.Add(90 * time.Minute) // old: 24h30m idle, fresh: 1h30m
	e.svc.Sweep()

	r := req("again")
	r.ConversationID = oldID
	if _, err := e.run(context.Background(), r); codeOf(err) != "CONVERSATION_NOT_FOUND" {
		t.Errorf("the swept conversation: err = %v", err)
	}
	r.ConversationID = freshID
	if _, err := e.run(context.Background(), r); err != nil {
		t.Errorf("the fresh conversation: %v", err)
	}
	if len(e.turn.deleted) != 1 {
		t.Errorf("deleted transcripts = %v, want one", e.turn.deleted)
	}
}

func TestASweepNeverTouchesARunningTurn(t *testing.T) {
	e := newEnv(t)
	evs, _ := e.run(context.Background(), req("one"))
	id := conversationID(t, evs)
	started, release := make(chan struct{}), make(chan struct{})
	e.turn.script = func(int, archassistant.TurnCall, archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		close(started)
		<-release
		return archassistant.TurnResult{SessionID: "s1", Text: "ok"}, nil
	}
	r := req("two")
	r.ConversationID = id
	done := make(chan error, 1)
	go func() { _, err := e.run(context.Background(), r); done <- err }()
	<-started
	e.now = e.now.Add(48 * time.Hour)
	e.svc.Sweep()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(e.turn.deleted) != 0 {
		t.Errorf("a running conversation was swept: %v", e.turn.deleted)
	}
}

func TestAFirstTurnThatFailsBeforeAnySessionExistsLeavesTheConversationUsableAndTheNextStartsFresh(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	e.turn.script = func(int, archassistant.TurnCall, archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		cancel() // the person pressed Stop before claude reported a session
		return archassistant.TurnResult{}, ctx.Err()
	}
	var id string
	err := e.svc.Run(ctx, req("one"), func(ev ports.ArchitectureAssistantEvent) {
		if c, ok := ev.Data.(ports.ArchitectureAssistantConversation); ok {
			id = c.ConversationID
		}
	})
	if !errors.Is(err, context.Canceled) || id == "" {
		t.Fatalf("err = %v, id = %q", err, id)
	}
	e.turn.script = answering("fresh")
	r := req("two")
	r.ConversationID = id
	if _, err := e.run(context.Background(), r); err != nil {
		t.Fatalf("the conversation is not usable: %v", err)
	}
	calls := e.turn.callsMade()
	if calls[len(calls)-1].ResumeID != "" {
		t.Errorf("resumed %q, want a fresh claude session", calls[len(calls)-1].ResumeID)
	}
	e.svc.End(id)
	if len(e.turn.deleted) != 1 || e.turn.deleted[0] != "s1" {
		t.Errorf("deleted = %v, want the session the second turn made", e.turn.deleted)
	}
}

func TestAConversationEndedWhileItsTurnRunsLeavesNoTranscript(t *testing.T) {
	e := newEnv(t)
	started, release := make(chan struct{}), make(chan struct{})
	e.turn.script = func(int, archassistant.TurnCall, archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		close(started)
		<-release
		return archassistant.TurnResult{SessionID: "late", Text: "ok"}, nil
	}
	var id string
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = e.svc.Run(context.Background(), req("one"), func(ev ports.ArchitectureAssistantEvent) {
			if c, ok := ev.Data.(ports.ArchitectureAssistantConversation); ok {
				id = c.ConversationID
			}
		})
	}()
	<-started
	e.svc.End(id)
	close(release)
	<-done
	if len(e.turn.deleted) != 1 || e.turn.deleted[0] != "late" {
		t.Errorf("deleted = %v, want the session the turn made after the end", e.turn.deleted)
	}
}

func TestAConversationThatCouldNotReadItsContextIsNotLeftInTheRegistry(t *testing.T) {
	e := newEnv(t)
	e.agentsErr = errors.New("catalog unreadable")
	if _, err := e.run(context.Background(), req("one")); err == nil {
		t.Fatal("expected the context error")
	}
	if n := e.svc.Conversations(); n != 0 {
		t.Errorf("%d conversations left, want 0", n)
	}
}
