package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// TicketTools exposes ticket CRUD as MCP tools.
func TicketTools(planningSvc interface {
	ports.TicketBoard
	ports.TicketPatcher
}) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "list_tickets",
			Description: "List tickets (work items) in a project.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"}},"required":["project_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				pid := getString(args, "project_id", "")
				if pid == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
				}
				tickets, err := planningSvc.ListTickets(ctx, pid)
				if err != nil {
					return nil, err
				}
				return map[string]any{"tickets": tickets}, nil
			},
		},
		{
			Name:        "get_ticket",
			Description: "Get one ticket by id.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"ticket_id":{"type":"string","description":"Ticket ID"}},"required":["ticket_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				tk, err := planningSvc.GetTicket(ctx, getString(args, "ticket_id", ""))
				if err != nil {
					return nil, err
				}
				return map[string]any{"ticket": tk}, nil
			},
		},
		{
			Name:        "create_ticket",
			Description: "Create a ticket (work item) in a project. status defaults to backlog; valid values: backlog, todo, in_progress, review, done.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"title":{"type":"string","description":"Ticket title"},"description":{"type":"string","description":"Ticket description"},"status":{"type":"string","enum":["backlog","todo","in_progress","review","done"]}},"required":["project_id","title"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				tk, err := planningSvc.CreateTicket(ctx,
					getString(args, "project_id", ""),
					getString(args, "title", ""),
					getString(args, "description", ""),
					domain.TicketStatus(getString(args, "status", "")),
				)
				if err != nil {
					return nil, err
				}
				return map[string]any{"ticket": tk}, nil
			},
		},
		{
			Name: "update_ticket",
			Description: "Update a ticket. Fields left out keep their current value: pass only status to move it, only title and/or " +
				"description to edit its text. title cannot be blank; status is one of backlog, todo, in_progress, review, done.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"ticket_id":{"type":"string","description":"Ticket ID"},"title":{"type":"string","description":"New title; omit to keep the current one"},"description":{"type":"string","description":"New description; omit to keep, pass \"\" to clear"},"status":{"type":"string","enum":["backlog","todo","in_progress","review","done"],"description":"New status; omit to keep the current one"}},"required":["ticket_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				// A key that is absent, or null, keeps its value, as over HTTP.
				var patch ports.TicketPatch
				if v, ok := args["title"]; ok && v != nil {
					s := getString(args, "title", "")
					patch.Title = &s
				}
				if v, ok := args["description"]; ok && v != nil {
					s := getString(args, "description", "")
					patch.Description = &s
				}
				if v, ok := args["status"]; ok && v != nil {
					s := domain.TicketStatus(getString(args, "status", ""))
					patch.Status = &s
				}
				tk, err := planningSvc.PatchTicket(ctx, getString(args, "ticket_id", ""), patch)
				if err != nil {
					return nil, err
				}
				return map[string]any{"ticket": tk}, nil
			},
		},
		{
			Name:        "delete_ticket",
			Description: "Delete a ticket by id. Its document links are removed; the documents survive.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"ticket_id":{"type":"string","description":"Ticket ID"}},"required":["ticket_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "ticket_id", "")
				if err := planningSvc.DeleteTicket(ctx, id); err != nil {
					return nil, err
				}
				return map[string]string{"deleted": id}, nil
			},
		},
	}
}
