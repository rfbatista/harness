// Package web is the server half of the web client: a Backend For Frontend
// that renders pages with templ from the driving ports, in process. The
// browser half (web/) takes over once a page has loaded; see docs/WEB.md.
package web

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/history"
	"operators-mcp/internal/adapter/in/web/home"
	"operators-mcp/internal/adapter/in/web/projects"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/adapter/in/web/tasks"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Deps are the driving ports the pages read. Each page takes the narrowest.
type Deps struct {
	Projects ports.ProjectReader
	Tasks    ports.TicketReader
	Sessions ports.SessionReader
	// Agents and Repositories fill the new-session form.
	Agents       ports.AgentLister
	Repositories ports.RepositoryLister
	// EnvFiles lists a repository's env files for their page.
	EnvFiles ports.EnvFileLister
	// Documents reads a task's documents and the project's library; nil hides both.
	Documents ports.DocumentReader
	// History reads repositories' commit graphs for their history pages.
	History ports.RepositoryHistory
	// Now defaults to time.Now.
	Now func() time.Time
}

// NewHandler routes the web client's pages and assets. Requests it does not
// own go to fallback (nil: 404), which keeps the legacy designer reachable
// while the two coexist.
func NewHandler(deps Deps, assets *Assets, fallback http.Handler) http.Handler {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if fallback == nil {
		fallback = http.NotFoundHandler()
	}
	s := &server{deps: deps, assets: assets}

	homePage := home.Handler{Projects: deps.Projects, Layout: s.layout, Render: render}
	taskPages := tasks.Handler{
		Projects: deps.Projects, Tasks: deps.Tasks, Sessions: deps.Sessions,
		Agents: deps.Agents, Repositories: deps.Repositories, Docs: deps.Documents,
		Layout: s.layout, Render: render, Now: deps.Now,
	}
	projectPages := projects.Handler{Projects: deps.Projects, Repositories: deps.Repositories, EnvFiles: deps.EnvFiles, Layout: s.layout, Render: render}
	historyPages := history.Handler{
		Projects: deps.Projects, Repositories: deps.Repositories, History: deps.History, Sessions: deps.Sessions, Tasks: deps.Tasks,
		Layout: s.layout, Render: render, Now: deps.Now,
	}
	toProject := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/projects/"+url.PathEscape(r.PathValue("project")), http.StatusFound)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", assets.Handler())
	mux.Handle("GET /{$}", s.page(homePage.Index))
	mux.HandleFunc("GET /switch-project", switchProject)
	mux.Handle("GET /projects/new", s.page(projectPages.New))
	mux.Handle("GET /projects/{project}", s.page(taskPages.Project))
	mux.Handle("GET /projects/{project}/repositories", s.page(projectPages.RepositoryList))
	mux.Handle("GET /projects/{project}/repositories/{repository}/env", s.page(projectPages.EnvFileList))
	mux.Handle("GET /projects/{project}/repositories/{repository}/history", s.page(historyPages.Page))
	mux.Handle("GET /projects/{project}/repositories/{repository}/history/{commit}", s.page(historyPages.Page))
	mux.Handle("GET /projects/{project}/design", s.page(taskPages.ProjectDesign))
	mux.Handle("GET /projects/{project}/documents", s.page(taskPages.ProjectDocuments))
	mux.Handle("GET /projects/{project}/documents/{document}", s.page(taskPages.ProjectDocuments))
	mux.Handle("GET /projects/{project}/documents/{document}/view", s.page(taskPages.ProjectDocumentView))
	mux.HandleFunc("GET /projects/{project}/{$}", toProject)
	mux.HandleFunc("GET /projects/{project}/sessions", toProject) // the old page; sessions now live under their task
	mux.Handle("GET /projects/{project}/tasks/new", s.page(taskPages.New))
	mux.Handle("GET /projects/{project}/tasks/{task}", s.page(taskPages.Task))
	mux.Handle("GET /projects/{project}/tasks/{task}/sessions/{session}/history", s.page(historyPages.Session))
	mux.Handle("GET /projects/{project}/tasks/{task}/sessions/{session}/history/{commit}", s.page(historyPages.Session))
	mux.Handle("GET /projects/{project}/tasks/{task}/documents", s.page(taskPages.Documents))
	mux.Handle("GET /projects/{project}/tasks/{task}/documents/{document}", s.page(taskPages.Documents))
	mux.Handle("GET /projects/{project}/tasks/{task}/documents/{document}/view", s.page(taskPages.DocumentView))
	mux.Handle("/", fallback)
	return mux
}

type server struct {
	deps   Deps
	assets *Assets
}

// page adapts a page handler: an error becomes the error page for its code.
func (s *server) page(h shell.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.renderError(w, r, err)
		}
	})
}

// switchProject is the project picker's target: a GET form, so switching
// works without JavaScript too.
func switchProject(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("project")
	if id == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/projects/"+url.PathEscape(id), http.StatusFound)
}

// layout builds the shared frame: the assets, the project picker, and the
// selected project's tasks on the rail with their live sessions.
func (s *server) layout(ctx context.Context, title, projectID, taskID string) (shell.Frame, error) {
	frame := s.bareFrame(title)
	projects, err := s.deps.Projects.ListProjects(ctx)
	if err != nil {
		return shell.Frame{}, err
	}
	slices.SortFunc(projects, func(a, b *domain.Project) int {
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	frame.Top.Current = projectID
	for _, p := range projects {
		frame.Top.Projects = append(frame.Top.Projects, shell.Option{ID: p.ID, Name: p.Name})
	}
	if projectID == "" {
		return frame, nil
	}

	list, err := s.deps.Tasks.ListTickets(ctx, projectID)
	if err != nil {
		return shell.Frame{}, err
	}
	live, err := s.deps.Sessions.List(ctx, ports.SessionFilter{ProjectID: projectID})
	if err != nil {
		return shell.Frame{}, err
	}
	frame.Rail = tasks.BuildRail(projectID, list, live, taskID)
	frame.Rail.NewTaskHref = tasks.NewTaskHref(projectID)
	if s.deps.Documents != nil {
		frame.Rail.DocumentsHref = tasks.ProjectDocumentsHref(projectID, "")
	}
	frame.Rail.DesignHref = tasks.ProjectDesignHref(projectID)
	return frame, nil
}

// bareFrame is a frame without the rail, for when the rail cannot be read.
func (s *server) bareFrame(title string) shell.Frame {
	return shell.Frame{
		Title: title,
		CSS:   s.assets.URL("app.css"),
		JS:    s.assets.URL("app.js"),
		Built: s.assets.Built(),
	}
}

// renderError shows a coded error the way the API reports it: the message,
// the code, and what to do next. A *_NOT_FOUND is a 404; anything uncoded is
// a 500 whose details stay in the log.
func (s *server) renderError(w http.ResponseWriter, r *http.Request, err error) {
	code := errs.Code(err)
	status, message := http.StatusInternalServerError, "Something went wrong on the server."
	switch {
	case strings.HasSuffix(code, "_NOT_FOUND"):
		status, message = http.StatusNotFound, errorMessage(err)
	case code != "":
		message = errorMessage(err)
	default:
		code = "INTERNAL"
		slog.Error("web page failed", "path", r.URL.Path, "err", err)
	}
	if renderErr := render(w, r, status, errorPage(s.bareFrame("Error"), message, code, nextStep(code))); renderErr != nil {
		slog.Error("web error page failed", "path", r.URL.Path, "err", renderErr)
	}
}

func errorMessage(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) && se.Message != "" {
		return se.Message
	}
	return err.Error()
}

func nextStep(code string) string {
	switch code {
	case "PROJECT_NOT_FOUND":
		return "pick another project at the top"
	case "TICKET_NOT_FOUND":
		return "it was deleted or moved; pick a task on the rail"
	case "SESSION_NOT_FOUND":
		return "it was deleted; go back to the list"
	case "INTERNAL":
		return "check the server log, then reload"
	default:
		return "reload, or check the server log"
	}
}

// render writes page with status. It renders to a buffer first, so a template
// failure becomes an error instead of half a page.
func render(w http.ResponseWriter, r *http.Request, status int, page templ.Component) error {
	var buf bytes.Buffer
	if err := page.Render(r.Context(), &buf); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}
