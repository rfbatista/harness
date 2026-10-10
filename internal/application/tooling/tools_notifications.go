package tooling

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The notification tools (see "Contract: Harness server ↔ Agent sessions —
// notification tools"): agents read the person's inbox across every project
// and post to it. Nothing here changes read state, which is the person's.

// NotificationTooling is what the session-scoped notification tools need.
// The zero value leaves them answering that notifications are unavailable.
type NotificationTooling struct {
	Notifier ports.Notifier
}

var errNotificationsOff = &domain.StructuredError{Code: "UNAVAILABLE", Message: "notifications are not available on this server"}

const notifyAdvice = "Use it for what the person has to know or do, never for progress updates: " +
	"request_user_review and moving a task to review already notify the person. "

func notificationLevelSchema() string {
	return `"level":{"type":"string","enum":["info","action"],"description":"action when the person has to do something. Default: info."}`
}

const notificationTextSchema = `"title":{"type":"string","description":"1–120 characters."},` +
	`"body":{"type":"string","description":"Up to 2000 characters of plain text."},`

const notificationListSchema = `"unread_only":{"type":"boolean","description":"Only unread notifications. Default: true."},` +
	`"since":{"type":"string","description":"Only notifications created strictly after this time, RFC 3339 (e.g. 2026-10-09T14:00:00Z)."},` +
	`"limit":{"type":"integer","minimum":1,"maximum":200,"description":"At most this many, newest first. Default: 50."}`

// notificationQuery reads the arguments both list_notifications tools share.
func notificationQuery(args map[string]any) (ports.NotificationQuery, error) {
	q := ports.NotificationQuery{UnreadOnly: getBool(args, "unread_only", true)}
	if s := getString(args, "since", ""); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return q, invalidInput("since must be an RFC 3339 time, such as 2026-10-09T14:00:00Z")
		}
		q.Since = t
	}
	if _, ok := args["limit"]; ok {
		q.Limit = getInt(args, "limit", 0)
		if q.Limit == 0 {
			q.Limit = -1 // an explicit 0 is out of range, not the default
		}
	}
	return q, nil
}

func listNotifications(ctx context.Context, n ports.Notifier, q ports.NotificationQuery) (any, error) {
	page, err := n.ListNotifications(ctx, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"notifications": page.Notifications, "count": len(page.Notifications), "unread_count": page.UnreadCount}, nil
}

func notify(ctx context.Context, n ports.Notifier, req ports.NotifyRequest, args map[string]any) (any, error) {
	req.Title, req.Body, req.Link = getString(args, "title", ""), getString(args, "body", ""), getString(args, "link", "")
	req.Level = domain.NotificationLevel(getString(args, "level", ""))
	got, err := n.Notify(ctx, req)
	if err != nil {
		return nil, err
	}
	return map[string]any{"notification": got}, nil
}

// sessionNotificationTools let a session read the inbox and post to it, as
// itself: what it posts is attributed to the session, its task and its
// project.
func sessionNotificationTools(planningSvc ports.Planning, sessions ports.SessionRepository, nt NotificationTooling) []domain.Tool {
	return []domain.Tool{
		{
			Name: "list_notifications",
			Description: "List the person's notifications across every project, newest first: review requests, sessions waiting for " +
				"approval, tasks in review, failed sessions, and what agents posted. Reading never marks anything read. " +
				"count is how many this call returned; unread_count is the global unread count.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{` +
				`"scope":{"type":"string","enum":["all","project"],"description":"all: every project; project: this session's project. Default: all."},` +
				notificationListSchema + `}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				if nt.Notifier == nil {
					return nil, errNotificationsOff
				}
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				q, err := notificationQuery(args)
				if err != nil {
					return nil, err
				}
				switch getString(args, "scope", "all") {
				case "all":
				case "project":
					q.ProjectID = scope.session.ProjectID
				default:
					return nil, invalidInput("scope must be all or project")
				}
				return listNotifications(ctx, nt.Notifier, q)
			},
		},
		{
			Name: "notify",
			Description: "Post a notification to the person's inbox, attributed to this session, its task and its project. It shows in " +
				"the web UI live and goes out through every enabled delivery provider. " + notifyAdvice +
				"At most 20 per session per rolling hour (NOTIFICATION_RATE_LIMITED).",
			InputSchema: schemaFromJSON(`{"type":"object","required":["title"],"properties":{` + notificationTextSchema +
				notificationLevelSchema() + `,` +
				`"link":{"type":"string","description":"A harness path starting with \"/\", such as /projects/<project>/tasks/<task>. Default: this task's page."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				if nt.Notifier == nil {
					return nil, errNotificationsOff
				}
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				return notify(ctx, nt.Notifier, ports.NotifyRequest{
					SessionID: scope.session.ID, ProjectID: scope.ticket.ProjectID, TaskID: scope.ticket.ID,
				}, args)
			},
		},
	}
}

// NotificationTools are the global MCP server's notification tools, for
// agents outside a harness session: they name the project (and task)
// themselves, and what they post has no session.
func NotificationTools(n ports.Notifier) []domain.Tool {
	return []domain.Tool{
		{
			Name: "list_notifications",
			Description: "List the person's notifications, newest first, across every project or one: review requests, sessions waiting " +
				"for approval, tasks in review, failed sessions, and what agents posted. Reading never marks anything read. " +
				"count is how many this call returned; unread_count is the global unread count.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{` +
				`"project_id":{"type":"string","description":"Only this project's notifications. Default: every project."},` +
				notificationListSchema + `}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				q, err := notificationQuery(args)
				if err != nil {
					return nil, err
				}
				q.ProjectID = getString(args, "project_id", "")
				return listNotifications(ctx, n, q)
			},
		},
		{
			Name: "notify",
			Description: "Post a notification to the person's inbox in a project, optionally about one of its tasks. It shows in the web " +
				"UI live and goes out through every enabled delivery provider. " + notifyAdvice +
				"At most 20 per project per rolling hour for notifications posted without a session (NOTIFICATION_RATE_LIMITED).",
			InputSchema: schemaFromJSON(`{"type":"object","required":["project_id","title"],"properties":{` +
				`"project_id":{"type":"string","description":"The project it belongs to."},` +
				`"task_id":{"type":"string","description":"The task it is about, in that project."},` +
				notificationTextSchema + notificationLevelSchema() + `,` +
				`"link":{"type":"string","description":"A harness path starting with \"/\". Default: the task's page, or the project's board without a task."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return notify(ctx, n, ports.NotifyRequest{ProjectID: getString(args, "project_id", ""), TaskID: getString(args, "task_id", "")}, args)
			},
		},
	}
}
