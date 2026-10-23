package agentisolation

import "operators-mcp/internal/ports"

// CredentialView is the Agent credential section (#credential): whether agents
// have a credential and its kind. It never holds the value.
type CredentialView struct {
	Status ports.AgentCredentialStatus
}

// CredentialSeed is what the section's component takes over from: the body of
// GET /api/get_agent_credential.
type CredentialSeed = ports.AgentCredentialStatus

// NewCredentialView reads the credential's status; without a store it shows
// as not set.
func NewCredentialView(c ports.AgentCredential) CredentialView {
	if c == nil {
		return CredentialView{}
	}
	return CredentialView{Status: c.Status()}
}

// Seed is the section's seed.
func (v CredentialView) Seed() CredentialSeed { return v.Status }

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
	if v.Status.Set {
		return ""
	}
	return "primary"
}
