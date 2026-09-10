package tooling

import (
	"context"
	"time"

	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// SessionTaskToolNames lists the tools SessionTaskTools returns, in order. It is
// the single source of truth for the orchestration's allow-list.
var SessionTaskToolNames = []string{
	"get_task",
	"list_task_documents",
	"read_task_document",
	"create_task_document",
	"update_task_document",
}

// SessionTaskTools exposes the task a session was spawned into, and the documents
// linked to that task, as MCP tools.
//
// The tools take no project or ticket id: the session id travels in the context
// (see WithSessionID) and every handler resolves the scope from it, so a session
// can only ever reach its own task and the documents linked to it.
func SessionTaskTools(planningSvc *planning.Service, sessions ports.SessionRepository) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "get_task",
			Description: "Get the task this session is working on, with the documents linked to it.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"task":      scope.ticket,
					"documents": summarizeDocuments(planningSvc.ListTicketDocuments(scope.ticket.ID)),
				}, nil
			},
		},
		{
			Name:        "list_task_documents",
			Description: "List the documents linked to this session's task. Titles only — use read_task_document for the body.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"documents": summarizeDocuments(planningSvc.ListTicketDocuments(scope.ticket.ID)),
				}, nil
			},
		},
		{
			Name:        "read_task_document",
			Description: "Read one document linked to this session's task, including its markdown content.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"}},"required":["document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				doc, err := scope.document(planningSvc, getString(args, "document_id", ""))
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name:        "create_task_document",
			Description: "Write a new markdown document and link it to this session's task. Use it to hand plans, findings and decisions to whoever picks the task up next.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"title":{"type":"string","description":"Document title"},"content":{"type":"string","description":"Markdown content"}},"required":["title"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				doc, err := planningSvc.CreateDocument(
					scope.session.ProjectID,
					getString(args, "title", ""),
					getString(args, "content", ""),
				)
				if err != nil {
					return nil, err
				}
				if err := planningSvc.LinkDocument(scope.ticket.ID, doc.ID); err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name:        "update_task_document",
			Description: "Revise a document linked to this session's task. Omitted fields keep their current value.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"},"title":{"type":"string","description":"New title; omit to keep the current one"},"content":{"type":"string","description":"New markdown content; omit to keep the current one"}},"required":["document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				current, err := scope.document(planningSvc, getString(args, "document_id", ""))
				if err != nil {
					return nil, err
				}
				// Partial edit: the underlying use case replaces both fields, so
				// anything the agent left out is filled back in from the stored doc.
				doc, err := planningSvc.UpdateDocument(
					current.ID,
					getString(args, "title", current.Title),
					getString(args, "content", current.Content),
				)
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
	}
}

// taskScope is the task a session is allowed to act on: resolved once per call
// from the session id in the context, never from the tool arguments.
type taskScope struct {
	session *domain.Session
	ticket  *domain.Ticket
}

func resolveTaskScope(ctx context.Context, planningSvc *planning.Service, sessions ports.SessionRepository) (*taskScope, error) {
	id := SessionIDFrom(ctx)
	if id == "" {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no session in context"}
	}
	sess := sessions.Get(id)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	if sess.TicketID == "" {
		return nil, &domain.StructuredError{Code: "SESSION_HAS_NO_TASK", Message: "this session is not working on a task"}
	}
	tk := planningSvc.GetTicket(sess.TicketID)
	if tk == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "task not found"}
	}
	return &taskScope{session: sess, ticket: tk}, nil
}

// document returns the document only when it is linked to the scoped task, so a
// session cannot read or edit a document belonging to another task.
func (s *taskScope) document(planningSvc *planning.Service, documentID string) (*domain.Document, error) {
	if documentID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "document_id is required"}
	}
	for _, d := range planningSvc.ListTicketDocuments(s.ticket.ID) {
		if d.ID == documentID {
			return d, nil
		}
	}
	return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_ON_TASK", Message: "document is not linked to this session's task"}
}

// documentSummary is a document without its body, for listings that would
// otherwise flood the agent's context with markdown.
type documentSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

func summarizeDocuments(docs []*domain.Document) []documentSummary {
	out := make([]documentSummary, 0, len(docs))
	for _, d := range docs {
		out = append(out, documentSummary{ID: d.ID, Title: d.Title, UpdatedAt: d.UpdatedAt})
	}
	return out
}
