package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// StopNotAllowed refuses stop_task_session for a session the caller may not
// stop: only the task's architect stops any session of its task, and any
// other session only one it started.
const StopNotAllowed = "STOP_NOT_ALLOWED"

// sessionStopTools is stop_task_session: a running session of the caller's
// task is stopped the way the person's Stop stops it, and can be brought back
// with resume_task_session. A nil stopper leaves it answering that it is
// unavailable.
func sessionStopTools(planningSvc ports.Planning, sessions ports.SessionRepository, channel ports.TaskChannel, stopper ports.SessionStopper) []domain.Tool {
	return []domain.Tool{{
		Name: "stop_task_session",
		Description: "Stop a running agent session of this session's task, as the person's Stop does: its process ends, it " +
			"shows as stopped, and its worktree, branch and conversation stay, so resume_task_session brings it back. Stop a " +
			"delegate whose work is done or has gone off track. The task's architect stops any session of its task; any other " +
			"session stops only the sessions it started itself. A session that runs in the person's terminal client is closed there, not here.",
		InputSchema: schemaFromJSON(`{"type":"object","required":["session_id"],"properties":{` +
			`"session_id":{"type":"string","description":"The full id of a running session of this task, from list_task_sessions."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			scope, err := resolveTaskScope(ctx, planningSvc, sessions)
			if err != nil {
				return nil, err
			}
			if stopper == nil {
				return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "stopping sessions is not available on this server"}
			}
			target, _, err := ownTaskSession(ctx, scope, sessions, channel, args, "stop", StopNotAllowed)
			if err != nil {
				return nil, err
			}
			if target.Status.IsTerminal() {
				return nil, &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "session " + target.ID + " has already ended (" +
					string(target.Status) + "): resume it with resume_task_session if it should go on"}
			}
			if err := stopper.Stop(ctx, target.ID); err != nil {
				return nil, refusalNaming(err, target.ID, "stopped")
			}
			status := domain.SessionStopped
			if cur, err := sessions.Find(ctx, target.ID); err == nil && cur != nil {
				status = cur.Status
			}
			return map[string]any{
				"session_id": target.ID,
				"status":     status,
				"note":       "Its worktree, branch and conversation are kept: resume_task_session brings it back.",
			}, nil
		},
	}}
}
