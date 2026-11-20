package ports

import "time"

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
}

// ProjectAgentCredentialStore keeps one agent credential per project (driven);
// Get answers nil when the project has none.
type ProjectAgentCredentialStore interface {
	GetFor(projectID string) (*AgentCredentialRecord, error)
	PutFor(projectID string, r AgentCredentialRecord) error
	DeleteFor(projectID string) error
}
