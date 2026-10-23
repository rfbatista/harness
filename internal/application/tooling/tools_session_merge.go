package tooling

import (
	"context"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// MergeNotApproved refuses merge_session_branch for a delegate whose work the
// person has not approved.
const MergeNotApproved = "MERGE_NOT_APPROVED"

// sessionMergeTools is the architect's merge: once the person approved a
// review request about a delegate, the server merges its branch into the
// default branch of the local checkout exactly as the web action does
// (ports.BranchMerger), and the result lands on the delegate's session. A nil
// merger or channel leaves it answering that it is unavailable.
func sessionMergeTools(planningSvc ports.Planning, sessions ports.SessionRepository, channel ports.TaskChannel, merger ports.BranchMerger) []domain.Tool {
	return []domain.Tool{{
		Name: "merge_session_branch",
		Description: "Architect only. Merge a delegate's branch into the default branch of its repository's local checkout, " +
			"on the server, after the person approved your review request about that delegate (request_user_review with " +
			"about_session_id). It merges with --no-ff, never pushes, and does not move the task's status. It is refused, " +
			"and the checkout left as it was, when no approved review request names that delegate, the checkout has " +
			"uncommitted changes, is not on the default branch, or the merge conflicts; it is also refused when the branch has a commit made after the person approved (request a new review); the answer then says why. On " +
			"success it answers the new HEAD. The same result shows on the delegate's session.",
		InputSchema: schemaFromJSON(`{"type":"object","required":["session_id"],"properties":{` +
			`"session_id":{"type":"string","description":"The delegate whose branch to merge: its session id, or its branch name, as list_task_sessions gives them. It must be a session of your own task."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			scope, err := resolveTaskScope(ctx, planningSvc, sessions)
			if err != nil {
				return nil, err
			}
			if channel == nil || merger == nil {
				return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "merging a session's branch is not available on this server"}
			}
			role, err := channel.SessionRole(ctx, scope.session.ID)
			if err != nil {
				return nil, err
			}
			if role != domain.RoleArchitect {
				return nil, &domain.StructuredError{Code: "ARCHITECT_ONLY", Message: "only the task's architect merges a delegate's branch"}
			}
			ref := strings.TrimSpace(getString(args, "session_id", ""))
			if ref == "" {
				return nil, invalidInput("session_id is required: the delegate whose branch to merge")
			}
			target := taskSessionByRef(sessions.List(ctx, ports.SessionFilter{ProjectID: scope.session.ProjectID, TicketID: scope.ticket.ID}), scope.ticket.ID, ref)
			if target == nil || target.ID == scope.session.ID {
				return nil, &domain.StructuredError{Code: "SESSION_NOT_ON_TASK", Message: "no delegate " + ref + " on this task: pass a session id or branch from list_task_sessions"}
			}
			approvedAt, err := requireApprovedReview(ctx, channel, scope.ticket.ID, target.ID)
			if err != nil {
				return nil, err
			}
			changed, err := merger.BranchChangedSince(ctx, target.ID, approvedAt)
			if err != nil {
				return nil, mergeNotApproved("could not verify the branch is unchanged since approval")
			}
			if changed {
				return nil, mergeNotApproved("the branch changed after the person approved; request a new review")
			}
			return merger.MergeSessionBranch(ctx, target.ID)
		},
	}}
}

// taskSessionByRef finds the session of the task whose id or branch is ref.
func taskSessionByRef(list []*domain.Session, taskID, ref string) *domain.Session {
	var byBranch *domain.Session
	for _, s := range list {
		if s.TicketID != taskID {
			continue
		}
		if s.ID == ref {
			return s
		}
		if byBranch == nil && s.Branch != "" && s.Branch == ref {
			byBranch = s
		}
	}
	return byBranch
}

// requireApprovedReview answers when the person approved (RespondedAt) the newest review request; it passes when the person's answer to the newest review
// request about the session (withdrawn ones do not count) is approval.
func requireApprovedReview(ctx context.Context, channel ports.TaskChannel, taskID, sessionID string) (time.Time, error) {
	reviews, err := channel.ListReviewRequests(ctx, taskID, "")
	if err != nil {
		return time.Time{}, err
	}
	for _, r := range reviews { // newest first
		if r.AboutSessionID != sessionID || r.State == domain.ReviewWithdrawn {
			continue
		}
		switch r.State {
		case domain.ReviewApproved:
			if r.RespondedAt != nil {
				return *r.RespondedAt, nil
			}
			return r.UpdatedAt, nil
		case domain.ReviewChangesRequested:
			return time.Time{}, mergeNotApproved("the person asked for changes on the review request about session " + sessionID + " (" + r.Subject + "): address them and ask for review again")
		default:
			return time.Time{}, mergeNotApproved("the person has not answered the review request about session " + sessionID + " (" + r.Subject + ") yet")
		}
	}
	return time.Time{}, mergeNotApproved("no review request about session " + sessionID + ": ask the person with request_user_review (about_session_id) and merge once they approve")
}

func mergeNotApproved(reason string) error {
	return &domain.StructuredError{Code: MergeNotApproved, Message: "the branch is not merged: " + reason}
}
