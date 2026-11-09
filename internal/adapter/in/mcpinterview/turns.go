package mcpinterview

import (
	"github.com/rfbatista/llmkit/interview"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Turns is llmkit's answer validation and turn rendering over the domain's
// interview. The text is llmkit's own; the harness never edits it.
type Turns struct{}

var _ ports.InterviewTurns = Turns{}

func (Turns) ValidateAnswers(iv domain.Interview, answers []domain.InterviewAnswer) error {
	return interview.ValidateAnswers(toLLMKit(iv), llmkitAnswers(answers))
}

func (Turns) RenderAnswers(iv domain.Interview, answers []domain.InterviewAnswer) string {
	return interview.RenderAnswers(toLLMKit(iv), llmkitAnswers(answers))
}

func (Turns) RenderDismissed(iv domain.Interview, note string) string {
	return interview.RenderDismissed(toLLMKit(iv), note)
}

func toLLMKit(iv domain.Interview) interview.Interview {
	out := interview.Interview{ID: iv.ID, SessionID: iv.SessionID, Title: iv.Title, Questions: make([]interview.Question, 0, len(iv.Questions))}
	for _, q := range iv.Questions {
		lq := interview.Question{Title: q.Title, Body: q.Body, Kind: interview.Kind(q.Kind), Options: make([]interview.Option, 0, len(q.Options))}
		for _, o := range q.Options {
			lq.Options = append(lq.Options, interview.Option{Label: o.Label, Description: o.Description})
		}
		out.Questions = append(out.Questions, lq)
	}
	return out
}

func llmkitAnswers(in []domain.InterviewAnswer) interview.Answers {
	out := make(interview.Answers, len(in))
	for i, a := range in {
		out[i] = interview.Answer{Chosen: a.Chosen, Text: a.Text, Note: a.Note, Skipped: a.Skipped}
	}
	return out
}
