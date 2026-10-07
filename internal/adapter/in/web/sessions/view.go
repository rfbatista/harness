// Package sessions renders a task's sessions: the inner list beside the
// selected session's detail. The server renders the first paint and embeds
// the seed; web/src/modules/sessions takes over in the browser.
package sessions

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"operators-mcp/internal/adapter/in/web/components"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// The rules below are the Go twin of web/src/modules/sessions (domain/session.js
// and presentation/view.js); web/testdata/views/session-status.json pins both.

// StatusView is a status as the .status block shows it.
type StatusView struct{ State, Word string }

var statusViews = map[domain.SessionStatus]StatusView{
	domain.SessionStarting:        {"running", "starting"},
	domain.SessionRunning:         {"running", "running"},
	domain.SessionThinking:        {"running", "thinking"},
	domain.SessionIdle:            {"waiting", "your turn"},
	domain.SessionWaitingApproval: {"waiting", "approval"},
	domain.SessionPaused:          {"idle", "paused"},
	domain.SessionDone:            {"done", "done"},
	domain.SessionStopped:         {"done", "stopped"},
	domain.SessionFailed:          {"failed", "failed"},
}

// statusOf reads pending approvals before the status: a running session with
// a tool call to approve is waiting on the developer.
func statusOf(s *domain.Session) StatusView {
	if !s.Status.IsTerminal() && s.PendingApprovals > 0 {
		return statusViews[domain.SessionWaitingApproval]
	}
	if v, ok := statusViews[s.Status]; ok {
		return v
	}
	return StatusView{"idle", string(s.Status)}
}

// NeedsYou: an approval, or an interactive session at its turn boundary.
func NeedsYou(s *domain.Session) bool {
	if s.Status.IsTerminal() {
		return false
	}
	return s.Status == domain.SessionWaitingApproval || s.Status == domain.SessionIdle || s.PendingApprovals > 0
}

// MaxDepth is how far a delegate's row is indented: deeper ones line up at
// the last step. The browser's MAX_DEPTH (view.js) is the same.
const MaxDepth = 3

// Row is one session in the list. Role, Depth and Badge place it in the
// architect's group: the architect badged, its delegates indented.
type Row struct {
	ID, Title, State, Word, Meta string
	Selected, Attention          bool
	Role                         string
	Depth                        int
	Badge                        string
}

// Group is one triage group with its rows.
type Group struct {
	Key, Label, Tone string
	Count            int
	Rows             []Row
}

// IsLive: the session's process is alive (not done, failed or stopped).
func IsLive(s *domain.Session) bool { return !s.Status.IsTerminal() }

// DocumentsLink is the toolbar's way to the task's documents: how many there
// are, kept current by the browser (tasksDocumentWatch) as agents write more.
type DocumentsLink struct {
	Href      string
	TicketID  string
	Count     int
	Signature string
}

// Seed is what the browser starts from: the API's JSON shape
// (GET /api/sessions plus the task), decoded by the sessions gateway.
// AgentNames lets live updates show an agent's name, as the first paint does.
type Seed struct {
	ProjectID  string            `json:"project_id"`
	TicketID   string            `json:"ticket_id"`
	Sessions   []*domain.Session `json:"sessions"`
	AgentNames map[string]string `json:"agent_names"`
	// RepositoryNames lets the App tab say which repository a run's code
	// comes from.
	RepositoryNames map[string]string `json:"repository_names"`
	// DocumentTitles names the task's documents a task message links.
	DocumentTitles map[string]string `json:"document_titles,omitempty"`
}

// Option is one choice in the new-session form.
type Option struct{ ID, Name string }

// NewSessionForm is what the new-session form offers: which agent runs, in
// which repository. With no repository a session cannot start.
type NewSessionForm struct {
	Agents       []Option
	Repositories []Option
}

// CanStart reports whether the project has a repository to run a session in.
func (f NewSessionForm) CanStart() bool { return len(f.Repositories) > 0 }

// DefaultRepository is preselected: the first, which is the only one in most
// projects.
func (f NewSessionForm) DefaultRepository() string {
	if len(f.Repositories) == 0 {
		return ""
	}
	return f.Repositories[0].ID
}

// StatusChoice is one option of the task's status picker.
type StatusChoice struct {
	Value, Label string
	Selected     bool
}

// PageView is everything page.templ renders: one task and its sessions.
type PageView struct {
	Frame shell.Frame
	// Task seeds the task editor (tasksTaskEditor), in the API's shape.
	Task            *domain.Ticket
	TaskDescription string
	StatusChoices   []StatusChoice
	ProjectID       string
	ProjectName     string
	TaskTitle       string
	TaskStatus      string
	Summary         string
	Groups          []Group
	Seed            Seed
	// Documents links the task's documents page; nil hides it.
	Documents *DocumentsLink
	// Reviews seeds the review band (reviewsInbox); nil hides it.
	Reviews *components.ReviewsSeed
	// PendingReviews is how many requests wait on the person, for the band's
	// first paint.
	PendingReviews int
	NewSession     NewSessionForm
}

// Empty reports a project without sessions.
func (v PageView) Empty() bool { return len(v.Groups) == 0 }

// NewPageView builds the page for a task's sessions at time now. The first
// session on screen starts selected, as the browser's page component does.
func NewPageView(frame shell.Frame, project *domain.Project, task *domain.Ticket, list []*domain.Session, agents []*domain.Agent, repos []*domain.Repository, now time.Time) PageView {
	if list == nil {
		list = []*domain.Session{} // the seed must be [], never null
	}
	groups := group(list)
	selected := ""
	if len(groups) > 0 {
		selected = groups[0].sessions[0].ID
	}

	names := make(map[string]string, len(agents))
	form := NewSessionForm{}
	for _, a := range agents {
		names[a.ID] = a.Name
		form.Agents = append(form.Agents, Option{a.ID, a.Name})
	}
	repoNames := make(map[string]string, len(repos))
	for _, r := range repos {
		repoNames[r.ID] = r.Name
		form.Repositories = append(form.Repositories, Option{r.ID, r.Name})
	}

	byID := make(map[string]*domain.Session, len(list))
	for _, s := range list {
		byID[s.ID] = s
	}
	views := make([]Group, 0, len(groups))
	for _, g := range groups {
		rows := make([]Row, 0, len(g.sessions))
		for _, s := range g.sessions {
			rows = append(rows, toRow(s, selected, names, now, byID, g.depth[s.ID]))
		}
		views = append(views, Group{Key: g.key, Label: groupLabels[g.key], Tone: groupTones[g.key], Count: len(rows), Rows: rows})
	}

	return PageView{
		Frame:           frame,
		ProjectID:       project.ID,
		ProjectName:     project.Name,
		TaskTitle:       task.Title,
		Task:            task,
		TaskDescription: task.Description,
		TaskStatus:      StatusLabel(task.Status),
		PendingReviews:  task.PendingReviews,
		Summary:         summary(list),
		Groups:          views,
		Seed:            Seed{ProjectID: project.ID, TicketID: task.ID, Sessions: list, AgentNames: names, RepositoryNames: repoNames},
		NewSession:      form,
	}
}

// StatusLabel is a task status as words: "in progress".
func StatusLabel(s domain.TicketStatus) string {
	return strings.ReplaceAll(string(s), "_", " ")
}

// StartedBy is how the session that started s reads — its agent, or
// "another session" when it is not among others — and "" when a person did.
// The browser's startedBy (view.js) says the same.
func StartedBy(s *domain.Session, others map[string]*domain.Session, agentNames map[string]string) string {
	if s.ParentSessionID == "" {
		return ""
	}
	if p, ok := others[s.ParentSessionID]; ok {
		return AgentLabel(p.AgentID, agentNames)
	}
	return "another session"
}

func toRow(s *domain.Session, selectedID string, agentNames map[string]string, now time.Time, others map[string]*domain.Session, depth int) Row {
	st := statusOf(s)
	title := s.Task
	if title == "" {
		title = "Untitled session"
	}
	agent := RunsAs(s, agentNames)
	return Row{
		ID:        s.ID,
		Title:     title,
		State:     st.State,
		Word:      st.Word,
		Meta:      rowMeta(s, agent, others, agentNames, now),
		Selected:  s.ID == selectedID,
		Attention: NeedsYou(s),
		Role:      string(s.Role),
		Depth:     min(depth, MaxDepth),
		Badge:     badge(s),
	}
}

// rowMeta is a row's faint line. A delegate's says its status check instead
// of who started it (the tree shows that); the browser adds its last report
// once the task's messages have loaded (delegateMeta in view.js).
func rowMeta(s *domain.Session, agent string, others map[string]*domain.Session, agentNames map[string]string, now time.Time) string {
	if s.Role != domain.RoleDelegate {
		return meta(agent, StartedBy(s, others, agentNames), relativeTime(s.UpdatedAt, now))
	}
	if s.StatusCheck != nil {
		return agent + " · " + StatusCheckOf(s.StatusCheck, now).Short
	}
	return agent + " · " + relativeTime(s.UpdatedAt, now)
}

// CheckView is a status-check loop as the page shows it: the .status state,
// the row's short word and the header's detail.
type CheckView struct{ State, Short, Detail string }

var checkStates = map[domain.StatusCheckState]string{
	domain.StatusCheckActive: "running",
	domain.StatusCheckPaused: "idle",
	domain.StatusCheckEnded:  "done",
}

// StatusCheckOf is how a loop reads at now. The browser's statusCheckView
// (view.js) says the same; web/testdata/views/status-check.json pins both.
func StatusCheckOf(c *domain.StatusCheck, now time.Time) CheckView {
	last := "no check yet"
	if c.LastFiredAt != nil {
		ago := relativeTime(*c.LastFiredAt, now) + " ago"
		if ago == "now ago" {
			ago = "just now"
		}
		last = "last " + ago + " · " + count(c.FiredCount, "check")
	}
	if c.State != domain.StatusCheckActive {
		word := "checks ended"
		if c.State == domain.StatusCheckPaused {
			word = "checks paused"
		}
		return CheckView{checkStates[c.State], word, word + " · " + last}
	}
	next := ""
	if c.NextAt != nil && c.NextAt.Sub(now) >= time.Minute {
		next = relativeTime(now, *c.NextAt)
	}
	every := relativeTime(time.Time{}, time.Time{}.Add(c.Every()))
	if next == "" {
		return CheckView{checkStates[c.State], "check due", "checks every " + every + " · next due now · " + last}
	}
	return CheckView{checkStates[c.State], "check in " + next, "checks every " + every + " · next in " + next + " · " + last}
}

// badge marks the task's architect in the list.
func badge(s *domain.Session) string {
	if s.Role == domain.RoleArchitect {
		return "architect"
	}
	return ""
}

var (
	groupOrder  = []string{"needs-you", "active", "finished"}
	groupLabels = map[string]string{"architect": "Architect", "needs-you": "Needs you", "active": "Running", "finished": "Earlier"}
	groupTones  = map[string]string{"needs-you": "attention"}
)

type sessionGroup struct {
	key      string
	sessions []*domain.Session
	// depth is how far each session is below the architect, on its group.
	depth map[string]int
}

// group sorts most recent first and splits for triage, leaving out empty
// groups. On a task with an architect, the architect and its delegates lead
// in their own group. The browser's group (domain/session.js) is the same;
// web/testdata/views/session-roles.json pins both.
func group(list []*domain.Session) []sessionGroup {
	sorted := slices.Clone(list)
	slices.SortStableFunc(sorted, func(a, b *domain.Session) int { return cmp.Compare(b.UpdatedAt.UnixNano(), a.UpdatedAt.UnixNano()) })

	var out []sessionGroup
	lead := architectGroup(sorted)
	if lead != nil {
		out = append(out, *lead)
	}

	byKey := map[string][]*domain.Session{}
	for _, s := range sorted {
		if lead != nil {
			if _, ok := lead.depth[s.ID]; ok {
				continue
			}
		}
		switch {
		case NeedsYou(s):
			byKey["needs-you"] = append(byKey["needs-you"], s)
		case s.Status.IsTerminal():
			byKey["finished"] = append(byKey["finished"], s)
		default:
			byKey["active"] = append(byKey["active"], s)
		}
	}
	for _, k := range groupOrder {
		if len(byKey[k]) > 0 {
			out = append(out, sessionGroup{key: k, sessions: byKey[k]})
		}
	}
	return out
}

// architectGroup is the architect and its delegates, depth-first, newest
// first among siblings (sorted is most recent first). A delegate whose parent
// is not in the list sits right under the architect. Nil without an architect.
func architectGroup(sorted []*domain.Session) *sessionGroup {
	var architect *domain.Session
	var delegates []*domain.Session
	for _, s := range sorted {
		switch {
		case s.Role == domain.RoleArchitect && architect == nil:
			architect = s
		case s.Role == domain.RoleDelegate:
			delegates = append(delegates, s)
		}
	}
	if architect == nil {
		return nil
	}
	isDelegate := make(map[string]bool, len(delegates))
	for _, s := range delegates {
		isDelegate[s.ID] = true
	}
	parentOf := func(s *domain.Session) string {
		if s.ParentSessionID != "" && isDelegate[s.ParentSessionID] {
			return s.ParentSessionID
		}
		return architect.ID
	}

	g := sessionGroup{key: "architect", depth: map[string]int{}}
	var visit func(s *domain.Session, d int)
	visit = func(s *domain.Session, d int) {
		if _, seen := g.depth[s.ID]; seen {
			return // a cycle; each session shows once
		}
		g.sessions = append(g.sessions, s)
		g.depth[s.ID] = d
		for _, child := range delegates {
			if child.ID != s.ID && parentOf(child) == s.ID {
				visit(child, d+1)
			}
		}
	}
	visit(architect, 0)
	// Delegates on a cycle that never reaches the architect still show, under it.
	for _, s := range delegates {
		if _, seen := g.depth[s.ID]; !seen {
			visit(s, 1)
		}
	}
	return &g
}

func summary(list []*domain.Session) string {
	waiting := 0
	for _, s := range list {
		if NeedsYou(s) {
			waiting++
		}
	}
	if waiting > 0 {
		return fmt.Sprintf("%s · %d waiting", count(len(list), "session"), waiting)
	}
	return count(len(list), "session")
}

// AgentLabel is how a session's agent reads: its name, its ID when the agent
// is gone, "plain claude" when the session runs without one.
func AgentLabel(id string, names map[string]string) string {
	switch {
	case id == "":
		return "plain claude"
	case names[id] != "":
		return names[id]
	default:
		return id
	}
}

// RunsAs is how a session's agent reads with the mode it was started in:
// "Reviewer", "plain claude as architect". The browser's runsAs (view.js)
// says the same.
func RunsAs(s *domain.Session, names map[string]string) string {
	agent := AgentLabel(s.AgentID, names)
	if s.Mode == domain.SessionModeDefault {
		return agent
	}
	return agent + " as " + string(s.Mode)
}

// RelativeTime is how long ago then was, as the pages write it: "now",
// "5m", "3h", "2d".
func RelativeTime(then, now time.Time) string { return relativeTime(then, now) }

// relativeTime and count mirror web/src/shared/presentation/format.js.
func relativeTime(then, now time.Time) string {
	d := max(now.Sub(then), 0)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// meta is a row's faint line: the agent, who started it (if a session did),
// and when it last changed.
func meta(agent, startedBy, when string) string {
	if startedBy == "" {
		return agent + " · " + when
	}
	return agent + " · started by " + startedBy + " · " + when
}
