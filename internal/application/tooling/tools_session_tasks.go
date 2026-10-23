package tooling

import (
	"context"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sessionTasksTools let a session shape the work of its project: create a
// task and list the project's tasks. They go through the board
// (ports.TicketBoard), which owns the parent rule, and are scoped by the
// session alone — a session without a task (a product session on a project)
// can use them, so they resolve the project, not the task.
func sessionTasksTools(board ports.TicketBoard, sessions ports.SessionRepository) []domain.Tool {
	return []domain.Tool{
		{
			Name: "create_task",
			Description: "Create a task in this session's project. parent_id defaults to this session's task, so a child " +
				"hangs off the feature you are on; pass parent_id as the empty string \"\" explicitly to create a top-level " +
				"task (the feature task itself, when your session has no task). Any other parent_id must be a top-level " +
				"task of this project, else TICKET_PARENT_INVALID. The task starts in backlog.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["title"],"properties":{` +
				`"title":{"type":"string","description":"The task's title: the piece of work in a line."},` +
				`"description":{"type":"string","description":"What the task is about, in a few sentences."},` +
				`"parent_id":{"type":"string","description":"The feature task this one belongs to. Default: this session's task; \"\" for a top-level task."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, sessions)
				if err != nil {
					return nil, err
				}
				title := strings.TrimSpace(getString(args, "title", ""))
				if title == "" {
					return nil, invalidInput("title is required")
				}
				parentID := sess.TicketID
				if _, given := args["parent_id"]; given {
					parentID = getString(args, "parent_id", "")
				}
				tk, err := board.CreateTicket(ctx, sess.ProjectID, title, getString(args, "description", ""), domain.TicketStatusBacklog, parentID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"task": tk}, nil
			},
		},
		{
			Name: "list_project_tasks",
			Description: "List the tasks of this session's project — id, title, status and parent_id (\"\" for a top-level " +
				"task). Pass parent_id to list only the children of that task; pass it as \"\" to list only the top-level " +
				"tasks. Check it before creating a task the project already has.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{` +
				`"parent_id":{"type":"string","description":"List only the children of this task; \"\" for the top-level tasks. Default: every task."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, sessions)
				if err != nil {
					return nil, err
				}
				var filter ports.TicketFilter
				if _, given := args["parent_id"]; given {
					parentID := getString(args, "parent_id", "")
					filter.ParentID = &parentID
				}
				list, err := board.ListTickets(ctx, sess.ProjectID, filter)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(list))
				for _, tk := range list {
					out = append(out, map[string]any{
						"id":        tk.ID,
						"title":     tk.Title,
						"status":    tk.Status,
						"parent_id": tk.ParentID,
					})
				}
				return map[string]any{"count": len(out), "tasks": out}, nil
			},
		},
	}
}

// resolveProjectScope is the lighter cousin of resolveTaskScope: the session
// in context, whether or not it works on a task. Tools that act on the
// session's project rather than on its task use it.
func resolveProjectScope(ctx context.Context, sessions ports.SessionRepository) (*domain.Session, error) {
	id := SessionIDFrom(ctx)
	if id == "" {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no session in context"}
	}
	sess := sessions.Get(id)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return sess, nil
}
