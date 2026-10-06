package tasks

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/httpapi"
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

// DocumentViewHref is where a task document's HTML body is served for the
// page's frame; v carries the version so a rewrite is not served from cache.
func DocumentViewHref(projectID, taskID, documentID string, updatedAt time.Time) string {
	return DocumentsHref(projectID, taskID, documentID) + "/view?v=" + strconv.FormatInt(updatedAt.UnixMilli(), 10)
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

// OpenDocument is the document being read: a Markdown one rendered into
// Body, an HTML one framed from FrameSrc. Never both.
type OpenDocument struct {
	Title    string
	Updated  string
	Format   domain.DocumentFormat
	Body     templ.Component // markdown
	FrameSrc string          // html
}

// IsHTML says the open document is a page for the frame.
func (o *OpenDocument) IsHTML() bool { return o.Format == domain.DocumentFormatHTML }

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

// taskDocuments resolves the project, the task (which must be in it) and the
// task's documents, newest first: the scope the documents page and the view
// route share.
func (h Handler) taskDocuments(r *http.Request) (*domain.Project, *domain.Ticket, []*domain.Document, error) {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return nil, nil, nil, err
	}
	task, err := h.Tasks.GetTicket(ctx, r.PathValue("task"))
	if err != nil {
		return nil, nil, nil, err
	}
	if task.ProjectID != project.ID {
		return nil, nil, nil, errs.Newf("TICKET_NOT_FOUND", "task %s is not in project %s", task.ID, project.Name)
	}
	docs := slices.Clone(h.Docs.ListTicketDocuments(task.ID))
	slices.SortStableFunc(docs, func(a, b *domain.Document) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return project, task, docs, nil
}

// DocumentView serves GET …/documents/{document}/view: the HTML body of a
// document linked to the task, as its own page under the artifact CSP. The
// documents page frames it in <iframe sandbox="allow-scripts">; both halves
// keep an agent's script away from the harness API and the parent page.
func (h Handler) DocumentView(w http.ResponseWriter, r *http.Request) error {
	if h.Docs == nil {
		return errs.Newf("UNAVAILABLE", "documents are not available on this server")
	}
	_, _, docs, err := h.taskDocuments(r)
	if err != nil {
		return err
	}
	id := r.PathValue("document")
	i := slices.IndexFunc(docs, func(d *domain.Document) bool { return d.ID == id })
	if i < 0 {
		return errs.Newf("DOCUMENT_NOT_FOUND", "document %s is not linked to this task", id)
	}
	if docs[i].Format != domain.DocumentFormatHTML {
		return errs.Newf("DOCUMENT_NOT_FOUND", "document %s is not an HTML page", id)
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Content-Security-Policy", httpapi.ArtifactCSP)
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Disposition", "inline")
	hdr.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, err = io.WriteString(w, docs[i].Content)
	return err
}

// Documents serves GET /projects/{project}/tasks/{task}/documents and
// …/documents/{document}: the task's documents, one of them open (the
// newest when none is named).
func (h Handler) Documents(w http.ResponseWriter, r *http.Request) error {
	if h.Docs == nil {
		return errs.Newf("UNAVAILABLE", "documents are not available on this server")
	}
	ctx := r.Context()
	project, task, docs, err := h.taskDocuments(r)
	if err != nil {
		return err
	}

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
			open := &OpenDocument{Title: titleOf(d), Updated: updated, Format: d.Format}
			if d.Format == domain.DocumentFormatHTML {
				open.FrameSrc = DocumentViewHref(project.ID, task.ID, d.ID, d.UpdatedAt)
			} else {
				open.Body = renderMarkdown(d.Content) // markdown, and the legacy empty format
			}
			view.Open = open
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
