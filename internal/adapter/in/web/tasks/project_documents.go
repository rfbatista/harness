package tasks

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// ProjectDocumentViewHref is where a project document's HTML body is served
// for the library's frame; v carries the version so a rewrite is not served
// from cache.
func ProjectDocumentViewHref(projectID, documentID string, updatedAt time.Time) string {
	return ProjectDocumentsHref(projectID, documentID) + "/view?v=" + strconv.FormatInt(updatedAt.UnixMilli(), 10)
}

// ProjectDocumentsView is the project's documents library: its project
// documents, newest first, and the open one rendered with the tasks it came
// from.
type ProjectDocumentsView struct {
	Frame       shell.Frame
	ProjectID   string
	ProjectName string
	Documents   []DocumentLink
	Open        *OpenDocument
}

// projectDocuments resolves the project and its project-scoped documents,
// newest first: the scope the library page and its view route share.
func (h Handler) projectDocuments(r *http.Request) (*domain.Project, []*domain.Document, error) {
	project, err := h.Projects.GetProject(r.Context(), r.PathValue("project"))
	if err != nil {
		return nil, nil, err
	}
	return project, newestFirst(h.Docs.ListDocuments(project.ID, domain.DocumentScopeProject)), nil
}

// ProjectDocumentView serves GET /projects/{project}/documents/{document}/view:
// the HTML body of a project document, as its own page under the artifact
// CSP, for the library page's sandboxed frame.
func (h Handler) ProjectDocumentView(w http.ResponseWriter, r *http.Request) error {
	if h.Docs == nil {
		return errs.Newf("UNAVAILABLE", "documents are not available on this server")
	}
	_, docs, err := h.projectDocuments(r)
	if err != nil {
		return err
	}
	id := r.PathValue("document")
	i := slices.IndexFunc(docs, func(d *domain.Document) bool { return d.ID == id })
	if i < 0 {
		return errs.Newf("DOCUMENT_NOT_FOUND", "document %s is not a project document of this project", id)
	}
	return serveDocumentPage(w, docs[i])
}

// ProjectDocuments serves GET /projects/{project}/documents and
// …/documents/{document}: the project's documents, one of them open (the
// newest when none is named). Task documents are not here: a task's page
// lists them.
func (h Handler) ProjectDocuments(w http.ResponseWriter, r *http.Request) error {
	if h.Docs == nil {
		return errs.Newf("UNAVAILABLE", "documents are not available on this server")
	}
	ctx := r.Context()
	project, docs, err := h.projectDocuments(r)
	if err != nil {
		return err
	}
	openID := r.PathValue("document")
	if openID == "" && len(docs) > 0 {
		openID = docs[0].ID
	}
	now := h.Now()
	view := ProjectDocumentsView{ProjectID: project.ID, ProjectName: project.Name}
	for _, d := range docs {
		updated := sessions.RelativeTime(d.UpdatedAt, now)
		view.Documents = append(view.Documents, DocumentLink{
			Title: titleOf(d), Href: ProjectDocumentsHref(project.ID, d.ID), Updated: updated, Project: true, Current: d.ID == openID,
		})
		if d.ID == openID {
			view.Open = h.openDocument(project.ID, d, updated, ProjectDocumentViewHref(project.ID, d.ID, d.UpdatedAt))
		}
	}
	if view.Open == nil && r.PathValue("document") != "" {
		return errs.Newf("DOCUMENT_NOT_FOUND", "document %s is not a project document of this project", openID)
	}
	title := "Documents · " + project.Name
	if view.Open != nil {
		title = view.Open.Title + " · " + project.Name
	}
	frame, err := h.Layout(ctx, title, project.ID, "")
	if err != nil {
		return err
	}
	view.Frame = frame
	return h.Render(w, r, http.StatusOK, ProjectDocumentsPage(view))
}
