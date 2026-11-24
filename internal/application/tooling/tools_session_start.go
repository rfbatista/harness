package tooling

import (
	"context"
	"strconv"
	"strings"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// startPeer starts a session on the caller's task, on the server, recorded as
// started by the caller. Arguments name things the way an agent knows them:
// agents and repositories by name (or id). When the caller is the task's
// architect, the server starts a status-check loop on the new session inside
// StartInteractive, and the result carries it.
func startPeer(ctx context.Context, scope *taskScope, sessions ports.SessionRepository, agents ports.AgentLister, start PeerStarter, channel ports.TaskChannel, args map[string]any) (any, error) {
	prompt := strings.TrimSpace(stringArg(args, "prompt"))
	if prompt == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "prompt is required: say what the new session should do"}
	}

	// A failed read must not let the limit through.
	onTask, err := sessions.Query(ctx, ports.SessionFilter{ProjectID: scope.session.ProjectID, TicketID: scope.ticket.ID})
	if err != nil {
		return nil, err
	}
	live := 0
	for _, s := range onTask {
		if !s.Status.IsTerminal() {
			live++
		}
	}
	if live >= MaxLiveTaskSessions {
		return nil, &domain.StructuredError{Code: "TASK_SESSION_LIMIT", Message: strconv.Itoa(live) +
			" sessions already run on this task (the limit is " + strconv.Itoa(MaxLiveTaskSessions) + "); wait for one to finish or coordinate with them"}
	}

	// Checked before anything starts, so a bad interval never leaves a session behind.
	var statusCheckMinutes *int
	if _, given := args["status_check_minutes"]; given {
		n, ok := intArg(args, "status_check_minutes")
		if !ok {
			return nil, invalidInput("status_check_minutes must be a number of minutes: 0, or between 2 and 240")
		}
		if err := domain.ValidStatusCheckMinutes(n); err != nil {
			return nil, invalidInput("status_check_minutes must be 0, or between 2 and 240")
		}
		statusCheckMinutes = &n
	}

	mode := strings.TrimSpace(stringArg(args, "mode"))
	join, err := resolveJoin(ctx, scope, sessions, args, mode)
	if err != nil {
		return nil, err
	}

	agent, err := findAgent(ctx, agents, stringArg(args, "agent"))
	if err != nil {
		return nil, err
	}
	repoWant := stringArg(args, "repository")
	repoOwner := scope.session
	if join != nil {
		// The new session runs where the joined one does: its repository is
		// the default, and naming another is refused.
		repoOwner = join.session
	}
	repo, err := findRepository(ctx, start.Repositories, repoOwner, repoWant)
	if err != nil {
		return nil, err
	}
	if join != nil && strings.TrimSpace(repoWant) != "" && repo.id != join.session.RepositoryID {
		return nil, invalidInput("session " + join.session.ID + " works in repository " + join.session.RepositoryID +
			", so a session joining its worktree cannot run in " + repo.name + ": leave repository empty or name its own")
	}

	req := ports.InteractiveRequest{
		ProjectID:       scope.session.ProjectID,
		RepositoryID:    repo.id,
		TicketID:        scope.ticket.ID,
		Prompt:          prompt,
		BaseBranch:      strings.TrimSpace(stringArg(args, "base_branch")),
		RunsOn:          domain.RunnerServer,
		ParentSessionID: scope.session.ID,
		Mode:            mode,
		// A peer gets no more than its parent: unattended only if the parent is.
		AutoAccept:         "off",
		StatusCheckMinutes: statusCheckMinutes,
	}
	if scope.session.AutoRun {
		req.AutoAccept = "all"
	}
	if join != nil {
		req.WorktreeID = join.session.WorkspaceID
		req.Access = string(join.access)
	}
	if agent != nil {
		req.AgentID = agent.ID
	}
	sess, _, err := start.Sessions.StartInteractive(ctx, req)
	if err != nil {
		if join != nil {
			err = joinStartRefusal(err, join.session.ID)
		}
		return nil, err
	}

	out := map[string]any{
		"session_id": sess.ID,
		"status":     sess.Status,
		"branch":     sess.Branch,
		"repository": repo.name,
		"note":       "It is running on the server and shows on the task's page. Follow it with list_task_sessions.",
	}
	if agent != nil {
		out["agent"] = agent.Name
	}
	if channel != nil {
		if role, err := channel.SessionRole(ctx, scope.session.ID); err == nil && role == domain.RoleArchitect {
			out["status_check"] = statusCheckOf(ctx, channel, scope.ticket.ID, sess.ID)
		} else if statusCheckMinutes != nil {
			out["note"] = out["note"].(string) + " status_check_minutes was ignored: only the task's architect gets a status-check loop."
		}
	}
	return out, nil
}

// joinTarget is the worktree a start joins: the session whose worktree it is,
// and the access the new session gets there.
type joinTarget struct {
	session *domain.Session
	access  domain.SessionAccess
}

// resolveJoin reads join_session_id and access. It returns nil when the start
// cuts its own worktree. Every refusal is a message that names the session and
// the reason, since the MCP bridge drops codes, and it is decided before
// anything starts. The worktree's being on disk is checked by the start itself.
func resolveJoin(ctx context.Context, scope *taskScope, sessions ports.SessionRepository, args map[string]any, mode string) (*joinTarget, error) {
	access := domain.SessionAccess(strings.TrimSpace(stringArg(args, "access")))
	switch access {
	case "", domain.AccessWrite, domain.AccessReadOnly:
	default:
		return nil, invalidInput("access " + string(access) + " is not valid: use write or read_only")
	}
	id := strings.TrimSpace(stringArg(args, "join_session_id"))
	if id == "" {
		if access == domain.AccessReadOnly {
			return nil, invalidInput("access read_only needs join_session_id: a session that cuts its own worktree writes it")
		}
		return nil, nil
	}
	if strings.TrimSpace(stringArg(args, "base_branch")) != "" {
		return nil, invalidInput("join_session_id " + id + " and base_branch cannot be combined: a session that joins a worktree runs on that session's branch")
	}
	switch domain.SessionMode(mode) {
	case domain.SessionModeArchitect, domain.SessionModeProduct, domain.SessionModeDesignSystem:
		return nil, invalidInput("a session in mode " + mode + " cannot join the worktree of session " + id + ": it plans and delegates, it does not work in a worktree")
	}
	joined, err := sessions.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if joined == nil || joined.ProjectID != scope.session.ProjectID {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "there is no session " + id + " on this task: name one from list_task_sessions"}
	}
	if joined.TicketID != scope.ticket.ID {
		return nil, &domain.StructuredError{Code: "WORKTREE_NOT_ON_TASK", Message: "session " + id + " belongs to another task, so its worktree cannot be joined from this one"}
	}
	if joined.WorkspaceID == "" || joined.WorktreeClearedAt != nil {
		return nil, &domain.StructuredError{Code: "WORKTREE_NOT_FOUND", Message: "the worktree of session " + id + " was cleared or is gone: start on a new branch instead"}
	}
	if access == "" {
		access = domain.DefaultAccess(domain.SessionMode(mode))
	}
	return &joinTarget{session: joined, access: access}, nil
}

// joinStartRefusal puts the session in the start path's refusals of a join. The
// start path words them for the New session form, which is no help to an
// architect; the code stays, only the message changes.
func joinStartRefusal(err error, sessionID string) error {
	switch errs.Code(err) {
	case "WORKTREE_NOT_FOUND":
		return &domain.StructuredError{Code: "WORKTREE_NOT_FOUND", Message: "the worktree of session " + sessionID +
			" is gone from disk: nothing was started; start on a new branch instead"}
	case "ACCESS_INVALID":
		return &domain.StructuredError{Code: "ACCESS_INVALID", Message: "session " + sessionID + "'s worktree cannot be joined read-only, " +
			"because the agent sandbox is off for this session and the sandbox is what keeps a read-only session from writing; " +
			"pass access: write to join with write access"}
	}
	return err
}

// statusCheckOf is the loop the server started on a new delegate, or nil when
// there is none (status_check_minutes 0) or it cannot be read.
func statusCheckOf(ctx context.Context, channel ports.TaskChannel, taskID, delegateID string) *domain.StatusCheck {
	checks, err := channel.ListStatusChecks(ctx, taskID)
	if err != nil {
		return nil
	}
	for _, c := range checks {
		if c.DelegateSessionID == delegateID {
			return c
		}
	}
	return nil
}

// findAgent resolves an agent by id or by name (ignoring case); empty means none.
func findAgent(ctx context.Context, agents ports.AgentLister, want string) (*domain.Agent, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		return nil, nil
	}
	if agents == nil {
		return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "no agents are available on this server"}
	}
	list, err := agents.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list))
	for _, a := range list {
		if a.ID == want || strings.EqualFold(a.Name, want) {
			return a, nil
		}
		names = append(names, a.Name)
	}
	return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "no agent called " + want + "; the agents are: " + strings.Join(names, ", ")}
}

type repoChoice struct{ id, name string }

// findRepository resolves a repository of the caller's project by id or name;
// empty means the caller's own.
func findRepository(ctx context.Context, repos ports.RepositoryLister, caller *domain.Session, want string) (repoChoice, error) {
	want = strings.TrimSpace(want)
	var list []*domain.Repository
	if repos != nil {
		var err error
		if list, err = repos.ListRepositories(ctx, caller.ProjectID); err != nil {
			return repoChoice{}, err
		}
	}
	if want == "" {
		if caller.RepositoryID == "" {
			return repoChoice{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "you are not in a repository; name the repository to work in"}
		}
		for _, r := range list {
			if r.ID == caller.RepositoryID {
				return repoChoice{r.ID, r.Name}, nil
			}
		}
		return repoChoice{caller.RepositoryID, ""}, nil
	}
	names := make([]string, 0, len(list))
	for _, r := range list {
		if r.ID == want || strings.EqualFold(r.Name, want) {
			return repoChoice{r.ID, r.Name}, nil
		}
		names = append(names, r.Name)
	}
	return repoChoice{}, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "no repository called " + want + " in this project; there are: " + strings.Join(names, ", ")}
}

func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}
