// Package spawn is the wizard that starts an agent session: project,
// repository, base branch, agent profile, task, then the details form. Each
// step is a picker; the last is a form. It mirrors the Flutter spawn modal,
// including the branch suggestion with a four-hex suffix.
//
// spawn.go holds the state machine and message loop, steps.go what each
// picker lists and where it leads, details.go the final form and the start
// request, view.go the rendering.
package spawn

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/domain"
)

// Step is one wizard page.
type Step int

const (
	StepProject Step = iota
	StepRepository
	StepBranch
	StepAgent
	StepTask
	StepDetails
)

var stepLabels = map[Step]string{
	StepProject: "Project", StepRepository: "Repository", StepBranch: "Base branch",
	StepAgent: "Agent", StepTask: "Task", StepDetails: "Details",
}

// Options prefill the wizard; a TicketID also fixes the project.
type Options struct {
	ProjectID string
	TicketID  string
}

// DoneMsg is emitted with the started session.
type DoneMsg struct{ Session *domain.Session }

// CancelMsg is emitted when the user backs out of the first step.
type CancelMsg struct{}

// branchesMsg is the outcome of listing a repository's branches.
type branchesMsg struct {
	repoID   string
	branches []domain.GitBranch
	err      error
}

// startedMsg is the outcome of starting the session.
type startedMsg struct {
	session *domain.Session
	err     error
}

// Model is the wizard state.
type Model struct {
	ctx  core.Context
	be   backend.Backend
	snap backend.Snapshot
	opts Options

	step       Step
	projectID  string
	repoID     string
	baseBranch string
	agentID    string
	ticketID   string
	newTask    bool

	branches   []domain.GitBranch
	loading    bool
	cursor     int
	form       *components.Form
	suggestion string
	submitting bool
	err        string
}

// New builds the wizard, skipping the steps the options already answer.
func New(ctx core.Context, be backend.Backend, snap backend.Snapshot, opts Options) Model {
	m := Model{ctx: ctx, be: be, snap: snap, opts: opts, projectID: opts.ProjectID, ticketID: opts.TicketID}
	if m.ticketID != "" {
		for _, t := range snap.Tickets {
			if t.ID == m.ticketID {
				m.projectID = t.ProjectID
			}
		}
	}
	if m.projectID == "" && len(snap.Projects) == 1 {
		m.projectID = snap.Projects[0].ID
	}
	if m.projectID != "" {
		m.step = StepRepository
	}
	return m
}

// Step is the current page.
func (m Model) Step() Step { return m.step }

// Init is a no-op; the wizard starts on a picker.
func (m Model) Init() tea.Cmd { return nil }

// Update handles pickers, async loads and the details form.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ContextMsg:
		m.ctx = msg.Ctx
		return m, nil
	case core.SnapshotMsg:
		m.snap = msg.Snapshot
		return m, nil
	case branchesMsg:
		if msg.repoID != m.repoID {
			return m, nil // a stale answer for a repository no longer chosen
		}
		m.loading = false
		if msg.err != nil {
			m.err = core.Message(msg.err)
			return m, nil
		}
		m.branches = msg.branches
		m.cursor = 0
		for i, b := range m.branches {
			if b.IsHead {
				m.cursor = i
			}
		}
		return m, nil
	case startedMsg:
		m.submitting = false
		if msg.err != nil {
			if m.form != nil {
				m.form.SetError(core.Message(msg.err))
			}
			return m, nil
		}
		session := msg.session
		return m, tea.Batch(
			func() tea.Msg { return DoneMsg{Session: session} },
			func() tea.Msg { return core.RefreshMsg{} },
		)
	case components.FormSubmitMsg:
		return m.submit(msg)
	case components.FormCancelMsg:
		return m.back()
	case components.EditorDoneMsg:
		return m.formUpdate(msg)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// formUpdate feeds the details form and keeps the branch suggestion in step.
func (m Model) formUpdate(msg tea.Msg) (Model, tea.Cmd) {
	if m.form == nil {
		return m, nil
	}
	f, cmd := m.form.Update(msg)
	m.form = &f
	m.syncBranch()
	return m, cmd
}

func (m Model) key(k tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.step == StepDetails && m.form != nil {
		return m.formUpdate(k)
	}
	rows := m.rows()
	switch k.String() {
	case "esc":
		return m.back()
	case "j", "down":
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = len(rows) - 1
	case "enter":
		if m.loading || len(rows) == 0 {
			return m, nil
		}
		return m.choose(rows[m.cursor])
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m, nil
}

func (m *Model) ticket() *domain.Ticket {
	for _, t := range m.snap.Tickets {
		if t.ID == m.ticketID {
			return t
		}
	}
	return nil
}

// firstStep is where esc cancels instead of going back: the project picker,
// unless the options or a single project already answered it.
func (m Model) firstStep() Step {
	if m.opts.ProjectID != "" || m.opts.TicketID != "" || len(m.snap.Projects) == 1 {
		return StepRepository
	}
	return StepProject
}

// suffixOf returns the random hex suffix of a suggested branch name.
func suffixOf(suggestion string) string {
	return suggestion[strings.LastIndex(suggestion, "-")+1:]
}
