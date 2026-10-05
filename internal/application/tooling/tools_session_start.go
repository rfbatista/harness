package tooling

import (
	"context"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// startPeer starts a session on the caller's task, on the server, recorded as
// started by the caller. Arguments name things the way an agent knows them:
// agents and repositories by name (or id).
func startPeer(ctx context.Context, scope *taskScope, sessions ports.SessionRepository, agents ports.AgentLister, start PeerStarter, args map[string]any) (any, error) {
	prompt := strings.TrimSpace(stringArg(args, "prompt"))
	if prompt == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "prompt is required: say what the new session should do"}
	}

	live := 0
	for _, s := range sessions.List(ports.SessionFilter{ProjectID: scope.session.ProjectID, TicketID: scope.ticket.ID}) {
		if !s.Status.IsTerminal() {
			live++
		}
	}
	if live >= MaxLiveTaskSessions {
		return nil, &domain.StructuredError{Code: "TASK_SESSION_LIMIT", Message: strconv.Itoa(live) +
			" sessions already run on this task (the limit is " + strconv.Itoa(MaxLiveTaskSessions) + "); wait for one to finish or coordinate with them"}
	}

	agent, err := findAgent(ctx, agents, stringArg(args, "agent"))
	if err != nil {
		return nil, err
	}
	repo, err := findRepository(ctx, start.Repositories, scope.session, stringArg(args, "repository"))
	if err != nil {
		return nil, err
	}

	req := ports.InteractiveRequest{
		ProjectID:       scope.session.ProjectID,
		RepositoryID:    repo.id,
		TicketID:        scope.ticket.ID,
		Prompt:          prompt,
		BaseBranch:      strings.TrimSpace(stringArg(args, "base_branch")),
		RunsOn:          domain.RunnerServer,
		ParentSessionID: scope.session.ID,
		// A peer gets no more than its parent: unattended only if the parent is.
		AutoAccept: "off",
	}
	if scope.session.AutoRun {
		req.AutoAccept = "all"
	}
	if agent != nil {
		req.AgentID = agent.ID
	}
	sess, _, err := start.Sessions.StartInteractive(ctx, req)
	if err != nil {
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
	return out, nil
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
