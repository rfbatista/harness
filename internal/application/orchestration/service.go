package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	Repositories interface {
		ports.RepositoryReader
		ports.RepositoryLister
	}
	Agents interface {
		GetAgent(ctx context.Context, id string) (*domain.Agent, error)
		ResolveAgentRelations(a *domain.Agent)
	}
	MCPServers interface{ ListMCPServers() []*domain.MCPServer }
	Zones      ports.ZoneReader
	// Contexts lists a project's bounded contexts for the brief of a
	// project-level session; nil leaves them out of it.
	Contexts interface {
		ListBoundedContexts(projectID string) []*domain.BoundedContext
	}
}

// sessionSubject is what a session works on: its task, or, for a
// project-level product session (ADR 0016), its project alone.
type sessionSubject struct {
	project *domain.Project
	ticket  *domain.Ticket // nil for a session with no task
	mode    domain.SessionMode
}

// projectLevel says the session has no task and works on its project: only
// a mode that runs without a task does, and only with its project in hand.
func (sub sessionSubject) projectLevel() bool {
	return sub.ticket == nil && sub.project != nil && !sub.mode.NeedsTask()
}

// ticketResolver is the slice of the planning ticket store the orchestration
// needs to validate spawn-into-ticket requests. ports.TicketRepository satisfies it.
type ticketResolver interface {
	Get(ctx context.Context, id string) *domain.Ticket
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
	// SessionShellURL returns where a session's edit proxy checks and
	// commits its shell's files (…/api/sessions/{id}/shell); a session with
	// a shell gets ShellHooks aimed at it.
	SessionShellURL func(sessionID string) string
	// Terminals hosts RunnerServer interactive sessions. Nil means this
	// server does not run agents itself; such sessions answer
	// SERVER_HOSTING_UNAVAILABLE.
	Terminals ports.TerminalHost
	// Events announces SessionDeleted, so other contexts stop what runs in a
	// session's worktree before it goes. Nil announces nothing.
	Events ports.EventPublisher
	// GitAuth gives each session's processes the stored git credentials and
	// nothing of the machine's (see "Contract: Harness server ↔ Agent
	// sessions — git credentials"). Nil leaves sessions' git as it is.
	GitAuth ports.GitAuth
	// AgentCredential gives each agent process the env entries ("NAME=value")
	// of the agent credential (ADR 0018), read at every start and resume. Nil
	// or empty adds nothing.
	AgentCredential func() []string
	// Ready refuses an agent start while the agent credential, the managed
	// claude or the sandbox driver is missing (agentready.Gate). Nil enforces
	// nothing.
	Ready interface {
		Check(ctx context.Context) error
	}
	// Roles says what a session is on its task, for the role-aware brief. Nil:
	// every session is a peer.
	Roles interface {
		SessionRole(ctx context.Context, sessionID string) (domain.SessionRole, error)
	}
	// Decorator fills a session's derived role fields (role, the task's
	// architect, its status check) as it leaves the service. Nil leaves them
	// empty.
	Decorator ports.SessionDecorator
	// Transcripts checks an interactive session can be resumed. Nil skips the
	// check and lets the CLI report a missing conversation itself.
	Transcripts ports.ClaudeTranscripts
	// TranscriptReader reads a session's saved conversation. Nil: no session
	// has one to show.
	TranscriptReader ports.ClaudeTranscriptReader
	// Artifacts lists the project assets attached to a session's task, for
	// its brief. Nil leaves them out.
	Artifacts ports.ArtifactReader
	// Resources and Comments count what the person added to a session's
	// task, for its brief. Nil leaves the counts out.
	Resources ports.TaskResourceReader
	Comments  ports.TaskCommentReader
	// Shells gives each session with a task its virtual shell (see
	// "Contract: Harness server ↔ Agent sessions — virtual shell"). Nil:
	// sessions get none.
	Shells ports.VirtualShells
	// ShellIndex is what each running shell holds, so a [task update]
	// turn names the path the session's shell holds the new file at. Nil:
	// turns name the path the shell gives such a file.
	ShellIndex ports.ShellBaseVersions
	// ShimBinary is what a task session's shim commands (sed, tee, cp, mv,
	// rm, touch, mkdir) link to, first on its PATH; they call the session's
	// SessionShellURL. Empty: sessions get no shims.
	ShimBinary string
	// TokenSavers says which token savers a session runs with, when it is
	// prepared (ADR 0015). Nil: sessions run with none.
	TokenSavers ports.SessionTokenSavers
	// IndexBuilds says when a repository index build runs on a checkout, so
	// a session start does not copy an index being written. Nil: never.
	IndexBuilds ports.RepositoryIndexBuilds
	// Settings holds the architect and implementation models a new session
	// starts with when its start names none. Nil: the built-in ones.
	Settings ports.SettingsReader
	// Sandbox is the agent sandbox step of a session start (ADR 0017). Nil:
	// sessions run unconfined.
	Sandbox *Sandbox

	// updates feeds the task-update turns to the courier, one at a time;
	// deliverUpdate replaces the courier in tests.
	updates       updateQueue
	deliverUpdate func(sessionID, key, text string) error

	mu  sync.Mutex
	seq map[string]int64
	// statuses is the last status announced per session, so a status is
	// announced on the bus only when it actually changes.
	statuses map[string]domain.SessionStatus
	cleanups map[string]func()
	// busy marks sessions with a turn in flight: set when a user message is
	// written to the CLI, cleared by the "result" line that ends the turn. It
	// exists so a late "system"/init — the CLI emits it after initializing,
	// which is routinely *after* the initial task was already sent — cannot
	// report the session as idle while Claude is working on that first turn.
	busy map[string]bool
	// courier holds the turns waiting for a session's turn to end, and what
	// the server knows of interactive sessions' turns.
	courier courier
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
		resuming: map[string]bool{}, statuses: map[string]domain.SessionStatus{},
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

	p, err := s.prepare(ctx, prepareInput{
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

		TokenSavers:             p.savers.active,
		TokenSaversNotInstalled: p.savers.notInstalled,
		CavemanIntensity:        p.savers.cavemanIntensity,
		Sandbox:                 p.sandbox.facts,
	})
	if err != nil {
		_ = sess.Stop()
		s.runCleanup(sess.ID())
		return nil, fmt.Errorf("persist session: %w", err)
	}
	provisioned = nil
	if stop := s.collectViolations(sess.ID(), p.cfg.WorkingDir, p.sandbox.facts); stop != nil {
		s.mu.Lock()
		s.cleanups[sess.ID()] = withViolations(s.cleanups[sess.ID()], stop)
		s.mu.Unlock()
	}

	go s.pump(sess)

	// Through Send, not sess.Send: the initial task is the session's first turn
	// like any other, so it must mark the session busy (otherwise the CLI's
	// "system"/init line, which lands after this, would report idle mid-turn)
	// and be recorded as a user_message so the timeline opens with what was
	// actually asked.
	if err := s.Send(ctx, sess.ID(), req.Task); err != nil {
		slog.Error("send initial task failed", "session", sess.ID(), "err", err)
	}
	return s.withResumability(ctx, created), nil
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
	// Role is what the session will be on its task, for the brief; empty is a peer.
	Role domain.SessionRole
}

// prepared is a session whose worktree exists and whose configuration is
// built, but whose CLI has not been launched yet. Until the caller records it,
// the worktree has to be discarded and cleanup run on failure.
type prepared struct {
	cfg        llmkit.SessionConfig
	ws         *domain.Workspace
	branch     string
	permission llmkit.PermissionMode
	cleanup    func() // removes the materialized skill plugin dir and the git directory; nil if none
	envFile    string // the session's git env file, for login shells; "" without GitAuth
	savers     sessionSavers
	sandbox    sandboxed
}

// prepare validates a start request, provisions the session's worktree and
// builds its CLI configuration. Start launches the result headless;
// StartInteractive hands it to a terminal.
func (s *Service) prepare(ctx context.Context, in prepareInput) (*prepared, error) {
	if in.ProjectID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
	}
	proj, err := s.catalog.Projects.GetProject(ctx, in.ProjectID)
	if err != nil {
		return nil, err
	}

	subject := sessionSubject{project: proj, mode: in.Mode}
	if in.TicketID != "" {
		tk, err := s.ticketIn(ctx, in.ProjectID, in.TicketID)
		if err != nil {
			return nil, err
		}
		subject.ticket = tk
	}

	if in.RepositoryID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "repository_id is required"}
	}
	repo, err := s.catalog.Repositories.GetRepository(ctx, in.RepositoryID)
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

	if err := s.ready(ctx); err != nil {
		if ag.cleanup != nil {
			ag.cleanup()
		}
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
	cfg := s.sessionConfig(id, ws.Path, in.Model, in.AllowedTools, permission, ag, subject, in.ZoneID, in.Role)
	cleanup, err := s.withShell(id, subject, &cfg, ag.cleanup)
	if err != nil {
		_ = s.workspaces.Discard(ws.ID)
		return nil, err
	}
	envFile, cleanup, err := s.withGit(id, in.RepositoryID, &cfg, cleanup)
	if err != nil {
		_ = s.workspaces.Discard(ws.ID)
		return nil, err
	}
	s.withAgentCredential(&cfg)
	savers := s.withTokenSavers(ctx, in, repo.RootDir, ws.Path, &cfg)
	sb, err := s.sandbox(ctx, in, id, repo, ws.Path, cfg.Env)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		_ = s.workspaces.Discard(ws.ID)
		return nil, err
	}
	return &prepared{
		sandbox:    sb,
		cfg:        cfg,
		ws:         ws,
		branch:     in.Branch,
		permission: permission,
		cleanup:    cleanup,
		envFile:    envFile,
		savers:     savers,
	}, nil
}

// ready is the refusal of a start while something agents need is missing.
func (s *Service) ready(ctx context.Context) error {
	if s.Ready == nil {
		return nil
	}
	return s.Ready.Check(ctx)
}

// withAgentCredential adds the agent credential to the agent process env.
func (s *Service) withAgentCredential(cfg *llmkit.SessionConfig) {
	if s.AgentCredential == nil {
		return
	}
	if env := s.AgentCredential(); len(env) > 0 {
		cfg.Env = append(slices.Clone(cfg.Env), env...)
	}
}

// withGit gives the session's processes its git directory's environment and
// briefs the agent when its git cannot authenticate. The cleanup it answers
// runs cleanup and removes the directory. Without GitAuth it changes
// nothing.
func (s *Service) withGit(id, repositoryID string, cfg *llmkit.SessionConfig, cleanup func()) (string, func(), error) {
	if s.GitAuth == nil {
		return "", cleanup, nil
	}
	sg, err := s.GitAuth.SessionEnv(context.TODO(), id, repositoryID)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, fmt.Errorf("prepare the session's git credentials: %w", err)
	}
	cfg.Env = append(slices.Clone(cfg.Env), sg.Env...)
	if sg.Note != "" {
		cfg.AppendSystem = appendSection(cfg.AppendSystem, sg.Note)
	}
	return sg.EnvFile, func() {
		if cleanup != nil {
			cleanup()
		}
		s.GitAuth.EndSession(id)
	}, nil
}

// withShell gives a session with a task, or a project-level one, its
// virtual shell: made and filled now, passed with --add-dir and
// HARNESS_SHELL, named in the brief, with the edit proxy's hooks in its
// claude settings. The cleanup it answers runs cleanup and removes the
// shell. A headless session with no task, or a service without Shells,
// changes nothing.
func (s *Service) withShell(id string, subject sessionSubject, cfg *llmkit.SessionConfig, cleanup func()) (func(), error) {
	if s.Shells == nil || (subject.ticket == nil && !subject.projectLevel()) {
		return cleanup, nil
	}
	var scope ports.ShellScope
	if subject.ticket != nil {
		scope.TicketID = subject.ticket.ID
	} else {
		scope.ProjectID = subject.project.ID
	}
	dir, err := s.Shells.Open(context.TODO(), id, scope)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("make the session's virtual shell: %w", err)
	}
	cfg.AddDirs = append(slices.Clone(cfg.AddDirs), dir)
	cfg.Env = append(slices.Clone(cfg.Env), ports.ShellDirEnv+"="+dir)
	cfg.AppendSystem = appendSection(cfg.AppendSystem, shellSection(dir))
	if s.SessionShellURL != nil {
		if dc, ok := withHooks(cfg.Driver, ShellHooks(s.SessionShellURL(id))); ok {
			cfg.Driver = dc
		}
	}
	return s.withShims(id, cfg, func() {
		if cleanup != nil {
			cleanup()
		}
		s.Shells.Close(id, dir)
	})
}

// withShims puts the session's shim dir first on its PATH: one link per
// shim command to ShimBinary, which finds the session's shell endpoint in
// HARNESS_SHELL_URL. PATH is an override of the server's own, so it wins
// over the inherited one. The cleanup it answers runs cleanup and removes
// the dir. Without ShimBinary it changes nothing.
func (s *Service) withShims(id string, cfg *llmkit.SessionConfig, cleanup func()) (func(), error) {
	if s.ShimBinary == "" {
		return cleanup, nil
	}
	fail := func(err error) (func(), error) {
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("make the session's shims: %w", err)
	}
	dir, err := os.MkdirTemp("", "harness-shims-*")
	if err != nil {
		return fail(err)
	}
	for _, name := range ports.ShellShimCommands {
		if err := os.Symlink(s.ShimBinary, filepath.Join(dir, name)); err != nil {
			_ = os.RemoveAll(dir)
			return fail(err)
		}
	}
	path := dir
	if inherited := os.Getenv("PATH"); inherited != "" {
		path += string(os.PathListSeparator) + inherited
	}
	cfg.Env = append(slices.Clone(cfg.Env), "PATH="+path, ports.ShimDirEnv+"="+dir)
	if s.SessionShellURL != nil {
		cfg.Env = append(cfg.Env, ports.ShellURLEnv+"="+s.SessionShellURL(id))
	}
	return func() {
		if cleanup != nil {
			cleanup()
		}
		_ = os.RemoveAll(dir)
	}, nil
}

// ticketIn loads a ticket and checks it belongs to the project.
func (s *Service) ticketIn(ctx context.Context, projectID, ticketID string) (*domain.Ticket, error) {
	if s.tickets == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "tickets are not available"}
	}
	tk := s.tickets.Get(ctx, ticketID)
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
// interactive sessions: the agent's prompt and skills, the task brief (or
// a project-level session's project brief) and task MCP server, the
// agent's MCP servers and the zone's extra dirs.
func (s *Service) sessionConfig(id, dir, model string, allowedTools []string, permission llmkit.PermissionMode, ag resolvedAgent, subject sessionSubject, zoneID string, role domain.SessionRole) llmkit.SessionConfig {
	ticket := subject.ticket
	cfg := llmkit.SessionConfig{
		ID:           id,
		WorkingDir:   dir,
		Model:        model,
		AllowedTools: allowedTools,
		Permission:   permission,
		Env:          s.DefaultEnv,
		AppendSystem: ag.appendSystem,
		// What the session's LLM requests are attributed to in the usage
		// records (ADR 0006).
		Attribution: id,
		Driver:      claude.Config{SkillDirs: ag.skillDirs},
	}

	// After the agent's own prompt, so the task brief lands at the end of the
	// system prompt rather than being overwritten by it. The id is minted
	// before this because the per-session task MCP server URL is built from it.
	var taskURL string
	if s.TaskServerURL != nil {
		taskURL = s.TaskServerURL(id)
	}
	if subject.projectLevel() {
		applyProjectContext(&cfg, s.briefProject(subject.project), taskURL)
	} else {
		applyTaskContext(&cfg, ticket, taskURL, role, s.taskCounts(ticket))
	}
	if section := attachedAssetsSection(s.attachedAssets(ticket)); section != "" {
		cfg.AppendSystem = appendSection(cfg.AppendSystem, section)
	}

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

		var change sessionChange
		if m := s.metricsOf(id); m != nil {
			switch ev.Type {
			case "output":
				if ev.Text != "" {
					m.LastAction = truncate(ev.Text, 200)
				}
			case "tool_use":
				m.LastAction = "tool: " + ev.ToolName
			case "usage":
				m.CostUSD += ev.CostUSD
				if ev.Usage != nil {
					m.Tokens.Input += ev.Usage.InputTokens
					m.Tokens.Output += ev.Usage.OutputTokens
					m.Tokens.CacheCreation += ev.Usage.CacheCreationInputTokens
					m.Tokens.CacheRead += ev.Usage.CacheReadInputTokens
				}
			}
			// A dead session has no outstanding decisions: broker.DenyAll has
			// answered whatever was pending, and a lingering count would leave
			// the session owing the user a decision forever.
			if hasStatus && status.IsTerminal() {
				m.PendingApprovals = 0
			}
			change.metrics = m
		}
		if hasStatus {
			ev.Status = status
			change.status = status
		}
		s.record(id, ev, change)
		if ce.Type == llmkit.EventResult {
			s.announce(domain.SessionTurnEnded{SessionID: id})
			s.flushHeadless(id)
		}
	}
	s.runCleanup(id)
	if sess := s.sessions.Get(id); sess != nil && sess.Status.IsTerminal() {
		s.hub.End(id)
		s.announce(domain.SessionEnded{SessionID: id, Status: sess.Status})
	}
}

// announce publishes a lifecycle event on the bus. Subscribers react on their
// own; their failure is logged, never the caller's.
func (s *Service) announce(ev domain.Event) {
	if s.Events == nil {
		return
	}
	if err := s.Events.Publish(context.Background(), ev); err != nil {
		slog.Warn("announce session event", "event", ev.EventName(), "err", err)
	}
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
	s.record(e.SessionID, SessionEvent{
		Type:     "approval_expired",
		Status:   domain.SessionThinking,
		ToolName: e.ToolName,
		Text:     e.Reason,
		Approval: &ApprovalInfo{ReqID: e.ReqID, ToolName: e.ToolName},
		At:       time.Now(),
	}, s.releaseApproval(e.SessionID))
}

func (s *Service) handleApproval(req approval.Request) {
	change := sessionChange{status: domain.SessionWaitingApproval}
	if m := s.metricsOf(req.SessionID); m != nil {
		m.PendingApprovals++
		change.metrics = m
	}
	s.record(req.SessionID, SessionEvent{
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
	}, change)
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
	s.record(id, SessionEvent{
		Type:   "user_message",
		Status: domain.SessionThinking,
		Text:   text,
		At:     time.Now(),
	}, sessionChange{status: domain.SessionThinking})
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
		s.record(id, SessionEvent{
			Type:     "approval_resolved",
			Status:   domain.SessionThinking,
			Text:     "auto-approved",
			ToolName: req.ToolName,
			Approval: &ApprovalInfo{ReqID: req.ID, ToolName: req.ToolName},
			At:       time.Now(),
		}, s.releaseApproval(id))
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
	s.record(id, SessionEvent{
		Type:     "approval_resolved",
		Status:   domain.SessionThinking,
		Text:     text,
		Approval: &ApprovalInfo{ReqID: reqID},
		At:       time.Now(),
	}, s.releaseApproval(id))
	return nil
}

// releaseApproval is the bookkeeping every outstanding approval ends with,
// whether it was decided or expired: one fewer pending decision, and the
// session back to working rather than idle — idle would wrongly invite the user
// to type mid-turn. It is stored with the event that ends the approval.
func (s *Service) releaseApproval(id string) sessionChange {
	change := sessionChange{status: domain.SessionThinking}
	if m := s.metricsOf(id); m != nil {
		if m.PendingApprovals > 0 {
			m.PendingApprovals--
		}
		change.metrics = m
	}
	return change
}

// metricsOf is the session's stored metrics, nil when it is gone or its read
// failed (logged).
func (s *Service) metricsOf(id string) *ports.SessionMetrics {
	cur, err := s.sessions.Find(context.Background(), id)
	if err != nil {
		slog.Error("session not read; its metrics miss this event", "session", id, "err", err)
	}
	if cur == nil {
		return nil
	}
	return &ports.SessionMetrics{CostUSD: cur.CostUSD, Tokens: ports.TokensOf(cur), LastAction: cur.LastAction, PendingApprovals: cur.PendingApprovals}
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
	s.hub.End(id)
	s.mu.Lock()
	delete(s.seq, id)
	s.mu.Unlock()
	return nil
}

func (s *Service) Subscribe(id string) (<-chan SessionEvent, []SessionEvent, func()) {
	return s.hub.Subscribe(id)
}

// History is the session's stored events after fromSeq, or the error its
// read failed with.
func (s *Service) History(id string, fromSeq int64) ([]SessionEvent, error) {
	stored, err := s.sessions.ListEvents(id, fromSeq)
	if err != nil {
		return nil, fmt.Errorf("session history: %w", err)
	}
	out := make([]SessionEvent, 0, len(stored))
	for _, e := range stored {
		var ev SessionEvent
		if err := json.Unmarshal(e.Payload, &ev); err == nil {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id string) (*domain.Session, error) {
	sess, err := s.sessions.Find(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return s.withResumability(ctx, sess), nil
}

func (s *Service) List(ctx context.Context, f ports.SessionFilter) ([]*domain.Session, error) {
	list, err := s.sessions.Query(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	for _, sess := range list {
		s.fillResumability(sess)
	}
	if s.Decorator != nil {
		s.Decorator.DecorateSessions(ctx, list...) // the whole list at once: its queries do not grow with it
	}
	return list, nil
}

func (s *Service) publish(sessionID string, ev SessionEvent) {
	s.record(sessionID, ev, sessionChange{})
}

// sessionChange is what an agent event changes on its session besides its
// log: its metrics when set, its status when set.
type sessionChange struct {
	metrics *ports.SessionMetrics
	status  domain.SessionStatus
}

// record sequences ev, stores it with change in one transaction, then fans
// it out live. A store failure is logged; the live stream still gets it.
func (s *Service) record(sessionID string, ev SessionEvent, change sessionChange) {
	s.mu.Lock()
	s.seq[sessionID]++
	ev.Seq = s.seq[sessionID]
	moved := ev.Status != "" && s.statuses[sessionID] != ev.Status
	if moved {
		s.statuses[sessionID] = ev.Status
	}
	s.mu.Unlock()

	ev.SessionID = sessionID
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	payload, _ := json.Marshal(ev)
	if err := s.sessions.RecordEvent(sessionID, ports.SessionEventRecord{
		Seq: ev.Seq, Type: ev.Type, Payload: payload, Metrics: change.metrics, Status: change.status,
	}); err != nil {
		slog.Error("session event not stored", "session", sessionID, "seq", ev.Seq, "type", ev.Type, "err", err)
	}
	s.hub.Publish(sessionID, ev)
	s.notify(sessionID, ev.Type)
	if moved {
		s.announce(domain.SessionStatusChanged{SessionID: sessionID, Status: ev.Status})
	}
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
	s.courier.forget(id)
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
