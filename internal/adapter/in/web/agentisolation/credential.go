package agentisolation

import (
	"context"
	"time"

	"operators-mcp/internal/ports"
)

// StatusTimeout bounds the `claude auth status` the page waits for when it
// loads: a slower one shows as "could not check" and the page renders anyway.
const StatusTimeout = 5 * time.Second

// AuthView is what `claude auth status` says about the agent home: the
// account fields it prints, never a token or the login file's content.
type AuthView struct {
	SignedIn     bool   `json:"signed_in"`
	Account      string `json:"account"`
	Subscription string `json:"subscription"`
	AuthMethod   string `json:"auth_method"`
	// CheckedOK is false when the status could not be read in time.
	CheckedOK bool `json:"checked_ok"`
}

// CredentialView is the Agent credential section (#credential): whether agents
// have a credential and its kind. It never holds the value.
type CredentialView struct {
	Status ports.AgentCredentialStatus
	// OAuth is the sign-in flow now; never holds the token.
	OAuth ports.AgentOAuthState
	// Auth is the login the agent home keeps, from `claude auth status`
	// read when the page loads (never from the login file).
	Auth AuthView
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
	Auth      AuthView              `json:"auth"`
}

// NewCredentialView reads the credential's status, the sign-in flow and, with
// a claude to ask, who the agent home is signed in as (bounded by
// StatusTimeout); without a store it shows as not set, without the flow or a
// claude the sign-in is not offered.
func NewCredentialView(ctx context.Context, c ports.AgentCredential, oauth ports.AgentOAuth, auth ports.AgentAuth, bin ports.ManagedClaudeBin) CredentialView {
	v := CredentialView{OAuth: ports.AgentOAuthState{Phase: ports.AgentOAuthIdle}, Auth: AuthView{CheckedOK: true}}
	if c != nil {
		v.Status = c.Status()
	}
	if oauth != nil {
		v.OAuth = oauth.State()
		v.CanSignIn = bin != nil && bin.Installed()
	}
	if auth != nil && bin != nil && bin.Installed() {
		ctx, cancel := context.WithTimeout(ctx, StatusTimeout)
		defer cancel()
		v.Auth = NewAuthView(auth.Status(ctx))
	}
	return v
}

// NewAuthView is the answer of a status read; a failure is not checked.
func NewAuthView(st ports.AgentAuthStatus, err error) AuthView {
	if err != nil {
		return AuthView{}
	}
	return AuthView{SignedIn: st.LoggedIn, Account: st.Email, Subscription: st.SubscriptionType, AuthMethod: st.AuthMethod, CheckedOK: true}
}

// SignedIn: `claude auth status` reports an account.
func (v CredentialView) SignedIn() bool { return v.Auth.SignedIn }

// SignedOut: the status was read and there is no account (and a claude
// that could have said so).
func (v CredentialView) SignedOut() bool { return v.Auth.CheckedOK && !v.Auth.SignedIn && v.CanSignIn }

// Unchecked: the status could not be read.
func (v CredentialView) Unchecked() bool { return !v.Auth.CheckedOK }

// AccountName is the email claude printed, or a plain word without one.
func (v CredentialView) AccountName() string {
	if v.Auth.Account != "" {
		return v.Auth.Account
	}
	return "Claude account"
}

// NoCredential: agents have neither a pasted credential nor a login.
func (v CredentialView) NoCredential() bool { return !v.Status.Set && !v.Auth.SignedIn }

// Seed is the section's seed.
func (v CredentialView) Seed() CredentialSeed {
	return CredentialSeed{AgentCredentialStatus: v.Status, OAuth: v.OAuth, CanSignIn: v.CanSignIn, Auth: v.Auth}
}

// SignInLabel is the sign-in button.
func (v CredentialView) SignInLabel() string { return "Sign in with Claude" }

// SignInTone is the sign-in button's variant: primary while nothing is set.
func (v CredentialView) SignInTone() string {
	if v.Status.Set {
		return ""
	}
	return "primary"
}

// SecondaryExpanded is the toggle's aria-expanded.
func (v CredentialView) SecondaryExpanded() string {
	if v.SecondaryOpen() {
		return "true"
	}
	return "false"
}

// SecondaryOpen: the pasted credential shows unless the person is signed in.
func (v CredentialView) SecondaryOpen() bool { return !v.Auth.SignedIn }

// SignInDisabled: no claude to run, or a flow already runs.
func (v CredentialView) SignInDisabled() bool { return !v.CanSignIn }

// Set: agents have a credential.
func (v CredentialView) Set() bool { return v.Status.Set }

// NotSet: agents cannot start for want of one.
func (v CredentialView) NotSet() bool { return v.NoCredential() }

// ChipText and ChipTone are the section's status chip.
func (v CredentialView) ChipText() string {
	if v.Auth.SignedIn {
		return "Signed in"
	}
	if v.Status.Set {
		return "Set"
	}
	return "Not set"
}

func (v CredentialView) ChipTone() string {
	if v.Status.Set || v.Auth.SignedIn {
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
