package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// TicketTools exposes ticket CRUD as MCP tools.
func TicketTools(planningSvc ports.TicketBoard) []domain.Tool {
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
			Name:        "update_ticket",
			Description: "Update a ticket's title, description, and status.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"ticket_id":{"type":"string","description":"Ticket ID"},"title":{"type":"string"},"description":{"type":"string"},"status":{"type":"string","enum":["backlog","todo","in_progress","review","done"]}},"required":["ticket_id","title"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				tk, err := planningSvc.UpdateTicket(ctx,
					getString(args, "ticket_id", ""),
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
