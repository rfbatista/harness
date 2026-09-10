package session

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/rollup"
)

// questionBar answers an approval that carries questions: one radio or
// checkbox group per question plus a free-text "Other".
type questionBar struct{}

func (questionBar) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "j/k", Desc: "option"}, {Key: "space", Desc: "pick"}, {Key: "tab", Desc: "next question"}, {Key: "ctrl+s", Desc: "submit"}, {Key: "esc", Desc: "skip"}}
}

// questions returns the answer state for the pending request, starting a
// fresh one when the request changed.
func (m *Model) questions(req Item) *questionState {
	if m.question == nil || m.question.reqID != req.ReqID {
		q := newQuestionState(req)
		m.question = &q
	}
	return m.question
}

func (questionBar) key(m Model, k tea.KeyPressMsg) (Model, tea.Cmd) {
	req, ok := m.pendingRequest()
	if !ok {
		return m, nil
	}
	qs := m.questions(req)
	if qs.typingOther() {
		switch k.String() {
		case "esc", "enter":
			qs.other[qs.qi].Blur()
			qs.editing = false
			return m, nil
		}
		in, cmd := qs.other[qs.qi].Update(k)
		qs.other[qs.qi] = in
		return m, cmd
	}
	switch k.String() {
	case "esc":
		// Skipping the questions denies the request.
		m.deciding = true
		return m, m.action(actionDeny, func(ctx context.Context) error { return m.be.Resolve(ctx, m.id, req.ReqID, false, "") })
	case "tab", "right", "l":
		qs.nextQuestion(1)
	case "shift+tab", "left", "h":
		qs.nextQuestion(-1)
	case "j", "down":
		qs.moveOption(1)
	case "k", "up":
		qs.moveOption(-1)
	case "space", " ", "enter":
		qs.toggle()
		if qs.editing {
			return m, qs.other[qs.qi].Focus()
		}
	case "ctrl+s":
		answers, notes, ok := qs.answers()
		if !ok || m.deciding {
			return m, nil
		}
		m.deciding = true
		return m, m.action(actionAnswer, func(ctx context.Context) error { return m.be.Answer(ctx, m.id, req.ReqID, answers, notes) })
	case "a", "S", "D":
		next, cmd, _ := m.controlKey(k)
		return next, cmd
	}
	return m, nil
}

func (questionBar) view(m *Model, width int) string {
	th := m.ctx.Theme
	req, ok := m.pendingRequest()
	if !ok {
		return ""
	}
	qs := m.questions(req)
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Foreground(th.StatusColor(rollup.Review)).Bold(true).Render("The agent has a question"))
	for i, q := range qs.questions {
		head := q.Question
		if q.Header != "" {
			head = th.Meta().Render("["+q.Header+"] ") + head
		}
		if i == qs.qi {
			head = th.AccentText().Render("▸ ") + head
		} else {
			head = "  " + head
		}
		if q.MultiSelect {
			head += th.Meta().Render("  (pick any)")
		}
		lines = append(lines, head)
		for oi, o := range q.Options {
			row := "    " + checkbox(q.MultiSelect, qs.picked[i][oi]) + " " + o.Label
			if o.Description != "" {
				row += th.Meta().Render("  " + o.Description)
			}
			if i == qs.qi && oi == qs.oi {
				row = th.Selected().Render(row)
			}
			lines = append(lines, row)
		}
		other := "    " + checkbox(false, qs.picked[i][len(q.Options)]) + " Other: " + qs.other[i].View()
		if i == qs.qi && qs.oi == len(q.Options) {
			other = th.Selected().Render(other)
		}
		lines = append(lines, other)
	}
	_, _, ready := qs.answers()
	foot := "j/k option  ·  space pick  ·  tab next question  ·  esc skip"
	if ready {
		foot = th.AccentText().Render("ctrl+s submit") + th.Meta().Render("  ·  "+foot)
	} else {
		foot = th.Meta().Render("answer every question, then ctrl+s  ·  " + foot)
	}
	if m.deciding {
		foot = th.Meta().Render("Submitting…")
	}
	lines = append(lines, foot)
	if m.decideErr != "" {
		lines = append(lines, th.Error().Render(m.decideErr))
	}
	return lipgloss.NewStyle().Width(width - 4).Render(strings.Join(lines, "\n"))
}

// checkbox draws a radio for single choice, a box for multi-select.
func checkbox(multi, picked bool) string {
	switch {
	case multi && picked:
		return "[x]"
	case multi:
		return "[ ]"
	case picked:
		return "(•)"
	}
	return "( )"
}
