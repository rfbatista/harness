package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A product-owner thread is a headless session in product-owner mode (ADR
// 0109): on its project with no task and no worktree of its own, started and
// resumed here, driven and read by the product owner service.

var _ ports.ProductOwnerSessions = (*Service)(nil)

// StartProductOwnerSession starts a thread's session: headless, on the
// project, running in the main checkout of the project's first repository
// (read only for this mode) with the whole project readable.
func (s *Service) StartProductOwnerSession(ctx context.Context, spec ports.ProductOwnerSessionSpec) (string, error) {
	id := llmkit.NewSessionID()
	p, err := s.productOwnerPrepare(ctx, id, spec)
	if err != nil {
		return "", err
	}
	sess, err := s.runtime.Start(ctx, p.cfg)
	if err != nil {
		if p.cleanup != nil {
			p.cleanup()
		}
		return "", startFailure(err)
	}
	if _, err := s.sessions.Create(&domain.Session{
		ID:         id,
		ProjectID:  spec.ProjectID,
		AgentID:    spec.AgentID,
		Task:       "Product owner: " + p.project.Name,
		WorkingDir: p.cfg.WorkingDir,
		Model:      p.cfg.Model,
		Status:     domain.SessionStarting,
		AutoRun:    p.permission == llmkit.PermissionBypass,
		Mode:       domain.SessionModeProductOwner,
		Sandbox:    p.sandbox.facts,
	}); err != nil {
		_ = sess.Stop()
		if p.cleanup != nil {
			p.cleanup()
		}
		return "", fmt.Errorf("persist product owner session: %w", err)
	}
	if p.cleanup != nil {
		s.adoptCleanup(id, p.cleanup)
	}
	// ADR 0099: what the thread started with, for its Context tab. The first
	// message is the thread's own and not part of the entry.
	s.recordStart(ctx, id, p.prepared, "")
	go s.pump(sess)
	return id, nil
}

// ResumeProductOwnerSession starts the process of a thread's session again
// over its claude conversation (claude --resume), as the thread started: its
// agent and prompt are the spec's.
func (s *Service) ResumeProductOwnerSession(ctx context.Context, sessionID string, spec ports.ProductOwnerSessionSpec) error {
	if !s.claimResume(sessionID) {
		return &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session is being resumed"}
	}
	defer s.releaseResume(sessionID)
	if _, ok := s.runtime.Get(sessionID); ok {
		return &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session is running"}
	}
	sess := s.sessions.Get(sessionID)
	if sess == nil || sess.Mode != domain.SessionModeProductOwner {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no product owner session " + sessionID}
	}
	resumer, ok := s.runtime.(llmkit.Resumable)
	if !ok {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "this agent runtime cannot resume a conversation"}
	}
	spec.ProjectID = sess.ProjectID
	p, err := s.productOwnerPrepare(ctx, sessionID, spec)
	if err != nil {
		return err
	}
	live, err := resumer.Resume(ctx, sessionID, p.cfg)
	if err != nil {
		if p.cleanup != nil {
			p.cleanup()
		}
		return startFailure(err)
	}
	if p.sandbox.facts != nil {
		_ = s.sessions.UpdateSandbox(sessionID, *p.sandbox.facts)
	}
	// ADR 0099: a new entry at each resume, from what this resume handed to claude.
	s.recordResume(ctx, sess, p.context, "")
	_ = s.sessions.UpdateStatus(sessionID, domain.SessionStarting)
	if p.cleanup != nil {
		s.adoptCleanup(sessionID, p.cleanup)
	}
	go s.pump(live)
	return nil
}

// ProductOwnerRunning says a thread's session has a process on this server.
func (s *Service) ProductOwnerRunning(sessionID string) bool {
	_, ok := s.runtime.Get(sessionID)
	return ok
}

// startFailure turns the runtime's refusal to start into the code clients
// branch on, as a headless start does.
func startFailure(err error) error {
	var missing *llmkit.BinNotFoundError
	if errors.As(err, &missing) {
		return &domain.StructuredError{Code: "CLAUDE_CLI_NOT_FOUND",
			Message: fmt.Sprintf("Claude CLI not found (%q). Install it with: %s", missing.Bin, missing.InstallHint)}
	}
	return fmt.Errorf("start product owner session: %w", err)
}

type productOwnerPrepared struct {
	*prepared
	project *domain.Project
}

// productOwnerPrepare builds the configuration a thread's process runs with:
// the agent's prompt and skills, the product owner's brief and task tools,
// the project's virtual shell, the sandbox with every repository read-only. It
// provisions no worktree. The caller launches it and owns the cleanup.
func (s *Service) productOwnerPrepare(ctx context.Context, id string, spec ports.ProductOwnerSessionSpec) (*productOwnerPrepared, error) {
	proj, err := s.catalog.Projects.GetProject(ctx, spec.ProjectID)
	if err != nil {
		return nil, err
	}
	mode := domain.SessionModeProductOwner
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	ag, err := s.resolveAgent(spec.AgentID, mode)
	if err != nil {
		return nil, err
	}
	if ag.agent != nil && !proj.AgentEnabled(ag.agent.Name) {
		if ag.cleanup != nil {
			ag.cleanup()
		}
		return nil, &domain.StructuredError{Code: "AGENT_NOT_ENABLED",
			Message: "AGENT_NOT_ENABLED: agent " + ag.agent.Name + " is not enabled in project " + proj.Name}
	}
	repos, err := s.catalog.Repositories.ListRepositories(ctx, proj.ID)
	if err != nil {
		if ag.cleanup != nil {
			ag.cleanup()
		}
		return nil, err
	}
	dir, repo := "", &domain.Repository{}
	if len(repos) > 0 {
		repo = repos[0]
		dir = repo.RootDir
	}
	subject := sessionSubject{project: proj, mode: mode, productOwnerPrompt: spec.Prompt}
	model := s.sessionModel(mode, "")
	// Its tools read; nothing it runs needs a person's approval, and the
	// sandbox and the tools hold what it may not do.
	permission := llmkit.PermissionBypass
	trail := &pieceTrail{}
	cfg, res := s.sessionConfigTrail(trail, id, dir, model, nil, permission, ag, subject, "", "", "")
	// Its LLM calls are counted for the product owner and its project (ADR 0006).
	cfg.Attribution = ports.UsageProductOwnerAttribution + ":" + proj.ID
	cleanup, err := s.withShell(id, "", subject, &cfg, ag.cleanup)
	if err != nil {
		return nil, err
	}
	trail.mark(&cfg, domain.PieceOther, "Virtual shell", "Harness")
	s.withAgentCredential(&cfg, proj.ID)
	s.withKeychainShim(&cfg)
	s.withAttribution(&cfg)
	cleanup, err = s.withTempDir(id, &cfg, cleanup)
	if err != nil {
		return nil, err
	}
	sb, err := s.sandbox(ctx, prepareInput{ProjectID: proj.ID, Mode: mode, Headless: true}, id, repo, dir, cfg.Env, ag.skillDirs)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, err
	}
	if sb.wrap != nil {
		cfg.Wrap = sb.wrap
	}
	if sb.policy != nil {
		withClaudeSandboxOff(&cfg)
	}
	withChromeEnvPinned(&cfg)
	trail.mark(&cfg, domain.PieceOther, "Sandbox and Chrome", "Harness")
	facts := s.contextFacts(prepareInput{Mode: mode, Headless: true}, ag, cfg, &domain.Workspace{Path: dir}, sessionSavers{}, sb, permission)
	return &productOwnerPrepared{
		prepared: &prepared{
			cfg: cfg, permission: permission, cleanup: cleanup, sandbox: sb,
			context: contextInput{
				Cfg: cfg, System: trail.pieces, Versions: res.versions, Repository: repo.Name,
				Agent: ag.agent, Project: proj, Attached: res.attached, KnownTools: res.known,
				Skills: ag.contextSkills, BuiltIn: s.builtInTools(), Facts: facts,
			},
		},
		project: proj,
	}, nil
}

// applyProductOwnerContext is applyProjectContext for a product-owner thread:
// the per-session task MCP server with the mode's allow-list, and the product
// owner's brief, then the project's own prompt, last.
func applyProductOwnerContext(cfg *llmkit.SessionConfig, in projectBriefInput, taskServerURL, projectPrompt string) {
	if taskServerURL != "" {
		cfg.MCPServers = append(cfg.MCPServers, llmkit.MCPServerSpec{Name: taskServerName, Transport: "http", URL: taskServerURL})
	}
	for _, name := range tooling.ProductOwnerToolNames {
		cfg.AllowedTools = append(cfg.AllowedTools, llmkit.MCPToolRef(taskServerName, name))
	}
	cfg.AppendSystem = appendSection(cfg.AppendSystem, productOwnerBrief(in))
	if p := strings.TrimSpace(projectPrompt); p != "" {
		cfg.AppendSystem = appendSection(cfg.AppendSystem, "## Instructions from the person for this project\n\n"+p)
	}
}

// productOwnerBrief is the product owner's opening instructions: what it is,
// the project, and how it answers. Each later piece of the product owner adds
// its own section here (writes, memory, search, digests) beside these.
func productOwnerBrief(in projectBriefInput) string {
	var b strings.Builder
	p := in.Project
	b.WriteString("## You are the product owner of project " + p.Name + "\n\n")
	b.WriteString(productOwnerRole)
	if len(in.Repositories) > 0 {
		b.WriteString("\n\nRepositories (one application each):\n\n")
		for _, r := range in.Repositories {
			line := "- " + r.Name
			if d := strings.TrimSpace(r.Description); d != "" {
				line += " — " + d
			}
			b.WriteString(line + "\n")
		}
	}
	if len(in.Contexts) > 0 {
		b.WriteString("\nBounded contexts:\n\n")
		for _, c := range in.Contexts {
			line := "- " + c.Name
			if d := strings.TrimSpace(c.Purpose); d != "" {
				line += " — " + d
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n" + productOwnerSources + "\n\n" + productOwnerDigests + "\n\n" + productOwnerBehaviours)
	return b.String()
}

const productOwnerRole = `You are the one product owner of this project: the person's partner for what was done, what is in progress, what waits on them and what comes next. You answer the person's questions in a conversation that continues across messages, and you help shape new features. Only the person talks to you.`

const productOwnerSources = `What you can read, all read-only:

- The project's tasks with their status history and children, through the task tools (list_project_tasks, get_project_task). Your other tools are list_project_repositories, list_bounded_contexts and list_notifications (the person's inbox).
- Every project and task document, design asset and the person's notification inbox. The documents and designs are files in your virtual shell, $HARNESS_SHELL: read them with your ordinary tools (ls, grep, sed -n, Read). The shell's README.md maps its layout.
- The code of every repository of the project, in the checkouts your sandbox lets you read.
- Of any other session, only its status, outcome and summary (list_project_sessions, list_project_branches). Never read another session's transcript or worktree, even when asked: you cannot, and you say so.`

const productOwnerDigests = `The daily digests: once a day the server records a digest of what moved in the project, when something did. Asked about the past ("what happened last Tuesday?"), read that day's digest with list_product_owner_digests, answer from it, and say that the answer comes from the digest. A day with no digest was quiet, or no digest time was set.`

const productOwnerBehaviours = `How you answer:

- Say where you looked: name the tasks, documents or files an answer comes from.
- Say so when you cannot know something, and what you would need to know it. Never guess a status, a date or a decision.
- In a project with no tasks, say that it has none and offer to shape its first feature with the person.
- You cannot change anything yet: no task, document, status or file. When the person asks for a change, explain that you cannot change anything yet, and say what you would change so they can do it.
- Keep answers short and in the language the person writes in.`

// isProductOwnerEvent says ev is about the session of a product-owner thread:
// the inbox, the agent board and the architect channel hear nothing of it.
func (s *Service) isProductOwnerEvent(ev domain.Event) bool {
	var id string
	switch e := ev.(type) {
	case domain.SessionStatusChanged:
		id = e.SessionID
	case domain.SessionTurnEnded:
		id = e.SessionID
	case domain.SessionEnded:
		id = e.SessionID
	case domain.SessionWorktreeProvisioned:
		id = e.SessionID
	default:
		return false
	}
	sess := s.sessions.Get(id)
	return sess != nil && sess.Mode == domain.SessionModeProductOwner
}
