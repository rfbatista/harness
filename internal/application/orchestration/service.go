package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/approval"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service satisfies every driving port of this package, checked at compile time.
var _ ports.Orchestration = (*Service)(nil)

// StartRequest is the headless start request; see ports.StartRequest.
type StartRequest = ports.StartRequest

// Catalog is what the orchestration reads from the catalog contexts to set a
// session up: its project and repository, the agent it runs as, the MCP
// servers that agent attaches, and the zone that scopes it.
type Catalog struct {
	Projects interface {
		GetProject(ctx context.Context, id string) (*domain.Project, error)
	}
	Repositories ports.RepositoryReader
	Agents       interface {
		GetAgent(ctx context.Context, id string) (*domain.Agent, error)
		ResolveAgentRelations(a *domain.Agent)
	}
	MCPServers interface{ ListMCPServers() []*domain.MCPServer }
	Zones      ports.ZoneReader
}

// ticketResolver is the slice of the planning ticket store the orchestration
// needs to validate spawn-into-ticket requests. ports.TicketRepository satisfies it.
type ticketResolver interface {
	Get(id string) *domain.Ticket
}

type Service struct {
	runtime    llmkit.Manager
	broker     *approval.Broker
	hub        *Hub
	feed       feed // a project's session changes, for its followers
	sessions   ports.SessionRepository
	catalog    Catalog
	tickets    ticketResolver
	workspaces ports.WorkspaceProvisioner

	DefaultEnv []string // test-only: extra env for spawned sessions

	// TaskServerURL returns the per-session task MCP endpoint for a session id.
	// Injected, so the application layer does not need to know the route.
	TaskServerURL func(sessionID string) string
	// SessionHookURL returns where an interactive session's SessionStart hook
	// reports its conversation id. Nil leaves the hook out.
	SessionHookURL func(sessionID string) string
	// Terminals hosts RunnerServer interactive sessions. Nil means this
	// server does not run agents itself; such sessions answer
	// SERVER_HOSTING_UNAVAILABLE.
	Terminals ports.TerminalHost
	// Events announces SessionDeleted, so other contexts stop what runs in a
	// session's worktree before it goes. Nil announces nothing.
	Events ports.EventPublisher
	// Transcripts checks an interactive session can be resumed. Nil skips the
	// check and lets the CLI report a missing conversation itself.
	Transcripts ports.ClaudeTranscripts

	mu       sync.Mutex
	seq      map[string]int64
	cleanups map[string]func()
	// busy marks sessions with a turn in flight: set when a user message is
	// written to the CLI, cleared by the "result" line that ends the turn. It
	// exists so a late "system"/init — the CLI emits it after initializing,
	// which is routinely *after* the initial task was already sent — cannot
	// report the session as idle while Claude is working on that first turn.
	busy map[string]bool
	// resuming marks sessions a ResumeInteractive call is bringing back, from
	// its status check until the session is recorded running, so two calls at
	// once cannot both pass the check.
	resuming map[string]bool
}

func NewService(runtime llmkit.Manager, broker *approval.Broker, hub *Hub, sessions ports.SessionRepository, catalog Catalog, tickets ticketResolver, workspaces ports.WorkspaceProvisioner) *Service {
	s := &Service{
		runtime: runtime, broker: broker, hub: hub, sessions: sessions, catalog: catalog, tickets: tickets,
		workspaces: workspaces,
		seq:        map[string]int64{}, cleanups: map[string]func(){}, busy: map[string]bool{},
		resuming: map[string]bool{},
	}
	go s.approvalLoop()
	go s.expiryLoop()
	return s
}

func (s *Service) Start(ctx context.Context, req StartRequest) (*domain.Session, error) {
	if req.ProjectID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
	}
	if req.Task == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "task is required"}
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = defaultBranchName(req.Task)
	}

	p, err := s.prepare(prepareInput{
		ProjectID:    req.ProjectID,
		RepositoryID: req.RepositoryID,
		AgentID:      req.AgentID,
		ZoneID:       req.ZoneID,
		TicketID:     req.TicketID,
		Branch:       branch,
		BaseBranch:   req.BaseBranch,
		Model:        req.ModelOverride,
		AllowedTools: req.AllowedTools,
		AutoAccept:   req.AutoAccept,
	})
	if err != nil {
		return nil, err
	}
	// Every failure below this line leaves a worktree and branch behind unless
	// they are removed, so the rollback is a defer cleared on the success path.
	// Discard (not Delete): the branch is seconds old and provably has no
	// commits, so removing it here does not risk losing work, and keeping it
	// would poison a retry with a stale BRANCH_EXISTS.
	provisioned := p.ws
	defer func() {
		if provisioned != nil {
			_ = s.workspaces.Discard(provisioned.ID)
		}
	}()
	cleanup := p.cleanup

	sess, err := s.runtime.Start(ctx, p.cfg)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		// The library reports a missing agent binary as a typed error; the app
		// owns its own error codes, so it is translated here into the one the
		// client already branches on.
		//
		// The message is rebuilt from the error's exported fields rather than
		// taken from its Error() string: the client renders it verbatim to the
		// user, so it must not leak the library's "llmkit:" namespace, and it
		// must read identically to claudetext's message for the same condition.
		var missing *llmkit.BinNotFoundError
		if errors.As(err, &missing) {
			return nil, &domain.StructuredError{
				Code:    "CLAUDE_CLI_NOT_FOUND",
				Message: fmt.Sprintf("Claude CLI not found (%q). Install it with: %s", missing.Bin, missing.InstallHint),
			}
		}
		return nil, fmt.Errorf("start session: %w", err)
	}
	if cleanup != nil {
		s.mu.Lock()
		s.cleanups[sess.ID()] = cleanup
		s.mu.Unlock()
	}

	// A session started in bypass mode is already running unattended, so it
	// starts with the gate open — recording anything else would have the UI
	// offer to "enable" what is already on.
	autoRun := p.permission == llmkit.PermissionBypass
	if autoRun {
		s.broker.SetAutoRun(sess.ID(), true)
	}

	created, err := s.sessions.Create(&domain.Session{
		ID:           sess.ID(),
		ProjectID:    req.ProjectID,
		RepositoryID: req.RepositoryID,
		WorkspaceID:  p.ws.ID,
		Branch:       p.branch,
		AgentID:      req.AgentID,
		ZoneID:       req.ZoneID,
		TicketID:     req.TicketID,
		Task:         req.Task,
		WorkingDir:   p.cfg.WorkingDir,
		Model:        req.ModelOverride,
		Status:       domain.SessionStarting,
		AutoRun:      autoRun,
	})
	if err != nil {
		_ = sess.Stop()
		s.runCleanup(sess.ID())
		return nil, fmt.Errorf("persist session: %w", err)
	}
	provisioned = nil

	go s.pump(sess)

	// Through Send, not sess.Send: the initial task is the session's first turn
	// like any other, so it must mark the session busy (otherwise the CLI's
	// "system"/init line, which lands after this, would report idle mid-turn)
	// and be recorded as a user_message so the timeline opens with what was
	// actually asked.
	if err := s.Send(ctx, sess.ID(), req.Task); err != nil {
		slog.Error("send initial task failed", "session", sess.ID(), "err", err)
	}
	return s.withResumability(created), nil
}

// prepareInput is what every way of starting a session shares: enough to
// provision its worktree and build the configuration its CLI runs with.
type prepareInput struct {
	// ID is the session id; empty mints one. A caller sets it when it needs
	// the id before provisioning, e.g. to name the branch after it.
	ID           string
	ProjectID    string
	RepositoryID string
	AgentID      string
	ZoneID       string
	TicketID     string
	Branch       string
	BaseBranch   string
	Model        string
	AllowedTools []string
	AutoAccept   string
	Mode         domain.SessionMode
}

// prepared is a session whose worktree exists and whose configuration is
// built, but whose CLI has not been launched yet. Until the caller records it,
// the worktree has to be discarded and cleanup run on failure.
type prepared struct {
	cfg        llmkit.SessionConfig
	ws         *domain.Workspace
	branch     string
	permission llmkit.PermissionMode
	cleanup    func() // removes the materialized skill plugin dir; nil if none
}

// prepare validates a start request, provisions the session's worktree and
// builds its CLI configuration. Start launches the result headless;
// StartInteractive hands it to a terminal.
func (s *Service) prepare(in prepareInput) (*prepared, error) {
	if in.ProjectID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
	}
	if _, err := s.catalog.Projects.GetProject(context.TODO(), in.ProjectID); err != nil {
		return nil, err
	}

	var ticket *domain.Ticket
	if in.TicketID != "" {
		tk, err := s.ticketIn(in.ProjectID, in.TicketID)
		if err != nil {
			return nil, err
		}
		ticket = tk
	}

	if in.RepositoryID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "repository_id is required"}
	}
	repo, err := s.catalog.Repositories.GetRepository(context.TODO(), in.RepositoryID)
	if err != nil {
		return nil, err
	}
	if repo.ProjectID != in.ProjectID {
		return nil, &domain.StructuredError{Code: "CROSS_PROJECT_ACCESS", Message: "repository does not belong to project"}
	}
	if repo.RootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir"}
	}
	if s.workspaces == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "workspaces are not available"}
	}
	if err := validateBranchName(in.Branch); err != nil {
		return nil, err
	}

	// Agent and skill resolution happen before provisioning: git worktree add
	// is slow and side-effecting, so a bad agent_id or a broken skill must
	// fail cheaply instead of creating (and then rolling back) a worktree.
	ag, err := s.resolveAgent(in.AgentID, in.Mode)
	if err != nil {
		return nil, err
	}

	ws, err := s.workspaces.Create(in.RepositoryID, domain.Slug(in.Branch), in.Branch, in.BaseBranch)
	if err != nil {
		if ag.cleanup != nil {
			ag.cleanup()
		}
		return nil, err
	}

	id := in.ID
	if id == "" {
		id = llmkit.NewSessionID()
	}
	permission := parseAutoAccept(in.AutoAccept)
	return &prepared{
		cfg:        s.sessionConfig(id, ws.Path, in.Model, in.AllowedTools, permission, ag, ticket, in.ZoneID),
		ws:         ws,
		branch:     in.Branch,
		permission: permission,
		cleanup:    ag.cleanup,
	}, nil
}

// ticketIn loads a ticket and checks it belongs to the project.
func (s *Service) ticketIn(projectID, ticketID string) (*domain.Ticket, error) {
	if s.tickets == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "tickets are not available"}
	}
	tk := s.tickets.Get(ticketID)
	if tk == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	if tk.ProjectID != projectID {
		return nil, &domain.StructuredError{Code: "CROSS_PROJECT_ACCESS", Message: "ticket does not belong to project"}
	}
	return tk, nil
}

// resolvedAgent is an agent template made ready to run: its prompt and its
// skills materialized as a plugin dir.
type resolvedAgent struct {
	agent        *domain.Agent // nil for plain claude
	appendSystem string
	skillDirs    []string
	cleanup      func() // removes skillDirs; nil if none
}

// resolveAgent loads the agent a session runs as, plus the skills its mode
// brings. An empty id is plain claude.
func (s *Service) resolveAgent(agentID string, mode domain.SessionMode) (resolvedAgent, error) {
	var r resolvedAgent
	skills, err := modeSkills(mode)
	if err != nil {
		return r, err
	}
	if agentID != "" {
		a, err := s.catalog.Agents.GetAgent(context.TODO(), agentID)
		if err != nil {
			return r, err
		}
		r.agent = a
		s.catalog.Agents.ResolveAgentRelations(r.agent)
		if r.agent.Prompt != nil {
			r.appendSystem = r.agent.Prompt.Content
		}
		skills = append(slices.Clone(r.agent.Skills), skills...)
	}
	if len(skills) > 0 {
		dirs, c, err := resolveSkillDirs(skills)
		if err != nil {
			return r, fmt.Errorf("resolve skills: %w", err)
		}
		if len(dirs) > 0 {
			r.skillDirs, r.cleanup = dirs, c
		}
	}
	return r, nil
}

// sessionConfig builds the CLI configuration shared by headless and
// interactive sessions: the agent's prompt and skills, the task brief and
// task MCP server, the agent's MCP servers and the zone's extra dirs.
func (s *Service) sessionConfig(id, dir, model string, allowedTools []string, permission llmkit.PermissionMode, ag resolvedAgent, ticket *domain.Ticket, zoneID string) llmkit.SessionConfig {
	cfg := llmkit.SessionConfig{
		ID:           id,
		WorkingDir:   dir,
		Model:        model,
		AllowedTools: allowedTools,
		Permission:   permission,
		Env:          s.DefaultEnv,
		AppendSystem: ag.appendSystem,
		Driver:       claude.Config{SkillDirs: ag.skillDirs},
	}

	// After the agent's own prompt, so the task brief lands at the end of the
	// system prompt rather than being overwritten by it. The id is minted
	// before this because the per-session task MCP server URL is built from it.
	var taskURL string
	if s.TaskServerURL != nil {
		taskURL = s.TaskServerURL(id)
	}
	applyTaskContext(&cfg, ticket, taskURL)

	for _, m := range resolveAttachedMCPServers(ag.agent, s.catalog.MCPServers.ListMCPServers()) {
		cfg.MCPServers = append(cfg.MCPServers, llmkit.MCPServerSpec{
			Name:      domain.Slug(m.Name),
			Transport: m.Transport,
			Command:   m.Command,
			URL:       m.URL,
			Args:      m.Args,
			Env:       m.Env,
			Headers:   m.Headers,
		})
	}

	if zoneID != "" {
		if z := s.catalog.Zones.GetZone(zoneID); z != nil {
			cfg.AddDirs = append(cfg.AddDirs, z.ExplicitPaths...)
		}
	}
	return cfg
}

func (s *Service) pump(sess llmkit.Session) {
	id := sess.ID()
	for ce := range sess.Events() {
		ev, status, hasStatus := fromAgentEvent(ce)
		ev.SessionID = id
		if st, ok := s.turnBoundary(id, ce); ok {
			status, hasStatus = st, true
		}

		logSessionEvent(id, ev)

		if ev.Type == "output_delta" {
			s.publishDelta(id, ev)
			continue
		}

		if cur := s.sessions.Get(id); cur != nil {
			cost, in, out, last, pend := cur.CostUSD, cur.InputTokens, cur.OutputTokens, cur.LastAction, cur.PendingApprovals
			switch ev.Type {
			case "output":
				if ev.Text != "" {
					last = truncate(ev.Text, 200)
				}
			case "tool_use":
				last = "tool: " + ev.ToolName
			case "usage":
				cost += ev.CostUSD
				if ev.Usage != nil {
					in += ev.Usage.InputTokens
					out += ev.Usage.OutputTokens
				}
			}
			// A dead session has no outstanding decisions: broker.DenyAll has
			// answered whatever was pending, and a lingering count would leave
			// the session owing the user a decision forever.
			if hasStatus && status.IsTerminal() {
				pend = 0
			}
			_ = s.sessions.UpdateMetrics(id, cost, in, out, last, pend)
		}
		if hasStatus {
			_ = s.sessions.UpdateStatus(id, status)
			ev.Status = status
		}
		s.publish(id, ev)
	}
	s.runCleanup(id)
}

// turnBoundary resolves the two statuses that depend on whether a turn is in
// flight, and keeps the busy flag in step. A "result" line ends the turn and
// hands control back to the user (idle); a "system"/init line means the CLI is
// ready, but only reports idle when no turn is already running.
func (s *Service) turnBoundary(id string, ce llmkit.Event) (domain.SessionStatus, bool) {
	switch {
	case ce.Type == llmkit.EventResult:
		s.setBusy(id, false)
		return domain.SessionIdle, true
	case ce.Type == llmkit.EventSystem && ce.Subtype == "init":
		if s.isBusy(id) {
			return "", false
		}
		return domain.SessionIdle, true
	default:
		return "", false
	}
}

func (s *Service) setBusy(id string, busy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if busy {
		s.busy[id] = true
		return
	}
	delete(s.busy, id)
}

func (s *Service) isBusy(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy[id]
}

// logSessionEvent emits one structured slog line per Claude event. Streaming
// token deltas are high-volume and logged at Debug; each meaningful message
// (assistant text, tool use/result, usage, lifecycle) is logged at Info, and
// errors at Error. Text is truncated so logs stay readable.
func logSessionEvent(id string, ev SessionEvent) {
	switch ev.Type {
	case "output_delta":
		slog.Debug("claude delta", "session", id, "kind", ev.DeltaKind, "index", ev.Index, "len", len(ev.Text))
	case "output":
		slog.Info("claude message", "session", id, "type", ev.Type, "text", truncate(ev.Text, 200))
	case "tool_use":
		slog.Info("claude message", "session", id, "type", ev.Type, "tool", ev.ToolName)
	case "tool_result":
		slog.Info("claude message", "session", id, "type", ev.Type, "len", len(ev.Text))
	case "usage":
		var in, out int
		if ev.Usage != nil {
			in, out = ev.Usage.InputTokens, ev.Usage.OutputTokens
		}
		slog.Info("claude message", "session", id, "type", ev.Type, "cost_usd", ev.CostUSD, "in", in, "out", out)
	case "error":
		slog.Error("claude message", "session", id, "type", ev.Type, "text", truncate(ev.Text, 200))
	default:
		slog.Info("claude message", "session", id, "type", ev.Type)
	}
}

func (s *Service) approvalLoop() {
	for req := range s.broker.Requests() {
		s.handleApproval(req)
	}
}

func (s *Service) expiryLoop() {
	for e := range s.broker.Expiries() {
		s.handleExpiry(e)
	}
}

// handleExpiry retracts an approval nobody can answer any more. It is
// handleApproval in reverse: the count goes back down, the session leaves
// waiting_approval, and the timeline records that the request died undecided
// rather than leaving a decision surface pointing at a dead request.
//
// The status goes back to thinking, not idle: the CLI got a tool error and
// keeps working. A session that actually died gets the truth from its own exit
// event moments later.
func (s *Service) handleExpiry(e approval.Expiry) {
	s.releaseApproval(e.SessionID)
	s.publish(e.SessionID, SessionEvent{
		Type:     "approval_expired",
		Status:   domain.SessionThinking,
		ToolName: e.ToolName,
		Text:     e.Reason,
		Approval: &ApprovalInfo{ReqID: e.ReqID, ToolName: e.ToolName},
		At:       time.Now(),
	})
}

func (s *Service) handleApproval(req approval.Request) {
	_ = s.sessions.UpdateStatus(req.SessionID, domain.SessionWaitingApproval)
	if cur := s.sessions.Get(req.SessionID); cur != nil {
		_ = s.sessions.UpdateMetrics(req.SessionID, cur.CostUSD, cur.InputTokens, cur.OutputTokens, cur.LastAction, cur.PendingApprovals+1)
	}
	s.publish(req.SessionID, SessionEvent{
		Type:     "approval_needed",
		Status:   domain.SessionWaitingApproval,
		ToolName: req.ToolName,
		Approval: &ApprovalInfo{
			ReqID:     req.ID,
			ToolName:  req.ToolName,
			Input:     req.Input,
			Questions: ParseQuestions(req.ToolName, req.Input),
		},
		At: time.Now(),
	})
}

// Send writes one user message into the live CLI process, starting a new turn.
// The message is published as its own "user_message" event — not as "output",
// which is Claude's own text — so the timeline can tell the two apart.
func (s *Service) Send(ctx context.Context, id, text string) error {
	if err := s.rejectInteractive(id); err != nil {
		return err
	}
	sess, ok := s.runtime.Get(id)
	if !ok {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not running"}
	}
	if err := sess.Send(ctx, text); err != nil {
		return err
	}
	s.setBusy(id, true)
	_ = s.sessions.UpdateStatus(id, domain.SessionThinking)
	s.publish(id, SessionEvent{
		Type:   "user_message",
		Status: domain.SessionThinking,
		Text:   text,
		At:     time.Now(),
	})
	return nil
}

func (s *Service) Resolve(ctx context.Context, id, reqID string, allow bool, msg string) error {
	text := "denied"
	if allow {
		text = "allowed"
	}
	return s.decide(id, reqID, approval.Decision{Allow: allow, Message: msg}, text)
}

// Answer resolves a pending AskUserQuestion request with the user's selections.
//
// It is an allow decision like any other — the difference is what the agent
// reads back: the answers ride on the tool's updated input rather than the
// decision itself. Skipping a question is not this method; it is a plain
// Resolve with allow=false, which is what tells the agent the questions went
// unanswered.
func (s *Service) Answer(ctx context.Context, id, reqID string, answers, notes map[string]string) error {
	if len(answers) == 0 {
		return &domain.StructuredError{Code: "NO_ANSWERS", Message: "no answers provided"}
	}
	d := approval.Decision{Allow: true, Answers: answers, Notes: notes}
	return s.decide(id, reqID, d, answerSummary(answers))
}

// SetAutoRun opens or closes the session's permission gate while it runs.
//
// Switching it on flushes whatever was already waiting: the user has just said
// they no longer want to make these decisions, so leaving the agent blocked on
// one would be an odd reading of the switch they flipped. Each flushed request
// gets the same bookkeeping a manual decision would — the broker had already
// published them, so a client is still rendering a decision surface for each.
//
// AskUserQuestion is never flushed and never auto-allowed. Auto-run grants
// permission; it cannot answer a question on the user's behalf.
func (s *Service) SetAutoRun(ctx context.Context, id string, autoRun bool) error {
	if s.sessions.Get(id) == nil {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	if err := s.rejectInteractive(id); err != nil {
		return err
	}

	flushed := s.broker.SetAutoRun(id, autoRun)
	if err := s.sessions.UpdateAutoRun(id, autoRun); err != nil {
		return err
	}

	text := "auto-run disabled"
	if autoRun {
		text = "auto-run enabled"
	}
	// No Status: the gate is not a lifecycle state, and stamping one here would
	// make the timeline claim a transition that never happened.
	s.publish(id, SessionEvent{
		Type:    "auto_run",
		Text:    text,
		AutoRun: &autoRun,
		At:      time.Now(),
	})

	for _, req := range flushed {
		s.releaseApproval(id)
		s.publish(id, SessionEvent{
			Type:     "approval_resolved",
			Status:   domain.SessionThinking,
			Text:     "auto-approved",
			ToolName: req.ToolName,
			Approval: &ApprovalInfo{ReqID: req.ID, ToolName: req.ToolName},
			At:       time.Now(),
		})
	}
	return nil
}

// decide delivers one decision to the waiting broker and records it. Shared by
// Resolve and Answer so the two cannot drift on the bookkeeping that follows a
// decision — which is what actually unblocks the session.
func (s *Service) decide(id, reqID string, d approval.Decision, text string) error {
	if err := s.rejectInteractive(id); err != nil {
		return err
	}
	if err := s.broker.Resolve(id, reqID, d); err != nil {
		return &domain.StructuredError{Code: "NO_PENDING_APPROVAL", Message: err.Error()}
	}
	s.releaseApproval(id)
	s.publish(id, SessionEvent{
		Type:     "approval_resolved",
		Status:   domain.SessionThinking,
		Text:     text,
		Approval: &ApprovalInfo{ReqID: reqID},
		At:       time.Now(),
	})
	return nil
}

// releaseApproval is the bookkeeping every outstanding approval ends with,
// whether it was decided or expired: one fewer pending decision, and the
// session back to working rather than idle — idle would wrongly invite the user
// to type mid-turn.
func (s *Service) releaseApproval(id string) {
	if cur := s.sessions.Get(id); cur != nil {
		pend := cur.PendingApprovals
		if pend > 0 {
			pend--
		}
		_ = s.sessions.UpdateMetrics(id, cur.CostUSD, cur.InputTokens, cur.OutputTokens, cur.LastAction, pend)
	}
	_ = s.sessions.UpdateStatus(id, domain.SessionThinking)
}

// answerSummary renders the chosen answers for the resolved timeline row.
// Sorted by question text because map order is random and the row would
// otherwise reshuffle between a live event and its replay from the log.
func answerSummary(answers map[string]string) string {
	questions := make([]string, 0, len(answers))
	for q := range answers {
		questions = append(questions, q)
	}
	sort.Strings(questions)

	chosen := make([]string, 0, len(questions))
	for _, q := range questions {
		if v := answers[q]; v != "" {
			chosen = append(chosen, v)
		}
	}
	if len(chosen) == 0 {
		return "answered"
	}
	return "answered: " + strings.Join(chosen, "; ")
}

func (s *Service) Stop(ctx context.Context, id string) error {
	if sess := s.sessions.Get(id); sess != nil && sess.Interactive {
		if sess.RunsOn == domain.RunnerServer {
			return s.stopHosted(id)
		}
		return &domain.StructuredError{Code: "SESSION_RUNS_ON_TUI", Message: "this session runs in the client that started it; close it there"}
	}
	s.broker.DenyAll(id)
	if err := s.runtime.Stop(id); err != nil {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: err.Error()}
	}
	return nil
}

// Delete stops the session's runtime if it is still alive, then removes the
// session and its event log. Deleting a session that was never running (or
// already stopped) is fine — only a missing record is an error.
func (s *Service) Delete(ctx context.Context, id string) error {
	s.broker.DenyAll(id)
	if _, ok := s.runtime.Get(id); ok {
		_ = s.runtime.Stop(id)
	}
	if s.Terminals != nil {
		if t, err := s.Terminals.Attach(id); err == nil {
			_ = t.Kill()
		}
	}
	s.runCleanup(id)
	if s.Events != nil {
		// What else runs in the worktree stops before it is removed; a failure
		// there must not keep the session undeletable.
		_ = s.Events.Publish(ctx, domain.SessionDeleted{SessionID: id})
	}

	// Read the workspace and project before the row is gone.
	workspaceID, projectID := "", ""
	if sess := s.sessions.Get(id); sess != nil {
		workspaceID, projectID = sess.WorkspaceID, sess.ProjectID
	}
	if err := s.sessions.Delete(id); err != nil {
		return err
	}
	if projectID != "" {
		s.feed.send(ports.SessionChange{Session: &domain.Session{ID: id, ProjectID: projectID}, Deleted: true})
	}
	// A worktree that will not go away must not make the session undeletable:
	// the workspace row survives and stays retryable through delete_workspace.
	// The branch is never deleted, so committed work outlives the session.
	if workspaceID != "" && s.workspaces != nil {
		if err := s.workspaces.Delete(workspaceID); err != nil {
			slog.Warn("remove session worktree failed", "session", id, "workspace", workspaceID, "err", err)
		}
	}
	s.mu.Lock()
	delete(s.seq, id)
	s.mu.Unlock()
	return nil
}

func (s *Service) Subscribe(id string) (<-chan SessionEvent, []SessionEvent, func()) {
	return s.hub.Subscribe(id)
}

func (s *Service) History(id string, fromSeq int64) []SessionEvent {
	stored := s.sessions.ListEvents(id, fromSeq)
	out := make([]SessionEvent, 0, len(stored))
	for _, e := range stored {
		var ev SessionEvent
		if err := json.Unmarshal(e.Payload, &ev); err == nil {
			out = append(out, ev)
		}
	}
	return out
}

func (s *Service) Get(_ context.Context, id string) (*domain.Session, error) {
	sess := s.sessions.Get(id)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return s.withResumability(sess), nil
}

func (s *Service) List(_ context.Context, f ports.SessionFilter) ([]*domain.Session, error) {
	list := s.sessions.List(f)
	for _, sess := range list {
		s.withResumability(sess)
	}
	return list, nil
}

func (s *Service) publish(sessionID string, ev SessionEvent) {
	s.mu.Lock()
	s.seq[sessionID]++
	ev.Seq = s.seq[sessionID]
	s.mu.Unlock()

	ev.SessionID = sessionID
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	payload, _ := json.Marshal(ev)
	_ = s.sessions.AppendEvent(sessionID, ev.Seq, ev.Type, payload)
	s.hub.Publish(sessionID, ev)
	s.notify(sessionID, ev.Type)
}

// publishDelta streams an ephemeral token-delta event to live subscribers only:
// no seq, no persistence, not buffered in the replay ring. The persisted
// consolidated "output" event remains authoritative.
func (s *Service) publishDelta(sessionID string, ev SessionEvent) {
	ev.SessionID = sessionID
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	s.hub.PublishEphemeral(sessionID, ev)
}

// Announce records ev on the session's log and fans it out live, exactly as
// the session's own events are: sequenced, persisted, replayed to late
// joiners. Other contexts (artifacts) speak on the stream through it.
func (s *Service) Announce(sessionID string, ev SessionEvent) { s.publish(sessionID, ev) }

func (s *Service) runCleanup(id string) {
	s.mu.Lock()
	c := s.cleanups[id]
	delete(s.cleanups, id)
	delete(s.busy, id)
	s.mu.Unlock()
	if c != nil {
		c()
	}
}

func parseAutoAccept(v string) llmkit.PermissionMode {
	switch v {
	case "edits":
		return llmkit.PermissionAcceptEdits
	case "all":
		return llmkit.PermissionBypass
	default:
		return llmkit.PermissionAsk
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// defaultBranchName derives a session's branch from its task when the caller
// named none. It carries no random suffix, so two identical spawns collide with
// BRANCH_EXISTS instead of silently sharing a branch; the UI always sends a
// suffixed name of its own.
func defaultBranchName(task string) string {
	slug := domain.Slug(task)
	if slug == "" {
		slug = "session"
	}
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	return "agent/" + slug
}

// branchNameChars is every character validateBranchName allows in a branch
// name: alphanumerics plus the punctuation a hierarchical git ref actually
// needs. It is not a full git refname validator (see git-check-ref-format);
// it exists to turn the common typos into a branch-shaped error message
// instead of a raw git failure or an INVALID_NAME that names "workspace name"
// — a field the caller never saw.
var branchNameChars = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// branchNameAlnum requires at least one alphanumeric character: branch names
// built only from the allowed punctuation (e.g. "___") slugify to "", which
// would otherwise surface downstream as workspaces.Create's INVALID_NAME
// "workspace name is required" — again a field the caller never named.
var branchNameAlnum = regexp.MustCompile(`[A-Za-z0-9]`)

// validateBranchName rejects a branch that would otherwise reach git raw
// (a 500 wrapping git's own error text) or reach workspaces.Create's slug
// validation, which speaks in terms of "workspace name" rather than "branch".
func validateBranchName(branch string) error {
	invalid := &domain.StructuredError{Code: "INVALID_NAME", Message: "branch name is not a valid git branch"}
	switch {
	case branch == "":
		return invalid
	case strings.ContainsAny(branch, " \t\n\r"):
		return invalid
	case strings.Contains(branch, ".."):
		return invalid
	case strings.HasPrefix(branch, "/"), strings.HasSuffix(branch, "/"):
		return invalid
	case strings.HasPrefix(branch, "-"), strings.HasSuffix(branch, "-"):
		return invalid
	case strings.HasSuffix(branch, "."), strings.HasSuffix(branch, ".lock"):
		return invalid
	case !branchNameChars.MatchString(branch):
		return invalid
	case !branchNameAlnum.MatchString(branch):
		return invalid
	}
	return nil
}
