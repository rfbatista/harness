package spawn

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// row is one picker line.
type row struct {
	id    string
	label string
	hint  string
}

// rows lists what the current step offers to pick from.
func (m Model) rows() []row {
	var out []row
	switch m.step {
	case StepProject:
		for _, p := range m.snap.Projects {
			out = append(out, row{p.ID, p.Name, p.RootDir})
		}
	case StepRepository:
		for _, r := range m.snap.RepositoriesOf(m.projectID) {
			out = append(out, row{r.ID, r.Name, r.RootDir})
		}
	case StepBranch:
		for _, b := range m.branches {
			hint := ""
			if b.IsHead {
				hint = "HEAD"
			} else if b.Remote {
				hint = "remote"
			}
			out = append(out, row{b.Name, b.Name, hint})
		}
	case StepAgent:
		for _, a := range m.snap.Agents {
			out = append(out, row{a.ID, a.Name, a.Description})
		}
	case StepTask:
		out = append(out, row{"", "New task…", "create a task for this run"})
		for _, t := range m.snap.Tickets {
			if t.ProjectID == m.projectID {
				out = append(out, row{t.ID, t.Title, string(t.Status)})
			}
		}
	}
	return out
}

// choose records the pick for the current step and advances. The task step
// is skipped when the wizard was opened on a task.
func (m Model) choose(r row) (Model, tea.Cmd) {
	switch m.step {
	case StepProject:
		m.projectID = r.id
		return m.goTo(StepRepository)
	case StepRepository:
		m.repoID = r.id
		return m.goTo(StepBranch)
	case StepBranch:
		m.baseBranch = r.id
		return m.goTo(StepAgent)
	case StepAgent:
		m.agentID = r.id
		if m.ticketID != "" {
			return m.goTo(StepDetails)
		}
		return m.goTo(StepTask)
	case StepTask:
		m.ticketID, m.newTask = r.id, r.id == ""
		return m.goTo(StepDetails)
	}
	return m, nil
}

// goTo enters a step: the branch step loads branches, the details step
// builds its form.
func (m Model) goTo(s Step) (Model, tea.Cmd) {
	m.step, m.cursor, m.err = s, 0, ""
	switch s {
	case StepBranch:
		m.loading = true
		m.branches = nil
		return m, m.loadBranches()
	case StepDetails:
		m.buildForm()
	}
	return m, nil
}

func (m Model) loadBranches() tea.Cmd {
	be, repo := m.be, m.repoID
	return func() tea.Msg {
		branches, err := be.ListBranches(repo)
		return branchesMsg{repoID: repo, branches: branches, err: err}
	}
}

// back returns to the previous step, or cancels from the first one.
func (m Model) back() (Model, tea.Cmd) {
	if m.step == m.firstStep() {
		return m, func() tea.Msg { return CancelMsg{} }
	}
	prev := m.step - 1
	if prev == StepTask && m.opts.TicketID != "" {
		prev = StepAgent
	}
	m.form = nil
	m.step, m.cursor, m.err = prev, 0, ""
	if prev == StepBranch {
		for i, b := range m.branches {
			if b.Name == m.baseBranch {
				m.cursor = i
			}
		}
	}
	return m, nil
}

func (m Model) prompt() string {
	switch m.step {
	case StepProject:
		return "Which project?"
	case StepRepository:
		return "Which repository gets the worktree?"
	case StepBranch:
		return "Cut the session branch from:"
	case StepAgent:
		return "Which agent profile?"
	case StepTask:
		return "Work on:"
	}
	return ""
}

func (m Model) emptyText() string {
	switch m.step {
	case StepRepository:
		return "This project has no repositories. Add one under Settings first."
	case StepBranch:
		return "No branches found."
	case StepAgent:
		return "No agent profiles. Create one under Agents first."
	}
	return fmt.Sprintf("Nothing to choose at step %s.", stepLabels[m.step])
}
