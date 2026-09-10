package session

import (
	"strings"

	"charm.land/bubbles/v2/textinput"

	"operators-mcp/internal/application/orchestration"
)

// questionState collects the answers to one approval's questions. The
// option index equal to len(Options) is the free-text "Other" entry.
type questionState struct {
	reqID     string
	questions []orchestration.Question
	picked    []map[int]bool // per question, option index → picked
	other     []textinput.Model
	qi, oi    int // current question and option
	editing   bool
}

func newQuestionState(req Item) questionState {
	qs := questionState{reqID: req.ReqID, questions: req.Questions}
	for range req.Questions {
		qs.picked = append(qs.picked, map[int]bool{})
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = "type your own answer"
		qs.other = append(qs.other, in)
	}
	return qs
}

func (qs *questionState) typingOther() bool { return qs.editing }

// nextQuestion moves by delta with wrap-around and resets the option cursor.
func (qs *questionState) nextQuestion(delta int) {
	n := len(qs.questions)
	qs.qi = (qs.qi + delta + n) % n
	qs.oi = 0
}

// moveOption moves the option cursor, clamped to include "Other".
func (qs *questionState) moveOption(delta int) {
	last := len(qs.questions[qs.qi].Options)
	next := qs.oi + delta
	if next >= 0 && next <= last {
		qs.oi = next
	}
}

// toggle picks the current option: single-choice questions replace the pick,
// multi-select ones flip it, and "Other" also starts typing.
func (qs *questionState) toggle() {
	q := qs.questions[qs.qi]
	p := qs.picked[qs.qi]
	if qs.oi == len(q.Options) { // Other
		if !q.MultiSelect {
			clear(p)
		}
		p[qs.oi] = true
		qs.editing = true
		return
	}
	if q.MultiSelect {
		p[qs.oi] = !p[qs.oi]
		return
	}
	clear(p)
	p[qs.oi] = true
}

// answers returns the picks keyed by question text; ok is false until every
// question has at least one pick (Other counts only with text).
func (qs *questionState) answers() (answers, notes map[string]string, ok bool) {
	answers, notes = map[string]string{}, map[string]string{}
	for i, q := range qs.questions {
		var labels []string
		for oi := range q.Options {
			if qs.picked[i][oi] {
				labels = append(labels, q.Options[oi].Label)
			}
		}
		if qs.picked[i][len(q.Options)] {
			text := strings.TrimSpace(qs.other[i].Value())
			if text != "" {
				labels = append(labels, text)
				notes[q.Question] = text
			}
		}
		if len(labels) == 0 {
			return nil, nil, false
		}
		answers[q.Question] = strings.Join(labels, ", ")
	}
	return answers, notes, true
}
