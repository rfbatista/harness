package projects

import (
	"operators-mcp/internal/ports"
)

// AgentCredentialSeed is what projectAgentCredential starts from: the
// project's own credential in the body shape of GET
// /api/get_project_agent_credential, and beside it whether the global
// credential is set, which the project's sessions fall back to. It never holds
// a value.
type AgentCredentialSeed struct {
	ProjectID string `json:"project_id"`
	ports.AgentCredentialStatus
	Global ports.AgentCredentialStatus `json:"global"`
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

// ChipText and ChipTone are the section's status chip.
func (v AgentCredentialView) ChipText() string {
	if v.Seed.Set {
		return "Own credential"
	}
	return "Global"
}

func (v AgentCredentialView) ChipTone() string {
	if v.Seed.Set {
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
