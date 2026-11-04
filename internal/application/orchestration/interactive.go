package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/rfbatista/harnesskit/errs"
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
	// The mode comes first: only a product-mode session may start with no
	// task, on its project alone (ADR 0016).
	mode, err := domain.ParseSessionMode(req.Mode)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	if req.TicketID == "" && mode.NeedsTask() {
		return nil, ports.AgentSpec{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	if (req.DesignSystemID != "") != (mode == domain.SessionModeDesignSystem) {
		return nil, ports.AgentSpec{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "design_system_id is required by, and only for, a design session"}
	}
	// The project is read before the ticket, though prepare checks it too:
	// a missing project must answer PROJECT_NOT_FOUND, not read as a ticket
	// that belongs elsewhere. The ticket, when there is one, comes next: its
	// title names the branch, so both are in hand before prepare runs.
	project, err := s.catalog.Projects.GetProject(ctx, req.ProjectID)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	var designSystem *domain.DesignSystem
	if mode == domain.SessionModeDesignSystem {
		if designSystem, err = s.designSessionTarget(ctx, project, req.DesignSystemID); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}
	var ticket *domain.Ticket
	if req.TicketID != "" {
		if ticket, err = s.ticketIn(ctx, req.ProjectID, req.TicketID); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}
	if req.StatusCheckMinutes != nil {
		if err := domain.ValidStatusCheckMinutes(*req.StatusCheckMinutes); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}

	// A selection to bring under the new architect is judged before anything
	// is created: nothing exists yet to clean up when it is refused.
	var plan *ports.BringUnderPlan
	if len(req.BringUnderSessionIDs) > 0 {
		if plan, err = s.planBringUnder(ctx, mode, ticket, req.BringUnderSessionIDs); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}

	// A task can hold several sessions at once, so the branch carries part of
	// the session id: two sessions on one task must never race for one branch.
	// A session with no task is named after the product work on its project.
	model := s.sessionModel(mode, req.ParentSessionID, req.Model)
	id := llmkit.NewSessionID()
	task, branch := "Product: "+project.Name, defaultBranchName("product "+project.Name)
	if designSystem != nil {
		task, branch = "Design system: "+designSystem.Name, defaultBranchName("design system "+designSystem.Slug)
	}
	if ticket != nil {
		task, branch = ticket.Title, defaultBranchName(ticket.Title)
	}
	branch += "-" + id[:8]
	named := strings.TrimSpace(req.BranchName)
	if named != "" {
		if err := s.checkNamedBranch(ctx, req.RepositoryID, named, req.TicketID); err != nil {
			return nil, ports.AgentSpec{}, err
		}
		branch = named
	}
	p, err := s.prepare(ctx, prepareInput{
		ID:           id,
		ProjectID:    req.ProjectID,
		RepositoryID: req.RepositoryID,
		AgentID:      req.AgentID,
		ZoneID:       req.ZoneID,
		TicketID:     req.TicketID,
		Branch:       branch,
		BaseBranch:   req.BaseBranch,
		Model:        model,
		AutoAccept:   req.AutoAccept,
		Mode:         mode,
		DesignSystem: designSystem,
		Role:         s.newSessionRole(ctx, mode, req.ParentSessionID),

		BranchChecked: named != "" && s.branchNames != nil,
	})
	if err != nil {
		if named != "" && errs.Code(err) == domain.BranchExists {
			// Taken since the check: the worktree's own refusal, told as the check's.
			if refusal := s.checkNamedBranch(ctx, req.RepositoryID, named, req.TicketID); refusal != nil {
				return nil, ports.AgentSpec{}, refusal
			}
		}
		return nil, ports.AgentSpec{}, err
	}

	opening := modePrompt(mode, ticket, project, req.Prompt)
	if designSystem != nil {
		opening = designSessionPrompt(project, designSystem, req.Prompt)
	}
	if plan != nil {
		opening = s.architectOverPrompt(ctx, ticket, plan.Eligible, req.Prompt)
	}
	spec := s.agentSpec(p.cfg, ports.Conversation{ID: id}, opening)
	spec.EnvFile = p.envFile
	confineSpec(&spec, p.sandbox.policy)
	created, err := s.sessions.Create(&domain.Session{
		ID:              id,
		ProjectID:       req.ProjectID,
		RepositoryID:    req.RepositoryID,
		WorkspaceID:     p.ws.ID,
		Branch:          p.branch,
		AgentID:         req.AgentID,
		ZoneID:          req.ZoneID,
		TicketID:        req.TicketID,
		Task:            task,
		WorkingDir:      p.cfg.WorkingDir,
		Model:           model,
		Status:          domain.SessionRunning,
		AutoRun:         p.permission == llmkit.PermissionBypass,
		Interactive:     true,
		ClaudeSessionID: id,
		RunsOn:          runsOn,
		RunnerHost:      runnerHost(runsOn, req.RunnerHost),
		ParentSessionID: req.ParentSessionID,
		Mode:            mode,
		DesignSystemID:  req.DesignSystemID,

		TokenSavers:             p.savers.active,
		TokenSaversNotInstalled: p.savers.notInstalled,
		CavemanIntensity:        p.savers.cavemanIntensity,
		Sandbox:                 p.sandbox.facts,
	})
	if err == nil {
		s.adoptCleanup(id, withViolations(p.cleanup, s.collectViolations(id, p.cfg.WorkingDir, p.sandbox.facts)))
		s.courier.setTurn(id, startTurn(spec))
		s.publish(id, SessionEvent{Type: "status", Status: domain.SessionRunning, Text: startedText(runsOn), At: time.Now()})
		// Announced before the CLI runs, so the architect channel has the
		// session's status-check loop in place when the start returns.
		s.announce(domain.SessionStarted{SessionID: id, ProjectID: req.ProjectID, TicketID: req.TicketID,
			ParentSessionID: req.ParentSessionID, Mode: mode, StatusCheckMinutes: req.StatusCheckMinutes})
		// A guard that could not be put in at start is announced now that the
		// session is recorded.
		if s.commitCheckReason(id) != "" {
			s.announceCommitCheck(id, false)
		}
		if runsOn == domain.RunnerServer {
			if err := s.spawn(ctx, id, spec, req.Size); err != nil {
				return nil, ports.AgentSpec{}, err
			}
		}
		if plan != nil {
			// The architect is running: the sessions move under it, and each
			// running one is told. A session that stopped being eligible since
			// the check is skipped; the architect is started either way.
			if _, err := s.BringUnder.BringUnderNewArchitect(context.WithoutCancel(ctx), id, req.BringUnderSessionIDs); err != nil {
				slog.Warn("bring sessions under the new architect", "architect", id, "err", err)
			}
		}
		return s.withResumability(ctx, created), spec, nil
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
// resumed CLI runs with the configuration they have now. Its token savers
// are not: it runs with the ones it started with (ADR 0015).
//
// The permission mode is not stored, only whether the session ran unattended:
// a session that did resumes in bypass mode, any other in the default mode.
//
// Concurrent calls for one session resume it once; the others answer
// SESSION_ALREADY_RUNNING. A resume is not held to the task's session limit
// (TASK_SESSION_LIMIT): like starting one from the TUI or the web, it is a
// person's action, and the limit only bounds the sessions agents start. Once
// running, it counts toward that limit again.
func (s *Service) ResumeInteractive(ctx context.Context, req ports.ResumeRequest) (*domain.Session, ports.AgentSpec, error) {
	runsOn, err := s.runner(req.RunsOn)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	if !s.claimResume(req.SessionID) {
		return nil, ports.AgentSpec{}, &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session is being resumed"}
	}
	defer s.releaseResume(req.SessionID)
	sess, err := s.interactiveSession(req.SessionID)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	if err := s.resumeBlock(sess); err != nil {
		return nil, ports.AgentSpec{}, err
	}

	subject := sessionSubject{mode: sess.Mode}
	if sess.TicketID != "" && s.tickets != nil {
		subject.ticket = s.tickets.Get(context.WithoutCancel(ctx), sess.TicketID) // the resume goes on without the request
	}
	if sess.DesignSystemID != "" {
		if subject.designSystem, err = s.designSessionTarget(context.WithoutCancel(ctx), nil, sess.DesignSystemID); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}
	if sess.TicketID == "" && !sess.Mode.NeedsTask() {
		// A session with no task is briefed on its project again, and its
		// shell is the project's: the project has to be there.
		if subject.project, err = s.catalog.Projects.GetProject(context.WithoutCancel(ctx), sess.ProjectID); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}
	ag, err := s.resolveAgent(sess.AgentID, sess.Mode)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	if err := s.checkAssigned(ctx, sess.RepositoryID, ag.agent); err != nil {
		if ag.cleanup != nil {
			ag.cleanup()
		}
		return nil, ports.AgentSpec{}, err
	}
	s.addRepositoryPrompt(ctx, &ag, sess.RepositoryID)
	if err := s.ready(ctx); err != nil {
		if ag.cleanup != nil {
			ag.cleanup()
		}
		return nil, ports.AgentSpec{}, err
	}
	permission := llmkit.PermissionAsk
	if sess.AutoRun {
		permission = llmkit.PermissionBypass
	}
	cfg := s.sessionConfig(sess.ID, sess.WorkingDir, sess.Model, nil, permission, ag, subject, sess.ZoneID, s.sessionRole(ctx, sess.ID), sess.RepositoryID)
	cleanup, err := s.withShell(sess.ID, sess.RepositoryID, subject, &cfg, ag.cleanup)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	cleanup = s.withCommitCheck(sess.ID, sess.WorkingDir, &cfg, true, cleanup)
	envFile, cleanup, err := s.withGit(sess.ID, sess.RepositoryID, &cfg, cleanup)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	s.withAgentCredential(&cfg)
	s.withKeychainShim(&cfg)
	s.withAttribution(&cfg)
	s.rewireTokenSavers(ctx, sess, &cfg)
	// A resume starts under the rules now in force, not the ones it started
	// under, and records them.
	cleanup, err = s.withTempDir(sess.ID, &cfg, cleanup)
	if err != nil {
		return nil, ports.AgentSpec{}, err
	}
	sb, err := s.resumeSandbox(ctx, sess, cfg.Env, ag.skillDirs)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, ports.AgentSpec{}, err
	}
	spec := s.agentSpec(cfg, ports.Conversation{ID: claudeID(sess), Resume: true}, "")
	spec.EnvFile = envFile
	confineSpec(&spec, sb.policy)
	err = s.sessions.UpdateRunner(sess.ID, runsOn, runnerHost(runsOn, req.RunnerHost))
	if err == nil && sb.facts != nil {
		err = s.sessions.UpdateSandbox(sess.ID, *sb.facts)
	}
	if err == nil {
		err = s.sessions.UpdateStatus(sess.ID, domain.SessionRunning)
	}
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, ports.AgentSpec{}, err
	}
	s.adoptCleanup(sess.ID, withViolations(cleanup, s.collectViolations(sess.ID, sess.WorkingDir, sb.facts)))
	s.courier.setTurn(sess.ID, turnIdle) // a resumed conversation waits for its next prompt
	s.publish(sess.ID, SessionEvent{Type: "status", Status: domain.SessionRunning, Text: "resumed" + strings.TrimPrefix(startedText(runsOn), "started"), At: time.Now()})
	if runsOn == domain.RunnerServer {
		if err := s.spawn(ctx, sess.ID, spec, req.Size); err != nil {
			return nil, ports.AgentSpec{}, err
		}
	}
	return s.withResumability(ctx, s.sessions.Get(sess.ID)), spec, nil
}

// resumeSandbox is the sandbox step of a resume: the session's policy under
// the current settings.
func (s *Service) resumeSandbox(ctx context.Context, sess *domain.Session, env, skillDirs []string) (sandboxed, error) {
	repo := &domain.Repository{}
	if s.Sandbox != nil && sess.RepositoryID != "" {
		r, err := s.catalog.Repositories.GetRepository(ctx, sess.RepositoryID)
		if err != nil {
			return sandboxed{}, err
		}
		repo = r
	}
	return s.sandbox(ctx, prepareInput{ProjectID: sess.ProjectID, Mode: sess.Mode}, sess.ID, repo, sess.WorkingDir, env, skillDirs)
}

// claimResume marks id as being resumed; false when another call already is.
func (s *Service) claimResume(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resuming[id] {
		return false
	}
	s.resuming[id] = true
	return true
}

func (s *Service) releaseResume(id string) {
	s.mu.Lock()
	delete(s.resuming, id)
	s.mu.Unlock()
}

// resumeBlock is the error a resume of sess would answer now, nil when it
// would be accepted. Only an ended interactive session reaches the
// filesystem, so listing many sessions stays cheap.
func (s *Service) resumeBlock(sess *domain.Session) error {
	switch {
	case !sess.Interactive:
		return &domain.StructuredError{Code: "SESSION_NOT_INTERACTIVE", Message: "session is not interactive"}
	case !sess.Status.IsTerminal():
		return &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session is still running"}
	case s.Transcripts != nil:
		return s.Transcripts.CanResume(sess.WorkingDir, claudeID(sess))
	}
	return nil
}

// withResumability fills sess's derived fields — Resumable and
// ResumeBlocked, and through the Decorator its role — for a session about to
// leave the service. Nil stays nil.
func (s *Service) withResumability(ctx context.Context, sess *domain.Session) *domain.Session {
	if sess == nil {
		return nil
	}
	s.fillResumability(sess)
	s.fillAssignment(ctx, sess, map[string]bool{})
	if s.Decorator != nil {
		s.Decorator.DecorateSessions(ctx, sess)
	}
	return sess
}

// fillResumability fills sess's Resumable and ResumeBlocked. It reads no
// database: an ended interactive session stats its transcript, nothing else.
func (s *Service) fillResumability(sess *domain.Session) {
	sess.ResumeBlocked = errs.Code(s.resumeBlock(sess))
	sess.Resumable = sess.ResumeBlocked == ""
	sess.CommitCheckOff = s.commitCheckReason(sess.ID)
}

// claudeID is the claude conversation sess resumes into.
func claudeID(sess *domain.Session) string {
	if sess.ClaudeSessionID != "" {
		return sess.ClaudeSessionID
	}
	return sess.ID
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
		return s.withResumability(context.Background(), sess), nil
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
	s.hub.End(id)
	s.announce(domain.SessionEnded{SessionID: id, Status: status})
	return s.withResumability(context.Background(), s.sessions.Get(id)), nil
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
	return inputStamp{Terminal: t, touched: func() { s.courier.touched(id) }}, nil
}

// startTurn is where a just-launched interactive CLI is: working on its first
// prompt, or waiting for one.
func startTurn(spec ports.AgentSpec) turnState {
	if spec.Prompt != "" {
		return turnBusy
	}
	return turnIdle
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
	for _, f := range cfg.InstructionFiles {
		spec.InstructionFiles = append(spec.InstructionFiles, ports.AgentInstructionFile{Kind: string(f.Kind), Path: f.Path, Content: f.Content})
	}
	if dc, ok := cfg.Driver.(claude.Config); ok {
		spec.SkillDirs = dc.SkillDirs
		spec.Hooks = specHooks(dc.Settings)
		if dc.Settings != nil && len(dc.Settings.Extra) > 0 {
			spec.Settings = maps.Clone(dc.Settings.Extra)
		}
	}
	if s.SessionHookURL != nil {
		spec.HookURL = s.SessionHookURL(cfg.ID)
	}
	return spec
}

// agentsMoveDocuments is the setting "Agents move documents to project" now;
// a brief states it as its session starts (ADR 0020).
func (s *Service) agentsMoveDocuments() bool {
	return s.Settings != nil && domain.AgentsMoveDocuments(s.Settings.Setting(domain.SettingAgentsMoveDocuments))
}

// sessionModel is the model a new session starts with: the one its start
// names, else its tier's from the settings (domain.ResolveSessionModel).
func (s *Service) sessionModel(mode domain.SessionMode, parentSessionID, named string) string {
	if named != "" {
		return named
	}
	var architect, implementation string
	if s.Settings != nil {
		architect = s.Settings.Setting(domain.SettingModelArchitect)
		implementation = s.Settings.Setting(domain.SettingModelImplementation)
	}
	return domain.ResolveSessionModel(mode, parentSessionID != "",
		domain.TierModel(architect, domain.DefaultArchitectModel),
		domain.TierModel(implementation, domain.DefaultImplementationModel))
}

// UseBranchNames makes a start with a branch name check it first.
func (s *Service) UseBranchNames(c ports.BranchNameChecker) { s.branchNames = c }

// UseAssignments makes a start or a resume check that the agent is assigned
// to the repository, and a session read say when it is not (ADR 0061).
func (s *Service) UseAssignments(a ports.AgentAssignments) { s.assignments = a }

// checkAssigned is AGENT_NOT_ASSIGNED when the agent is unassigned from the
// repository. A session with no agent, or a server with no assignments, is
// never refused.
func (s *Service) checkAssigned(ctx context.Context, repositoryID string, agent *domain.Agent) error {
	if s.assignments == nil || agent == nil || repositoryID == "" {
		return nil
	}
	return s.assignments.CheckAssigned(ctx, repositoryID, agent.Name)
}

// fillAssignment sets AgentUnassigned, and blocks a resume that would
// otherwise be accepted. seen remembers, within one read, which agents of
// which repositories were asked about.
func (s *Service) fillAssignment(ctx context.Context, sess *domain.Session, seen map[string]bool) {
	if s.assignments == nil || sess.AgentID == "" || sess.RepositoryID == "" {
		return
	}
	key := sess.RepositoryID + "\x00" + sess.AgentID
	unassigned, ok := seen[key]
	if !ok {
		name, found := s.catalogAgentName(ctx, sess.AgentID)
		unassigned = found && s.assignments.IsUnassigned(ctx, sess.RepositoryID, name)
		seen[key] = unassigned
	}
	sess.AgentUnassigned = unassigned
	// A running or headless session keeps the reason it cannot be resumed; an
	// ended interactive one is blocked by its agent, whatever else is wrong.
	if unassigned && sess.Interactive && sess.Status.IsTerminal() {
		sess.ResumeBlocked = "AGENT_NOT_ASSIGNED"
		sess.Resumable = false
	}
}

// catalogAgentName is the name of the agent with the id, and whether the
// catalog has it. It asks for the names only: resolving an agent's prompt,
// skills and servers to learn its name reads the catalog once for each of them.
func (s *Service) catalogAgentName(ctx context.Context, id string) (string, bool) {
	list, err := s.catalog.Agents.ListAgentNames(ctx)
	if err != nil {
		return "", false
	}
	for _, a := range list {
		if a.ID == id {
			return a.Name, true
		}
	}
	return "", false
}

// UseRepositoryPrompts makes a start or a resume of an agent append the
// repository's prompt after the agent's own (ADR 0061).
func (s *Service) UseRepositoryPrompts(p ports.RepositoryPromptSections) { s.repoPrompts = p }

// addRepositoryPrompt appends the agent's repository section to the prompt the
// session starts with. A session with no agent gets nothing.
func (s *Service) addRepositoryPrompt(ctx context.Context, ag *resolvedAgent, repositoryID string) {
	if s.repoPrompts == nil || ag.agent == nil || repositoryID == "" {
		return
	}
	if section := s.repoPrompts.RepositoryPromptSection(ctx, repositoryID, ag.agent.Name); section != "" {
		ag.appendSystem = appendSection(ag.appendSystem, section)
	}
}

// UseBaseBranches makes a start from a named base check it first, and fetch
// it when it is origin's (ADR 0049).
func (s *Service) UseBaseBranches(p ports.BaseBranchPreparer) { s.baseBranches = p }

// checkNamedBranch refuses a name the person typed for a session's branch
// when it is not a branch name, or is one already: with the reason, and for a
// taken name the first free variant. Nothing is created either way. Without a
// checker nothing is refused here.
func (s *Service) checkNamedBranch(ctx context.Context, repositoryID, name, ticketID string) error {
	if s.branchNames == nil {
		return nil
	}
	c, err := s.branchNames.CheckBranchName(ctx, repositoryID, name, ticketID)
	if err != nil {
		return err
	}
	switch c.Code {
	case domain.BranchNameInvalid:
		return &domain.DetailedError{
			StructuredError: &domain.StructuredError{Code: c.Code, Message: c.Code + ": " + c.Reason},
			Details:         map[string]string{"name": name, "reason": c.Reason},
		}
	case domain.BranchExists:
		return &domain.DetailedError{
			StructuredError: &domain.StructuredError{Code: c.Code, Message: c.Code + ": " + c.Reason},
			Details:         map[string]string{"name": name, "suggestion": c.Suggestion},
		}
	}
	return nil
}
