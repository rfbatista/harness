package agentisolation

// ClaudeView is the Managed claude section (#claude): an empty shell until
// the managed claude ticket fills it.
type ClaudeView struct{}

// NewClaudeView reads the section's state.
func NewClaudeView() ClaudeView { return ClaudeView{} }
