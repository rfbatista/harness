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
func sessionResumeTools(planningSvc ports.Planning, sessions ports.SessionRepository, start PeerStarter, channel ports.TaskChannel) []domain.Tool {
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
			if start.Sessions == nil {
				return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "resuming sessions is not available on this server"}
			}
			return resumePeer(ctx, scope, sessions, start.Sessions, channel, args)
		},
	}}
}

func resumePeer(ctx context.Context, scope *taskScope, sessions ports.SessionRepository, resumer ports.InteractiveSessions, channel ports.TaskChannel, args map[string]any) (any, error) {
	id := strings.TrimSpace(stringArg(args, "session_id"))
	if id == "" {
		return nil, invalidInput("session_id is required: the ended session to resume, from list_task_sessions with include_ended")
	}
	if id == scope.session.ID {
		return nil, invalidInput("session " + id + " is your own session: a session cannot resume itself")
	}
	target, err := sessions.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if target == nil || target.ProjectID != scope.session.ProjectID {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "there is no session " + id + ": name one from list_task_sessions with include_ended"}
	}
	if target.TicketID != scope.ticket.ID {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_ON_TASK", Message: "session " + id + " belongs to another task, so it cannot be resumed from this one"}
	}
	architect := false
	if channel != nil {
		role, err := channel.SessionRole(ctx, scope.session.ID)
		if err != nil {
			return nil, err
		}
		architect = role == domain.RoleArchitect
	}
	if !architect && target.ParentSessionID != scope.session.ID {
		return nil, &domain.StructuredError{Code: ResumeNotAllowed, Message: "session " + id + " was not started by you, and only the task's " +
			"architect resumes a session it did not start: ask the architect with message_architect"}
	}
	if err := checkTaskSessionLimit(ctx, scope, sessions); err != nil {
		return nil, err
	}

	sess, _, err := resumer.ResumeInteractive(ctx, ports.ResumeRequest{
		SessionID: id, RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 120, Rows: 32},
		Prompt: strings.TrimSpace(stringArg(args, "prompt")),
	})
	if err != nil {
		return nil, resumeRefusal(err, id)
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

// resumeRefusal names the session in orchestration's refusal of a resume,
// which speaks of "the session" for the person's Resume button. The code stays.
func resumeRefusal(err error, id string) error {
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		return err
	}
	return &domain.StructuredError{Code: se.Code, Message: "session " + id + " was not resumed: " + se.Message}
}
