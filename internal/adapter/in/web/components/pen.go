package components

import "encoding/json"

// Pen configures one magic pen (web/src/modules/writinghelper, the writingPen
// component): the field kind, what the panel names, what it reads besides the
// field, and whether the helper can run.
type Pen struct {
	// ID is unique on the page: the panel's id and the question inputs' ids start with it.
	ID string
	// Kind is the field kind the step request names: task_title, task_description or comment.
	Kind string
	// Noun is what the pen's label says ("description"), Label what the panel names ("Description").
	Noun, Label string
	// ProjectID and ProjectName: the step runs in the project, and the panel names it.
	ProjectID, ProjectName string
	// Siblings are the other fields the helper reads.
	Siblings []PenSibling
	// Available is whether the helper can run; when it cannot, Reason and
	// FixURL say why and where to fix it (the page's shell.Frame.Helper).
	Available      bool
	Reason, FixURL string
	// Above puts the panel over the field: for a field at the bottom of its pane.
	Above bool
}

// PenSibling is another field the helper reads: its text is taken from the
// element Selector names when the panel opens, or is Text where the field is
// not on the page.
type PenSibling struct {
	Label    string `json:"label"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
}

// SiblingsJSON is the siblings as the component reads them.
func (p Pen) SiblingsJSON() string {
	b, err := json.Marshal(p.Siblings)
	if err != nil || len(p.Siblings) == 0 {
		return "[]"
	}
	return string(b)
}

// FieldKind is the .pen-field exception: a one-line input or a textarea.
func (p Pen) FieldKind() string {
	if p.Kind == "task_title" {
		return "line"
	}
	return "text"
}

// PenLabel is the pen's accessible name.
func (p Pen) PenLabel() string {
	if !p.Available {
		return "Writing helper unavailable"
	}
	return "Write the " + p.Noun + " with the helper"
}

// PenTitle is the pen's tooltip.
func (p Pen) PenTitle() string {
	if !p.Available {
		if p.Reason != "" {
			return "Writing helper unavailable: " + p.Reason
		}
		return "Writing helper unavailable"
	}
	return p.PenLabel() + " ⌘J"
}
