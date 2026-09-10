package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
)

// OpDoneMsg reports the outcome of one backend write. Tag names the operation
// so a screen can recognise it when it needs a follow-up (see Overlays.Done).
type OpDoneMsg struct {
	Tag   string
	Toast string
	Err   error
}

// Op wraps a write in a command that yields OpDoneMsg. fn runs off the UI
// goroutine, so it must only use values captured when the command was built.
func Op(tag, toast string, fn func() error) tea.Cmd {
	return func() tea.Msg { return OpDoneMsg{Tag: tag, Toast: toast, Err: fn()} }
}

// SubmitFunc turns the values of an accepted form into the command to run.
type SubmitFunc func(FormSubmitMsg) tea.Cmd

// ConfirmFunc is the command to run once a confirm is accepted.
type ConfirmFunc func() tea.Cmd

// Overlays is the form-or-confirm slot every list and detail owns. At most
// one of the two is open.
//
// Opening an overlay pairs it with the command to run when the user accepts
// (the Command pattern): the screen decides what "submit" means at the moment
// it opens the form, capturing the ids it needs, so nothing has to be looked
// up again when the answer arrives.
type Overlays struct {
	Form      *Form
	onSubmit  SubmitFunc
	Confirm   *Confirm
	onConfirm ConfirmFunc
}

// Active reports whether a form or confirm is open.
func (o *Overlays) Active() bool { return o.Form != nil || o.Confirm != nil }

// OpenForm shows a form; onSubmit runs when the user accepts it.
func (o *Overlays) OpenForm(f Form, onSubmit SubmitFunc) {
	o.Close()
	o.Form, o.onSubmit = &f, onSubmit
}

// OpenConfirm shows a yes/no prompt; onConfirm runs when the user accepts.
func (o *Overlays) OpenConfirm(th theme.Theme, title, body string, onConfirm ConfirmFunc) {
	o.Close()
	c := NewConfirm(th, title, body, "")
	o.Confirm, o.onConfirm = &c, onConfirm
}

// Close drops whatever is open.
func (o *Overlays) Close() {
	o.Form, o.onSubmit, o.Confirm, o.onConfirm = nil, nil, nil, nil
}

// Update feeds a message to whichever overlay is open: keys and editor
// results go to the widget, an accept runs the pending command, a cancel
// closes. handled=false means nothing is open or the message is not an
// overlay message, so the screen should handle it itself.
func (o *Overlays) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !o.Active() {
		return nil, false
	}
	switch msg := msg.(type) {
	case FormSubmitMsg:
		if o.Form == nil || o.onSubmit == nil {
			return nil, true
		}
		return o.onSubmit(msg), true
	case ConfirmedMsg:
		if o.Confirm == nil || o.onConfirm == nil {
			return nil, true
		}
		return o.onConfirm(), true
	case FormCancelMsg, ConfirmCancelledMsg:
		o.Close()
		return nil, true
	case tea.KeyPressMsg, EditorDoneMsg:
		if o.Confirm != nil {
			c, cmd := o.Confirm.Update(msg)
			o.Confirm = &c
			return cmd, true
		}
		f, cmd := o.Form.Update(msg)
		o.Form = &f
		return cmd, true
	}
	return nil, false
}

// Done applies a write outcome: success closes and refreshes; an inline error
// keeps the form open with the message; anything else closes with a toast.
// A conflict (core.ErrConflict) leaves the overlay untouched and returns
// conflict=true so the owner can offer a forced retry.
func (o *Overlays) Done(msg OpDoneMsg) (cmd tea.Cmd, conflict bool) {
	if msg.Err == nil {
		o.Close()
		cmds := []tea.Cmd{func() tea.Msg { return core.RefreshMsg{} }}
		if msg.Toast != "" {
			toast := msg.Toast
			cmds = append(cmds, func() tea.Msg { return core.ToastMsg{Text: toast} })
		}
		return tea.Batch(cmds...), false
	}
	text := core.Message(msg.Err)
	switch core.Classify(msg.Err) {
	case core.ErrInline:
		if o.Form != nil {
			o.Form.SetError(text)
			return nil, false
		}
	case core.ErrConflict:
		return nil, true
	}
	o.Close()
	return ErrorToast(text), false
}

// ErrorToast is the command that shows text as an error toast.
func ErrorToast(text string) tea.Cmd {
	return func() tea.Msg { return core.ToastMsg{Text: text, IsError: true} }
}

// View renders the open overlay at the given width.
func (o *Overlays) View(width int) string {
	switch {
	case o.Confirm != nil:
		return o.Confirm.View(width)
	case o.Form != nil:
		return o.Form.View(width)
	}
	return ""
}

// OverlayOn draws the overlay centred over the body. Terminal UIs have no
// real layering, so the body lines above the overlay stay as context and the
// overlay replaces the rest.
func OverlayOn(body, overlay string, width, height int) string {
	placed := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, overlay)
	bodyLines := strings.Split(body, "\n")
	overlayLines := strings.Split(placed, "\n")
	top := max((height-lipgloss.Height(overlay))/2, 0)
	var out []string
	for i := 0; i < height; i++ {
		switch {
		case i < top && i < len(bodyLines):
			out = append(out, bodyLines[i])
		case i < len(overlayLines):
			out = append(out, overlayLines[i])
		default:
			out = append(out, "")
		}
	}
	return strings.Join(out, "\n")
}
