package tooling

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ProductOwnerToolNames are the tools a product-owner thread may call, in
// order: a mode allow-list (ADR 0109). The first four are task tools every
// session has; then the product owner's own reads, its memory tools, and its
// task writes (create_task, update_task, archive_task). None starts a
// session, moves a branch, pushes, merges, moves a task to done or deletes,
// and the endpoint serves a product-owner session no other tool, so the line
// is held by the server, not by the prompt.
var ProductOwnerToolNames = []string{
	"list_project_tasks",
	"list_project_repositories",
	"list_bounded_contexts",
	"list_notifications",
	"get_project_task",
	"list_project_sessions",
	"list_project_branches",
	"list_product_owner_digests",
	"list_memory",
	"save_memory",
	"change_memory",
	"drop_memory",
	// Its writes to tasks: create_task is the session tool every product
	// session has; update_task is its own. Neither moves a task to done,
	// deletes or starts a session, and the tools hold that, not the prompt.
	"create_task",
	"update_task",
	// archive_task holds the archive for the person's click (ADR 0110).
	"archive_task",
}

// ProductOwnerSources is what the product owner's own reads come from.
type ProductOwnerSources struct {
	Tasks    ports.TicketReader
	Changes  ports.TaskStatusChangeRepository
	Sessions ports.SessionRepository
	// Digests are the project's daily digests; nil leaves the tool out.
	Digests ports.ProductOwnerDigests
	// Memory drives the product owner's memory tools (ADR 0111).
	Memory ports.ProductOwnerMemoryTools
	// Patcher changes a task; the product owner's writes go through it, so
	// a session's rules (no move to done) and its attribution apply.
	Patcher ports.TicketPatcher
	// Holds keeps an action for the person's answer; the product owner's
	// archives wait in it.
	Holds ports.ProductOwnerHolds
}

// ProductOwnerTools are the tools the product-owner session endpoint serves:
// of the session task tools in all, those in the allow-list, and the
// product owner's reads of tasks with their status history, of other
// sessions' status, outcome and summary, and of branches and merges.
func ProductOwnerTools(all []domain.Tool, src ProductOwnerSources, with ...ProductOwnerSource) []domain.Tool {
	for _, w := range with {
		w(&src)
	}
	var out []domain.Tool
	for _, name := range ProductOwnerToolNames {
		if i := slices.IndexFunc(all, func(t domain.Tool) bool { return t.Name == name }); i >= 0 {
			out = append(out, all[i])
		}
	}
	out = append(append(out, productOwnerReads(src)...), productOwnerDigestReads(src)...)
	out = append(out, productOwnerMemoryTools(src.Memory)...)
	return append(out, productOwnerWrites(src)...)
}

func productOwnerWrites(src ProductOwnerSources) []domain.Tool {
	return []domain.Tool{
		{
			Name: "archive_task",
			Description: "Ask the person to archive a done task. It does not archive: the person sees a card in this thread " +
				"and confirms or declines it, and you learn the answer on your next turn. Say in your message that you asked.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["task_id"],"properties":{` +
				`"task_id":{"type":"string","description":"The done task's id, from list_project_tasks."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, src.Sessions)
				if err != nil {
					return nil, err
				}
				if src.Holds == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "UNAVAILABLE: held actions are not available on this server"}
				}
				id := getString(args, "task_id", "")
				tk, err := src.Tasks.GetTicket(ctx, id)
				if err != nil || tk == nil || tk.ProjectID != sess.ProjectID {
					return nil, &domain.StructuredError{Code: "TASK_NOT_FOUND", Message: "TASK_NOT_FOUND: no task " + id + " in this project: use an id from list_project_tasks"}
				}
				if tk.Status != domain.TicketStatusDone {
					return nil, &domain.StructuredError{Code: "TASK_NOT_DONE", Message: "TASK_NOT_DONE: only a done task can be archived"}
				}
				if tk.ArchivedAt != nil {
					return nil, &domain.StructuredError{Code: "TASK_ARCHIVED", Message: "TASK_ARCHIVED: the task is archived already"}
				}
				summary := "Archive the task \"" + tk.Title + "\""
				if kids, err := src.Tasks.ListTickets(ctx, sess.ProjectID, ports.TicketFilter{ParentID: &id}); err == nil && len(kids) > 0 {
					summary += " and its " + strconv.Itoa(len(kids)) + " child tasks"
				}
				item, err := src.Holds.HoldAction(ctx, sess.ID, domain.HeldAction{Kind: domain.HeldActionArchiveTask, TaskID: id},
					summary, domain.HeldTarget{Kind: "task", ID: id, Title: tk.Title, URL: "/projects/" + tk.ProjectID + "/tasks/" + id})
				if err != nil {
					return nil, err
				}
				return map[string]any{"held_action_id": item.ID, "state": item.State,
					"message": "Awaiting the person: nothing is archived until they confirm. You learn their answer on your next turn."}, nil
			},
		},
		{
			Name: "update_task",
			Description: "Change a task of this project: its title, its description or its status. Pass only what changes. " +
				"Only the person moves a task to done: asking for done fails with TASK_DONE_IS_THE_PERSONS, so tell the " +
				"person to do it. The change is recorded as yours, with this thread.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["task_id"],"properties":{` +
				`"task_id":{"type":"string","description":"The task's id, from list_project_tasks."},` +
				`"title":{"type":"string"},` +
				`"description":{"type":"string"},` +
				`"status":{"type":"string","enum":["backlog","todo","in_progress","review","done"],"description":"The task's new status. done is refused."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, src.Sessions)
				if err != nil {
					return nil, err
				}
				id := getString(args, "task_id", "")
				tk, err := src.Tasks.GetTicket(ctx, id)
				if err != nil || tk == nil || tk.ProjectID != sess.ProjectID {
					return nil, &domain.StructuredError{Code: "TASK_NOT_FOUND", Message: "TASK_NOT_FOUND: no task " + id + " in this project: use an id from list_project_tasks"}
				}
				var patch ports.TicketPatch
				if v, ok := args["title"].(string); ok {
					patch.Title = &v
				}
				if v, ok := args["description"].(string); ok {
					patch.Description = &v
				}
				if v, ok := args["status"].(string); ok && v != "" {
					st := domain.TicketStatus(v)
					patch.Status = &st
				}
				if patch.Status != nil && *patch.Status == domain.TicketStatusDone {
					return nil, domain.ErrTaskDoneIsThePersons()
				}
				// A task the person made, or whose maker nobody recorded, changes only
				// when the person confirms (ADR 0110).
				if tk.CreatedBy.ByThePerson() {
					return holdChange(ctx, src, sess, tk, patch)
				}
				changed, err := src.Patcher.PatchTicket(ctx, id, patch)
				if err != nil {
					return nil, err
				}
				return map[string]any{"task": changed}, nil
			},
		},
	}
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

// holdChange keeps a change to a task of the person's for their answer: the
// summary says each field's old and new value.
func holdChange(ctx context.Context, src ProductOwnerSources, sess *domain.Session, tk *domain.Ticket, patch ports.TicketPatch) (any, error) {
	if src.Holds == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "UNAVAILABLE: held actions are not available on this server"}
	}
	action := domain.HeldAction{Kind: domain.HeldActionChangeTask, TaskID: tk.ID,
		BaseTitle: tk.Title, BaseDescription: tk.Description, BaseStatus: string(tk.Status)}
	var changes []string
	if patch.Title != nil && *patch.Title != tk.Title {
		action.Title = patch.Title
		changes = append(changes, "title \""+tk.Title+"\" → \""+*patch.Title+"\"")
	}
	if patch.Description != nil && *patch.Description != tk.Description {
		action.Description = patch.Description
		changes = append(changes, "description \""+tk.Description+"\" → \""+*patch.Description+"\"")
	}
	if patch.Status != nil && *patch.Status != tk.Status {
		st := string(*patch.Status)
		action.Status = &st
		changes = append(changes, "status "+string(tk.Status)+" → "+st)
	}
	if len(changes) == 0 {
		return map[string]any{"task": tk, "message": "Nothing to change."}, nil
	}
	item, err := src.Holds.HoldAction(ctx, sess.ID, action, "Change the task \""+tk.Title+"\": "+strings.Join(changes, "; "),
		domain.HeldTarget{Kind: "task", ID: tk.ID, Title: tk.Title, URL: "/projects/" + tk.ProjectID + "/tasks/" + tk.ID})
	if err != nil {
		return nil, err
	}
	return map[string]any{"held_action_id": item.ID, "state": item.State,
		"message": "Awaiting the person: this task is theirs, so nothing changes until they confirm. You learn their answer on your next turn."}, nil
}

// WithHolds gives the product owner's tools the place where an action waits
// for the person's answer (ADR 0110).
func WithHolds(h ports.ProductOwnerHolds) ProductOwnerSource {
	return func(s *ProductOwnerSources) { s.Holds = h }
}
