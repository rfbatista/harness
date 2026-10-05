package history

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/tasks"
	"operators-mcp/internal/domain"
)

// workingID names the uncommitted changes where a commit hash would go.
// It is not hex, so it can never be taken for a commit.
const workingID = "working"

// SessionHref is a session's history page.
func SessionHref(projectID, taskID, sessionID string) string {
	return tasks.Href(projectID, taskID) + "/sessions/" + url.PathEscape(sessionID) + "/history"
}

// Session serves GET /projects/{project}/tasks/{task}/sessions/{session}/history
// and …/history/{commit}: the session's branch beside the one it was cut
// from, how far it is ahead, and — as the graph's first row — what it has
// not committed yet, open by default when there is any.
func (h Handler) Session(w http.ResponseWriter, r *http.Request) error {
	if h.History == nil || h.Sessions == nil || h.Tasks == nil {
		return errs.Newf("UNAVAILABLE", "session history is not available on this server")
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
	sess, err := h.Sessions.Get(ctx, r.PathValue("session"))
	if err != nil {
		return err
	}
	if task.ProjectID != project.ID || sess.ProjectID != project.ID || sess.TicketID != task.ID {
		return errs.Newf("SESSION_NOT_FOUND", "session %s is not on this task", sess.ID)
	}
	if sess.WorkspaceID == "" {
		return errs.Newf("WORKSPACE_NOT_FOUND", "this session has no worktree: it was not started on a repository")
	}

	q := readQuery(r)
	q.remotes = false
	hist, err := h.History.WorkspaceHistory(ctx, sess.WorkspaceID, q.limit)
	if err != nil {
		return err
	}
	hasWorking := !hist.Gone && hist.Head != "" && len(hist.Changes) > 0

	open := r.PathValue("commit")
	if open == "" || (open == workingID && !hasWorking) {
		open = ""
		if hasWorking {
			open = workingID
		} else if len(hist.Commits) > 0 {
			open = hist.Commits[0].Hash
		}
	}
	var detail *domain.CommitDetail
	if open != "" && open != workingID {
		if detail, err = h.History.RepositoryCommit(ctx, hist.RepositoryID, open); err != nil {
			return err
		}
	}

	base := SessionHref(project.ID, task.ID, sess.ID)
	view := PageView{
		Crumbs: []Crumb{
			{Label: project.Name, Href: "/projects/" + url.PathEscape(project.ID)},
			{Label: task.Title, Href: tasks.Href(project.ID, task.ID)},
		},
		Heading: "Git history",
		Meta:    branchMeta(hist),
	}
	if hist.Gone {
		view.Notice = "This session's worktree was removed, so there is nothing uncommitted to show; its branch and commits are kept."
	}
	if len(hist.Commits) == q.limit && q.limit < domain.MaxHistory {
		view.MoreHref = base + query{limit: min(q.limit+pageSize, domain.MaxHistory)}.encode()
	}
	g := graphRows{
		branchTasks: h.sessionBranches(r, project.ID, hist.RepositoryID),
		mine:        hist.Branch,
		now:         h.Now(),
		href:        func(hash string) string { return base + "/" + hash + q.encode() + "#c-" + short(hash) },
	}
	var working *domain.Commit
	if hasWorking {
		working = &domain.Commit{Hash: workingID, Parents: []string{hist.Head}}
	}
	rows, wg := g.rows(hist.Commits, working, detail)
	view.Rows = rows
	if hasWorking {
		view.Working = &WorkingRow{Href: base + "/" + workingID + q.encode(), Summary: changesSummary(hist.Changes), Current: open == workingID, Graph: *wg}
		if open == workingID {
			view.OpenWorking = workingView(hist.Changes)
		}
	}
	if detail != nil {
		view.Open = openView(detail, g)
	}

	frame, err := h.Layout(ctx, "History · "+task.Title, project.ID, task.ID)
	if err != nil {
		return err
	}
	view.Frame = frame
	return h.Render(w, r, http.StatusOK, Page(view))
}

// branchMeta says where the session's branch stands: "agent/feed · 2 ahead,
// 1 behind main".
func branchMeta(h *domain.WorkspaceHistory) string {
	switch {
	case h.Base == "" || h.Base == h.Branch:
		return h.Branch
	case h.Ahead == 0 && h.Behind == 0:
		return h.Branch + " · even with " + h.Base
	case h.Behind == 0:
		return h.Branch + " · " + strconv.Itoa(h.Ahead) + " ahead of " + h.Base
	default:
		return h.Branch + " · " + strconv.Itoa(h.Ahead) + " ahead, " + strconv.Itoa(h.Behind) + " behind " + h.Base
	}
}

func changesSummary(changes []domain.WorkingChange) string {
	if len(changes) == 1 {
		return "1 uncommitted change"
	}
	return strconv.Itoa(len(changes)) + " uncommitted changes"
}

func workingView(changes []domain.WorkingChange) *WorkingView {
	v := &WorkingView{}
	added, deleted := 0, 0
	for _, c := range changes {
		f := FileView{Path: c.Path, State: changeState(c.Status), Binary: c.Added < 0}
		if !f.Binary {
			f.Added, f.Deleted = "+"+strconv.Itoa(c.Added), "−"+strconv.Itoa(c.Deleted)
			added += c.Added
			deleted += c.Deleted
		}
		v.Files = append(v.Files, f)
	}
	v.Summary = changesSummary(changes) + " · +" + strconv.Itoa(added) + " −" + strconv.Itoa(deleted) + " in tracked files"
	return v
}

// changeState is git's porcelain code in a word: the staged side when it
// changed, else the worktree side.
func changeState(code string) string {
	if code == "??" {
		return "untracked"
	}
	c := code[0]
	if c == ' ' && len(code) > 1 {
		c = code[1]
	}
	switch c {
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'U':
		return "conflict"
	default:
		return "modified"
	}
}
