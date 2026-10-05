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

// Row is one session in the list.
type Row struct {
	ID, Title, State, Word, Meta string
	Selected, Attention          bool
}

// Group is one triage group with its rows.
type Group struct {
	Key, Label, Tone string
	Count            int
	Rows             []Row
}

// IsLive: the session's process is alive (not done, failed or stopped).
func IsLive(s *domain.Session) bool { return !s.Status.IsTerminal() }

// Seed is what the browser starts from: the API's JSON shape
// (GET /api/sessions plus the task), decoded by the sessions gateway.
// AgentNames lets live updates show an agent's name, as the first paint does.
type Seed struct {
	ProjectID  string            `json:"project_id"`
	TicketID   string            `json:"ticket_id"`
	Sessions   []*domain.Session `json:"sessions"`
	AgentNames map[string]string `json:"agent_names"`
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
	ProjectID   string
	ProjectName string
	TaskTitle   string
	TaskStatus  string
	Summary     string
	Groups      []Group
	Seed        Seed
	NewSession  NewSessionForm
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
	for _, r := range repos {
		form.Repositories = append(form.Repositories, Option{r.ID, r.Name})
	}

	views := make([]Group, 0, len(groups))
	for _, g := range groups {
		rows := make([]Row, 0, len(g.sessions))
		for _, s := range g.sessions {
			rows = append(rows, toRow(s, selected, names, now))
		}
		views = append(views, Group{Key: g.key, Label: groupLabels[g.key], Tone: groupTones[g.key], Count: len(rows), Rows: rows})
	}

	return PageView{
		Frame:       frame,
		ProjectID:   project.ID,
		ProjectName: project.Name,
		TaskTitle:   task.Title,
		Task:        task,
		TaskDescription: task.Description,
		TaskStatus:  StatusLabel(task.Status),
		Summary:     summary(list),
		Groups:      views,
		Seed:        Seed{ProjectID: project.ID, TicketID: task.ID, Sessions: list, AgentNames: names},
		NewSession:  form,
	}
}

// StatusLabel is a task status as words: "in progress".
func StatusLabel(s domain.TicketStatus) string {
	return strings.ReplaceAll(string(s), "_", " ")
}

func toRow(s *domain.Session, selectedID string, agentNames map[string]string, now time.Time) Row {
	st := statusOf(s)
	title := s.Task
	if title == "" {
		title = "Untitled session"
	}
	agent := AgentLabel(s.AgentID, agentNames)
	return Row{
		ID:        s.ID,
		Title:     title,
		State:     st.State,
		Word:      st.Word,
		Meta:      agent + " · " + relativeTime(s.UpdatedAt, now),
		Selected:  s.ID == selectedID,
		Attention: NeedsYou(s),
	}
}

var (
	groupOrder  = []string{"needs-you", "active", "finished"}
	groupLabels = map[string]string{"needs-you": "Needs you", "active": "Running", "finished": "Earlier"}
	groupTones  = map[string]string{"needs-you": "attention"}
)

type sessionGroup struct {
	key      string
	sessions []*domain.Session
}

// group sorts most recent first and splits for triage, leaving out empty groups.
func group(list []*domain.Session) []sessionGroup {
	sorted := slices.Clone(list)
	slices.SortStableFunc(sorted, func(a, b *domain.Session) int { return cmp.Compare(b.UpdatedAt.UnixNano(), a.UpdatedAt.UnixNano()) })

	byKey := map[string][]*domain.Session{}
	for _, s := range sorted {
		switch {
		case NeedsYou(s):
			byKey["needs-you"] = append(byKey["needs-you"], s)
		case s.Status.IsTerminal():
			byKey["finished"] = append(byKey["finished"], s)
		default:
			byKey["active"] = append(byKey["active"], s)
		}
	}
	var out []sessionGroup
	for _, k := range groupOrder {
		if len(byKey[k]) > 0 {
			out = append(out, sessionGroup{k, byKey[k]})
		}
	}
	return out
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
