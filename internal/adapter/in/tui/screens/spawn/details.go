package spawn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// startTimeout bounds creating the worktree and launching the agent.
const startTimeout = 60 * time.Second

// buildForm creates the details form: title and goal for a new task, then
// the instruction and the suggested branch.
func (m *Model) buildForm() {
	th := m.ctx.Theme
	title := ""
	if t := m.ticket(); t != nil {
		title = t.Title
	}
	m.suggestion = SuggestBranch(title, randHex())
	var fields []components.Field
	if m.newTask {
		fields = append(fields,
			components.TextField("title", "Title", "").Required(),
			components.MultilineField("goal", "Goal (task description)", ""),
		)
	}
	fields = append(fields,
		components.TextField("instruction", "Instruction for the agent", "").Required().Placeholder("what this run should accomplish"),
		components.TextField("branch", "Branch", m.suggestion).Required(),
	)
	f := components.NewForm(th, "Spawn agent", fields...)
	m.form = &f
}

// syncBranch keeps the branch suggestion following the title until the user
// edits the branch by hand.
func (m *Model) syncBranch() {
	if m.form == nil || !m.newTask {
		return
	}
	v := m.form.Values()
	if v["branch"] != m.suggestion {
		return
	}
	next := SuggestBranch(v["title"], suffixOf(m.suggestion))
	if next != m.suggestion {
		m.suggestion = next
		m.form.SetValue("branch", next)
	}
}

// submit starts the session, creating the task first when the user chose
// "New task…". The outcome comes back as startedMsg.
func (m Model) submit(sub components.FormSubmitMsg) (Model, tea.Cmd) {
	if m.submitting {
		return m, nil
	}
	m.submitting = true
	be := m.be
	v := sub.Values
	req := orchestration.StartRequest{
		ProjectID: m.projectID, RepositoryID: m.repoID, AgentID: m.agentID, TicketID: m.ticketID,
		Task: strings.TrimSpace(v["instruction"]), BaseBranch: m.baseBranch, Branch: strings.TrimSpace(v["branch"]),
	}
	newTask, title, goal := m.newTask, strings.TrimSpace(v["title"]), v["goal"]
	return m, func() tea.Msg {
		if newTask {
			t, err := be.CreateTicket(req.ProjectID, title, goal, domain.TicketStatusTodo)
			if err != nil {
				return startedMsg{err: err}
			}
			req.TicketID = t.ID
		}
		ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		s, err := be.StartSession(ctx, req)
		return startedMsg{session: s, err: err}
	}
}

// SuggestBranch slugifies the title and appends the suffix; an empty title
// yields agent-<suffix>.
func SuggestBranch(title, suffix string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "agent"
	}
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return slug + "-" + suffix
}

// randHex is a variable so tests can pin the suffix.
var randHex = func() string {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "0000"
	}
	return hex.EncodeToString(buf[:])
}
