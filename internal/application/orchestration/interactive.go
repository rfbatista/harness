package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type (
	InteractiveRequest = ports.InteractiveRequest
	Launch             = ports.Launch
)

// StartInteractive provisions and records a session like Start does — its own
// worktree, the task brief and task MCP server, the agent's prompt, skills and
// MCP servers — but does not run it. It returns the command line the client
// runs in a terminal it owns.
func (s *Service) StartInteractive(ctx context.Context, req InteractiveRequest) (*domain.Session, Launch, error) {
	if req.ProjectID == "" {
		return nil, Launch{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
	}
	if req.TicketID == "" {
		return nil, Launch{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	// prepare checks the project too, but the ticket is needed first (its
	// title names the branch), and a missing project must not read as a
	// ticket that belongs elsewhere.
	if s.bp.GetProject(req.ProjectID) == nil {
		return nil, Launch{}, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	ticket, err := s.ticketIn(req.ProjectID, req.TicketID)
	if err != nil {
		return nil, Launch{}, err
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
		return nil, Launch{}, err
	}

	args, err := s.interactiveArgs(p.cfg, conversation{id: id}, req.Prompt)
	if err == nil {
		var created *domain.Session
		created, err = s.sessions.Create(&domain.Session{
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
		})
		if err == nil {
			s.adoptCleanup(id, p.cleanup)
			s.publish(id, SessionEvent{Type: "status", Status: domain.SessionRunning, Text: "started in a terminal", At: time.Now()})
			return created, Launch{SessionID: id, Dir: p.cfg.WorkingDir, Args: args, Env: p.cfg.Env}, nil
		}
	}
	// Not recorded: nothing will ever run in this worktree, and its branch is
	// seconds old with no commits, so Discard (not Delete) is safe.
	if p.cleanup != nil {
		p.cleanup()
	}
	_ = s.workspaces.Discard(p.ws.ID)
	return nil, Launch{}, fmt.Errorf("start interactive session: %w", err)
}

// ResumeInteractive reopens an ended interactive session's conversation in
// its worktree. The agent, skills and MCP servers are resolved again, so the
// resumed CLI runs with the configuration they have now.
//
// The permission mode is not stored, only whether the session ran unattended:
// a session that did resumes in bypass mode, any other in the default mode.
func (s *Service) ResumeInteractive(ctx context.Context, id string) (*domain.Session, Launch, error) {
	sess, err := s.interactiveSession(id)
	if err != nil {
		return nil, Launch{}, err
	}
	if !sess.Status.IsTerminal() {
		return nil, Launch{}, &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session is still running"}
	}
	claudeID := sess.ClaudeSessionID
	if claudeID == "" {
		claudeID = sess.ID
	}
	if s.Transcripts != nil {
		if err := s.Transcripts.CanResume(sess.WorkingDir, claudeID); err != nil {
			return nil, Launch{}, err
		}
	}

	var ticket *domain.Ticket
	if sess.TicketID != "" && s.tickets != nil {
		ticket = s.tickets.Get(sess.TicketID)
	}
	ag, err := s.resolveAgent(sess.AgentID)
	if err != nil {
		return nil, Launch{}, err
	}
	permission := llmkit.PermissionAsk
	if sess.AutoRun {
		permission = llmkit.PermissionBypass
	}
	cfg := s.sessionConfig(sess.ID, sess.WorkingDir, sess.Model, nil, permission, ag, ticket, sess.ZoneID)
	args, err := s.interactiveArgs(cfg, conversation{id: claudeID, resume: true}, "")
	if err == nil {
		err = s.sessions.UpdateStatus(sess.ID, domain.SessionRunning)
	}
	if err != nil {
		if ag.cleanup != nil {
			ag.cleanup()
		}
		return nil, Launch{}, err
	}
	s.adoptCleanup(sess.ID, ag.cleanup)
	s.publish(sess.ID, SessionEvent{Type: "status", Status: domain.SessionRunning, Text: "resumed in a terminal", At: time.Now()})
	return s.sessions.Get(sess.ID), Launch{SessionID: sess.ID, Dir: sess.WorkingDir, Args: args, Env: cfg.Env}, nil
}

// EndInteractive records that an interactive session's CLI is gone: done on a
// clean exit, failed on any other, stopped when the user closed the terminal.
// Ending a session that already ended changes nothing.
func (s *Service) EndInteractive(ctx context.Context, id string, exitCode int, closedByUser bool) (*domain.Session, error) {
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
func (s *Service) RecordClaudeSession(id, claudeSessionID string) error {
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

// rejectInteractive guards the operations that drive a CLI the server owns.
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

// conversation says which claude conversation the CLI opens: a new one under
// id, or an existing one resumed.
type conversation struct {
	id     string
	resume bool
}

// interactiveArgs is the interactive CLI's argv for cfg. It carries what the
// headless argv does (llmkit's, which is unexported and hardwired to --print
// and stream-json) minus the protocol flags and the approval server: in a
// terminal claude asks its questions itself. It adds the SessionStart hook
// that reports conversation changes back to the server.
func (s *Service) interactiveArgs(cfg llmkit.SessionConfig, conv conversation, prompt string) ([]string, error) {
	var a []string
	if conv.resume {
		a = append(a, "--resume", conv.id)
	} else {
		a = append(a, "--session-id", conv.id)
	}
	if cfg.Model != "" {
		a = append(a, "--model", cfg.Model)
	}
	// --add-dir and --allowedTools take a variable number of values; one
	// flag per dir and a comma-joined tool list keep them from swallowing
	// the prompt.
	for _, d := range cfg.AddDirs {
		a = append(a, "--add-dir", d)
	}
	if len(cfg.MCPServers) > 0 {
		mcp, err := mcpConfigJSON(cfg.MCPServers)
		if err != nil {
			return nil, err
		}
		a = append(a, "--mcp-config", mcp)
	}
	if len(cfg.AllowedTools) > 0 {
		a = append(a, "--allowedTools", strings.Join(cfg.AllowedTools, ","))
	}
	switch cfg.Permission {
	case llmkit.PermissionAcceptEdits:
		a = append(a, "--permission-mode", "acceptEdits")
	case llmkit.PermissionBypass:
		a = append(a, "--permission-mode", "bypassPermissions")
	}
	if dc, ok := cfg.Driver.(claude.Config); ok {
		for _, d := range dc.SkillDirs {
			a = append(a, "--plugin-dir", d)
		}
	}
	if cfg.AppendSystem != "" {
		a = append(a, "--append-system-prompt", cfg.AppendSystem)
	}
	if s.SessionHookURL != nil {
		settings, err := sessionStartHookSettings(s.SessionHookURL(cfg.ID))
		if err != nil {
			return nil, err
		}
		a = append(a, "--settings", settings)
	}
	if prompt != "" {
		a = append(a, "--", prompt)
	}
	return a, nil
}

// mcpConfigJSON is the --mcp-config payload for servers, in the shape llmkit
// writes for headless sessions (its builder is unexported, and also adds the
// approval server interactive sessions must not have).
func mcpConfigJSON(servers []llmkit.MCPServerSpec) (string, error) {
	out := map[string]any{}
	for _, sv := range servers {
		entry := map[string]any{}
		switch sv.Transport {
		case "http", "streamable-http", "sse":
			entry["type"] = "http"
			entry["url"] = sv.URL
			if len(sv.Headers) > 0 {
				entry["headers"] = sv.Headers
			}
		default: // stdio
			entry["command"] = sv.Command
			if len(sv.Args) > 0 {
				entry["args"] = sv.Args
			}
			if len(sv.Env) > 0 {
				entry["env"] = sv.Env
			}
		}
		out[sv.Name] = entry
	}
	b, err := json.Marshal(map[string]any{"mcpServers": out})
	return string(b), err
}

// sessionStartHookSettings is a --settings payload whose SessionStart hook
// posts the hook's input — which names the current conversation — to url.
// --settings layers over the user's own settings rather than replacing them.
//
// The hook must never disturb the session: its stdout would be added to
// claude's context, so the response is discarded, and a server that is down
// only costs the five-second timeout.
func sessionStartHookSettings(url string) (string, error) {
	cmd := "curl -fsS -m 5 -X POST -H 'Content-Type: application/json' --data-binary @- '" +
		url + "' >/dev/null 2>&1 || true"
	b, err := json.Marshal(map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd}}},
			},
		},
	})
	return string(b), err
}
