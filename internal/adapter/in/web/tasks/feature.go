package tasks

import (
	"slices"
	"strings"
	"time"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/domain"
)

// prdPrefix is the title convention a product-mode session follows for the
// feature's PRD (ADR 0016: a convention, not a document type).
const prdPrefix = "prd:"

// isPRD: the document's title starts with "PRD:" (any case).
func isPRD(d *domain.Document) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(d.Title)), prdPrefix)
}

// childOrder is the order the strip's Feature panel lists children in: what
// waits on the person first. The browser's toChildRows says the same.
var childOrder = []string{"review", "in_progress", "todo", "backlog", "done"}

func childRank(status string) int {
	if i := slices.Index(childOrder, status); i >= 0 {
		return i
	}
	return len(childOrder)
}

// BuildFeature is the task's feature section: its children (the project's
// tasks whose parent it is, in review, in progress, todo, backlog, done
// order; ties in list order) with their documents, newest
// first, and its PRD (the newest of its own documents titled "PRD: …"). nil
// when the task has no children. docsOf reads a child's documents; nil reads
// none.
func BuildFeature(projectID string, task *domain.Ticket, tickets []*domain.Ticket, docs []*domain.Document, docsOf func(string) []*domain.Document, now time.Time) *sessions.FeatureView {
	var children []sessions.ChildView
	for _, tk := range tickets {
		if tk.ParentID != task.ID || tk.ID == task.ID {
			continue
		}
		child := sessions.ChildView{ID: tk.ID, Title: tk.Title, Href: Href(projectID, tk.ID), Status: string(tk.Status), StatusLabel: sessions.StatusLabel(tk.Status)}
		if docsOf != nil {
			for _, d := range newestFirst(docsOf(tk.ID)) {
				child.Documents = append(child.Documents, docLink(projectID, tk.ID, d, now))
			}
		}
		children = append(children, child)
	}
	if len(children) == 0 {
		return nil
	}
	slices.SortStableFunc(children, func(a, b sessions.ChildView) int { return childRank(a.Status) - childRank(b.Status) })
	view := &sessions.FeatureView{Children: children}
	for _, d := range newestFirst(docs) {
		if isPRD(d) {
			link := docLink(projectID, task.ID, d, now)
			view.PRD = &link
			break
		}
	}
	return view
}

// ParentOf links a child task's feature task; nil when the task is
// top-level, or its parent is not among the project's tasks.
func ParentOf(projectID string, task *domain.Ticket, tickets []*domain.Ticket) *sessions.ParentLink {
	if task.ParentID == "" {
		return nil
	}
	for _, tk := range tickets {
		if tk.ID == task.ParentID {
			return &sessions.ParentLink{ID: tk.ID, Title: tk.Title, Href: Href(projectID, tk.ID)}
		}
	}
	return nil
}

func docLink(projectID, taskID string, d *domain.Document, now time.Time) sessions.DocLink {
	return sessions.DocLink{Title: titleOf(d), Href: DocumentsHref(projectID, taskID, d.ID), Updated: sessions.RelativeTime(d.UpdatedAt, now)}
}
