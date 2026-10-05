package tasks

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/a-h/templ"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// DocumentsHref is a task's documents page; with documentID, that document
// open on it.
func DocumentsHref(projectID, taskID, documentID string) string {
	href := Href(projectID, taskID) + "/documents"
	if documentID != "" {
		href += "/" + url.PathEscape(documentID)
	}
	return href
}

// DocumentsView is a task's documents page: the documents linked to the
// task, newest first, and the open one rendered.
type DocumentsView struct {
	Frame       shell.Frame
	ProjectID   string
	ProjectName string
	TaskHref    string
	TaskTitle   string
	Documents   []DocumentLink
	Open        *OpenDocument
	Watch       DocumentWatch
}

// DocumentLink is one document in the list.
type DocumentLink struct {
	Title, Href, Updated string
	Current              bool
}

// OpenDocument is the document being read.
type OpenDocument struct {
	Title   string
	Updated string
	Body    templ.Component
}

// DocumentWatch lets the browser notice documents written after the page
// was rendered: the task, and a signature of what it shows.
type DocumentWatch struct {
	TicketID  string
	Signature string
	Count     int
}

// documentSignature identifies a set of documents and their versions: each
// id with its updated_at exactly as the API writes it, sorted. The browser's
// documentSignature (tasks/domain/documents.js) computes the same from
// GET /api/list_ticket_documents.
func documentSignature(docs []*domain.Document) string {
	parts := make([]string, 0, len(docs))
	for _, d := range docs {
		at, _ := d.UpdatedAt.MarshalJSON()
		parts = append(parts, d.ID+"@"+strings.Trim(string(at), `"`))
	}
	slices.Sort(parts)
	return strings.Join(parts, ",")
}

// Documents serves GET /projects/{project}/tasks/{task}/documents and
// …/documents/{document}: the task's documents, one of them open (the
// newest when none is named).
func (h Handler) Documents(w http.ResponseWriter, r *http.Request) error {
	if h.Docs == nil {
		return errs.Newf("UNAVAILABLE", "documents are not available on this server")
	}
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	task, err := h.Tasks.GetTicket(ctx, r.PathValue("task"))
	if err != nil {
		return err
	}
	if task.ProjectID != project.ID {
		return errs.Newf("TICKET_NOT_FOUND", "task %s is not in project %s", task.ID, project.Name)
	}
	docs := slices.Clone(h.Docs.ListTicketDocuments(task.ID))
	slices.SortStableFunc(docs, func(a, b *domain.Document) int { return b.UpdatedAt.Compare(a.UpdatedAt) })

	openID := r.PathValue("document")
	if openID == "" && len(docs) > 0 {
		openID = docs[0].ID
	}
	now := h.Now()
	view := DocumentsView{
		ProjectID:   project.ID,
		ProjectName: project.Name,
		TaskHref:    Href(project.ID, task.ID),
		TaskTitle:   task.Title,
		Watch:       DocumentWatch{TicketID: task.ID, Signature: documentSignature(docs), Count: len(docs)},
	}
	for _, d := range docs {
		updated := sessions.RelativeTime(d.UpdatedAt, now)
		view.Documents = append(view.Documents, DocumentLink{
			Title: titleOf(d), Href: DocumentsHref(project.ID, task.ID, d.ID), Updated: updated, Current: d.ID == openID,
		})
		if d.ID == openID {
			view.Open = &OpenDocument{Title: titleOf(d), Updated: updated, Body: renderMarkdown(d.Content)}
		}
	}
	if view.Open == nil && r.PathValue("document") != "" {
		// Only documents linked to this task open here.
		return errs.Newf("DOCUMENT_NOT_FOUND", "document %s is not linked to this task", openID)
	}

	title := "Documents · " + task.Title
	if view.Open != nil {
		title = view.Open.Title + " · " + task.Title
	}
	frame, err := h.Layout(ctx, title, project.ID, task.ID)
	if err != nil {
		return err
	}
	view.Frame = frame
	return h.Render(w, r, http.StatusOK, DocumentsPage(view))
}

func titleOf(d *domain.Document) string {
	if strings.TrimSpace(d.Title) == "" {
		return "Untitled document"
	}
	return d.Title
}
