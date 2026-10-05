package history

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/adapter/in/web/tasks"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// pageSize is how many commits a page shows at first; More adds as many.
const pageSize = 200

// Handler serves a repository's history.
type Handler struct {
	Projects     ports.ProjectReader
	Repositories ports.RepositoryLister
	History      ports.RepositoryHistory
	// Tasks names a session's task on its history page.
	Tasks ports.TicketReader
	// Sessions links branches that are agent sessions' to their tasks.
	Sessions ports.SessionReader
	Layout   shell.Layout
	Render   shell.Renderer
	Now      func() time.Time
}

// Href is a repository's history page.
func Href(projectID, repositoryID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/repositories/" + url.PathEscape(repositoryID) + "/history"
}

// query is what the page shows: how many commits, and whether remote
// branches are in the graph. It travels in the URL, so links keep it.
type query struct {
	limit   int
	remotes bool
}

func readQuery(r *http.Request) query {
	q := query{limit: pageSize}
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		q.limit = min(n, domain.MaxHistory)
	}
	q.remotes = r.URL.Query().Get("remotes") == "1"
	return q
}

func (q query) encode() string {
	v := url.Values{}
	if q.limit != pageSize {
		v.Set("limit", strconv.Itoa(q.limit))
	}
	if q.remotes {
		v.Set("remotes", "1")
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// Page serves GET …/repositories/{repository}/history and
// …/history/{commit}: the graph, and one commit open beside it (the first
// when none is named).
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	if h.History == nil {
		return errs.Newf("UNAVAILABLE", "repository history is not available on this server")
	}
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	repos, err := h.Repositories.ListRepositories(ctx, project.ID)
	if err != nil {
		return err
	}
	var repo *domain.Repository
	for _, x := range repos {
		if x.ID == r.PathValue("repository") {
			repo = x
		}
	}
	if repo == nil {
		return errs.Newf("REPOSITORY_NOT_FOUND", "repository %s is not in project %s", r.PathValue("repository"), project.Name)
	}

	q := readQuery(r)
	commits, err := h.History.RepositoryLog(ctx, repo.ID, ports.HistoryQuery{Limit: q.limit, Remotes: q.remotes})
	if err != nil {
		return err
	}
	open := r.PathValue("commit")
	if open == "" && len(commits) > 0 {
		open = commits[0].Hash
	}
	var detail *domain.CommitDetail
	if open != "" {
		if detail, err = h.History.RepositoryCommit(ctx, repo.ID, open); err != nil {
			return err
		}
	}

	base := Href(project.ID, repo.ID)
	view := PageView{
		Crumbs: []Crumb{
			{Label: project.Name, Href: "/projects/" + url.PathEscape(project.ID)},
			{Label: "Repositories", Href: "/projects/" + url.PathEscape(project.ID) + "/repositories"},
		},
		Heading:     repo.Name,
		Meta:        commitCount(len(commits)),
		Remotes:     q.remotes,
		RemotesHref: base + query{limit: q.limit, remotes: !q.remotes}.encode(),
	}
	if len(commits) == q.limit && q.limit < domain.MaxHistory {
		more := query{limit: min(q.limit+pageSize, domain.MaxHistory), remotes: q.remotes}
		view.MoreHref = base + more.encode()
	}
	g := graphRows{
		branchTasks: h.sessionBranches(r, project.ID, repo.ID),
		now:         h.Now(),
		href:        func(hash string) string { return base + "/" + hash + q.encode() + "#c-" + short(hash) },
	}
	view.Rows, _ = g.rows(commits, nil, detail)
	if detail != nil {
		view.Open = openView(detail, g)
	}

	title := "History · " + repo.Name
	frame, err := h.Layout(ctx, title, project.ID, "")
	if err != nil {
		return err
	}
	view.Frame = frame
	return h.Render(w, r, http.StatusOK, Page(view))
}

// sessionBranches maps the branches agent sessions work on to their tasks'
// pages. A failure to read sessions only loses the links.
func (h Handler) sessionBranches(r *http.Request, projectID, repositoryID string) map[string]string {
	out := map[string]string{}
	if h.Sessions == nil {
		return out
	}
	list, err := h.Sessions.List(r.Context(), ports.SessionFilter{ProjectID: projectID})
	if err != nil {
		return out
	}
	for _, s := range list {
		if s.RepositoryID == repositoryID && s.Branch != "" && s.TicketID != "" {
			out[s.Branch] = tasks.Href(projectID, s.TicketID)
		}
	}
	return out
}

func short(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}

func refViews(refs []domain.GitRef, branchTasks map[string]string) []RefView {
	out := make([]RefView, 0, len(refs))
	for _, ref := range refs {
		v := RefView{Name: ref.Name, Kind: string(ref.Kind), Head: ref.Head}
		if ref.Kind == domain.RefBranch {
			v.TaskHref = branchTasks[ref.Name]
		}
		out = append(out, v)
	}
	return out
}

// graphRows turns commits into the graph's rows: how a page links a commit,
// which branches are agent sessions' (and which one is this page's own).
type graphRows struct {
	branchTasks map[string]string
	mine        string
	now         time.Time
	href        func(hash string) string
}

// rows lays commits out; with working (the uncommitted changes, a
// pseudo-commit on HEAD) it is the first row's graph, returned apart.
func (g graphRows) rows(commits []domain.Commit, working *domain.Commit, open *domain.CommitDetail) ([]Row, *Graph) {
	all := commits
	if working != nil {
		all = append([]domain.Commit{*working}, commits...)
	}
	graphs := Layout(all)
	var first *Graph
	if working != nil {
		first, graphs = &graphs[0], graphs[1:]
	}
	rows := make([]Row, 0, len(commits))
	for i, c := range commits {
		rows = append(rows, Row{
			Hash: c.Hash, Short: short(c.Hash), Subject: c.Subject, Author: c.AuthorName,
			When: sessions.RelativeTime(c.AuthoredAt, g.now), Href: g.href(c.Hash),
			Current: open != nil && c.Hash == open.Hash, Refs: g.refs(c.Refs), Graph: graphs[i],
		})
	}
	return rows, first
}

func (g graphRows) refs(refs []domain.GitRef) []RefView {
	out := refViews(refs, g.branchTasks)
	for i := range out {
		out[i].Mine = out[i].Kind == string(domain.RefBranch) && out[i].Name == g.mine
	}
	return out
}

func openView(d *domain.CommitDetail, g graphRows) *CommitView {
	v := &CommitView{
		Hash: d.Hash, Subject: d.Subject, Body: d.Body,
		Author: d.AuthorName, Email: d.AuthorEmail, Date: d.AuthoredAt.Format("Mon 2 Jan 2006 15:04 -0700"),
		Refs: g.refs(d.Refs),
	}
	for _, p := range d.Parents {
		v.Parents = append(v.Parents, ParentLink{Short: short(p), Href: g.href(p)})
	}
	for _, f := range d.Files {
		fv := FileView{Path: f.Path, Binary: f.Added < 0}
		if !fv.Binary {
			fv.Added, fv.Deleted = "+"+strconv.Itoa(f.Added), "−"+strconv.Itoa(f.Deleted)
			v.Added += f.Added
			v.Deleted += f.Deleted
		}
		v.Files = append(v.Files, fv)
	}
	v.Summary = filesSummary(len(d.Files), v.Added, v.Deleted)
	return v
}

func filesSummary(files, added, deleted int) string {
	noun := "files"
	if files == 1 {
		noun = "file"
	}
	if files == 0 {
		return "No file changes."
	}
	return strings.Join([]string{strconv.Itoa(files), noun, "changed ·", "+" + strconv.Itoa(added), "−" + strconv.Itoa(deleted)}, " ")
}

func commitCount(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return strconv.Itoa(n) + " commits"
}
