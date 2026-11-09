package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// WithBranchPush serves push_branch, with which an agent asks the harness to
// push a branch to a remote (ADR 0071). Without it the tool answers that it is
// unavailable.
func WithBranchPush(p ports.BranchPusher) Option {
	return func(o *options) { o.pushes = p }
}

// sessionPushTools is push_branch: it files a push branch request and returns
// at once. The person's answer, and after an approval the result of the push,
// arrive as a turn. A refusal carries its code at the start of the message,
// because MCP shows an agent the message only (codedTools does it).
func sessionPushTools(sessions ports.SessionRepository, pushes ports.BranchPusher) []domain.Tool {
	return []domain.Tool{{
		Name: "push_branch",
		Description: "Ask the person to push a branch of your repository to a remote. It files a push branch request and returns at " +
			"once with its id and state pending; it never blocks and never forces. The person's answer reaches you as a new turn: " +
			"`[branch request <id> · pushed]` once the harness pushed it with the stored credential, `[branch request <id> · push failed]` " +
			"with the reason (a remote that refused it, a host with no credential), or `[branch request <id> · denied]` with their note. " +
			"Use it instead of git push when you do not need to wait. Refused with BRANCH_NOT_FOUND, REMOTE_NOT_FOUND or INVALID_INPUT, " +
			"the code first in the message.",
		InputSchema: schemaFromJSON(`{"type":"object","required":["branch"],"properties":{` +
			`"branch":{"type":"string","description":"The local branch to push."},` +
			`"remote":{"type":"string","description":"The remote's name. Default: origin."},` +
			`"remote_branch":{"type":"string","description":"The branch name it lands on at the remote. Default: the branch's own name."},` +
			`"reason":{"type":"string","description":"Why the person should approve it; shown on the request."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			id := SessionIDFrom(ctx)
			if id == "" || sessions.Get(id) == nil {
				return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no such session"}
			}
			if pushes == nil {
				return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "pushing a branch is not available on this server"}
			}
			r, err := pushes.RequestPush(ctx, id, ports.PushBranchInput{
				Branch: getString(args, "branch", ""), Remote: getString(args, "remote", ""),
				RemoteBranch: getString(args, "remote_branch", ""), Reason: getString(args, "reason", ""),
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{"request_id": r.ID, "state": string(r.State)}, nil
		},
	}}
}
