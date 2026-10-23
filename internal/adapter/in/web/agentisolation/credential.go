package agentisolation

// CredentialView is the Agent credential section (#credential): an empty
// shell until the credential ticket fills it.
type CredentialView struct{}

// NewCredentialView reads the section's state.
func NewCredentialView() CredentialView { return CredentialView{} }
