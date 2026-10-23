package agentisolation

import "operators-mcp/internal/ports"

// CredentialView is the Agent credential section (#credential): whether agents
// have a credential and its kind. It never holds the value.
type CredentialView struct {
	Status ports.AgentCredentialStatus
	// OAuth is the sign-in flow now; never holds the token.
	OAuth ports.AgentOAuthState
	// CanSignIn says the flow can run: it is wired and a claude is installed.
	CanSignIn bool
	// HomeExposed: the agent home is in a git work tree that does not ignore
	// it (see homeWarning).
	HomeExposed bool
}

// CredentialSeed is what the section's component takes over from: the body of
// GET /api/get_agent_credential, with the sign-in flow beside it so a page
// that was left mid-flow resumes it on the first paint.
type CredentialSeed struct {
	ports.AgentCredentialStatus
	OAuth     ports.AgentOAuthState `json:"oauth"`
	CanSignIn bool                  `json:"can_sign_in"`
}

// NewCredentialView reads the credential's status and the sign-in flow;
// without a store it shows as not set, without the flow or a claude the
// sign-in is not offered.
func NewCredentialView(c ports.AgentCredential, oauth ports.AgentOAuth, bin ports.ManagedClaudeBin) CredentialView {
	v := CredentialView{OAuth: ports.AgentOAuthState{Phase: ports.AgentOAuthIdle}}
	if c != nil {
		v.Status = c.Status()
	}
	if oauth != nil {
		v.OAuth = oauth.State()
		v.CanSignIn = bin != nil && bin.Installed()
	}
	return v
}

// Seed is the section's seed.
func (v CredentialView) Seed() CredentialSeed {
	return CredentialSeed{AgentCredentialStatus: v.Status, OAuth: v.OAuth, CanSignIn: v.CanSignIn}
}

// SignInLabel is the sign-in button: again when a credential is set.
func (v CredentialView) SignInLabel() string {
	if v.Status.Set {
		return "Sign in again"
	}
	return "Sign in with Claude"
}

// SignInTone is the sign-in button's variant: primary while nothing is set.
func (v CredentialView) SignInTone() string {
	if v.Status.Set {
		return ""
	}
	return "primary"
}

// SignInDisabled: no claude to run, or a flow already runs.
func (v CredentialView) SignInDisabled() bool { return !v.CanSignIn }

// Set: agents have a credential.
func (v CredentialView) Set() bool { return v.Status.Set }

// NotSet: agents cannot start for want of one.
func (v CredentialView) NotSet() bool { return !v.Status.Set }

// ChipText and ChipTone are the section's status chip.
func (v CredentialView) ChipText() string {
	if v.Status.Set {
		return "Set"
	}
	return "Not set"
}

func (v CredentialView) ChipTone() string {
	if v.Status.Set {
		return "signal"
	}
	return "danger"
}

// KindName is the kind in the person's words.
func (v CredentialView) KindName() string {
	switch v.Status.Kind {
	case ports.AgentCredentialOAuth:
		return "OAuth token"
	case ports.AgentCredentialAPIKey:
		return "Anthropic API key"
	}
	return ""
}

// SavedDate is the day it was saved.
func (v CredentialView) SavedDate() string {
	if v.Status.Updated.IsZero() {
		return ""
	}
	return v.Status.Updated.UTC().Format("2006-01-02")
}

// PasteLabel is the paste button: Replace when one is set.
func (v CredentialView) PasteLabel() string {
	if v.Status.Set {
		return "Replace with a pasted token or key"
	}
	return "Paste a token or key"
}

// PasteTone is the paste button's variant: primary while nothing is set.
func (v CredentialView) PasteTone() string {
	if v.Status.Set || v.CanSignIn {
		return ""
	}
	return "primary"
}
