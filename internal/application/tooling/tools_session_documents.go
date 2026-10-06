package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sessionDocumentTools reach the project's documents from a task session: the
// ones moved to project scope, which every session of the project may read
// and revise, and the moves themselves, which a session may only make on
// documents linked to its own task. Deletion stays with people.
func sessionDocumentTools(planningSvc ports.Planning, sessions ports.SessionRepository) []domain.Tool {
	byID := schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"}},"required":["document_id"]}`)
	move := func(name, description string, to domain.DocumentScope) domain.Tool {
		return domain.Tool{
			Name:        name,
			Description: description,
			InputSchema: byID,
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				// Only a document linked to this session's task moves; the link stays.
				current, err := scope.document(planningSvc, getString(args, "document_id", ""))
				if err != nil {
					return nil, err
				}
				doc, err := planningSvc.SetDocumentScope(current.ID, to)
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		}
	}
	return []domain.Tool{
		{
			Name: "list_project_documents",
			Description: "List the project documents of this session's project: the ones moved to project scope, from any task or none — " +
				"architecture, conventions, decisions, specs and contracts that outlive one task. Titles only — use read_project_document for the body.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"documents": summarizeDocuments(planningSvc.ListDocuments(scope.session.ProjectID, domain.DocumentScopeProject)),
				}, nil
			},
		},
		{
			Name:        "read_project_document",
			Description: "Read one project document of this session's project, including its content and format (html for pages written by sessions, markdown for older documents or ones people wrote).",
			InputSchema: byID,
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				doc, err := scope.projectDocument(planningSvc, getString(args, "document_id", ""))
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name: "update_project_document",
			Description: "Revise a project document of this session's project. Omitted fields keep their current value. New content must be a complete HTML document " +
				"(Markdown is refused with DOCUMENT_NOT_HTML and nothing is stored); it makes an older markdown document an html one.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"},"title":{"type":"string","description":"New title; omit to keep the current one"},"content":{"type":"string","description":"New content as a complete HTML document; omit to keep the current one"}},"required":["document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				current, err := scope.projectDocument(planningSvc, getString(args, "document_id", ""))
				if err != nil {
					return nil, err
				}
				return reviseAsHTML(planningSvc, current, args)
			},
		},
		move("move_document_to_project",
			"Make one of this task's documents a project document: every session of the project can then read and revise it, and it shows on the project's documents page. "+
				"Do it for knowledge that outlives this task (architecture, conventions, decisions, specs and contracts other tasks build on). The document must be linked to this task (DOCUMENT_NOT_ON_TASK otherwise); its link to the task stays. Idempotent.",
			domain.DocumentScopeProject),
		move("move_document_to_task",
			"Move a project document linked to this task back to task scope. The document must be linked to this task (DOCUMENT_NOT_ON_TASK otherwise). Idempotent.",
			domain.DocumentScopeTask),
	}
}

// projectDocument returns the document only when it is a project document of
// the session's project: a task document stays out of reach (even one of this
// task, which the task tools serve), and so does anything of another project.
func (s *taskScope) projectDocument(planningSvc ports.Planning, documentID string) (*domain.Document, error) {
	if documentID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "document_id is required"}
	}
	d := planningSvc.GetDocument(documentID)
	if d == nil || d.ProjectID != s.session.ProjectID || d.Scope != domain.DocumentScopeProject {
		return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_IN_PROJECT", Message: "document is not a project document of this session's project"}
	}
	return d, nil
}

// reviseAsHTML is the partial edit both updaters share: the use case replaces
// both fields, so anything the agent left out is filled back in from the
// stored document. Only new content changes the format, to html, and only a
// complete HTML page is accepted.
func reviseAsHTML(planningSvc ports.Planning, current *domain.Document, args map[string]any) (any, error) {
	content, format := current.Content, domain.DocumentFormat("")
	if v, ok := args["content"].(string); ok {
		if !domain.IsHTMLDocument(v) {
			return nil, notHTML()
		}
		content, format = v, domain.DocumentFormatHTML
	}
	doc, err := planningSvc.UpdateDocument(current.ID, getString(args, "title", current.Title), content, format)
	if err != nil {
		return nil, err
	}
	return map[string]any{"document": doc}, nil
}
