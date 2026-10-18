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

	"operators-mcp/internal/adapter/in/web/code"
	"operators-mcp/internal/adapter/in/web/credentials"
	"operators-mcp/internal/adapter/in/web/history"
	"operators-mcp/internal/adapter/in/web/home"
	"operators-mcp/internal/adapter/in/web/notifications"
	"operators-mcp/internal/adapter/in/web/projects"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/adapter/in/web/tasks"
	"operators-mcp/internal/adapter/in/web/tokensavers"
	"operators-mcp/internal/adapter/in/web/usage"
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
	// RunCommands lists a repository's run commands for its settings page;
	// nil leaves the page's Run commands section out.
	RunCommands ports.RunCommandLister
	// Documents reads a task's documents and the project's library; nil hides both.
	Documents ports.DocumentReader
	// History reads repositories' commit graphs for their history pages.
	History ports.RepositoryHistory
	// Artifacts lists the design assets the task pages and the project's
	// design library seed; nil seeds none.
	Artifacts ports.ArtifactReader
	// Resources and Comments read what a person added to a task, for the
	// task page's first paint (the Web UI spec renders them); nil seeds none.
	Resources ports.TaskResourceReader
	Comments  ports.TaskCommentReader
	// ProjectSummaries counts every project for the projects list; nil
	// leaves /projects unserved and the top bar offers New project instead.
	ProjectSummaries ports.ProjectSummaries
	// PullRequests offers a session's branch as a pull request on its history
	// page and badges the task page's rows; nil leaves both out.
	PullRequests ports.PullRequests
	// Code reads code for the code pages, the session's Code tab and the
	// history pages' diffs; nil leaves all three out.
	Code ports.CodeBrowser
	// Workspaces names a session's base branch and lists a repository's
	// branches for the code pages' ref select; nil shortens both.
	Workspaces CodeWorkspaces
	// Notifications seeds the top bar's unread count and the inbox's latest
	// page; nil, or a failed read, renders the page without a seed.
	Notifications ports.NotificationReader
	// GitCredentials seeds the Git credentials page, the repositories'
	// credential and the pull-request panel; nil leaves them out.
	GitCredentials ports.GitCredentials
	// TokenSaverInstalls lists the token savers for their page; nil leaves
	// the page and its top bar link out.
	TokenSaverInstalls ports.TokenSaverInstallReader
	// Usage reads token usage for a project's Usage page and the task
	// page's totals; nil leaves the page, its rail link and the totals out.
	Usage ports.UsageDashboard
	// Now defaults to time.Now.
	Now func() time.Time
}

// CodeWorkspaces is what the code pages read of the workspaces:
// *workspaces.Service satisfies it.
type CodeWorkspaces interface {
	Get(id string) *domain.Workspace
	ListBranches(repositoryID string) ([]domain.GitBranch, error)
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
		Agents: deps.Agents, Repositories: deps.Repositories, Docs: deps.Documents, Artifacts: deps.Artifacts, PullRequests: deps.PullRequests,
		Resources: deps.Resources, Comments: deps.Comments, Code: deps.Code, Usage: deps.Usage,
		Layout: s.layout, Render: render, Now: deps.Now,
	}
	if deps.Workspaces != nil {
		taskPages.Workspaces = deps.Workspaces
	}
	projectPages := projects.Handler{
		Projects: deps.Projects, Repositories: deps.Repositories, EnvFiles: deps.EnvFiles, RunCommands: deps.RunCommands, Summaries: deps.ProjectSummaries, Credentials: deps.GitCredentials,
		Layout: s.layout, Render: render, Now: deps.Now,
	}
	historyPages := history.Handler{
		Projects: deps.Projects, Repositories: deps.Repositories, History: deps.History, Sessions: deps.Sessions, Tasks: deps.Tasks,
		PullRequests: deps.PullRequests, Code: deps.Code, Layout: s.layout, Render: render, Now: deps.Now,
	}
	codePages := code.Handler{
		Projects: deps.Projects, Repositories: deps.Repositories, Code: deps.Code, History: deps.History,
		Layout: s.layout, Render: render,
	}
	if deps.Workspaces != nil {
		codePages.Branches = deps.Workspaces
	}
	toProject := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/projects/"+url.PathEscape(r.PathValue("project")), http.StatusFound)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", assets.Handler())
	mux.Handle("GET /{$}", s.page(homePage.Index))
	mux.HandleFunc("GET /switch-project", switchProject)
	if deps.Notifications != nil {
		inbox := notifications.Handler{Notifications: deps.Notifications, Projects: deps.Projects, Layout: s.layout, Render: render, Now: deps.Now}
		mux.Handle("GET "+notifications.Href, s.page(inbox.Page))
	}
	if deps.ProjectSummaries != nil {
		mux.Handle("GET /projects", s.page(projectPages.List))
	}
	if deps.GitCredentials != nil {
		creds := credentials.Handler{Credentials: deps.GitCredentials, Layout: s.layout, Render: render, Now: deps.Now}
		mux.Handle("GET "+credentials.Href, s.page(creds.Page))
	}
	if deps.TokenSaverInstalls != nil {
		savers := tokensavers.Handler{Savers: deps.TokenSaverInstalls, Layout: s.layout, Render: render}
		mux.Handle("GET "+tokensavers.Href, s.page(savers.Page))
	}
	mux.Handle("GET /projects/new", s.page(projectPages.New))
	mux.Handle("GET /projects/{project}", s.page(taskPages.Project))
	mux.Handle("GET /projects/{project}/settings", s.page(projectPages.Settings))
	mux.Handle("GET /projects/{project}/repositories", s.page(projectPages.RepositoryList))
	mux.Handle("GET /projects/{project}/repositories/{repository}/env", s.page(projectPages.EnvFileList))
	mux.Handle("GET /projects/{project}/repositories/{repository}/history", s.page(historyPages.Page))
	mux.Handle("GET /projects/{project}/repositories/{repository}/history/{commit}", s.page(historyPages.Page))
	if deps.Code != nil {
		mux.Handle("GET /projects/{project}/code", s.page(codePages.Index))
		mux.Handle("GET /projects/{project}/repositories/{repository}/code", s.page(codePages.Root))
		mux.Handle("GET /projects/{project}/repositories/{repository}/code/files", s.page(codePages.Files))
		mux.Handle("GET /projects/{project}/repositories/{repository}/code/files/{path...}", s.page(codePages.Files))
		mux.Handle("GET /projects/{project}/repositories/{repository}/code/map", s.page(codePages.Map))
		session := "GET /projects/{project}/tasks/{task}/sessions/{session}/code"
		mux.Handle(session, s.page(taskPages.SessionCode("")))
		mux.Handle(session+"/changes", s.page(taskPages.SessionCode(code.ViewChanges)))
		mux.Handle(session+"/files", s.page(taskPages.SessionCode(code.ViewFiles)))
		mux.Handle(session+"/files/{path...}", s.page(taskPages.SessionCode(code.ViewFiles)))
		mux.Handle(session+"/map", s.page(taskPages.SessionCode(code.ViewMap)))
	}
	mux.Handle("GET /projects/{project}/design", s.page(taskPages.ProjectDesign))
	mux.Handle("GET /projects/{project}/reviews", s.page(taskPages.Reviews))
	if deps.Usage != nil {
		usagePage := usage.Handler{Projects: deps.Projects, Usage: deps.Usage, Layout: s.layout, Render: render}
		mux.Handle("GET /projects/{project}/usage", s.page(usagePage.Page))
	}
	mux.Handle("GET /projects/{project}/documents", s.page(taskPages.ProjectDocuments))
	mux.Handle("GET /projects/{project}/documents/{document}", s.page(taskPages.ProjectDocuments))
	mux.Handle("GET /projects/{project}/documents/{document}/view", s.page(taskPages.ProjectDocumentView))
	mux.HandleFunc("GET /projects/{project}/{$}", toProject)
	mux.HandleFunc("GET /projects/{project}/sessions", toProject) // the old page; sessions now live under their task
	mux.Handle("GET /projects/{project}/tasks/new", s.page(taskPages.New))
	mux.Handle("GET /projects/{project}/tasks/{task}", s.page(taskPages.Task))
	mux.Handle("GET /projects/{project}/tasks/{task}/sessions/{session}/history", s.page(historyPages.Session))
	mux.Handle("GET /projects/{project}/tasks/{task}/sessions/{session}/history/{commit}", s.page(historyPages.Session))
	mux.Handle("GET /projects/{project}/tasks/{task}/design", s.page(taskPages.TaskDesign))
	mux.Handle("GET /projects/{project}/tasks/{task}/resources", s.page(taskPages.TaskResources))
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
		r = r.WithContext(shell.WithCollapsed(r.Context(), r)) // the rail's folded groups, for the layout
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
	frame.Notifications = s.bell(ctx)
	all, err := s.deps.Projects.ListProjects(ctx)
	if err != nil {
		return shell.Frame{}, err
	}
	slices.SortFunc(all, func(a, b *domain.Project) int {
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	frame.Top.Current = projectID
	if s.deps.ProjectSummaries != nil {
		frame.Top.ProjectsHref = projects.ListHref
	}
	for _, p := range all {
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
	frame.Rail = tasks.BuildRail(projectID, list, live, taskID, shell.CollapsedFrom(ctx))
	frame.Rail.BoardHref = tasks.BoardHref(projectID)
	if s.deps.Code != nil {
		frame.Rail.CodeHref = code.IndexHref(projectID)
	}
	frame.Rail.NewTaskHref = tasks.NewTaskHref(projectID)
	frame.Rail.ReviewsHref = tasks.ReviewsHref(projectID)
	if s.deps.Documents != nil {
		frame.Rail.DocumentsHref = tasks.ProjectDocumentsHref(projectID, "")
	}
	frame.Rail.DesignHref = tasks.ProjectDesignHref(projectID)
	if s.deps.Usage != nil {
		frame.Rail.UsageHref = usage.Href(projectID)
	}
	return frame, nil
}

// bell is the top bar's way to the inbox, with the unread count and the
// panel's latest notifications; nil when the inbox is not served. A failed
// read still shows the bell: the browser reads the count itself.
func (s *server) bell(ctx context.Context) *shell.Bell {
	if s.deps.Notifications == nil {
		return nil
	}
	b := &shell.Bell{Href: notifications.Href}
	page, err := s.deps.Notifications.ListNotifications(ctx, ports.NotificationQuery{Limit: notifications.PanelLimit})
	if err != nil {
		slog.Warn("web: notifications seed not read; the browser reads it", "err", err)
		return b
	}
	b.Seed = page
	return b
}

// bareFrame is a frame without the rail, for when the rail cannot be read.
func (s *server) bareFrame(title string) shell.Frame {
	f := shell.Frame{
		Title: title,
		CSS:   s.assets.URL("app.css"),
		JS:    s.assets.URL("app.js"),
		Built: s.assets.Built(),
	}
	if s.deps.GitCredentials != nil {
		f.Top.CredentialsHref = credentials.Href
	}
	if s.deps.TokenSaverInstalls != nil {
		f.Top.TokenSaversHref = tokensavers.Href
	}
	return f
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
