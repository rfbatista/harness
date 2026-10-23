// Package shell renders what every web page shares: the document head, the
// top bar with the project picker, the task rail, and the stream bar. Pages
// fill the <main> it leaves open.
package shell

import (
	"context"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Frame is what the shell needs from a page.
type Frame struct {
	Title string
	Top   TopBar
	Rail  Rail
	// Asset URLs, content-hashed (web.Assets.URL).
	CSS, JS string
	// Built is false when `make web` has not produced the bundles yet.
	Built bool
	// Live: the page follows the session feed, so the stream bar shows the
	// connection. Pages that do not follow it show none, rather than a
	// "connecting" that never ends.
	Live bool
	// Hints are the keys the stream bar names; nil names the list keys (J K move).
	Hints []Hint
	// Notifications is the top bar's bell; nil leaves it out (the inbox is
	// not served).
	Notifications *Bell
}

// Bell is the top bar's way to the inbox: the global unread count, painted
// before any script runs, and the latest notifications the panel opens on.
type Bell struct {
	// Href is the inbox page.
	Href string
	// Seed is the latest page (the API's JSON), or nil when it could not be
	// read: the browser's first read then fills in the count.
	Seed *ports.NotificationPage
}

// Unread is the count to paint; 0 without a seed.
func (b *Bell) Unread() int {
	if b == nil || b.Seed == nil {
		return 0
	}
	return b.Seed.UnreadCount
}

// badgeMax is where the badge stops counting (the browser's badgeText).
const badgeMax = 99

// BadgeText is the bell's badge: "", "3", "99+".
func (b *Bell) BadgeText() string {
	n := b.Unread()
	switch {
	case n <= 0:
		return ""
	case n > badgeMax:
		return strconv.Itoa(badgeMax) + "+"
	}
	return strconv.Itoa(n)
}

// Label is the bell's accessible name, with the exact count (bellLabel).
func (b *Bell) Label() string {
	if n := b.Unread(); n > 0 {
		return "Notifications, " + strconv.Itoa(n) + " unread"
	}
	return "Notifications"
}

// Hint is one keyboard way the stream bar names: its keys and what they do.
type Hint struct {
	Keys  []string
	Label string
}

// TopBar holds the project picker.
type TopBar struct {
	Projects []Option
	Current  string // project ID; "" when none is selected
	// ProjectsHref is the projects list; "" when it is not served.
	ProjectsHref string
	// OnProjects marks the projects list as the current page.
	OnProjects bool
	// CredentialsHref is the Git credentials page; "" when it is not served.
	CredentialsHref string
	// OnCredentials marks the Git credentials page as the current page.
	OnCredentials bool
	// TokenSaversHref is the Token savers page; "" when it is not served.
	TokenSaversHref string
	// OnTokenSavers marks the Token savers page as the current page.
	OnTokenSavers bool
	// ModelsHref is the Models page; "" when it is not served.
	ModelsHref string
	// OnModels marks the Models page as the current page.
	OnModels bool
	// AgentIsolationHref is the Agent isolation page; "" when it is not served.
	AgentIsolationHref string
	// OnAgentIsolation marks the Agent isolation page as the current page.
	OnAgentIsolation bool
	// AgentIsolationAttention puts the amber mark on its link: something
	// there blocks agents from starting.
	AgentIsolationAttention bool
	// ServerHref is the Server page; "" when it is not served.
	ServerHref string
	// OnServer marks the Server page as the current page.
	OnServer bool
}

// Option is one project in the picker.
type Option struct{ ID, Name string }

// The project pages the rail links to; Rail.Page marks the current one.
const (
	PageBoard     = "board"
	PageCode      = "code"
	PageReviews   = "reviews"
	PageDocuments = "documents"
	PageDesign    = "design"
	PageUsage     = "usage"
)

// Rail is the current project's tasks, in kanban groups, and the ways to its
// pages.
type Rail struct {
	// NewTaskHref is the new-task page of the selected project; "" with none.
	NewTaskHref string
	// BoardHref is the project's board; "" hides it.
	BoardHref string
	// Page is the project page open now (PageBoard, …), marked current on
	// the rail; "" for any other page.
	Page string
	// CodeHref is the project's code pages; "" when code browsing is not
	// served.
	CodeHref string
	// DocumentsHref is the project's documents library; "" when documents
	// are not served.
	DocumentsHref string
	// DesignHref is the project's design assets: the artifacts moved to
	// project level.
	DesignHref string
	// ReviewsHref is the project's review inbox: the requests its tasks'
	// architects raised for the person; Reviews counts the pending ones.
	ReviewsHref string
	Reviews     int
	// UsageHref is the project's token usage; "" when usage is not served.
	UsageHref string
	Groups    []RailGroup
	// Empty is shown when a project is selected but has no tasks.
	Empty string
	// Current is the open task's id, "" when none: the live rail marks it.
	Current string
	// ReportsFeed: this rail is the page's feed (the project page), so it
	// reports the connection to the stream bar. A page with its own feed
	// (the task page) reports it itself.
	ReportsFeed bool
	// Seed lets the browser keep the rail and the board live: the project's
	// tasks and sessions, followed over the project's feed. Nil leaves the
	// rail static (no project selected).
	Seed *RailSeed
}

// RailSeed is the project's tasks and sessions, as much as the rail and the
// board need of each (the API's JSON shapes). The tasks module's rail
// gateway decodes it.
type RailSeed struct {
	ProjectID string           `json:"project_id"`
	Sessions  []RailSession    `json:"sessions"`
	Tasks     []*domain.Ticket `json:"tasks"`
}

// RailSession is a session as the rail counts it.
type RailSession struct {
	ID               string `json:"id"`
	TicketID         string `json:"ticket_id,omitempty"`
	Status           string `json:"status"`
	PendingApprovals int    `json:"pending_approvals"`
}

// RailGroup is one status group: "In progress", "Todo", …
type RailGroup struct {
	Label string
	// Key is the status value ("in_progress"), or "other".
	Key string
	// Total counts the group's tasks: Links and More.
	Total int
	// Collapsed: the person folded it (the rail_collapsed cookie). Its rows
	// still show the open task and what needs the person.
	Collapsed bool
	// Links are the rows shown. Done shows its most recently updated tasks,
	// plus the open task and every task that needs the person.
	Links []Link
	// More are Done's other rows, behind "Show N more".
	More []Link
}

// IsEmpty: the group has no tasks, so its heading has nothing to fold.
func (g RailGroup) IsEmpty() bool { return g.Total == 0 }

// NeedsYou counts the tasks waiting on the person, for the phone's Tasks
// trigger.
func (r Rail) NeedsYou() int {
	n := 0
	for _, g := range r.Groups {
		for _, links := range [][]Link{g.Links, g.More} {
			for _, l := range links {
				if l.Attention {
					n++
				}
			}
		}
	}
	return n
}

// Link is one task in the rail.
type Link struct {
	// TaskID ties the link to the live activity; "" keeps it static.
	TaskID  string
	Label   string
	Href    string
	Current bool
	// Live counts the task's sessions whose process is alive.
	Live int
	// Attention: one of its sessions is waiting on the developer, or a
	// review request its architect raised is.
	Attention bool
	// Reviews counts the review requests waiting on the person.
	Reviews int
}

// DocumentTitle is the <title>: the page's own title, then the app.
func (f Frame) DocumentTitle() string {
	title := "harness"
	if f.Title != "" {
		title = f.Title + " · harness"
	}
	// The unread count leads, as the browser keeps it (store.syncTitle).
	if n := f.Notifications.Unread(); n > 0 {
		title = "(" + strconv.Itoa(n) + ") " + title
	}
	return title
}

// Layout builds a page's Frame: assets, the project picker and the task rail.
// projectID and taskID say what is selected ("" for none).
type Layout func(ctx context.Context, title, projectID, taskID string) (Frame, error)

// Handler is a page handler. Returning an error renders the error page for
// its StructuredError code (a *_NOT_FOUND is a 404).
type Handler func(w http.ResponseWriter, r *http.Request) error

// Renderer writes a page with a status code.
type Renderer func(w http.ResponseWriter, r *http.Request, status int, page templ.Component) error

// blockingAttrs marks a top bar link whose page has something that blocks
// agents from starting.
func blockingAttrs(blocking bool) templ.Attributes {
	if blocking {
		return templ.Attributes{"data-blocking": "true"}
	}
	return templ.Attributes{}
}

// currentAttrs marks the current link with aria-current="page" and adds
// nothing to the others.
func currentAttrs(current bool) templ.Attributes {
	if current {
		return templ.Attributes{"aria-current": "page"}
	}
	return templ.Attributes{}
}

// selectedAttrs marks the picker's current option.
func selectedAttrs(selected bool) templ.Attributes {
	if selected {
		return templ.Attributes{"selected": true}
	}
	return templ.Attributes{}
}

// LiveState is the task's status dot, on the rail and on the board: amber
// when a session waits on the developer, teal when one is running, none when
// the task is quiet.
func (l Link) LiveState() string {
	switch {
	case l.Attention:
		return "waiting"
	case l.Live > 0:
		return "running"
	default:
		return ""
	}
}

// LiveWord is the dot's accessible word.
func (l Link) LiveWord() string {
	switch {
	case l.Attention:
		return "waiting on you"
	case l.Live > 0:
		return "running"
	default:
		return ""
	}
}

// railAttrs makes the rail live when it has a seed.
func railAttrs(r Rail) templ.Attributes {
	if r.Seed == nil {
		return nil
	}
	attrs := templ.Attributes{
		"x-data": "tasksRail", "data-seed": "rail-seed", "data-current-task": r.Current,
		// The phone drawer: opened from the top bar's Tasks button, closed by
		// Esc, Close or the scrim; "/" anywhere focuses the filter.
		"x-bind:data-open":       "drawerOpen",
		"x-on:rail-open.window":  "openDrawer",
		"x-on:rail-close.window": "closeDrawer",
		"x-on:keydown":           "drawerKey",
		"x-on:keydown.window":    "globalKey",
	}
	if r.ReportsFeed {
		attrs["data-reports-feed"] = ""
	}
	return attrs
}

// attentionAttrs washes a row that waits on the person.
func attentionAttrs(attention bool) templ.Attributes {
	if attention {
		return templ.Attributes{"data-attention": "true"}
	}
	return templ.Attributes{}
}

// groupAttrs paints a group folded (from the cookie) or empty, as the live
// copy binds it, so the handover changes nothing.
func groupAttrs(g RailGroup) templ.Attributes {
	attrs := templ.Attributes{}
	if g.Collapsed {
		attrs["data-collapsed"] = "true"
	}
	if g.IsEmpty() {
		attrs["data-empty"] = "true"
	}
	return attrs
}

// shownIf paints an x-show element as Alpine will: hidden unless shown, so
// the first paint does not flash what the live page hides.
func shownIf(shown bool) templ.Attributes {
	if shown {
		return templ.Attributes{}
	}
	return templ.Attributes{"style": "display: none"}
}

// The rail row's one meta item, by priority: the reviews badge, else
// "waiting", else "N live". The browser's toRow (web/src/modules/tasks/
// presentation/railView.js) says the same.

// ReviewsText is the badge: "1 review", "2 reviews"; "" without reviews.
func (l Link) ReviewsText() string {
	switch {
	case l.Reviews == 0:
		return ""
	case l.Reviews == 1:
		return "1 review"
	default:
		return strconv.Itoa(l.Reviews) + " reviews"
	}
}

// Waiting: a session waits on the person and no review request does.
func (l Link) Waiting() bool { return l.Reviews == 0 && l.Attention }

// Running: live sessions and nothing waiting.
func (l Link) Running() bool { return !l.Attention && l.Live > 0 }

// MetaLead and MetaTail are the hidden words around the meta item, so the
// link reads "Redesign sidebar, running, 2 live sessions".
func (l Link) MetaLead() string {
	switch {
	case l.Running():
		return ", running, "
	case l.Attention:
		return ", "
	default:
		return ""
	}
}

func (l Link) MetaTail() string {
	switch {
	case l.Reviews > 0:
		return " waiting on you"
	case l.Attention:
		return " on you"
	case l.Live == 1:
		return " session"
	case l.Live > 1:
		return " sessions"
	default:
		return ""
	}
}

// HeadingID and ListID tie a group's heading to its section and list.
func (g RailGroup) HeadingID() string { return "rail-" + g.Key + "-heading" }
func (g RailGroup) ListID() string    { return "rail-" + g.Key }

// CountWords follow the heading's count: "Backlog 9 tasks, 1 needs you".
func (g RailGroup) CountWords() string {
	words := " tasks"
	if g.Total == 1 {
		words = " task"
	}
	waiting := 0
	for _, links := range [][]Link{g.Links, g.More} {
		for _, l := range links {
			if l.Attention {
				waiting++
			}
		}
	}
	switch {
	case waiting == 1:
		words += ", 1 needs you"
	case waiting > 1:
		words += ", " + strconv.Itoa(waiting) + " need you"
	}
	return words
}

// MoreText is Done's summary: "Show 7 more".
func (g RailGroup) MoreText() string { return "Show " + strconv.Itoa(len(g.More)) + " more" }

// ProjectName is the selected project's name, for the phone drawer's header.
func (f Frame) ProjectName() string {
	for _, p := range f.Top.Projects {
		if p.ID == f.Top.Current {
			return p.Name
		}
	}
	return ""
}
