package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// DocumentTools exposes document CRUD and ticket links as MCP tools.
func DocumentTools(planningSvc ports.DocumentLibrary) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "list_documents",
			Description: "List documents in a project.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"}},"required":["project_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				pid := getString(args, "project_id", "")
				if pid == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
				}
				return map[string]any{"documents": planningSvc.ListDocuments(pid)}, nil
			},
		},
		{
			Name:        "get_document",
			Description: "Get one document by id.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"}},"required":["document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				doc := planningSvc.GetDocument(getString(args, "document_id", ""))
				if doc == nil {
					return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_FOUND", Message: "document not found"}
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name:        "create_document",
			Description: "Create a markdown document in a project. It exists standalone until linked to a ticket.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"title":{"type":"string","description":"Document title"},"content":{"type":"string","description":"Markdown content"}},"required":["project_id","title"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				doc, err := planningSvc.CreateDocument(
					getString(args, "project_id", ""),
					getString(args, "title", ""),
					getString(args, "content", ""),
					"",
				)
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name:        "update_document",
			Description: "Update a document's title and content.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"},"title":{"type":"string"},"content":{"type":"string"}},"required":["document_id","title"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				doc, err := planningSvc.UpdateDocument(
					getString(args, "document_id", ""),
					getString(args, "title", ""),
					getString(args, "content", ""),
					"",
				)
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name:        "delete_document",
			Description: "Delete a document by id. Its ticket links are removed; the tickets survive.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"}},"required":["document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "document_id", "")
				if err := planningSvc.DeleteDocument(id); err != nil {
					return nil, err
				}
				return map[string]string{"deleted": id}, nil
			},
		},
		{
			Name:        "link_document_to_ticket",
			Description: "Link a document to a ticket. Both must belong to the same project. Idempotent.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"ticket_id":{"type":"string","description":"Ticket ID"},"document_id":{"type":"string","description":"Document ID"}},"required":["ticket_id","document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				ticketID := getString(args, "ticket_id", "")
				documentID := getString(args, "document_id", "")
				if err := planningSvc.LinkDocument(ticketID, documentID); err != nil {
					return nil, err
				}
				return map[string]string{"ticket_id": ticketID, "document_id": documentID}, nil
			},
		},
		{
			Name:        "unlink_document_from_ticket",
			Description: "Remove the link between a document and a ticket. The document and ticket both survive.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"ticket_id":{"type":"string","description":"Ticket ID"},"document_id":{"type":"string","description":"Document ID"}},"required":["ticket_id","document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				ticketID := getString(args, "ticket_id", "")
				documentID := getString(args, "document_id", "")
				if err := planningSvc.UnlinkDocument(ticketID, documentID); err != nil {
					return nil, err
				}
				return map[string]string{"unlinked_ticket_id": ticketID, "unlinked_document_id": documentID}, nil
			},
		},
		{
			Name:        "list_ticket_documents",
			Description: "List the documents linked to a ticket.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"ticket_id":{"type":"string","description":"Ticket ID"}},"required":["ticket_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				ticketID := getString(args, "ticket_id", "")
				if ticketID == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
				}
				return map[string]any{"documents": planningSvc.ListTicketDocuments(ticketID)}, nil
			},
		},
	}
}
