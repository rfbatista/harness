package tooling

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ResumeNotAllowed refuses resume_task_session for a session the caller may
// not bring back: only the task's architect resumes any session of its task,
// and any other session only one it started.
const ResumeNotAllowed = "RESUME_NOT_ALLOWED"

// sessionResumeTools is resume_task_session: an ended session of the caller's
// task comes back on the server, in its worktree and branch, from its saved
// claude conversation, the way the person's Resume brings it back. A prompt,
// when given, is its first turn. Unlike the person's Resume, it is held to the
// task's session limit, like every session an agent starts.
func sessionResumeTools(planningSvc ports.Planning, sessions ports.SessionRepository, channel ports.TaskChannel, resumer ports.InteractiveSessions) []domain.Tool {
	return []domain.Tool{{
		Name: "resume_task_session",
		Description: "Resume an ended agent session of this session's task: it comes back on the server in its own worktree " +
			"and branch, with its agent, mode and role, and continues its saved conversation from where it stopped. " +
			"Give a prompt to make it the session's first turn, so it answers at once; without one it waits idle " +
			"(a session a restart interrupted first gets the turn that tells it so). The task's architect resumes any " +
			"session of its task; any other session resumes only the sessions it started itself. A running session " +
			"cannot be resumed; send it a message instead. Resume an ended delegate rather than start a new one, so " +
			"its branch and conversation carry on. At most " + strconv.Itoa(MaxLiveTaskSessions) + " sessions run on a task at once.",
		InputSchema: schemaFromJSON(`{"type":"object","required":["session_id"],"properties":{` +
			`"session_id":{"type":"string","description":"The full id of an ended session of this task, from list_task_sessions with include_ended."},` +
			`"prompt":{"type":"string","description":"The resumed session's first turn. Default: none, it resumes idle."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			scope, err := resolveTaskScope(ctx, planningSvc, sessions)
			if err != nil {
				return nil, err
			}
			if resumer == nil {
				return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "resuming sessions is not available on this server"}
			}
			return resumePeer(ctx, scope, sessions, resumer, channel, args)
		},
	}}
}

func resumePeer(ctx context.Context, scope *taskScope, sessions ports.SessionRepository, resumer ports.InteractiveSessions, channel ports.TaskChannel, args map[string]any) (any, error) {
	target, architect, err := ownTaskSession(ctx, scope, sessions, channel, args, "resume", ResumeNotAllowed)
	if err != nil {
		return nil, err
	}
	id := target.ID
	// A running session is refused as orchestration would refuse it, before
	// the limit it counts toward.
	if !target.Status.IsTerminal() {
		return nil, &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session " + id + " is still running, so it cannot be resumed: send it a message instead"}
	}
	if err := checkTaskSessionLimit(ctx, scope, sessions); err != nil {
		return nil, err
	}

	sess, _, err := resumer.ResumeInteractive(ctx, ports.ResumeRequest{
		SessionID: id, RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 120, Rows: 32},
		Prompt: strings.TrimSpace(stringArg(args, "prompt")),
	})
	if err != nil {
		return nil, refusalNaming(err, id, "resumed")
	}

	out := map[string]any{
		"session_id": sess.ID,
		"status":     sess.Status,
		"branch":     sess.Branch,
		"note": "It resumed on the server in its worktree and continues its saved conversation; it shows on the task's page. " +
			"Follow it with list_task_sessions.",
	}
	if architect {
		out["status_check"] = statusCheckOf(ctx, channel, scope.ticket.ID, sess.ID)
	}
	return out, nil
}

// ownTaskSession is the session of the caller's task that session_id names,
// when the caller may act on it: the task's architect on any of them, any
// other session on one it started; architect says which. verb words the
// refusals ("resume", "stop"); notAllowed is the code of the refusal for a
// session the caller did not start.
func ownTaskSession(ctx context.Context, scope *taskScope, sessions ports.SessionRepository, channel ports.TaskChannel, args map[string]any, verb, notAllowed string) (target *domain.Session, architect bool, err error) {
	id := strings.TrimSpace(stringArg(args, "session_id"))
	if id == "" {
		return nil, false, invalidInput("session_id is required: the session to " + verb + ", from list_task_sessions")
	}
	if id == scope.session.ID {
		return nil, false, invalidInput("session " + id + " is your own session: a session cannot " + verb + " itself")
	}
	target, err = sessions.Find(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if target == nil || target.ProjectID != scope.session.ProjectID {
		return nil, false, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "there is no session " + id + ": name one from list_task_sessions"}
	}
	if target.TicketID != scope.ticket.ID {
		return nil, false, &domain.StructuredError{Code: "SESSION_NOT_ON_TASK", Message: "session " + id + " belongs to another task, so you cannot " + verb + " it from this one"}
	}
	if channel != nil {
		role, err := channel.SessionRole(ctx, scope.session.ID)
		if err != nil {
			return nil, false, err
		}
		architect = role == domain.RoleArchitect
	}
	if !architect && target.ParentSessionID != scope.session.ID {
		return nil, false, &domain.StructuredError{Code: notAllowed, Message: "session " + id + " was not started by you, and only the task's " +
			"architect can " + verb + " a session it did not start: ask the architect with message_architect"}
	}
	return target, architect, nil
}

// refusalNaming puts the session in orchestration's refusal, which speaks of
// "the session" for the person's buttons. The code stays.
func refusalNaming(err error, id, what string) error {
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		return err
	}
	return &domain.StructuredError{Code: se.Code, Message: "session " + id + " was not " + what + ": " + se.Message}
}
