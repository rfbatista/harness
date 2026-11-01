// Package archassistant runs the architecture assistant (ADR 0060): a
// conversation in which claude reads a repository and proposes changes to its
// architecture record. Each turn is one claude turn, resumed by the
// conversation's claude session, with read-only code tools. The service keeps
// the conversations only in memory, never stores the record, and holds no
// database writer while a turn runs.
package archassistant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/application/writinghelper"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The codes the assistant's own errors carry.
const (
	CodeUnavailable   = "ARCHITECTURE_ASSISTANT_UNAVAILABLE"
	CodeFailed        = "ARCHITECTURE_ASSISTANT_FAILED"
	CodeNotFound      = "CONVERSATION_NOT_FOUND"
	CodeBusy          = "CONVERSATION_BUSY"
	attribution       = "harness:architecture-assistant:"
	maxMessageLen     = 20000
	maxDraftBytes     = 1 << 20
	eventsInContext   = 50
	askedEventLookup  = 500
	conversationTTL   = 24 * time.Hour
	noCheckoutReason  = "this repository has no local checkout for the assistant to read"
	unavailableFixURL = "/agent-isolation#claude"
)

// TurnCall is one claude turn the service asks for.
type TurnCall struct {
	System, Input, Model, Attribution string
	// Dir is the repository's checkout, the turn's working directory.
	Dir string
	// ResumeID is the claude session to continue, "" to start one.
	ResumeID string
}

// TurnCallbacks receive a turn's text deltas and tool uses as they happen.
type TurnCallbacks struct {
	OnText func(text string)
	// OnTool is given the tool's name and what it works on (a file path or a
	// search pattern).
	OnTool func(tool, target string)
}

// TurnResult is a finished turn: the claude session to resume next time and
// the whole answer.
type TurnResult struct {
	SessionID string
	Text      string
}

// Turn runs the read-only, resumable claude turns, behind the readiness gate
// and the sandbox. Cancelling ctx kills the turn.
type Turn interface {
	Run(ctx context.Context, call TurnCall, cb TurnCallbacks) (TurnResult, error)
	// DeleteTranscript removes a claude session's transcript; a missing one is
	// not an error.
	DeleteTranscript(sessionID string)
}

// Deps are the Service's collaborators. Sandbox, MCP, Documents and Contexts
// may be nil.
type Deps struct {
	Turn     Turn
	Settings ports.SettingsReader
	Repos    interface {
		GetRepository(ctx context.Context, id string) (*domain.Repository, error)
	}
	Architectures interface {
		GetRepositoryArchitecture(ctx context.Context, repositoryID string) (*domain.RepositoryArchitecture, error)
		DiscoverRepositoryArchitecture(ctx context.Context, repositoryID string) (*domain.ArchitectureDiscovery, error)
		ListArchitectureRuleEvents(ctx context.Context, repositoryID string, since time.Time, limit int) ([]domain.ArchitectureRuleEvent, error)
	}
	Agents ports.AgentLister
	MCP    interface{ ListMCPServers() []*domain.MCPServer }
	// Sandbox is the person's sandbox configuration.
	Sandbox interface {
		Enabled() bool
		Paths() []ports.SandboxPath
	}
	// Documents lists the project's documents; the Glossary and the ADR titles
	// come from the project-scoped ones.
	Documents interface {
		ListDocuments(projectID string, scope domain.DocumentScope) []*domain.Document
	}
	Contexts interface {
		ListBoundedContexts(projectID string) []*domain.BoundedContext
	}
	// Peek is the readiness state a page render reads: it must never start
	// anything. A nil Peek means always ready.
	Peek func(ctx context.Context) error
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Service runs the assistant's turns.
type Service struct {
	Deps
	mu    sync.Mutex
	convs map[string]*conversation
}

var _ ports.ArchitectureAssistant = (*Service)(nil)

// conversation is what the server remembers of one conversation.
type conversation struct {
	projectID, repositoryID string
	// session is the claude session the next turn resumes; "" until a turn
	// has finished.
	session string
	last    time.Time
	running bool
	// proposals counts the proposals the conversation made, for their ids.
	proposals int
}

// New returns the service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{Deps: d, convs: map[string]*conversation{}}
}

// Availability says whether a turn could run for the repository now, from the
// readiness state alone.
func (s *Service) Availability(ctx context.Context, repositoryID string) ports.ArchitectureAssistantAvailability {
	repo, err := s.Repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return ports.ArchitectureAssistantAvailability{Reason: "the repository was not found"}
	}
	if err := s.unavailable(ctx, repo); err != nil {
		var de *domain.DetailedError
		if errors.As(err, &de) {
			d, _ := de.Details.(map[string]string)
			return ports.ArchitectureAssistantAvailability{Reason: de.Message, FixURL: d["fix_url"]}
		}
	}
	return ports.ArchitectureAssistantAvailability{Available: true}
}

// unavailable is the refusal for a repository the assistant cannot run on, nil
// when it can.
func (s *Service) unavailable(ctx context.Context, repo *domain.Repository) error {
	if s.Peek != nil {
		if err := s.Peek(ctx); err != nil {
			return unavailableError(err)
		}
	}
	if repo.RootDir == "" {
		return refusal(noCheckoutReason, "/projects/"+repo.ProjectID+"/repositories")
	}
	return nil
}

func refusal(message, fix string) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: CodeUnavailable, Message: message},
		Details:         map[string]string{"fix_url": fix},
	}
}

// unavailableError turns the readiness gate's refusal into the assistant's.
func unavailableError(err error) error {
	if msg, fix, ok := writinghelper.Refusal(err); ok {
		return refusal(msg, fix)
	}
	return refusal(err.Error(), unavailableFixURL)
}

func invalid(msg string) error { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }

func failed(msg string) error { return &domain.StructuredError{Code: CodeFailed, Message: msg} }

func validate(r ports.ArchitectureAssistantRequest) error {
	switch {
	case strings.TrimSpace(r.RepositoryID) == "":
		return invalid("repository_id is required")
	case strings.TrimSpace(r.Message) == "":
		return invalid("message is required")
	case len(r.Draft) == 0 || string(r.Draft) == "null":
		return invalid("draft is required")
	case utf8.RuneCountInString(r.Message) > maxMessageLen:
		return invalid("message is longer than 20000 characters")
	case len(r.Draft) > maxDraftBytes:
		return invalid("draft is larger than 1 MB")
	case !json.Valid(r.Draft):
		return invalid("draft is not valid JSON")
	}
	return nil
}

// Run runs one turn. A refusal before anything ran, an unavailable assistant
// or a failed turn is the returned error; a cancelled ctx returns ctx.Err().
func (s *Service) Run(ctx context.Context, req ports.ArchitectureAssistantRequest, emit func(ports.ArchitectureAssistantEvent)) error {
	if err := validate(req); err != nil {
		return err
	}
	var draft domain.ArchitectureRecord
	if err := json.Unmarshal(req.Draft, &draft); err != nil {
		return invalid("draft is not a repository architecture record")
	}
	draft.Normalize()
	repo, err := s.Repos.GetRepository(ctx, req.RepositoryID)
	if err != nil {
		return err
	}
	if err := s.unavailable(ctx, repo); err != nil {
		return err
	}
	id, conv, err := s.begin(req.ConversationID, repo)
	if err != nil {
		return err
	}
	defer s.finish(conv)

	in, err := s.turnInput(ctx, repo, req)
	if err != nil {
		return err
	}
	emit(ports.ArchitectureAssistantEvent{Name: ports.ArchitectureAssistantEventConversation, Data: ports.ArchitectureAssistantConversation{ConversationID: id}})
	res, err := s.turn(ctx, repo, conv, in, emit, true)
	if err != nil {
		return err
	}
	out, err := s.conclude(ctx, repo, conv, draft, res, emit)
	if err != nil {
		return err
	}
	emit(ports.ArchitectureAssistantEvent{Name: ports.ArchitectureAssistantEventResult, Data: out})
	return nil
}

// turn runs one claude turn of the conversation, passing its text and tool
// uses on, and remembers the claude session it ended in. deltas says whether
// its text is streamed on: a retry's is not, because the result carries the
// corrected answer.
func (s *Service) turn(ctx context.Context, repo *domain.Repository, conv *conversation, input string, emit func(ports.ArchitectureAssistantEvent), deltas bool) (TurnResult, error) {
	s.mu.Lock()
	resume := conv.session
	s.mu.Unlock()
	res, err := s.Turn.Run(ctx, TurnCall{
		System:      systemPrompt,
		Input:       input,
		Model:       domain.ArchitectureAssistantModel(s.Settings.Setting(domain.SettingModelArchitectureAssistant)),
		Attribution: attribution + repo.ProjectID,
		Dir:         repo.RootDir,
		ResumeID:    resume,
	}, TurnCallbacks{
		OnText: func(text string) {
			if !deltas {
				return
			}
			emit(ports.ArchitectureAssistantEvent{Name: ports.ArchitectureAssistantEventDelta, Data: ports.ArchitectureAssistantDelta{Text: text}})
		},
		OnTool: func(tool, target string) {
			emit(ports.ArchitectureAssistantEvent{Name: ports.ArchitectureAssistantEventActivity, Data: ports.ArchitectureAssistantActivity{Tool: tool, Target: target}})
		},
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, ctxErr
		}
		return res, failed("the claude turn failed: " + err.Error())
	}
	if res.SessionID != "" {
		s.mu.Lock()
		conv.session = res.SessionID
		s.mu.Unlock()
	}
	res.Text = strings.TrimSpace(res.Text)
	if res.Text == "" {
		return res, failed("the assistant answered with nothing")
	}
	return res, nil
}

// begin finds or issues the conversation and marks it running.
func (s *Service) begin(id string, repo *domain.Repository) (string, *conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		id = newID()
		c := &conversation{projectID: repo.ProjectID, repositoryID: repo.ID, last: s.Now(), running: true}
		s.convs[id] = c
		return id, c, nil
	}
	c, ok := s.convs[id]
	if !ok || c.repositoryID != repo.ID {
		return "", nil, &domain.StructuredError{Code: CodeNotFound, Message: "the conversation was not found: it was ended, swept or never started here"}
	}
	if c.running {
		return "", nil, &domain.StructuredError{Code: CodeBusy, Message: "a turn of this conversation is still running"}
	}
	c.running = true
	return id, c, nil
}

func (s *Service) finish(c *conversation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.running = false
	c.last = s.Now()
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// End forgets a conversation and deletes its transcript. An unknown id is not
// an error.
func (s *Service) End(conversationID string) {
	s.mu.Lock()
	c, ok := s.convs[conversationID]
	delete(s.convs, conversationID)
	var session string
	if ok {
		session = c.session
	}
	s.mu.Unlock()
	if session != "" {
		s.Turn.DeleteTranscript(session)
	}
}

// Sweep forgets the conversations whose last turn ended more than a day ago,
// and deletes their transcripts. A conversation with a turn running is kept.
func (s *Service) Sweep() {
	var sessions []string
	s.mu.Lock()
	cutoff := s.Now().Add(-conversationTTL)
	for id, c := range s.convs {
		if !c.running && c.last.Before(cutoff) {
			delete(s.convs, id)
			if c.session != "" {
				sessions = append(sessions, c.session)
			}
		}
	}
	s.mu.Unlock()
	for _, session := range sessions {
		s.Turn.DeleteTranscript(session)
	}
}

// Run's sweeper: RunSweeper calls Sweep every interval until ctx ends.
func (s *Service) RunSweeper(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep()
		}
	}
}

// turnInput builds the turn's input: the context block, read now so the
// person's draft and the events are current, then the message. Nothing it
// reads is held after it returns.
func (s *Service) turnInput(ctx context.Context, repo *domain.Repository, req ports.ArchitectureAssistantRequest) (string, error) {
	var b strings.Builder
	block := func(tag, body string) { fmt.Fprintf(&b, "<%s>\n%s\n</%s>\n\n", tag, body, tag) }

	saved, err := s.Architectures.GetRepositoryArchitecture(ctx, repo.ID)
	if err != nil {
		return "", err
	}
	block("saved_record", fmt.Sprintf("version %d (exists: %t)\n%s", saved.Version, saved.Exists, jsonOf(saved.Record)))
	block("draft", fmt.Sprintf("started from saved version %d\n%s", req.DraftBaseVersion, string(req.Draft)))

	agents, err := s.Agents.ListAgents(ctx)
	if err != nil {
		return "", err
	}
	type agentInfo struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	infos := []agentInfo{{Name: domain.PlainAgent, Description: "a session that runs with no agent"}}
	for _, a := range agents {
		infos = append(infos, agentInfo{Name: a.Name, Description: a.Description})
	}
	block("agents", jsonOf(infos))

	// Only names: a server's env and headers can hold secrets.
	type mcpInfo struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Transport   string `json:"transport"`
	}
	servers := []mcpInfo{}
	if s.MCP != nil {
		for _, m := range s.MCP.ListMCPServers() {
			servers = append(servers, mcpInfo{Name: m.Name, Description: m.Description, Transport: m.Transport})
		}
	}
	block("mcp_servers", jsonOf(servers))

	sandbox := map[string]any{"enabled": false, "paths": []ports.SandboxPath{}}
	if s.Sandbox != nil {
		sandbox = map[string]any{"enabled": s.Sandbox.Enabled(), "paths": s.Sandbox.Paths()}
	}
	block("sandbox", jsonOf(sandbox))

	if d, err := s.Architectures.DiscoverRepositoryArchitecture(ctx, repo.ID); err != nil {
		block("discover", "not available: "+err.Error())
	} else {
		block("discover", jsonOf(d))
	}
	block("schema", string(domain.ArchitectureSchemaJSON))

	glossary, adrs := s.projectDocuments(repo.ProjectID)
	block("glossary", glossary)
	block("adr_titles", strings.Join(adrs, "\n"))

	events, err := s.Architectures.ListArchitectureRuleEvents(ctx, repo.ID, time.Time{}, askedEventLookup)
	if err != nil {
		block("rule_events", "not available: "+err.Error())
	} else {
		block("rule_events", jsonOf(events[:min(len(events), eventsInContext)]))
		if req.RuleEventID != "" {
			for _, ev := range events {
				if ev.ID == req.RuleEventID {
					block("asked_event", jsonOf(ev))
					break
				}
			}
		}
	}
	block("message", req.Message)
	return b.String(), nil
}

// projectDocuments is the project's Glossary and the titles of its ADRs.
func (s *Service) projectDocuments(projectID string) (glossary string, adrTitles []string) {
	if s.Documents == nil {
		return "", nil
	}
	for _, d := range s.Documents.ListDocuments(projectID, domain.DocumentScopeProject) {
		switch {
		case strings.HasPrefix(d.Title, "Glossary:"):
			glossary = d.Content
		case strings.HasPrefix(d.Title, "ADR:"):
			adrTitles = append(adrTitles, d.Title)
		}
	}
	return glossary, adrTitles
}

func jsonOf(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "null"
	}
	return string(b)
}
