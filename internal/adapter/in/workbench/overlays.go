package workbench

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// --- new-session picker -------------------------------------------------------

type pickerStep int

const (
	stepAgent pickerStep = iota
	stepRepo
	stepPrompt
)

// picker collects what a new session needs: which agent (or plain claude),
// which repository when the project has more than one, and an optional first
// message. Enter moves forward a step, esc back one.
type picker struct {
	step    pickerStep
	loading bool

	agents []Agent // index 0 is plain claude
	agent  int
	repos  []domain.Repository
	repo   int
	prompt textinput.Model
}

type pickerDataMsg struct {
	agents []Agent
	repos  []domain.Repository
	err    error
}

func (m Model) openPicker() (tea.Model, tea.Cmd) {
	in := textinput.New()
	in.Placeholder = "optional first message — enter to start"
	in.Prompt = "› "
	in.SetWidth(max(20, m.width-8))
	m.overlay = &picker{loading: true, prompt: in}
	m.status = ""
	be, projectID := m.be, m.project.ID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		agents, err := be.ListAgents(ctx)
		if err != nil {
			return pickerDataMsg{err: err}
		}
		repos, err := be.ListRepositories(ctx, projectID)
		return pickerDataMsg{agents: agents, repos: repos, err: err}
	}
}

func (p *picker) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pickerDataMsg:
		p.loading = false
		if msg.err != nil {
			m.overlay = nil
			*m = m.fail(msg.err)
			return nil
		}
		if len(msg.repos) == 0 {
			m.overlay = nil
			*m = m.fail(fmt.Errorf("project %s has no repositories; add one in coding_pool first", m.project.Name))
			return nil
		}
		p.agents = append([]Agent{{Name: "plain claude", Description: "no agent template"}}, msg.agents...)
		p.repos = msg.repos
		return nil
	case tea.KeyPressMsg:
		if p.loading {
			if msg.String() == "esc" {
				m.overlay = nil
			}
			return nil
		}
		return p.key(m, msg)
	case tea.PasteMsg:
		if p.step == stepPrompt {
			var cmd tea.Cmd
			p.prompt, cmd = p.prompt.Update(msg)
			return cmd
		}
	}
	if p.step == stepPrompt {
		var cmd tea.Cmd
		p.prompt, cmd = p.prompt.Update(msg)
		return cmd
	}
	return nil
}

func (p *picker) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch k {
	case "esc":
		switch {
		case p.step == stepPrompt && len(p.repos) > 1:
			p.step = stepRepo
		case p.step != stepAgent:
			p.step = stepAgent
		default:
			m.overlay = nil
		}
		p.prompt.Blur()
		return nil
	case "enter":
		switch p.step {
		case stepAgent:
			p.step = stepPrompt
			if len(p.repos) > 1 {
				p.step = stepRepo
			}
		case stepRepo:
			p.step = stepPrompt
		case stepPrompt:
			m.overlay = nil
			m.loading = true
			return m.start(p.agents[p.agent], p.repos[p.repo].ID, strings.TrimSpace(p.prompt.Value()))
		}
		if p.step == stepPrompt {
			return p.prompt.Focus()
		}
		return nil
	}
	switch p.step {
	case stepAgent:
		p.agent, _ = moveCursor(k, p.agent, len(p.agents))
	case stepRepo:
		p.repo, _ = moveCursor(k, p.repo, len(p.repos))
	case stepPrompt:
		var cmd tea.Cmd
		p.prompt, cmd = p.prompt.Update(msg)
		return cmd
	}
	return nil
}

// start asks the server for a session in the current task and launches it.
func (m Model) start(agent Agent, repoID, prompt string) tea.Cmd {
	be := m.be
	req := orchestration.InteractiveRequest{
		ProjectID:    m.project.ID,
		RepositoryID: repoID,
		TicketID:     m.ticket.ID,
		AgentID:      agent.ID,
		Prompt:       prompt,
	}
	label := agent.Name
	if agent.ID == "" {
		label = "claude"
	}
	ticketID := m.ticket.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		s, l, err := be.StartSession(ctx, req)
		return launchedMsg{ticketID: ticketID, label: label, session: s, launch: specOf(l), err: err}
	}
}

func specOf(l orchestration.Launch) launchSpec {
	return launchSpec{dir: l.Dir, args: l.Args, env: l.Env}
}

func (p *picker) view(m Model, height int) string {
	width := m.width
	if p.loading {
		return noteStyle.Render("  loading agents and repositories…  (esc cancel)")
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render("  New agent session") + "\n\n")

	b.WriteString(stepTitle("Agent", p.step == stepAgent))
	if p.step == stepAgent {
		for i, a := range p.agents {
			row := "    " + fit(a.Name, 24) + "  " + dimStyle.Render(a.Description)
			if i == p.agent {
				row = cursorStyle.Render(fit(row, width-2))
			}
			b.WriteString(row + "\n")
		}
	} else {
		b.WriteString("    " + p.agents[p.agent].Name + "\n")
	}

	if len(p.repos) > 1 {
		b.WriteString("\n" + stepTitle("Repository", p.step == stepRepo))
		if p.step == stepRepo {
			for i, r := range p.repos {
				row := "    " + fit(r.Name, 24) + "  " + dimStyle.Render(r.RootDir)
				if i == p.repo {
					row = cursorStyle.Render(fit(row, width-2))
				}
				b.WriteString(row + "\n")
			}
		} else if p.step > stepRepo {
			b.WriteString("    " + p.repos[p.repo].Name + "\n")
		}
	}

	if p.step == stepPrompt {
		b.WriteString("\n" + stepTitle("First message", true))
		b.WriteString("  " + p.prompt.View() + "\n")
	}
	b.WriteString("\n" + noteStyle.Render("  ↑↓ choose · enter next · esc back"))
	return b.String()
}

func stepTitle(s string, current bool) string {
	if current {
		return "  " + headerStyle.Render(s) + "\n"
	}
	return "  " + dimStyle.Render(s) + "\n"
}

// --- sessions overlay ---------------------------------------------------------

// sessionsOverlay lists every recorded session of the task. Enter focuses an
// open one, resumes an ended interactive one, and explains why a headless one
// cannot be opened here.
type sessionsOverlay struct{ cursor int }

func newSessionsOverlay() *sessionsOverlay { return &sessionsOverlay{} }

func (m Model) taskSessions() []domain.Session {
	var out []domain.Session
	for _, s := range m.sessions {
		if s.TicketID == m.ticket.ID {
			out = append(out, s)
		}
	}
	return out
}

func (o *sessionsOverlay) update(m *Model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	sessions := m.taskSessions()
	k := key.String()
	if c, ok := moveCursor(k, o.cursor, len(sessions)); ok {
		o.cursor = c
		return nil
	}
	switch k {
	case "esc", "q", "s":
		m.overlay = nil
	case "n", "c":
		next, cmd := m.openPicker()
		*m = next.(Model)
		return cmd
	case "enter":
		if len(sessions) == 0 {
			return nil
		}
		s := sessions[clamp(o.cursor, len(sessions))]
		if paneID, ticketID, open := m.paneOf(s.ID); open {
			m.decks[ticketID] = m.decks[ticketID].Focus(paneID)
			m.overlay = nil
			return nil
		}
		switch {
		case !s.Interactive:
			*m = m.note("headless session: follow it in the coding_pool web UI")
		case !s.Status.IsTerminal():
			*m = m.note("that session is running in another terminal")
		default:
			m.overlay = nil
			m.loading = true
			return m.resume(s)
		}
	}
	return nil
}

func (m Model) resume(s domain.Session) tea.Cmd {
	be := m.be
	label := strings.TrimPrefix(s.Branch, "agent/")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		got, l, err := be.ResumeSession(ctx, s.ID)
		return launchedMsg{ticketID: s.TicketID, label: label, session: got, launch: specOf(l), err: err}
	}
}

func (o *sessionsOverlay) view(m Model, height int) string {
	sessions := m.taskSessions()
	var b strings.Builder
	b.WriteString(headerStyle.Render("  Sessions of this task") + "\n\n")
	if len(sessions) == 0 {
		b.WriteString(noteStyle.Render("  none yet — n starts one") + "\n")
	}
	from, to := window(o.cursor, len(sessions), max(1, height-4))
	for i := from; i < to; i++ {
		s := sessions[i]
		mark := "  "
		if _, _, open := m.paneOf(s.ID); open {
			mark = liveStyle.Render("● ")
		}
		kind := "terminal"
		if !s.Interactive {
			kind = "headless"
		}
		row := "  " + mark + fit(string(s.Status), 18) + fit(kind, 10) + fit(s.Branch, max(10, m.width-60)) + " " + dimStyle.Render(age(s.CreatedAt))
		if i == o.cursor {
			row = cursorStyle.Render(fit(row, m.width-2))
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + noteStyle.Render("  enter open or resume · n new · esc close"))
	return b.String()
}

func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
