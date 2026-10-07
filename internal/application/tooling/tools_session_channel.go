package tooling

import (
	"context"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// queuedNote tells a sender why its message was not delivered yet, and how the
// recipient will still get it.
const queuedNote = "Queued: it goes in when the recipient's current turn ends; a session that is not running " +
	"reads it later with list_task_messages."

// sessionChannelTools are the architect channel: delegates message the task's
// architect, and the architect replies, asks the person for review and tunes
// the status-check loops that wake it. The server enforces who may do what;
// these tools resolve the caller from the context, check the cheap things
// (an empty body, a malformed since, a missing interval) and pass the
// server's errors through unchanged. A nil channel leaves them answering that
// the channel is unavailable.
func sessionChannelTools(planningSvc ports.Planning, sessions ports.SessionRepository, channel ports.TaskChannel) []domain.Tool {
	// scoped resolves the caller's task and checks the channel is there.
	scoped := func(ctx context.Context) (*taskScope, error) {
		scope, err := resolveTaskScope(ctx, planningSvc, sessions)
		if err != nil {
			return nil, err
		}
		if channel == nil {
			return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the architect channel is not available on this server"}
		}
		return scope, nil
	}

	return []domain.Tool{
		{
			Name: "message_architect",
			Description: "Send a message to this task's architect, the session that delegated your work. Use kind " +
				"review_request when your work (a plan, a branch, a document) is ready for it to review, status_report at " +
				"milestones and whenever you are blocked (set status: working, blocked, ready_for_review or done), and " +
				"question when you need a decision. Attach the task documents and artifacts it should look at by id. A " +
				"running architect gets it as a new turn (if it is mid-turn, as soon as that turn ends); one that is not " +
				"running reads it later. Its reply reaches you as a new turn starting with [task message …]. Fails with " +
				"NO_ARCHITECT when the task has none.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["kind","body"],"properties":{` +
				`"kind":{"type":"string","enum":["review_request","status_report","question"],"description":"review_request: your work is ready for review. status_report: where you are. question: you need a decision."},` +
				`"body":{"type":"string","description":"The message. Say what to look at, what you did, or what you need."},` +
				`"subject":{"type":"string","description":"A short subject line."},` +
				`"status":{"type":"string","enum":["working","blocked","ready_for_review","done"],"description":"On a status_report only: where your work stands."},` +
				`"document_ids":{"type":"array","items":{"type":"string"},"description":"Task documents the architect should read."},` +
				`"artifact_ids":{"type":"array","items":{"type":"string"},"description":"Task artifacts the architect should look at."},` +
				`"in_reply_to":{"type":"string","description":"The id of the message you are answering, from its [task message …] header."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := scoped(ctx)
				if err != nil {
					return nil, err
				}
				body, err := requiredBody(args)
				if err != nil {
					return nil, err
				}
				msg, err := channel.SendToArchitect(ctx, scope.session.ID, ports.TaskMessageInput{
					Kind:        domain.TaskMessageKind(getString(args, "kind", "")),
					Subject:     getString(args, "subject", ""),
					Body:        body,
					Status:      domain.ReportStatus(getString(args, "status", "")),
					InReplyTo:   getString(args, "in_reply_to", ""),
					DocumentIDs: getStringSlice(args, "document_ids"),
					ArtifactIDs: getStringSlice(args, "artifact_ids"),
				})
				if err != nil {
					return nil, err
				}
				return sentMessage(msg), nil
			},
		},
		{
			Name: "reply_to_session",
			Description: "Architect only. Answer a session on this task, usually one of your delegates. On a review_request, " +
				"set verdict to approved or changes_requested and say what to change in body. Pass in_reply_to with the " +
				"message id from the [task message …] header. A running session gets it as a new turn (if it is mid-turn, " +
				"as soon as that turn ends); one that is not running reads it later with list_task_messages.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["session_id","body"],"properties":{` +
				`"session_id":{"type":"string","description":"The session to answer, as list_task_sessions or the [task message …] header names it."},` +
				`"body":{"type":"string","description":"The reply."},` +
				`"in_reply_to":{"type":"string","description":"The id of the message you are answering."},` +
				`"verdict":{"type":"string","enum":["approved","changes_requested"],"description":"Only when answering a review_request."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := scoped(ctx)
				if err != nil {
					return nil, err
				}
				to := strings.TrimSpace(getString(args, "session_id", ""))
				if to == "" {
					return nil, invalidInput("session_id is required: the session to answer")
				}
				body, err := requiredBody(args)
				if err != nil {
					return nil, err
				}
				msg, err := channel.ReplyFromArchitect(ctx, scope.session.ID, to, ports.TaskMessageInput{
					Kind:      domain.MessageReply,
					Body:      body,
					Verdict:   domain.Verdict(getString(args, "verdict", "")),
					InReplyTo: getString(args, "in_reply_to", ""),
				})
				if err != nil {
					return nil, err
				}
				return sentMessage(msg), nil
			},
		},
		{
			Name: "list_task_messages",
			Description: "The messages between sessions on this task and its architect, oldest first. The architect sees " +
				"all of them (narrow with session_id); any other session sees only its own. since (RFC 3339) returns only " +
				"newer ones. Use it to catch up on messages sent while you were not running.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{` +
				`"session_id":{"type":"string","description":"Only messages from or to this session."},` +
				`"since":{"type":"string","description":"Only messages created after this time, RFC 3339 (e.g. 2026-10-07T14:00:00Z)."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := scoped(ctx)
				if err != nil {
					return nil, err
				}
				filter := ports.TaskMessageFilter{SessionID: strings.TrimSpace(getString(args, "session_id", ""))}
				if s := strings.TrimSpace(getString(args, "since", "")); s != "" {
					if filter.Since, err = time.Parse(time.RFC3339, s); err != nil {
						return nil, invalidInput("since must be an RFC 3339 time, e.g. 2026-10-07T14:00:00Z")
					}
				}
				msgs, err := channel.ListSessionMessages(ctx, scope.session.ID, filter)
				if err != nil {
					return nil, err
				}
				return map[string]any{"count": len(msgs), "messages": nonNil(msgs)}, nil
			},
		},
		{
			Name: "request_user_review",
			Description: "Architect only. Ask the person to review something: a plan, a branch, a document, an artifact. " +
				"It shows in the web UI until they approve or request changes; their answer reaches you as a turn with " +
				"review_response in the header. Opening a review does not change the task status: move it yourself with " +
				"update_task_status.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["subject","body"],"properties":{` +
				`"subject":{"type":"string","description":"What to review, in a few words."},` +
				`"body":{"type":"string","description":"What the person should look at and what you need from them."},` +
				`"about_session_id":{"type":"string","description":"The delegate whose work it is."},` +
				`"document_ids":{"type":"array","items":{"type":"string"},"description":"Task documents to review."},` +
				`"artifact_ids":{"type":"array","items":{"type":"string"},"description":"Task artifacts to review."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := scoped(ctx)
				if err != nil {
					return nil, err
				}
				body, err := requiredBody(args)
				if err != nil {
					return nil, err
				}
				review, err := channel.RequestUserReview(ctx, scope.session.ID, ports.ReviewRequestInput{
					Subject:        getString(args, "subject", ""),
					Body:           body,
					AboutSessionID: getString(args, "about_session_id", ""),
					DocumentIDs:    getStringSlice(args, "document_ids"),
					ArtifactIDs:    getStringSlice(args, "artifact_ids"),
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{"review_request": review}, nil
			},
		},
		{
			Name:        "withdraw_user_review",
			Description: "Architect only. Withdraw a pending review request that is no longer needed.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["review_id"],"properties":{` +
				`"review_id":{"type":"string","description":"The review request to withdraw, from list_review_requests."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := scoped(ctx)
				if err != nil {
					return nil, err
				}
				id := strings.TrimSpace(getString(args, "review_id", ""))
				if id == "" {
					return nil, invalidInput("review_id is required")
				}
				review, err := channel.WithdrawReview(ctx, scope.session.ID, id)
				if err != nil {
					return nil, err
				}
				return map[string]any{"review_request": review}, nil
			},
		},
		{
			Name:        "list_review_requests",
			Description: "The review requests the architect raised for the person on this task, newest first, optionally only those in one state.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{` +
				`"state":{"type":"string","enum":["pending","approved","changes_requested","withdrawn"],"description":"Only requests in this state. Default: all."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := scoped(ctx)
				if err != nil {
					return nil, err
				}
				state, err := domain.ParseReviewState(getString(args, "state", ""))
				if err != nil {
					return nil, err
				}
				reviews, err := channel.ListReviewRequests(ctx, scope.ticket.ID, state)
				if err != nil {
					return nil, err
				}
				return map[string]any{"count": len(reviews), "review_requests": nonNil(reviews)}, nil
			},
		},
		{
			Name: "set_status_check",
			Description: "Architect only. Change how often you are woken to check on a delegate you started, in minutes " +
				"(2–240), or 0 to pause it. A delegate that reports with status_report pushes its next check back on its own.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["session_id","every_minutes"],"properties":{` +
				`"session_id":{"type":"string","description":"The delegate the loop checks on."},` +
				`"every_minutes":{"type":"integer","minimum":0,"maximum":240,"description":"Minutes between checks, 2–240; 0 pauses the loop."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := scoped(ctx)
				if err != nil {
					return nil, err
				}
				delegate := strings.TrimSpace(getString(args, "session_id", ""))
				if delegate == "" {
					return nil, invalidInput("session_id is required: the delegate the loop checks on")
				}
				every, ok := intArg(args, "every_minutes")
				if !ok {
					return nil, invalidInput("every_minutes is required: minutes between checks (2–240), or 0 to pause")
				}
				check, err := channel.SetStatusCheck(ctx, scope.session.ID, delegate, every)
				if err != nil {
					return nil, err
				}
				return map[string]any{"status_check": check}, nil
			},
		},
		{
			Name:        "list_status_checks",
			Description: "Architect only. The status-check loops on this task: delegate, interval, next and last firing, and state.",
			InputSchema: emptySchema(),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := scoped(ctx)
				if err != nil {
					return nil, err
				}
				role, err := channel.SessionRole(ctx, scope.session.ID)
				if err != nil {
					return nil, err
				}
				if role != domain.RoleArchitect {
					return nil, &domain.StructuredError{Code: "ARCHITECT_ONLY", Message: "only the task's architect lists its status checks"}
				}
				checks, err := channel.ListStatusChecks(ctx, scope.ticket.ID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"count": len(checks), "status_checks": nonNil(checks)}, nil
			},
		},
	}
}

// sentMessage is a send's result: the stored message, whether it went in now,
// and when it did not, what happens to it instead.
func sentMessage(msg *domain.TaskMessage) map[string]any {
	out := map[string]any{"message": msg, "delivered": msg.Delivered}
	if !msg.Delivered {
		out["note"] = queuedNote
	}
	return out
}

// requiredBody refuses an empty body before the server sees it.
func requiredBody(args map[string]any) (string, error) {
	body := getString(args, "body", "")
	if strings.TrimSpace(body) == "" {
		return "", invalidInput("body is required")
	}
	return body, nil
}

// intArg reads an integer argument, telling a missing one apart from 0.
func intArg(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

// nonNil keeps an empty listing a JSON array rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func invalidInput(msg string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg}
}
