package projects

import (
	"context"
	"time"

	"operators-mcp/internal/ports"
)

// StatusTimeout bounds the `claude auth status` the page waits for when it
// paints the project login.
const StatusTimeout = 5 * time.Second

// AuthSeed is what `claude auth status` says about the project Claude home:
// the account fields it prints, never a token or the login file's content.
type AuthSeed struct {
	SignedIn     bool   `json:"signed_in"`
	Account      string `json:"account"`
	Subscription string `json:"subscription"`
	AuthMethod   string `json:"auth_method"`
	// CheckedOK is false when the status could not be read in time.
	CheckedOK bool `json:"checked_ok"`
}

// AgentCredentialSeed is what projectAgentCredential starts from: the
// project's own credential in the body shape of GET
// /api/get_project_agent_credential, and beside it whether the global
// credential is set, which the project's sessions fall back to. It never holds
// a value.
type AgentCredentialSeed struct {
	ProjectID string `json:"project_id"`
	ports.AgentCredentialStatus
	Global ports.AgentCredentialStatus `json:"global"`
	// OAuth is the project's sign-in flow now; never holds the token or the
	// code. CanSignIn says the flow can run: it is wired and a claude is
	// installed. HasLogin says the project Claude home holds a login, whose
	// account Auth gives.
	OAuth     ports.AgentOAuthState `json:"oauth"`
	CanSignIn bool                  `json:"can_sign_in"`
	HasLogin  bool                  `json:"has_login"`
	Auth      AuthSeed              `json:"auth"`
}

// AgentCredentialView is the Agent credential section of a project's settings.
type AgentCredentialView struct {
	ProjectName string
	Seed        AgentCredentialSeed
}

// NewAgentCredentialView reads the project's own credential and the global one;
// nil when the project credentials are not served or could not be read (the
// page then goes without the section).
func NewAgentCredentialView(own ports.ProjectAgentCredentials, global ports.AgentCredential, projectID, projectName string) (*AgentCredentialView, error) {
	if own == nil {
		return nil, nil
	}
	st, err := own.StatusFor(projectID)
	if err != nil {
		return nil, err
	}
	v := &AgentCredentialView{ProjectName: projectName, Seed: AgentCredentialSeed{ProjectID: projectID, AgentCredentialStatus: st}}
	if global != nil {
		v.Seed.Global = global.Status()
	}
	return v, nil
}

// WithSignIn adds the project's sign-in flow and, when its Claude home holds a
// login, who it is signed in as (bounded by StatusTimeout). Without the flow
// the section offers no sign-in.
func (v *AgentCredentialView) WithSignIn(ctx context.Context, signIn ports.ProjectAgentOAuth, bin ports.ManagedClaudeBin) {
	if signIn == nil {
		return
	}
	flow := signIn.For(v.Seed.ProjectID)
	v.Seed.OAuth = flow.State()
	v.Seed.CanSignIn = bin != nil && bin.Installed()
	v.Seed.HasLogin = signIn.HasLogin(v.Seed.ProjectID)
	if !v.Seed.HasLogin || !v.Seed.CanSignIn {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, StatusTimeout)
	defer cancel()
	st, err := flow.Status(ctx)
	if err != nil {
		return
	}
	v.Seed.Auth = AuthSeed{SignedIn: st.LoggedIn, Account: st.Email, Subscription: st.SubscriptionType, AuthMethod: st.AuthMethod, CheckedOK: true}
}

// SignedIn says the project is signed in, from the status read.
func (v AgentCredentialView) SignedIn() bool { return v.Seed.Auth.SignedIn }

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

// FallbackLine says what sessions of a project without its own credential use.
func (v AgentCredentialView) FallbackLine() string {
	if v.Seed.Global.Set {
		return "Sessions of this project use the global Agent credential, or a Claude login when the agents' home has one."
	}
	return "Sessions of this project use a Claude login in the agents' home. No global Agent credential is set either."
}

// PasteLabel is the paste button: Replace when one is set.
func (v AgentCredentialView) PasteLabel() string {
	if v.Seed.Set {
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
