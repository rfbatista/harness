// Package notifications is the inbox page, /notifications: every
// notification across projects, newest first, filtered by project and
// unread, read through ports.NotificationReader (the same page the API's GET
// answers). The browser's notificationsPage takes it over live.
package notifications

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
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

// The tabs of the page: what needs the person, what is unread, everything.
const (
	TabNeeds  = "needs"
	TabUnread = "unread"
	TabAll    = "all"
)

// Filter is what the page shows: one project or all, and a tab. It is the
// page's GET form. Tab is "" until Page resolves the default.
type Filter struct {
	ProjectID string
	Tab       string
	Before    string
}

// Unread is whether the page shows the unread ones only.
func (f Filter) Unread() bool { return f.Tab == TabUnread }

// FilterFrom reads ?project=&tab=&before= from the query; the older
// ?unread=1 still selects the Unread tab.
func FilterFrom(r *http.Request) Filter {
	q := r.URL.Query()
	tab := q.Get("tab")
	if tab != TabNeeds && tab != TabUnread && tab != TabAll {
		tab = ""
	}
	if unread := q.Get("unread"); tab == "" && (unread == "1" || unread == "true") {
		tab = TabUnread
	}
	return Filter{ProjectID: q.Get("project"), Tab: tab, Before: q.Get("before")}
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
	page, err := h.Notifications.ListNotifications(ctx, ports.NotificationQuery{ProjectID: f.ProjectID, UnreadOnly: f.Unread(), Before: f.Before, Limit: pageLimit})
	if err != nil {
		slog.Warn("notifications page: read failed; the browser reads it again", "err", err)
	} else {
		v.Seed = page
	}
	// Needs you is the default while anything is open, and All otherwise.
	if f.Tab == "" {
		f.Tab = TabAll
		if v.OpenCount() > 0 {
			f.Tab = TabNeeds
		}
		v.Filter = f
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
	// Sessions is a sessions_interrupted item's list; the browser adds its
	// Resume all interrupted button.
	Sessions []SessionLine
}

// SessionLine is one session a sessions_interrupted item lists.
type SessionLine struct{ Label, Href, State, Reason string }

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
	var lines []SessionLine
	for _, s := range n.InterruptedSessions {
		task := s.TaskTitle
		if task == "" {
			task = "No task"
		}
		label := task
		if s.AgentName != "" {
			label += " · " + s.AgentName
		}
		lines = append(lines, SessionLine{Label: label, Href: s.Link, State: string(s.State), Reason: s.Reason})
	}
	return Row{
		Sessions: lines,
		ID:       n.ID, Href: n.Link, Title: n.Title, Body: n.Body, Project: n.ProjectName, Task: n.TaskTitle,
		Level: string(n.Level), LevelLabel: label, State: state, Unread: unread,
		Age: sessions.RelativeTime(n.CreatedAt, now), At: n.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// isOpen is an open cause: level action and not settled.
func isOpen(n *domain.Notification) bool {
	return n.Level == domain.NotificationAction && n.ResolvedAt == nil
}

// OpenCount is how many open causes the seed holds.
func (v View) OpenCount() int {
	if v.Seed == nil {
		return 0
	}
	n := 0
	for _, x := range v.Seed.Notifications {
		if isOpen(x) {
			n++
		}
	}
	return n
}

// UnreadCount is the server's global unread count.
func (v View) UnreadCount() int {
	if v.Seed == nil {
		return 0
	}
	return v.Seed.UnreadCount
}

// Summary is "9 need you · 2 unread", beside the title.
func (v View) Summary() string {
	return strconv.Itoa(v.OpenCount()) + " need you · " + strconv.Itoa(v.UnreadCount()) + " unread"
}

// Group is a heading and its rows: Needs you, or a day.
type Group struct {
	Key, Title, Tone string
	Rows             []Row
}

// Count is how many rows the group holds.
func (g Group) Count() int { return len(g.Rows) }

// Groups paints the seed's notifications as the browser's groupsFor does:
// the open causes pinned as Needs you, then the rest by day (the server's
// day), newest first. The Needs you tab holds only the open causes.
func (v View) Groups() []Group {
	if v.Seed == nil {
		return nil
	}
	var groups []Group
	var needs Group
	for _, n := range v.Seed.Notifications {
		if isOpen(n) {
			needs.Rows = append(needs.Rows, ToRow(n, v.Now))
		}
	}
	if len(needs.Rows) > 0 {
		needs.Key, needs.Title, needs.Tone = "needs", "Needs you", "attention"
		groups = append(groups, needs)
	}
	if v.Filter.Tab == TabNeeds {
		return groups
	}
	for _, n := range v.Seed.Notifications {
		if isOpen(n) {
			continue
		}
		at := n.CreatedAt.In(v.Now.Location())
		key := "day:" + at.Format("2006-01-02")
		if last := len(groups) - 1; last >= 0 && groups[last].Key == key {
			groups[last].Rows = append(groups[last].Rows, ToRow(n, v.Now))
			continue
		}
		groups = append(groups, Group{Key: key, Title: dayLabel(at, v.Now), Rows: []Row{ToRow(n, v.Now)}})
	}
	return groups
}

// dayLabel is "Today", "Yesterday", then "Tue 10 Nov" (with the year when it is not this one).
func dayLabel(d, now time.Time) string {
	y, m, day := d.Date()
	ny, nm, nd := now.Date()
	if y == ny && m == nm && day == nd {
		return "Today"
	}
	ty, tm, td := now.AddDate(0, 0, -1).Date()
	if y == ty && m == tm && day == td {
		return "Yesterday"
	}
	if y != ny {
		return d.Format("Mon 2 Jan 2006")
	}
	return d.Format("Mon 2 Jan")
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
	case v.Filter.Tab == TabNeeds && v.ProjectName != "":
		return "Nothing needs you in " + v.ProjectName + "."
	case v.Filter.Tab == TabNeeds:
		return "Nothing needs you."
	case v.Filter.Unread() && v.ProjectName != "":
		return "Nothing unread in " + v.ProjectName + "."
	case v.Filter.Unread():
		return "Nothing unread."
	case v.ProjectName != "":
		return "No notifications in " + v.ProjectName + " yet."
	}
	return "No notifications yet."
}

// serverTone is the tone of the standing line a server-painted row shows.
func serverTone(row Row) string {
	if row.State == "settled" {
		return "quiet"
	}
	return "attention"
}
