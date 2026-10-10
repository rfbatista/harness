package code

import (
	"context"
	"net/http"
	"net/url"
	"slices"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Branches lists a repository's branches for the ref select: locals first,
// then remote-tracking ones (*workspaces.Service satisfies it).
type Branches interface {
	ListBranches(repositoryID string) ([]domain.GitBranch, error)
}

// Handler serves the project's Code page and each repository's code page.
type Handler struct {
	Projects     ports.ProjectReader
	Repositories ports.RepositoryLister
	Code         ports.CodeBrowser
	// Branches and History fill the ref select (branches, then the tags on
	// the commits read); nil leaves the select with the current ref only.
	Branches Branches
	History  ports.RepositoryHistory
	Layout   shell.Layout
	Render   shell.Renderer
}

// refLogLimit is how many commits the ref select reads tags from.
const refLogLimit = 500

// Index serves GET /projects/{project}/code: one repository opens its code
// page; several are listed.
func (h Handler) Index(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	repos, err := h.Repositories.ListRepositories(ctx, project.ID)
	if err != nil {
		return err
	}
	if len(repos) == 1 {
		http.Redirect(w, r, RepositoryHref(project.ID, repos[0].ID), http.StatusFound)
		return nil
	}
	frame, err := h.Layout(ctx, "Code", project.ID, "")
	if err != nil {
		return err
	}
	frame.Rail.Page = shell.PageCode
	view := IndexView{Frame: frame, ProjectID: project.ID, ProjectName: project.Name}
	for _, repo := range repos {
		view.Rows = append(view.Rows, IndexRow{Name: repo.Name, Href: RepositoryHref(project.ID, repo.ID), Detail: repo.RootDir})
	}
	return h.Render(w, r, http.StatusOK, IndexPage(view))
}

// Root serves …/repositories/{repository}/code: its Files.
func (h Handler) Root(w http.ResponseWriter, r *http.Request) error {
	return h.page(w, r, RepositoryRoute(r, ""))
}

// Files serves …/repositories/{repository}/code/files[/{path…}].
func (h Handler) Files(w http.ResponseWriter, r *http.Request) error {
	return h.page(w, r, RepositoryRoute(r, ViewFiles))
}

// Map serves …/repositories/{repository}/code/map.
func (h Handler) Map(w http.ResponseWriter, r *http.Request) error {
	return h.page(w, r, RepositoryRoute(r, ViewMap))
}

func (h Handler) page(w http.ResponseWriter, r *http.Request, rt Route) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, rt.ProjectID)
	if err != nil {
		return err
	}
	repos, err := h.Repositories.ListRepositories(ctx, project.ID)
	if err != nil {
		return err
	}
	var repo *domain.Repository
	for _, x := range repos {
		if x.ID == rt.RepositoryID {
			repo = x
		}
	}
	if repo == nil {
		return errs.Newf("REPOSITORY_NOT_FOUND", "repository %s is not in project %s", rt.RepositoryID, project.Name)
	}

	var seed Seed
	if h.Code == nil {
		seed = Seed{ProjectID: project.ID, Route: rt, RepositoryName: repo.Name, Error: &SeedError{Code: "UNAVAILABLE", Message: "code browsing is not available"}}
	} else {
		seed = Load(ctx, h.Code, rt, Source{Query: ports.CodeSourceQuery{RepositoryID: repo.ID, Ref: rt.Ref}, RepositoryName: repo.Name})
		if seed.Error != nil && seed.Error.Code == "PATH_NOT_FOUND" && rt.Path != "" {
			// The path is not at this ref: say so over the tree root (UX §2.2).
			root := Load(ctx, h.Code, rt.With(ViewFiles, ""), Source{Query: ports.CodeSourceQuery{RepositoryID: repo.ID, Ref: rt.Ref}, RepositoryName: repo.Name})
			root.Error = seed.Error
			root.Route.Path = rt.Path
			seed = root
		}
		seed.Refs = h.refs(ctx, repo.ID)
	}
	seed.HistoryHref = "/projects/" + url.PathEscape(project.ID) + "/repositories/" + url.PathEscape(repo.ID) + "/history"

	frame, err := h.Layout(ctx, "Code · "+repo.Name, project.ID, "")
	if err != nil {
		return err
	}
	frame.Rail.Page = shell.PageCode
	frame.Hints = Hints
	return h.Render(w, r, http.StatusOK, Page(PageView{
		Frame: frame, ProjectID: project.ID, ProjectName: project.Name, RepositoryName: repo.Name,
		HistoryHref: seed.HistoryHref, View: Build(seed),
	}))
}

// Hints are the code views' keys in the stream bar.
var Hints = []shell.Hint{
	{Keys: []string{"T"}, Label: "find file"},
	{Keys: []string{"L"}, Label: "line"},
	{Keys: []string{"G", "F/M"}, Label: "views"},
	{Keys: []string{"Y"}, Label: "permalink"},
}

// refs is the ref select's options: the checked-out branch, the other local
// branches, remote branches, then tags. A failed read only shortens it.
func (h Handler) refs(ctx context.Context, repositoryID string) []string {
	var out []string
	add := func(name string) {
		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	if h.Branches != nil {
		if branches, err := h.Branches.ListBranches(repositoryID); err == nil {
			for _, b := range branches {
				if b.IsHead {
					add(b.Name)
				}
			}
			for _, b := range branches {
				add(b.Name)
			}
		}
	}
	if h.History != nil {
		if commits, err := h.History.RepositoryLog(ctx, repositoryID, ports.HistoryQuery{Limit: refLogLimit}); err == nil {
			for _, c := range commits {
				for _, ref := range c.Refs {
					if ref.Kind == domain.RefTag {
						add(ref.Name)
					}
				}
			}
		}
	}
	return out
}

// IndexView is the project's Code page with several repositories.
type IndexView struct {
	Frame                  shell.Frame
	ProjectID, ProjectName string
	Rows                   []IndexRow
}

// IndexRow is one repository on it.
type IndexRow struct {
	Name, Href, Detail string
}

// PageView is a repository's code page.
type PageView struct {
	Frame                                  shell.Frame
	ProjectID, ProjectName, RepositoryName string
	HistoryHref                            string
	View                                   View
}
