package components

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/theme"
)

// FormSubmitMsg carries the collected values: Values for text, multiline and
// select fields, Multi for multi-select fields.
type FormSubmitMsg struct {
	Values map[string]string
	Multi  map[string][]string
}

// FormCancelMsg is emitted on Esc.
type FormCancelMsg struct{}

type fieldKind int

const (
	kindText fieldKind = iota
	kindMultiline
	kindSelect
	kindMulti
)

// Option is one choice of a select or multi-select field.
type Option struct {
	Label string
	Value string
}

// Field is one form entry. Build them with the constructors below.
type Field struct {
	Key      string
	Label    string
	kind     fieldKind
	required bool
	err      string

	input   textinput.Model // text
	text    string          // multiline
	options []Option        // select, multi
	sel     int             // select
	chosen  map[string]bool // multi
	optCur  int             // multi
}

// TextField is a single-line input.
func TextField(key, label, value string) Field {
	in := textinput.New()
	in.Prompt = ""
	in.SetValue(value)
	return Field{Key: key, Label: label, kind: kindText, input: in}
}

// MultilineField is edited in $EDITOR on Enter and shown as a summary.
func MultilineField(key, label, value string) Field {
	return Field{Key: key, Label: label, kind: kindMultiline, text: value}
}

// SelectField cycles through options with ←/→ or space.
func SelectField(key, label string, options []string, value string) Field {
	f := Field{Key: key, Label: label, kind: kindSelect}
	for i, o := range options {
		f.options = append(f.options, Option{Label: o, Value: o})
		if o == value {
			f.sel = i
		}
	}
	return f
}

// SelectOptions is a select whose labels differ from the stored values.
func SelectOptions(key, label string, options []Option, value string) Field {
	f := Field{Key: key, Label: label, kind: kindSelect, options: options}
	for i, o := range options {
		if o.Value == value {
			f.sel = i
		}
	}
	return f
}

// MultiSelectField is a checklist: ↑/↓ move, space toggles.
func MultiSelectField(key, label string, options []Option, chosen []string) Field {
	f := Field{Key: key, Label: label, kind: kindMulti, options: options, chosen: map[string]bool{}}
	for _, c := range chosen {
		f.chosen[c] = true
	}
	return f
}

// Required marks the field as mandatory; submit fails with an inline error.
func (f Field) Required() Field {
	f.required = true
	return f
}

// Placeholder sets the hint shown while a text field is empty.
func (f Field) Placeholder(p string) Field {
	f.input.Placeholder = p
	return f
}

func (f Field) value() string {
	switch f.kind {
	case kindText:
		return f.input.Value()
	case kindMultiline:
		return f.text
	case kindSelect:
		if len(f.options) == 0 {
			return ""
		}
		return f.options[f.sel].Value
	}
	return ""
}

func (f Field) multiValue() []string {
	var out []string
	for _, o := range f.options {
		if f.chosen[o.Value] {
			out = append(out, o.Value)
		}
	}
	return out
}

// Form is a vertical field set with one focused field.
type Form struct {
	th     theme.Theme
	Title  string
	fields []Field
	focus  int
	err    string
}

// NewForm builds a form with the first field focused.
func NewForm(th theme.Theme, title string, fields ...Field) Form {
	f := Form{th: th, Title: title, fields: fields}
	f.setFocus(0)
	return f
}

func (f *Form) setFocus(i int) {
	if len(f.fields) == 0 {
		return
	}
	if i < 0 {
		i = len(f.fields) - 1
	}
	if i >= len(f.fields) {
		i = 0
	}
	for j := range f.fields {
		if f.fields[j].kind == kindText {
			f.fields[j].input.Blur()
		}
	}
	f.focus = i
	if f.fields[i].kind == kindText {
		f.fields[i].input.Focus()
	}
}

// Values returns text, multiline and select values by key.
func (f Form) Values() map[string]string {
	out := map[string]string{}
	for _, fl := range f.fields {
		if fl.kind != kindMulti {
			out[fl.Key] = fl.value()
		}
	}
	return out
}

// MultiValues returns multi-select values by key.
func (f Form) MultiValues() map[string][]string {
	out := map[string][]string{}
	for _, fl := range f.fields {
		if fl.kind == kindMulti {
			out[fl.Key] = fl.multiValue()
		}
	}
	return out
}

// SetValue replaces a text or multiline field value programmatically.
func (f *Form) SetValue(key, value string) {
	for i := range f.fields {
		if f.fields[i].Key != key {
			continue
		}
		switch f.fields[i].kind {
		case kindText:
			f.fields[i].input.SetValue(value)
		case kindMultiline:
			f.fields[i].text = value
		}
	}
}

// SetFieldError shows an error under one field (server-side validation).
func (f *Form) SetFieldError(key, msg string) {
	for i := range f.fields {
		if f.fields[i].Key == key {
			f.fields[i].err = msg
		}
	}
}

// SetError shows a form-level error above the footer.
func (f *Form) SetError(msg string) { f.err = msg }

// Update routes keys to the focused field and handles navigation and submit.
func (f Form) Update(msg tea.Msg) (Form, tea.Cmd) {
	switch msg := msg.(type) {
	case EditorDoneMsg:
		if msg.Err != nil {
			f.err = msg.Err.Error()
			return f, nil
		}
		for i := range f.fields {
			if f.fields[i].Key == msg.Tag && f.fields[i].kind == kindMultiline {
				f.fields[i].text = msg.Content
				f.fields[i].err = ""
			}
		}
		return f, nil
	case tea.KeyPressMsg:
		return f.key(msg)
	}
	return f, nil
}

func (f Form) key(k tea.KeyPressMsg) (Form, tea.Cmd) {
	if len(f.fields) == 0 {
		return f, nil
	}
	cur := &f.fields[f.focus]
	switch k.String() {
	case "esc":
		return f, func() tea.Msg { return FormCancelMsg{} }
	case "ctrl+s":
		return f.submit()
	case "tab":
		f.setFocus(f.focus + 1)
		return f, nil
	case "shift+tab":
		f.setFocus(f.focus - 1)
		return f, nil
	case "enter":
		if cur.kind == kindMultiline {
			return f, EditInEditor(cur.text, ".md", cur.Key)
		}
		return f.submit()
	}
	switch cur.kind {
	case kindText:
		var cmd tea.Cmd
		cur.input, cmd = cur.input.Update(k)
		cur.err = ""
		f.err = ""
		return f, cmd
	case kindSelect:
		switch k.String() {
		case "right", "space", " ", "l":
			cur.sel = (cur.sel + 1) % len(cur.options)
		case "left", "h":
			cur.sel = (cur.sel + len(cur.options) - 1) % len(cur.options)
		case "down":
			f.setFocus(f.focus + 1)
		case "up":
			f.setFocus(f.focus - 1)
		}
	case kindMulti:
		switch k.String() {
		case "down", "j":
			if cur.optCur < len(cur.options)-1 {
				cur.optCur++
			}
		case "up", "k":
			if cur.optCur > 0 {
				cur.optCur--
			}
		case "space", " ", "x":
			if len(cur.options) > 0 {
				v := cur.options[cur.optCur].Value
				cur.chosen[v] = !cur.chosen[v]
			}
		}
	case kindMultiline:
		switch k.String() {
		case "down":
			f.setFocus(f.focus + 1)
		case "up":
			f.setFocus(f.focus - 1)
		}
	}
	return f, nil
}

// submit validates required fields, then emits FormSubmitMsg.
func (f Form) submit() (Form, tea.Cmd) {
	ok := true
	for i := range f.fields {
		fl := &f.fields[i]
		if fl.required && strings.TrimSpace(fl.value()) == "" {
			fl.err = "required"
			ok = false
		}
	}
	if !ok {
		return f, nil
	}
	msg := FormSubmitMsg{Values: f.Values(), Multi: f.MultiValues()}
	return f, func() tea.Msg { return msg }
}

// View renders the form inside a focused panel.
func (f Form) View(width int) string {
	th := f.th
	inner := width - 4
	var lines []string
	lines = append(lines, th.Title().Render(f.Title), "")
	for i, fl := range f.fields {
		label := th.Subtitle().Render(fl.Label)
		if i == f.focus {
			label = th.AccentText().Render("▸ " + fl.Label)
		} else {
			label = "  " + label
		}
		if fl.required {
			label += th.Meta().Render(" *")
		}
		lines = append(lines, label)
		lines = append(lines, "  "+f.control(fl, i == f.focus, inner-2))
		if fl.err != "" {
			lines = append(lines, "  "+th.Error().Render(fl.err))
		}
		lines = append(lines, "")
	}
	if f.err != "" {
		lines = append(lines, th.Error().Render(f.err), "")
	}
	lines = append(lines, th.Meta().Render("tab next  ·  enter save (ctrl+s from a text block)  ·  esc cancel"))
	return th.PanelFocused().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (f Form) control(fl Field, focused bool, width int) string {
	th := f.th
	switch fl.kind {
	case kindText:
		return fl.input.View()
	case kindMultiline:
		n := 0
		if fl.text != "" {
			n = strings.Count(fl.text, "\n") + 1
		}
		summary := fmt.Sprintf("%d lines", n)
		if n == 0 {
			summary = "empty"
		}
		hint := ""
		if focused {
			hint = th.Meta().Render("  enter to edit in $EDITOR")
		}
		return th.Subtitle().Render(summary) + hint
	case kindSelect:
		var parts []string
		for i, o := range fl.options {
			if i == fl.sel {
				parts = append(parts, th.AccentText().Render("["+o.Label+"]"))
			} else {
				parts = append(parts, th.Meta().Render(" "+o.Label+" "))
			}
		}
		return strings.Join(parts, " ")
	case kindMulti:
		var rows []string
		for i, o := range fl.options {
			box := "[ ]"
			if fl.chosen[o.Value] {
				box = "[x]"
			}
			row := box + " " + o.Label
			if focused && i == fl.optCur {
				row = th.Selected().Render(row)
			}
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			return th.Meta().Render("nothing to choose from")
		}
		return strings.Join(rows, "\n  ")
	}
	return ""
}
