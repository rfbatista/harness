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
			Description: "List documents in a project. scope narrows to task documents (linked to tickets, written by their sessions) or project documents (architecture, conventions, decisions, readable by every session); omit it for every document.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"scope":{"type":"string","enum":["task","project"],"description":"Only documents of this scope. Default: every document."}},"required":["project_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				pid := getString(args, "project_id", "")
				if pid == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
				}
				scope := domain.DocumentScope(getString(args, "scope", ""))
				if scope != "" && !scope.Valid() {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "scope must be task or project"}
				}
				return map[string]any{"documents": planningSvc.ListDocuments(pid, scope)}, nil
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
			Description: "Create a document in a project. It exists standalone until linked to a ticket. format is markdown (default) or html; an html body is a complete HTML page rendered in a sandboxed frame. scope is project (default: the document is the project's, for every session) or task.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"title":{"type":"string","description":"Document title"},"content":{"type":"string","description":"The body, in format"},"format":{"type":"string","enum":["markdown","html"],"description":"Body format. Default: markdown."},"scope":{"type":"string","enum":["task","project"],"description":"project (default) or task."}},"required":["project_id","title"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				doc, err := planningSvc.CreateDocument(
					getString(args, "project_id", ""),
					getString(args, "title", ""),
					getString(args, "content", ""),
					domain.DocumentFormat(getString(args, "format", "")),
					domain.DocumentScope(getString(args, "scope", "")),
				)
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name:        "update_document",
			Description: "Update a document's title and content. format (markdown or html) is optional; omitted keeps the stored one.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"},"title":{"type":"string"},"content":{"type":"string"},"format":{"type":"string","enum":["markdown","html"],"description":"Body format; omit to keep the current one."}},"required":["document_id","title"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				doc, err := planningSvc.UpdateDocument(
					getString(args, "document_id", ""),
					getString(args, "title", ""),
					getString(args, "content", ""),
					domain.DocumentFormat(getString(args, "format", "")),
				)
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name:        "set_document_scope",
			Description: "Move a document between task and project scope. Its ticket links are untouched: a project document stays listed on the tickets it came from. Idempotent.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"},"scope":{"type":"string","enum":["task","project"],"description":"The scope to move it to."}},"required":["document_id","scope"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				doc, err := planningSvc.SetDocumentScope(getString(args, "document_id", ""), domain.DocumentScope(getString(args, "scope", "")))
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
