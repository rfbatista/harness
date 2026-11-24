// Package productowner serves a product-owner thread's Context page,
// /projects/{project}/product-owner/threads/{thread}/context. A thread is a
// session with no task, kept out of every session list (ADR 0109), so the
// session pane that holds the Context tab (ADR 0099) never opens for it. The
// page shows the same Context island for the thread's session.
package productowner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/ports"
)

// ThreadHref is a thread's page, which the React web client serves.
func ThreadHref(projectID, threadID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/product-owner/threads/" + url.PathEscape(threadID)
}

// ContextHref is a thread's Context page.
func ContextHref(projectID, threadID string) string {
	return ThreadHref(projectID, threadID) + "/context"
}

// Threads reads one thread, as ports.ProductOwner does.
type Threads interface {
	Thread(ctx context.Context, threadID, before string) (*ports.ProductOwnerThreadPage, error)
}

// Handler serves the Context page.
type Handler struct {
	Projects ports.ProjectReader
	Threads  Threads
	Sessions ports.SessionReader
	Layout   shell.Layout
	Render   shell.Renderer
}

// contextProps are the Context island's props: the session it shows, its
// project (whose feed announces changes) and whether it is running.
type contextProps struct {
	SessionID string `json:"session_id"`
	ProjectID string `json:"project_id"`
	Live      bool   `json:"live"`
	Group     string `json:"group"`
	Parent    string `json:"parent"`
	Focus     bool   `json:"focus"`
	// WhiteboardHref is empty: a thread has no task page.
	WhiteboardHref string `json:"whiteboard_href"`
}

// Context renders the page. A thread of another project is not found.
func (h Handler) Context(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	page, err := h.Threads.Thread(ctx, r.PathValue("thread"), "")
	if err != nil {
		return err
	}
	t := page.Thread
	if t.ProjectID != project.ID || t.SessionID == "" {
		return errs.Newf("THREAD_NOT_FOUND", "thread %s is not in project %s", t.ID, project.Name)
	}
	live := false
	if sess, err := h.Sessions.Get(ctx, t.SessionID); err == nil {
		live = sessions.IsLive(sess)
	}
	props, err := json.Marshal(contextProps{SessionID: t.SessionID, ProjectID: project.ID, Live: live})
	if err != nil {
		return err
	}
	frame, err := h.Layout(ctx, "Thread context", project.ID, "")
	if err != nil {
		return err
	}
	frame.Live = true
	return h.Render(w, r, http.StatusOK, Page(frame, project.ID, project.Name, t.Title, ThreadHref(project.ID, t.ID), string(props)))
}
