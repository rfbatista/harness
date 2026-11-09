// Package mcpinterview is the harness side of llmkit's interview package: the
// Sink llmkit's handler hands each valid interview to. It converts the
// interview into the stored one and asks through the application service. It
// knows nothing of how a provider asks; that stays behind llmkit.
package mcpinterview

import (
	"context"

	"github.com/rfbatista/llmkit/interview"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Sink implements interview.Sink over the interview asker port.
type Sink struct{ Interviews ports.InterviewAsker }

// Asked stores iv as open on its session. An error (a session that is not
// live is INTERVIEW_UNAVAILABLE) fails the agent's call, and nothing is stored.
func (s Sink) Asked(ctx context.Context, iv interview.Interview) error {
	_, err := s.Interviews.Ask(ctx, convert(iv))
	return err
}

// convert is llmkit's interview as the domain's, with no state, time or agent
// name yet. Options are never nil: a text question has none.
func convert(iv interview.Interview) domain.Interview {
	out := domain.Interview{ID: iv.ID, SessionID: iv.SessionID, Title: iv.Title, Questions: make([]domain.InterviewQuestion, 0, len(iv.Questions))}
	for _, q := range iv.Questions {
		dq := domain.InterviewQuestion{Title: q.Title, Body: q.Body, Kind: domain.QuestionKind(q.Kind), Options: make([]domain.InterviewOption, 0, len(q.Options))}
		for _, o := range q.Options {
			dq.Options = append(dq.Options, domain.InterviewOption{Label: o.Label, Description: o.Description})
		}
		if r := q.Recommended; r != nil {
			dq.Recommended = &domain.Recommended{}
			if q.Kind == interview.Text {
				text := r.Text
				dq.Recommended.Text = &text
			} else {
				option := r.Option
				dq.Recommended.Option = &option
			}
		}
		out.Questions = append(out.Questions, dq)
	}
	return out
}
