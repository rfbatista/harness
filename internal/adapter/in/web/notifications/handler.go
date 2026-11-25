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
	"unicode/utf8"

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
	// Actor is the who filter (an actor's name) and Kind the kind filter;
	// "" for anyone and every kind. The browser reads such a page itself.
	Actor  string
	Kind   string
	Before string
}

// Filtered is whether who or kind narrows the page.
func (f Filter) Filtered() bool { return f.Actor != "" || f.Kind != "" }

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
	return Filter{ProjectID: q.Get("project"), Tab: tab, Actor: q.Get("actor"), Kind: q.Get("kind"), Before: q.Get("before")}
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
	// The server reads the first page by project and tab; who and kind are
	// read by the browser, which has the actors the server does not yet carry.
	if f.Filtered() {
		slog.Debug("notifications page: filtered by who or kind; the browser reads it")
	} else if page, err := h.Notifications.ListNotifications(ctx, ports.NotificationQuery{ProjectID: f.ProjectID, UnreadOnly: f.Unread(), Before: f.Before, Limit: pageLimit}); err != nil {
		slog.Warn("notifications page: read failed; the browser reads it again", "err", err)
	} else {
		v.Seed = page
	}
	// Needs you is the default while anything is open, and All otherwise.
	if f.Tab == "" {
		f.Tab = TabAll
		if v.opensHere() {
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
	// The ledger's columns: who (name, mode, kind and colour slot), what
	// (verb and object, or the title), where, when and where it stands.
	Kind, WhoName, WhoMode, WhoKind, WhoInitial, WhoSlot string
	Verb, ObjectText, Repository                         string
	ObjectCode                                           bool
	Clock, FullDate, StatusText, StatusTone, Label       string
	// Listed is a commit_check_off item's sessions (Reason holds the state word).
	Listed []SessionLine
	// Sessions is a sessions_interrupted item's list; the browser adds its
	// Resume all interrupted button.
	Sessions []SessionLine
}

// SessionLine is one session a sessions_interrupted item lists.
type SessionLine struct{ Label, Href, State, Reason string }

// outcomeWords are the words an outcome reads as, with their tone (the
// browser's OUTCOME_WORDS).
var outcomeWords = map[domain.NotificationOutcome][2]string{
	domain.OutcomeApproved:         {"Approved", "success"},
	domain.OutcomeChangesRequested: {"Changes requested", "attention"},
	domain.OutcomeDenied:           {"Denied", "danger"},
	domain.OutcomeExpired:          {"Expired", "quiet"},
	domain.OutcomeWithdrawn:        {"Withdrawn", "quiet"},
	domain.OutcomeAnswered:         {"Answered", "success"},
	domain.OutcomeDismissed:        {"Dismissed", "quiet"},
	domain.OutcomeAcknowledged:     {"Acknowledged", "success"},
	domain.OutcomeRepaired:         {"Repaired", "success"},
	domain.OutcomeResumed:          {"Resumed", "success"},
	domain.OutcomeEnded:            {"Ended", "quiet"},
	domain.OutcomeDone:             {"Done", "success"},
	domain.OutcomeReopened:         {"Reopened", "attention"},
}

// listedWords are the words a commit_check_off session's state reads as.
var listedWords = map[domain.CommitCheckState]string{
	domain.CommitCheckOff:      "check off",
	domain.CommitCheckRepaired: "repaired",
	domain.CommitCheckEnded:    "ended",
}

// agentSlots is how many colours an agent's avatar picks from.
const agentSlots = 8

// AgentSlot is the colour slot of an agent's name, the browser's agentSlot
// (a 31-hash over the name's code points, in 32 bits).
func AgentSlot(name string) int {
	var h uint32
	for _, c := range name {
		h = h*31 + uint32(c)
	}
	return int(h % agentSlots)
}

// standing is where a notification stands, in words and tone: Needs you, the
// outcome and its time, Settled, or Failed ("" for an information item).
func standing(n *domain.Notification, loc *time.Location) (string, string) {
	if n.ResolvedAt == nil {
		switch {
		case n.Kind == domain.NotificationSessionFailed:
			return "Failed", "danger"
		case n.Level == domain.NotificationAction:
			return "Needs you", "attention"
		}
		return "", "quiet"
	}
	if w, ok := outcomeWords[n.Outcome]; ok {
		return w[0] + " " + n.ResolvedAt.In(loc).Format("15:04"), w[1]
	}
	return "Settled", "quiet"
}

// ToRow paints n as of now: the browser's toRow, for the page without script.
func ToRow(n *domain.Notification, now time.Time) Row {
	loc := now.Location()
	unread := n.Unread()
	state := "read"
	switch {
	case n.ResolvedAt != nil:
		state = "settled"
	case unread:
		state = "unread"
	}
	statusText, statusTone := standing(n, loc)
	var lines []SessionLine
	for _, s := range n.InterruptedSessions {
		lines = append(lines, SessionLine{Label: sessionLabel(s.TaskTitle, s.AgentName), Href: s.Link, State: string(s.State), Reason: s.Reason})
	}
	var listed []SessionLine
	for _, s := range n.ListedSessions {
		word := listedWords[s.State]
		if word == "" {
			word = string(s.State)
		}
		listed = append(listed, SessionLine{Label: sessionLabel(s.TaskTitle, s.AgentName), Href: s.Link, State: string(s.State), Reason: word})
	}
	actor := n.Actor
	if actor.Name == "" {
		actor = domain.NotificationActor{Kind: domain.ActorSystem, Name: domain.ActorHarness}
	}
	what := n.Title
	var objectText string
	var objectCode bool
	if n.Verb != "" {
		what = n.Verb
		if n.Object != nil {
			objectText, objectCode = n.Object.Text, n.Object.Code
			what += " " + objectText
		}
	}
	where := n.ProjectName
	if n.TaskTitle != "" {
		where += " › " + n.TaskTitle
	}
	if n.RepositoryName != "" {
		where += " · " + n.RepositoryName
	}
	at := n.CreatedAt.In(loc)
	fullDate := at.Format("Mon 2 Jan 2006, 15:04")
	age := sessions.RelativeTime(n.CreatedAt, now)
	ago := age + " ago"
	if age == "now" {
		ago = "just now"
	}
	var parts []string
	if unread {
		parts = append(parts, "Unread.")
	}
	if isOpen(n) {
		parts = append(parts, "Needs you.")
	}
	parts = append(parts, actor.Name+" "+what+".")
	if where != "" {
		parts = append(parts, where+".")
	}
	parts = append(parts, fullDate+", "+ago+".")
	slot := ""
	if actor.Kind == domain.ActorAgent {
		slot = strconv.Itoa(AgentSlot(actor.Name))
	}
	initial, _ := utf8.DecodeRuneInString(actor.Name)
	return Row{
		Sessions: lines,
		Listed:   listed,
		ID:       n.ID, Href: n.Link, Title: n.Title, Body: n.Body, Project: n.ProjectName, Task: n.TaskTitle,
		Level: string(n.Level), LevelLabel: statusText, State: state, Unread: unread,
		Age: age, At: n.CreatedAt.UTC().Format(time.RFC3339),
		Kind: string(n.Kind), WhoName: actor.Name, WhoMode: actor.Mode, WhoKind: string(actor.Kind), WhoInitial: string(initial), WhoSlot: slot,
		Verb: n.Verb, ObjectText: objectText, ObjectCode: objectCode, Repository: n.RepositoryName,
		Clock: at.Format("15:04"), FullDate: fullDate, StatusText: statusText, StatusTone: statusTone,
		Label: strings.Join(parts, " "),
	}
}

// sessionLabel is "Ship · go-developer": a listed session's task and agent.
func sessionLabel(task, agent string) string {
	if task == "" {
		task = "No task"
	}
	if agent != "" {
		task += " · " + agent
	}
	return task
}

// isOpen is an open cause: level action and not settled.
func isOpen(n *domain.Notification) bool {
	return n.Level == domain.NotificationAction && n.ResolvedAt == nil
}

// OpenCount is the server's global count of open causes (ADR 0096).
func (v View) OpenCount() int {
	if v.Seed == nil {
		return 0
	}
	return v.Seed.OpenCount
}

// opensHere is whether the page lands on Needs you: the server's global open
// count says so, but within one project only the open causes of its first
// page can (the count is global whatever the filter).
func (v View) opensHere() bool {
	if v.Seed == nil {
		return false
	}
	if v.Filter.ProjectID == "" {
		return v.Seed.OpenCount > 0
	}
	for _, n := range v.Seed.Notifications {
		if isOpen(n) {
			return true
		}
	}
	return false
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

// KindOption is one entry of the kind filter, named as a person says it.
type KindOption struct{ Value, Label string }

// KindOptions are the notification kinds the page filters by.
var KindOptions = []KindOption{
	{"review_requested", "Review requests"},
	{"session_waiting", "Approvals and questions"},
	{"task_review", "Tasks in review"},
	{"session_failed", "Failed sessions"},
	{"commit_check_off", "Commit checks off"},
	{"branch_request", "Branch requests"},
	{"branch_finding", "Branch findings"},
	{"service_request", "Service requests"},
	{"sessions_interrupted", "Restarts"},
	{"agent", "Agent messages"},
}
