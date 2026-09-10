package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/theme"
)

func typeForm(f Form, s string) Form {
	for _, r := range s {
		f, _ = f.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return f
}

func keyForm(f Form, k tea.KeyPressMsg) (Form, tea.Cmd) { return f.Update(k) }

func sample() Form {
	return NewForm(theme.Dark(), "New agent",
		TextField("name", "Name", "").Required(),
		TextField("description", "Description", "reviews code"),
		SelectField("transport", "Transport", []string{"stdio", "sse", "streamable-http"}, "stdio"),
		MultiSelectField("skills", "Skills", []Option{{Label: "tdd", Value: "k1"}, {Label: "docs", Value: "k2"}}, []string{"k2"}),
	)
}

func TestFormTypesIntoFocusedFieldAndTabMoves(t *testing.T) {
	f := sample()
	f = typeForm(f, "reviewer")
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyTab})
	f = typeForm(f, "!")
	v := f.Values()
	if v["name"] != "reviewer" || v["description"] != "reviews code!" {
		t.Fatalf("values: %+v", v)
	}
}

func TestFormRequiredBlocksSubmitWithInlineError(t *testing.T) {
	f := sample()
	f, cmd := keyForm(f, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("submit with an empty required field must not emit")
	}
	if !contains(f.View(60), "required") {
		t.Fatalf("inline error expected:\n%s", f.View(60))
	}
}

func TestFormEnterSubmitsValuesAndMultiValues(t *testing.T) {
	f := typeForm(sample(), "reviewer")
	f, cmd := keyForm(f, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should submit")
	}
	msg, ok := cmd().(FormSubmitMsg)
	if !ok || msg.Values["name"] != "reviewer" || msg.Values["transport"] != "stdio" {
		t.Fatalf("submit msg: %#v", cmd())
	}
	if len(msg.Multi["skills"]) != 1 || msg.Multi["skills"][0] != "k2" {
		t.Fatalf("multi: %+v", msg.Multi)
	}
	_ = f
}

func TestFormSelectCyclesWithSpaceAndArrows(t *testing.T) {
	f := sample()
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyTab})
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyTab}) // transport
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyRight})
	if f.Values()["transport"] != "sse" {
		t.Fatalf("right should advance: %q", f.Values()["transport"])
	}
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if f.Values()["transport"] != "streamable-http" {
		t.Fatalf("space should advance: %q", f.Values()["transport"])
	}
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyLeft})
	if f.Values()["transport"] != "sse" {
		t.Fatalf("left should go back: %q", f.Values()["transport"])
	}
}

func TestFormMultiSelectTogglesWithSpace(t *testing.T) {
	f := sample()
	for i := 0; i < 3; i++ {
		f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyTab})
	}
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // toggle tdd on
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyDown})
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // toggle docs off
	got := f.MultiValues()["skills"]
	if len(got) != 1 || got[0] != "k1" {
		t.Fatalf("multi toggle: %+v", got)
	}
}

func TestFormEscCancels(t *testing.T) {
	f := sample()
	_, cmd := keyForm(f, tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc should emit")
	}
	if _, ok := cmd().(FormCancelMsg); !ok {
		t.Fatalf("want FormCancelMsg got %#v", cmd())
	}
}

func TestFormServerErrorsShowOnFieldOrForm(t *testing.T) {
	f := typeForm(sample(), "x")
	f.SetFieldError("name", "already taken")
	f.SetError("backend unavailable")
	v := f.View(60)
	if !contains(v, "already taken") || !contains(v, "backend unavailable") {
		t.Fatalf("errors missing:\n%s", v)
	}
	f = typeForm(f, "y")
	if contains(f.View(60), "already taken") {
		t.Fatal("typing should clear the field error")
	}
}

func TestFormEnterOnMultilineFieldEditsExternally(t *testing.T) {
	f := NewForm(theme.Dark(), "Doc", TextField("title", "Title", "t"), MultilineField("body", "Body", "hello"))
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyTab})
	_, cmd := keyForm(f, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a multiline field should open the editor")
	}
}

func TestFormApplyEditorResult(t *testing.T) {
	f := NewForm(theme.Dark(), "Doc", MultilineField("body", "Body", "hello"))
	f, _ = f.Update(EditorDoneMsg{Tag: "body", Content: "edited\ntext"})
	if f.Values()["body"] != "edited\ntext" {
		t.Fatalf("editor result not applied: %q", f.Values()["body"])
	}
	if !contains(f.View(60), "2 lines") {
		t.Fatalf("multiline summary expected:\n%s", f.View(60))
	}
}

func TestFormCtrlSSubmitsFromAMultilineField(t *testing.T) {
	f := NewForm(theme.Dark(), "Doc", TextField("title", "Title", "t"), MultilineField("body", "Body", "hello"))
	f, _ = keyForm(f, tea.KeyPressMsg{Code: tea.KeyTab})
	_, cmd := keyForm(f, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+s should submit")
	}
	if _, ok := cmd().(FormSubmitMsg); !ok {
		t.Fatalf("want FormSubmitMsg got %#v", cmd())
	}
}
