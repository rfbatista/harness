package projects

import (
	"context"

	"operators-mcp/internal/ports"
)

// AgentCredentialSeed is what projectAgentCredential starts from: the
// project's own credential in the body shape of GET
// /api/get_project_agent_credential, and beside it whether the global
// credential is set, which the project's sessions fall back to. It never holds
// a value.
type AgentCredentialSeed struct {
	ProjectID string `json:"project_id"`
	ports.ProjectCredentialStatus
	Global ports.AgentCredentialStatus `json:"global"`
	// OAuth is the project's sign-in flow now; never holds the token or the
	// code. CanSignIn says the flow can run: it is wired and a claude is
	// installed.
	OAuth     ports.AgentOAuthState `json:"oauth"`
	CanSignIn bool                  `json:"can_sign_in"`
}

// AgentCredentialView is the Agent credential section of a project's settings.
type AgentCredentialView struct {
	ProjectName string
	Seed        AgentCredentialSeed
}

// NewAgentCredentialView reads the project's own credential, with its login and
// the credential it effectively runs with (bounded by the credential service),
// and the global one; nil when the project credentials are not served or could
// not be read (the page then goes without the section).
func NewAgentCredentialView(ctx context.Context, own ports.ProjectAgentCredentials, global ports.AgentCredential, projectID, projectName string) (*AgentCredentialView, error) {
	if own == nil {
		return nil, nil
	}
	st, err := own.DetailFor(ctx, projectID)
	if err != nil {
		return nil, err
	}
	v := &AgentCredentialView{ProjectName: projectName, Seed: AgentCredentialSeed{ProjectID: projectID, ProjectCredentialStatus: st}}
	if global != nil {
		v.Seed.Global = global.Status()
	}
	return v, nil
}

// WithSignIn adds the project's sign-in flow. Without the flow the section
// offers no sign-in.
func (v *AgentCredentialView) WithSignIn(signIn ports.ProjectAgentOAuth, bin ports.ManagedClaudeBin) {
	if signIn == nil {
		return
	}
	v.Seed.OAuth = signIn.For(v.Seed.ProjectID).State()
	v.Seed.CanSignIn = bin != nil && bin.Installed()
}

// SignedIn says the project holds a project login.
func (v AgentCredentialView) SignedIn() bool { return v.Seed.Kind == ports.AgentCredentialLogin }

// SignInDisabled: no claude to run.
func (v AgentCredentialView) SignInDisabled() bool { return !v.Seed.CanSignIn }

// ChipText and ChipTone are the section's status chip.
func (v AgentCredentialView) ChipText() string {
	if v.SignedIn() {
		return "Signed in"
	}
	if v.Seed.Set {
		return "Own credential"
	}
	return "Global"
}

func (v AgentCredentialView) ChipTone() string {
	if v.SignedIn() || v.Seed.Set {
		return "signal"
	}
	return ""
}

// KindName is the kind in the person's words.
func (v AgentCredentialView) KindName() string {
	switch v.Seed.Kind {
	case ports.AgentCredentialOAuth:
		return "OAuth token"
	case ports.AgentCredentialAPIKey:
		return "Anthropic API key"
	case ports.AgentCredentialLogin:
		return "Claude login"
	}
	return ""
}

// SavedDate is the day it was saved.
func (v AgentCredentialView) SavedDate() string {
	if v.Seed.Updated.IsZero() {
		return ""
	}
	return v.Seed.Updated.UTC().Format("2006-01-02")
}

// FallbackLine says what sessions of a project without a project credential
// use instead.
func (v AgentCredentialView) FallbackLine() string {
	return FallbackText(v.Seed.Effective)
}

// FallbackText is the effective credential of a project without its own, in
// the person's words.
func FallbackText(e ports.EffectiveCredential) string {
	switch e.Source {
	case ports.EffectiveGlobalLogin:
		if e.Account == "" {
			return "Uses the global login."
		}
		return "Uses the global login: " + e.Account + ", " + e.Subscription
	case ports.EffectiveGlobalPasted:
		if e.Kind == ports.AgentCredentialAPIKey {
			return "Uses the global pasted API key."
		}
		return "Uses the global pasted OAuth token."
	}
	return "No credential: agents cannot start."
}

// PasteLabel is the paste button: Replace when one is set.
func (v AgentCredentialView) PasteLabel() string {
	if v.Seed.Set && !v.SignedIn() {
		return "Replace with a pasted token or key"
	}
	return "Paste a token or key"
}

// PasteTone is the paste button's variant: primary while none is set.
func (v AgentCredentialView) PasteTone() string {
	if v.Seed.Set {
		return ""
	}
	return "primary"
}
