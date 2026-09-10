package orchestration

import (
	"encoding/json"

	"github.com/rfbatista/llmkit/claude"
)

// AskUserQuestionTool is the CLI's built-in multiple-choice tool. It reaches us
// through the permission broker like any other tool needing a decision, but a
// bare allow/deny is the wrong answer to a question: the user's choices travel
// back as the tool's updated input.
//
// Defined by the driver, whose broker has to know the same name to keep
// auto-run from answering a question, and re-exported here so there is one
// spelling of it.
const AskUserQuestionTool = claude.AskUserQuestionTool

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Preview is optional mockup/snippet content the CLI renders beside the
	// options. Markdown in a monospace box, per the tool's own contract.
	Preview string `json:"preview,omitempty"`
}

type Question struct {
	// Header is a very short chip label ("Auth method", "Approach").
	Header      string           `json:"header,omitempty"`
	Question    string           `json:"question"`
	MultiSelect bool             `json:"multi_select,omitempty"`
	Options     []QuestionOption `json:"options"`
}

// ParseQuestions extracts the questions from an AskUserQuestion tool input.
//
// It returns nil for every other tool and for any payload that does not yield
// at least one answerable question. That nil is load-bearing: the UI falls back
// to the plain allow/deny approval surface, which is a worse but honest
// rendering, rather than showing an option panel with nothing to pick.
func ParseQuestions(toolName string, input json.RawMessage) []Question {
	if toolName != AskUserQuestionTool || len(input) == 0 {
		return nil
	}

	// The CLI's own field names — camelCase — which is why this shape is
	// decoded here and not reused from the Question type above, whose JSON is
	// our own snake_case wire format.
	var payload struct {
		Questions []struct {
			Header      string `json:"header"`
			Question    string `json:"question"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
				Preview     string `json:"preview"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil
	}

	out := make([]Question, 0, len(payload.Questions))
	for _, q := range payload.Questions {
		// A question with no text cannot be keyed — answers are matched back to
		// questions by their exact text — and one with no option has no
		// decision in it.
		if q.Question == "" {
			continue
		}
		opts := make([]QuestionOption, 0, len(q.Options))
		for _, o := range q.Options {
			if o.Label == "" {
				continue
			}
			opts = append(opts, QuestionOption{
				Label:       o.Label,
				Description: o.Description,
				Preview:     o.Preview,
			})
		}
		if len(opts) == 0 {
			continue
		}
		out = append(out, Question{
			Header:      q.Header,
			Question:    q.Question,
			MultiSelect: q.MultiSelect,
			Options:     opts,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
