package ports

import (
	"context"
	"time"
)

// Agent credential kinds (ADR 0018).
const (
	// AgentCredentialOAuth is a long-lived OAuth token from `claude setup-token`.
	AgentCredentialOAuth = "oauth"
	// AgentCredentialAPIKey is an Anthropic API key.
	AgentCredentialAPIKey = "api_key"
)

// AgentCredentialStatus is what the harness says about the agent credential:
// never its value.
type AgentCredentialStatus struct {
	Set     bool      `json:"set"`
	Kind    string    `json:"kind"` // AgentCredentialOAuth or AgentCredentialAPIKey; "" when not set
	Updated time.Time `json:"updated"`
}

// AgentCredentialLogin is the kind of a project that holds a project login
// (ADR 0102): the login claude keeps in its project Claude home, not a pasted
// value. It is a status only; no credential record has it.
const AgentCredentialLogin = "login"

// Where an effective credential comes from (ADR 0102).
const (
	// EffectiveProject: the project's own credential, a login or a pasted one.
	EffectiveProject = "project"
	// EffectiveGlobalLogin: the login in the agent Claude home.
	EffectiveGlobalLogin = "global_login"
	// EffectiveGlobalPasted: the global pasted credential.
	EffectiveGlobalPasted = "global_pasted"
	// EffectiveNone: the project has none, and its agents cannot start.
	EffectiveNone = "none"
)

// EffectiveCredential is the credential the project's next claude process
// runs with: the project credential, then the agent login, then the global
// pasted credential. Account and Subscription are filled for a login only.
type EffectiveCredential struct {
	Source       string `json:"source"`
	Kind         string `json:"kind"`
	Account      string `json:"account"`
	Subscription string `json:"subscription"`
}

// ProjectCredentialStatus is what the harness says about a project's
// credential: its own (a login or a pasted one) and the effective one, never a
// value. Account, Subscription and CheckedOK are filled for a login only, from
// `claude auth status` in the project Claude home; CheckedOK false means the
// status could not be read.
type ProjectCredentialStatus struct {
	AgentCredentialStatus
	Account      string              `json:"account"`
	Subscription string              `json:"subscription"`
	CheckedOK    bool                `json:"checked_ok"`
	Effective    EffectiveCredential `json:"effective"`
}

// AgentCredential is the credential agent processes sign in with, stored
// encrypted and passed only to agent processes (driving).
type AgentCredential interface {
	Status() AgentCredentialStatus
	// Set stores value as a credential of kind, replacing any. An empty value
	// or an unknown kind is refused with INVALID_INPUT.
	Set(kind, value string) (AgentCredentialStatus, error)
	Remove() (AgentCredentialStatus, error)
}

// AgentCredentialRecord is the stored credential: the value sealed.
type AgentCredentialRecord struct {
	Kind    string
	Sealed  []byte
	KeyID   string
	Updated time.Time
}

// AgentCredentialStore keeps the one agent credential (driven); Get answers
// nil when none is stored.
type AgentCredentialStore interface {
	Get() (*AgentCredentialRecord, error)
	Put(r AgentCredentialRecord) error
	Delete() error
}

// ProjectAgentCredentials are the credentials agent processes of one project
// sign in with, instead of the global one (driving). A project without its own
// falls back to the global AgentCredential, so StatusFor answers about the
// project's own only. An empty project id is refused with INVALID_INPUT.
type ProjectAgentCredentials interface {
	StatusFor(projectID string) (AgentCredentialStatus, error)
	SetFor(projectID, kind, value string) (AgentCredentialStatus, error)
	RemoveFor(projectID string) (AgentCredentialStatus, error)
	// DetailFor is the project's credential with its login and what it
	// effectively runs with; it may run `claude auth status`.
	DetailFor(ctx context.Context, projectID string) (ProjectCredentialStatus, error)
}

// ProjectAgentCredentialStore keeps one agent credential per project (driven);
// Get answers nil when the project has none.
type ProjectAgentCredentialStore interface {
	GetFor(projectID string) (*AgentCredentialRecord, error)
	PutFor(projectID string, r AgentCredentialRecord) error
	DeleteFor(projectID string) error
}
