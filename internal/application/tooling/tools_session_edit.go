package tooling

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const editSchema = `{"type":"object","required":["document_id","old_string","new_string"],"properties":{` +
	`"document_id":{"type":"string","description":"Document ID"},` +
	`"old_string":{"type":"string","description":"Text to replace, copied exactly from the stored markup (read the document with view raw to see it). It must match once, unless replace_all is set."},` +
	`"new_string":{"type":"string","description":"What replaces it."},` +
	`"replace_all":{"type":"boolean","description":"Replace every match instead of requiring exactly one. Default false."},` +
	`"expected_version":{"type":"string","description":"The version you read (from a read or a listing); the edit is refused with DOCUMENT_CHANGED if the document has changed since."}}}`

// editedDocument is a document as an edit answers it: no content.
type editedDocument struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Bytes     int       `json:"bytes"`
	Lines     int       `json:"lines"`
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// editTool is edit_task_document or edit_project_document: find resolves the
// document under the same scope rules as the matching update tool.
func editTool(name, whose string, planningSvc ports.Planning, sessions ports.SessionRepository,
	find func(scope *taskScope, documentID string) (*domain.Document, error)) domain.Tool {
	return domain.Tool{
		Name: name,
		Description: "Change part of " + whose + " without sending the whole page: replace old_string with new_string in its stored HTML. " +
			"old_string must match the stored markup exactly and only once (or set replace_all); there is no fuzzy matching. " +
			"Pass expected_version to make sure nobody changed it since you read it. The result must still be a complete HTML page. " +
			"Markdown documents are refused: rewrite them once as HTML with the update tool first.",
		InputSchema: schemaFromJSON(editSchema),
		Source:      "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			scope, err := resolveTaskScope(ctx, planningSvc, sessions)
			if err != nil {
				return nil, err
			}
			current, err := find(scope, getString(args, "document_id", ""))
			if err != nil {
				return nil, err
			}
			oldS, hasOld := args["old_string"].(string)
			newS, hasNew := args["new_string"].(string)
			if !hasOld || !hasNew {
				return nil, coded("INVALID_INPUT", "old_string and new_string are required")
			}
			d, err := planningSvc.EditDocument(current.ID, oldS, newS, getBool(args, "replace_all", false), getString(args, "expected_version", ""))
			if err != nil {
				return nil, err
			}
			return map[string]any{"document": editedDocument{ID: d.ID, Title: d.Title, Bytes: len(d.Content),
				Lines: len(newTextView(d.Content, documentKind(d)).lines), Version: domain.DocumentVersion(d.Content), UpdatedAt: d.UpdatedAt}}, nil
		},
	}
}
