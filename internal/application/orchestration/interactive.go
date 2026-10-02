package orchestration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type InteractiveRequest = ports.InteractiveRequest

// StartInteractive provisions and records a session like Start does — its own
// worktree, the task brief and task MCP server, the agent's prompt, skills and
// MCP servers — and returns the AgentSpec that runs it. A RunnerServer session
// is spawned on the server's terminal host here; a RunnerTUI one is the
// client's to run.
func (s *Service) StartInteractive(ctx context.Context, req InteractiveRequest) (*domain.Session, ports.AgentSpec, error) {
	runsOn, err := s.runner(req.RunsOn)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	if req.ProjectID == "" {
		return nil, ports.AgentSpec{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
	}
	if req.TicketID == "" {
		return nil, ports.AgentSpec{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	// prepare checks the project too, but the ticket is needed first (its
	// title names the branch), and a missing project must not read as a
	// ticket that belongs elsewhere.
	if _, err := s.catalog.Projects.GetProject(ctx, req.ProjectID); err != nil {
		return nil, ports.AgentSpec{}, err
	}
	ticket, err := s.ticketIn(req.ProjectID, req.TicketID)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}

	// A task can hold several sessions at once, so the branch carries part of
	// the session id: two sessions on one task must never race for one branch.
	id := llmkit.NewSessionID()
	p, err := s.prepare(prepareInput{
		ID:           id,
		ProjectID:    req.ProjectID,
		RepositoryID: req.RepositoryID,
		AgentID:      req.AgentID,
		ZoneID:       req.ZoneID,
		TicketID:     req.TicketID,
		Branch:       defaultBranchName(ticket.Title) + "-" + id[:8],
		BaseBranch:   req.BaseBranch,
		Model:        req.Model,
		AutoAccept:   req.AutoAccept,
	})
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}

	spec := s.agentSpec(p.cfg, ports.Conversation{ID: id}, req.Prompt)
	created, err := s.sessions.Create(&domain.Session{
		ID:              id,
		ProjectID:       req.ProjectID,
		RepositoryID:    req.RepositoryID,
		WorkspaceID:     p.ws.ID,
		Branch:          p.branch,
		AgentID:         req.AgentID,
		ZoneID:          req.ZoneID,
		TicketID:        req.TicketID,
		Task:            ticket.Title,
		WorkingDir:      p.cfg.WorkingDir,
		Model:           req.Model,
		Status:          domain.SessionRunning,
		AutoRun:         p.permission == llmkit.PermissionBypass,
		Interactive:     true,
		ClaudeSessionID: id,
		RunsOn:          runsOn,
		RunnerHost:      runnerHost(runsOn, req.RunnerHost),
	})
	if err == nil {
		s.adoptCleanup(id, p.cleanup)
		s.publish(id, SessionEvent{Type: "status", Status: domain.SessionRunning, Text: startedText(runsOn), At: time.Now()})
		if runsOn == domain.RunnerServer {
			if err := s.spawn(ctx, id, spec, req.Size); err != nil {
				return nil, ports.AgentSpec{}, err
			}
		}
		return created, spec, nil
	}
	// Not recorded: nothing will ever run in this worktree, and its branch is
	// seconds old with no commits, so Discard (not Delete) is safe.
	if p.cleanup != nil {
		p.cleanup()
	}
	_ = s.workspaces.Discard(p.ws.ID)
	return nil, ports.AgentSpec{}, fmt.Errorf("start interactive session: %w", err)
}

// ResumeInteractive reopens an ended interactive session's conversation in
// its worktree. The agent, skills and MCP servers are resolved again, so the
// resumed CLI runs with the configuration they have now.
//
// The permission mode is not stored, only whether the session ran unattended:
// a session that did resumes in bypass mode, any other in the default mode.
func (s *Service) ResumeInteractive(ctx context.Context, req ports.ResumeRequest) (*domain.Session, ports.AgentSpec, error) {
	runsOn, err := s.runner(req.RunsOn)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	sess, err := s.interactiveSession(req.SessionID)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	if !sess.Status.IsTerminal() {
		return nil, ports.AgentSpec{}, &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session is still running"}
	}
	claudeID := sess.ClaudeSessionID
	if claudeID == "" {
		claudeID = sess.ID
	}
	if s.Transcripts != nil {
		if err := s.Transcripts.CanResume(sess.WorkingDir, claudeID); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}

	var ticket *domain.Ticket
	if sess.TicketID != "" && s.tickets != nil {
		ticket = s.tickets.Get(sess.TicketID)
	}
	ag, err := s.resolveAgent(sess.AgentID)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	permission := llmkit.PermissionAsk
	if sess.AutoRun {
		permission = llmkit.PermissionBypass
	}
	cfg := s.sessionConfig(sess.ID, sess.WorkingDir, sess.Model, nil, permission, ag, ticket, sess.ZoneID)
	spec := s.agentSpec(cfg, ports.Conversation{ID: claudeID, Resume: true}, "")
	err = s.sessions.UpdateRunner(sess.ID, runsOn, runnerHost(runsOn, req.RunnerHost))
	if err == nil {
		err = s.sessions.UpdateStatus(sess.ID, domain.SessionRunning)
	}
	if err != nil {
		if ag.cleanup != nil {
			ag.cleanup()
		}
		return nil, ports.AgentSpec{}, err
	}
	s.adoptCleanup(sess.ID, ag.cleanup)
	s.publish(sess.ID, SessionEvent{Type: "status", Status: domain.SessionRunning, Text: "resumed" + strings.TrimPrefix(startedText(runsOn), "started"), At: time.Now()})
	if runsOn == domain.RunnerServer {
		if err := s.spawn(ctx, sess.ID, spec, req.Size); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}
	return s.sessions.Get(sess.ID), spec, nil
}

// runner checks where a session is asked to run; empty means RunnerTUI, the
// only place sessions ran before the server could host them.
func (s *Service) runner(r domain.Runner) (domain.Runner, error) {
	switch {
	case r == "":
		return domain.RunnerTUI, nil
	case !r.Valid():
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "runs_on must be server or tui"}
	case r == domain.RunnerServer && s.Terminals == nil:
		return "", &domain.StructuredError{Code: "SERVER_HOSTING_UNAVAILABLE", Message: "this server does not run agents itself"}
	}
	return r, nil
}

func runnerHost(r domain.Runner, host string) string {
	if r == domain.RunnerTUI {
		return host
	}
	return ""
}

func startedText(r domain.Runner) string {
	if r == domain.RunnerServer {
		return "started on the server"
	}
	return "started in a terminal"
}

// spawn runs a RunnerServer session on the server's terminal host. The
// host's exit callback ends it; if it cannot start, it is ended as failed so
// it does not linger as running.
func (s *Service) spawn(ctx context.Context, id string, spec ports.AgentSpec, size ports.TermSize) error {
	if size.Cols <= 0 || size.Rows <= 0 {
		size = ports.TermSize{Cols: 120, Rows: 40}
	}
	err := s.Terminals.Spawn(ctx, id, spec, size, func(code int) {
		_, _ = s.end(id, code, false)
	})
	if err != nil {
		_, _ = s.end(id, 127, false)
		return fmt.Errorf("run the session on the server: %w", err)
	}
	return nil
}

// EndInteractive records that a RunnerTUI session's CLI is gone. The server
// ends its own sessions, so a RunnerServer one answers SESSION_RUNS_ON_SERVER.
func (s *Service) EndInteractive(ctx context.Context, id string, exitCode int, closedByUser bool) (*domain.Session, error) {
	sess, err := s.interactiveSession(id)
	if err != nil {
		return nil, err
	}
	if sess.RunsOn == domain.RunnerServer {
		return nil, &domain.StructuredError{Code: "SESSION_RUNS_ON_SERVER", Message: "the server runs this session and ends it itself"}
	}
	return s.end(id, exitCode, closedByUser)
}

// end records that an interactive session's CLI is gone: done on a clean
// exit, failed on any other, stopped when the user closed it. Ending a
// session that already ended changes nothing.
func (s *Service) end(id string, exitCode int, closedByUser bool) (*domain.Session, error) {
	sess, err := s.interactiveSession(id)
	if err != nil {
		return nil, err
	}
	s.runCleanup(id)
	if sess.Status.IsTerminal() {
		return sess, nil
	}

	status, text := domain.SessionDone, "exited"
	switch {
	case closedByUser:
		status, text = domain.SessionStopped, "closed"
	case exitCode != 0:
		status, text = domain.SessionFailed, fmt.Sprintf("exited with status %d", exitCode)
	}
	if err := s.sessions.UpdateStatus(id, status); err != nil {
		return nil, err
	}
	s.publish(id, SessionEvent{Type: "done", Status: status, Text: text, At: time.Now()})
	return s.sessions.Get(id), nil
}

// RecordClaudeSession stores the claude conversation an interactive session is
// now in. The CLI's SessionStart hook calls it on startup, resume, /clear and
// compaction; only /clear actually moves the id, but recording every report
// keeps this independent of which ones do.
func (s *Service) RecordClaudeSession(_ context.Context, id, claudeSessionID string) error {
	if claudeSessionID == "" {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "claude session id is required"}
	}
	sess, err := s.interactiveSession(id)
	if err != nil {
		return err
	}
	if sess.ClaudeSessionID == claudeSessionID {
		return nil
	}
	if err := s.sessions.UpdateClaudeSessionID(id, claudeSessionID); err != nil {
		return err
	}
	s.publish(id, SessionEvent{Type: "status", Text: "conversation " + claudeSessionID, At: time.Now()})
	return nil
}

func (s *Service) interactiveSession(id string) (*domain.Session, error) {
	sess := s.sessions.Get(id)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	if !sess.Interactive {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_INTERACTIVE", Message: "session is not interactive"}
	}
	return sess, nil
}

// AttachTerminal returns the terminal a RunnerServer session runs on.
func (s *Service) AttachTerminal(_ context.Context, id string) (ports.Terminal, error) {
	sess, err := s.interactiveSession(id)
	if err != nil {
		return nil, err
	}
	if sess.RunsOn != domain.RunnerServer {
		return nil, &domain.StructuredError{Code: "SESSION_RUNS_ON_TUI", Message: "this session runs in the client that started it"}
	}
	notRunning := &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "session is not running"}
	if sess.Status.IsTerminal() || s.Terminals == nil {
		return nil, notRunning
	}
	t, err := s.Terminals.Attach(id)
	if err != nil {
		return nil, notRunning
	}
	return t, nil
}

// stopHosted stops a RunnerServer session: recorded as stopped first, so the
// exit its kill causes does not read as a failure.
func (s *Service) stopHosted(id string) error {
	if _, err := s.end(id, 0, true); err != nil {
		return err
	}
	if s.Terminals == nil {
		return nil
	}
	if t, err := s.Terminals.Attach(id); err == nil {
		return t.Kill()
	}
	return nil
}

// StopOrphanedServerSessions ends the RunnerServer sessions recorded as
// running that have no terminal on this server — at boot, every one of them:
// their processes died with the previous server. It returns how many it
// ended.
func (s *Service) StopOrphanedServerSessions(ctx context.Context) (int, error) {
	running, err := s.List(ctx, ports.SessionFilter{Statuses: []domain.SessionStatus{domain.SessionRunning}})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sess := range running {
		if !sess.Interactive || sess.RunsOn != domain.RunnerServer {
			continue
		}
		if s.Terminals != nil {
			if _, err := s.Terminals.Attach(sess.ID); err == nil {
				continue
			}
		}
		if _, err := s.end(sess.ID, 0, true); err == nil {
			n++
		}
	}
	return n, nil
}

// StopServerSessions stops every RunnerServer session still running,
// recording each as stopped. The server calls it on shutdown.
func (s *Service) StopServerSessions(ctx context.Context) {
	running, err := s.List(ctx, ports.SessionFilter{Statuses: []domain.SessionStatus{domain.SessionRunning}})
	if err != nil {
		return
	}
	for _, sess := range running {
		if sess.Interactive && sess.RunsOn == domain.RunnerServer {
			_ = s.stopHosted(sess.ID)
		}
	}
}

// rejectInteractive guards the operations that drive a CLI over stream-json.
// An interactive session's CLI belongs to a terminal, so they cannot reach it.
func (s *Service) rejectInteractive(id string) error {
	if sess := s.sessions.Get(id); sess != nil && sess.Interactive {
		return &domain.StructuredError{Code: "SESSION_INTERACTIVE", Message: "session runs in a terminal; use it there"}
	}
	return nil
}

// adoptCleanup makes id own a skill plugin dir, removed when the session ends.
func (s *Service) adoptCleanup(id string, cleanup func()) {
	if cleanup == nil {
		return
	}
	s.mu.Lock()
	prev := s.cleanups[id]
	s.cleanups[id] = cleanup
	s.mu.Unlock()
	if prev != nil {
		prev()
	}
}

// agentSpec is what a terminal host needs to run the interactive CLI for cfg.
// It carries what the headless session gets, minus the approval server: in a
// terminal the agent asks its questions itself. HookURL lets the CLI report
// the conversation it is in; the agent adapter turns all of it into flags.
func (s *Service) agentSpec(cfg llmkit.SessionConfig, conv ports.Conversation, prompt string) ports.AgentSpec {
	spec := ports.AgentSpec{
		Kind:         "claude",
		SessionID:    cfg.ID,
		Dir:          cfg.WorkingDir,
		Model:        cfg.Model,
		AppendSystem: cfg.AppendSystem,
		AllowedTools: cfg.AllowedTools,
		AddDirs:      cfg.AddDirs,
		Prompt:       prompt,
		Env:          cfg.Env,
		Conversation: conv,
	}
	switch cfg.Permission {
	case llmkit.PermissionAcceptEdits:
		spec.Permission = "accept_edits"
	case llmkit.PermissionBypass:
		spec.Permission = "bypass"
	}
	for _, sv := range cfg.MCPServers {
		spec.MCPServers = append(spec.MCPServers, ports.MCPServerSpec{
			Name: sv.Name, Transport: sv.Transport, Command: sv.Command, URL: sv.URL,
			Args: sv.Args, Env: sv.Env, Headers: sv.Headers,
		})
	}
	if dc, ok := cfg.Driver.(claude.Config); ok {
		spec.SkillDirs = dc.SkillDirs
	}
	if s.SessionHookURL != nil {
		spec.HookURL = s.SessionHookURL(cfg.ID)
	}
	return spec
}
