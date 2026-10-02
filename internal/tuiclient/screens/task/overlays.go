package task

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/tuiclient/nav"
	"operators-mcp/internal/tuiclient/panes"
	"operators-mcp/internal/tuiclient/ui"
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

	agents []*domain.Agent // index 0 is plain claude
	agent  int
	repos  []*domain.Repository
	repo   int
	prompt textinput.Model
}

type pickerDataMsg struct {
	agents []*domain.Agent
	repos  []*domain.Repository
	err    error
}

func (s *Screen) openPicker() tea.Cmd {
	in := textinput.New()
	in.Placeholder = "optional first message — enter to start"
	in.Prompt = "› "
	in.SetWidth(max(20, s.width-8))
	s.overlay = &picker{loading: true, prompt: in}
	agents, repos, pid := s.ports.Agents, s.ports.Repositories, s.project.ID
	return nav.Call(func(ctx context.Context) tea.Msg {
		as, err := agents.ListAgents(ctx)
		if err != nil {
			return pickerDataMsg{err: err}
		}
		rs, err := repos.ListRepositories(ctx, pid)
		return pickerDataMsg{agents: as, repos: rs, err: err}
	})
}

func (p *picker) update(s *Screen, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pickerDataMsg:
		p.loading = false
		if msg.err != nil {
			s.overlay = nil
			return nav.Fail(msg.err)
		}
		if len(msg.repos) == 0 {
			s.overlay = nil
			return nav.Fail(fmt.Errorf("project %s has no repositories; add one in coding_pool first", s.project.Name))
		}
		p.agents = append([]*domain.Agent{{Name: "plain claude", Description: "no agent template"}}, msg.agents...)
		p.repos = msg.repos
		return nil
	case tea.KeyPressMsg:
		if p.loading {
			if msg.String() == "esc" {
				s.overlay = nil
			}
			return nil
		}
		return p.key(s, msg)
	}
	if p.step == stepPrompt {
		var cmd tea.Cmd
		p.prompt, cmd = p.prompt.Update(msg)
		return cmd
	}
	return nil
}

func (p *picker) key(s *Screen, msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch k {
	case "esc":
		switch {
		case p.step == stepPrompt && len(p.repos) > 1:
			p.step = stepRepo
		case p.step != stepAgent:
			p.step = stepAgent
		default:
			s.overlay = nil
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
			s.overlay = nil
			return s.start(p.agents[p.agent], p.repos[p.repo].ID, strings.TrimSpace(p.prompt.Value()))
		}
		if p.step == stepPrompt {
			return p.prompt.Focus()
		}
		return nil
	}
	switch p.step {
	case stepAgent:
		p.agent, _ = ui.MoveCursor(k, p.agent, len(p.agents))
	case stepRepo:
		p.repo, _ = ui.MoveCursor(k, p.repo, len(p.repos))
	case stepPrompt:
		var cmd tea.Cmd
		p.prompt, cmd = p.prompt.Update(msg)
		return cmd
	}
	return nil
}

// start asks the server for a session in this task and launches it.
func (s *Screen) start(agent *domain.Agent, repoID, prompt string) tea.Cmd {
	runsOn, host := s.panes.Runner()
	req := ports.InteractiveRequest{
		ProjectID:    s.project.ID,
		RepositoryID: repoID,
		TicketID:     s.ticket.ID,
		AgentID:      agent.ID,
		Prompt:       prompt,
		RunsOn:       runsOn,
		RunnerHost:   host,
		Size:         s.panes.PaneSize(s.ticket.ID),
	}
	label := agent.Name
	if agent.ID == "" {
		label = "claude"
	}
	interactive := s.ports.Interactive
	return tea.Batch(nav.Note("starting…"), s.launch(label, func(ctx context.Context) (*domain.Session, ports.AgentSpec, error) {
		return interactive.StartInteractive(ctx, req)
	}))
}

func (p *picker) view(_ *Screen, width, _ int) string {
	if p.loading {
		return ui.Note.Render("  loading agents and repositories…  (esc cancel)")
	}
	var b strings.Builder
	b.WriteString(ui.Header.Render("  New agent session") + "\n\n")

	b.WriteString(stepTitle("Agent", p.step == stepAgent))
	if p.step == stepAgent {
		for i, a := range p.agents {
			row := "    " + ui.Fit(a.Name, 24) + "  " + ui.Dim.Render(a.Description)
			b.WriteString(ui.Row(row, i == p.agent, width-2) + "\n")
		}
	} else {
		b.WriteString("    " + p.agents[p.agent].Name + "\n")
	}

	if len(p.repos) > 1 {
		b.WriteString("\n" + stepTitle("Repository", p.step == stepRepo))
		if p.step == stepRepo {
			for i, r := range p.repos {
				row := "    " + ui.Fit(r.Name, 24) + "  " + ui.Dim.Render(r.RootDir)
				b.WriteString(ui.Row(row, i == p.repo, width-2) + "\n")
			}
		} else if p.step > stepRepo {
			b.WriteString("    " + p.repos[p.repo].Name + "\n")
		}
	}

	if p.step == stepPrompt {
		b.WriteString("\n" + stepTitle("First message", true))
		b.WriteString("  " + p.prompt.View() + "\n")
	}
	b.WriteString("\n" + ui.Note.Render("  ↑↓ choose · enter next · esc back"))
	return b.String()
}

func stepTitle(s string, current bool) string {
	if current {
		return "  " + ui.Header.Render(s) + "\n"
	}
	return "  " + ui.Dim.Render(s) + "\n"
}

// --- sessions overlay ---------------------------------------------------------

// sessionsOverlay lists every recorded session of the task. Enter focuses an
// open one, resumes an ended interactive one, and explains why a headless one
// cannot be opened here.
type sessionsOverlay struct{ cursor int }

func (o *sessionsOverlay) update(s *Screen, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	k := key.String()
	if c, ok := ui.MoveCursor(k, o.cursor, len(s.recorded)); ok {
		o.cursor = c
		return nil
	}
	switch k {
	case "esc", "q", "s":
		s.overlay = nil
	case "n", "c":
		return s.openPicker()
	case "enter":
		if len(s.recorded) == 0 {
			return nil
		}
		sess := s.recorded[ui.Clamp(o.cursor, len(s.recorded))]
		if s.panes.Focus(sess.ID) {
			s.overlay = nil
			return nil
		}
		switch {
		case !sess.Interactive:
			return nav.Note("headless session: follow it in the coding_pool web UI")
		case !sess.Status.IsTerminal() && sess.RunsOn == domain.RunnerServer:
			s.overlay = nil
			return func() tea.Msg { return panes.AttachMsg{Session: sess} }
		case !sess.Status.IsTerminal():
			return nav.Note("that session is running in another terminal")
		default:
			s.overlay = nil
			return s.resume(sess)
		}
	}
	return nil
}

func (s *Screen) resume(sess *domain.Session) tea.Cmd {
	runsOn, host := s.panes.Runner()
	interactive := s.ports.Interactive
	req := ports.ResumeRequest{SessionID: sess.ID, RunsOn: runsOn, RunnerHost: host, Size: s.panes.PaneSize(sess.TicketID)}
	label := strings.TrimPrefix(sess.Branch, "agent/")
	return tea.Batch(nav.Note("resuming…"), s.launch(label, func(ctx context.Context) (*domain.Session, ports.AgentSpec, error) {
		return interactive.ResumeInteractive(ctx, req)
	}))
}

func (o *sessionsOverlay) view(s *Screen, width, height int) string {
	var b strings.Builder
	b.WriteString(ui.Header.Render("  Sessions of this task") + "\n\n")
	if len(s.recorded) == 0 {
		b.WriteString(ui.Note.Render("  none yet — n starts one") + "\n")
	}
	from, to := ui.Window(o.cursor, len(s.recorded), max(1, height-4))
	for i := from; i < to; i++ {
		sess := s.recorded[i]
		mark := "  "
		if s.panes.Open(sess.ID) {
			mark = ui.Live.Render("● ")
		}
		kind := "terminal"
		switch {
		case !sess.Interactive:
			kind = "headless"
		case sess.RunsOn == domain.RunnerServer:
			kind = "server"
		}
		row := "  " + mark + ui.Fit(string(sess.Status), 18) + ui.Fit(kind, 10) + ui.Fit(sess.Branch, max(10, width-60)) + " " + ui.Dim.Render(ui.Age(sess.CreatedAt))
		b.WriteString(ui.Row(row, i == o.cursor, width-2) + "\n")
	}
	b.WriteString("\n" + ui.Note.Render("  enter open or resume · n new · esc close"))
	return b.String()
}
