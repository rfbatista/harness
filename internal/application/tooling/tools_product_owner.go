package tooling

import (
	"context"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ProductOwnerToolNames are the tools a product-owner thread may call, in
// order: a mode allow-list (ADR 0109). The first four are task tools every
// session has; the rest are the product owner's own reads. None starts a
// session, moves a branch, pushes, merges or writes, and the endpoint serves
// a product-owner session no other tool, so the line is held by the server,
// not by the prompt.
var ProductOwnerToolNames = []string{
	"list_project_tasks",
	"list_project_repositories",
	"list_bounded_contexts",
	"list_notifications",
	"get_project_task",
	"list_project_sessions",
	"list_project_branches",
	"list_memory",
	"save_memory",
	"change_memory",
	"drop_memory",
}

// ProductOwnerSources is what the product owner's own reads come from.
type ProductOwnerSources struct {
	Tasks    ports.TicketReader
	Changes  ports.TaskStatusChangeRepository
	Sessions ports.SessionRepository
	// Memory drives the product owner's memory tools (ADR 0111).
	Memory ports.ProductOwnerMemoryTools
}

// ProductOwnerTools are the tools the product-owner session endpoint serves:
// of the session task tools in all, those in the allow-list, and the
// product owner's reads of tasks with their status history, of other
// sessions' status, outcome and summary, and of branches and merges.
func ProductOwnerTools(all []domain.Tool, src ProductOwnerSources) []domain.Tool {
	var out []domain.Tool
	for _, name := range ProductOwnerToolNames {
		if i := slices.IndexFunc(all, func(t domain.Tool) bool { return t.Name == name }); i >= 0 {
			out = append(out, all[i])
		}
	}
	out = append(out, productOwnerReads(src)...)
	return append(out, productOwnerMemoryTools(src.Memory)...)
}

func productOwnerReads(src ProductOwnerSources) []domain.Tool {
	return []domain.Tool{
		{
			Name: "get_project_task",
			Description: "Read one task of this project: its title, description and status, the task it is a child of, its " +
				"children, and its status history, oldest first, with who moved it and why. Its documents are files in your " +
				"virtual shell, under tasks/<id>/docs/.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string","description":"The task's id, from list_project_tasks."}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, src.Sessions)
				if err != nil {
					return nil, err
				}
				tk, err := src.Tasks.GetTicket(ctx, getString(args, "task_id", ""))
				if err != nil || tk == nil || tk.ProjectID != sess.ProjectID {
					return nil, &domain.StructuredError{Code: "TASK_NOT_FOUND", Message: "TASK_NOT_FOUND: no task " + getString(args, "task_id", "") + " in this project: use an id from list_project_tasks"}
				}
				children, err := src.Tasks.ListTickets(ctx, sess.ProjectID, ports.TicketFilter{ParentID: &tk.ID})
				if err != nil {
					return nil, err
				}
				kids := make([]map[string]any, 0, len(children))
				for _, c := range children {
					kids = append(kids, map[string]any{"id": c.ID, "title": c.Title, "status": c.Status})
				}
				history := []map[string]any{}
				for _, c := range src.Changes.ListByTask(tk.ID) {
					history = append(history, map[string]any{"status": c.Status, "at": c.At, "by": c.By, "reason": c.Reason})
				}
				return map[string]any{
					"task": map[string]any{
						"id": tk.ID, "title": tk.Title, "description": tk.Description, "status": tk.Status, "parent_id": tk.ParentID,
						"created_at": tk.CreatedAt, "updated_at": tk.UpdatedAt, "archived_at": tk.ArchivedAt,
					},
					"children":       kids,
					"status_history": history,
				}, nil
			},
		},
		{
			Name: "list_project_sessions",
			Description: "List the other agent sessions of this project: for each, what it was asked (summary), its status, " +
				"its outcome (how it ended, and what became of its branch), the task it works on and its agent. " +
				"That is all you can see of a session: its transcript and its worktree are not available to you.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"task_id":{"type":"string","description":"Only the sessions of this task."},"include_ended":{"type":"boolean","description":"Also list sessions that finished, failed or were stopped. Default: only the live ones."}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, src.Sessions)
				if err != nil {
					return nil, err
				}
				list := src.Sessions.List(ctx, ports.SessionFilter{ProjectID: sess.ProjectID, TicketID: getString(args, "task_id", "")})
				ended, _ := args["include_ended"].(bool)
				rows := make([]map[string]any, 0, len(list))
				for _, s := range list {
					if s.ID == sess.ID || (!ended && s.Status.IsTerminal()) {
						continue
					}
					rows = append(rows, map[string]any{
						"id": s.ID, "agent_id": s.AgentID, "mode": s.Mode, "task_id": s.TicketID,
						"summary": s.Task, "status": s.Status,
						"outcome": sessionOutcome(s),
					})
				}
				return map[string]any{"count": len(rows), "sessions": rows}, nil
			},
		},
		{
			Name: "list_project_branches",
			Description: "List the branches the project's sessions work on, with the session and task each belongs to, and what " +
				"became of each: whether its branch was merged and into what, or why a merge was refused.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, src.Sessions)
				if err != nil {
					return nil, err
				}
				var rows []map[string]any
				for _, s := range src.Sessions.List(ctx, ports.SessionFilter{ProjectID: sess.ProjectID}) {
					if s.Branch == "" || s.ID == sess.ID {
						continue
					}
					rows = append(rows, map[string]any{
						"branch": s.Branch, "repository_id": s.RepositoryID, "session_id": s.ID, "task_id": s.TicketID,
						"session_status": s.Status, "last_merge": s.LastMerge,
					})
				}
				return map[string]any{"count": len(rows), "branches": rows}, nil
			},
		},
	}
}

// sessionOutcome is how a session ended, and what became of its branch, for
// the product owner: nothing of what it did.
func sessionOutcome(s *domain.Session) map[string]any {
	out := map[string]any{"ended": s.Status.IsTerminal()}
	if s.Status.IsTerminal() {
		out["result"] = s.Status
	}
	if s.LastMerge != nil {
		out["merge"] = s.LastMerge
	}
	return out
}
