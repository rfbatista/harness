// Package notifications is the inbox page, /notifications: every
// notification across projects, newest first, filtered by project and
// unread, read through ports.NotificationReader (the same page the API's GET
// answers). The browser's notificationsPage takes it over live.
package notifications

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is the inbox page.
const Href = "/notifications"

// PanelLimit is how many notifications the top bar's panel shows; the shell
// seeds that many (the browser's PANEL_LIMIT).
const PanelLimit = 20

// pageLimit is one page of the inbox (the API's default).
const pageLimit = 50

// Handler serves GET /notifications.
type Handler struct {
	Notifications ports.NotificationReader
	Projects      ports.ProjectReader
	Layout        shell.Layout
	Render        shell.Renderer
	Now           func() time.Time
}

// Filter is what the page shows: one project or all, every notification or
// the unread ones. It is the page's GET form.
type Filter struct {
	ProjectID string
	Unread    bool
	Before    string
}

// FilterFrom reads ?project=&unread=1&before= from the query.
func FilterFrom(r *http.Request) Filter {
	q := r.URL.Query()
	unread := q.Get("unread")
	return Filter{ProjectID: q.Get("project"), Unread: unread == "1" || unread == "true", Before: q.Get("before")}
}

// Page renders the inbox. A failed read still renders the page: the browser
// reads it again (there is no seed to take over).
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	f := FilterFrom(r)
	all, err := h.Projects.ListProjects(ctx)
	if err != nil {
		return err
	}
	v := View{Filter: f, Projects: projectOptions(all), Now: h.Now()}
	for _, p := range all {
		if p.ID == f.ProjectID {
			v.ProjectName = p.Name
		}
	}
	if f.ProjectID != "" && v.ProjectName == "" {
		return errs.Newf("PROJECT_NOT_FOUND", "project %s not found", f.ProjectID)
	}
	page, err := h.Notifications.ListNotifications(ctx, ports.NotificationQuery{ProjectID: f.ProjectID, UnreadOnly: f.Unread, Before: f.Before, Limit: pageLimit})
	if err != nil {
		slog.Warn("notifications page: read failed; the browser reads it again", "err", err)
	} else {
		v.Seed = page
	}
	frame, err := h.Layout(ctx, "Notifications", f.ProjectID, "")
	if err != nil {
		return err
	}
	return h.Render(w, r, http.StatusOK, InboxPage(frame, v))
}

// View is what the inbox page renders.
type View struct {
	Filter      Filter
	ProjectName string // the filter's project; "" for all
	Projects    []shell.Option
	// Seed is the first page; nil when it could not be read.
	Seed *ports.NotificationPage
	Now  time.Time
}

func projectOptions(all []*domain.Project) []shell.Option {
	opts := make([]shell.Option, 0, len(all))
	for _, p := range all {
		opts = append(opts, shell.Option{ID: p.ID, Name: p.Name})
	}
	slices.SortFunc(opts, func(a, b shell.Option) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return opts
}

// Row is one notification as the page paints it before the browser takes
// over; the browser's view.js toRow has the same rules.
type Row struct {
	ID, Href, Title, Body, Project, Task string
	Level, LevelLabel, State, Age, At    string
	Unread                               bool
}

// ToRow paints n as of now.
func ToRow(n *domain.Notification, now time.Time) Row {
	unread := n.Unread()
	state, label := "read", ""
	switch {
	case n.ResolvedAt != nil:
		state, label = "settled", "Settled"
	case unread:
		state = "unread"
	}
	if label == "" && n.Level == domain.NotificationAction {
		label = "Needs you"
	}
	return Row{
		ID: n.ID, Href: n.Link, Title: n.Title, Body: n.Body, Project: n.ProjectName, Task: n.TaskTitle,
		Level: string(n.Level), LevelLabel: label, State: state, Unread: unread,
		Age: sessions.RelativeTime(n.CreatedAt, now), At: n.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// Rows paints the seed's notifications; none without a seed.
func (v View) Rows() []Row {
	if v.Seed == nil {
		return nil
	}
	rows := make([]Row, 0, len(v.Seed.Notifications))
	for _, n := range v.Seed.Notifications {
		rows = append(rows, ToRow(n, v.Now))
	}
	return rows
}

// MarkAllLabel names Mark all read for the filter's scope.
func (v View) MarkAllLabel() string {
	if v.ProjectName != "" {
		return "Mark all read in " + v.ProjectName
	}
	return "Mark all read"
}

// EmptyText says why the list is empty (the browser's emptyText).
func (v View) EmptyText() string {
	switch {
	case v.Filter.Unread && v.ProjectName != "":
		return "Nothing unread in " + v.ProjectName + "."
	case v.Filter.Unread:
		return "Nothing unread."
	case v.ProjectName != "":
		return "No notifications in " + v.ProjectName + " yet."
	}
	return "No notifications yet."
}
