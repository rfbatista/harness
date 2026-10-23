package agentisolation

import (
	"testing"

	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

type unsetCredential struct{ ports.AgentCredential }

func (unsetCredential) Status() ports.AgentCredentialStatus { return ports.AgentCredentialStatus{} }

// A login in the agent home satisfies the credential requirement, so the top
// bar's attention mark must not stay on for it.
func TestALoginAloneDoesNotMarkTheTopBar(t *testing.T) {
	driver := func() ports.SandboxDriver { return &portstest.RecordingSandboxDriver{} }
	login := func() bool { return true }
	noLogin := func() bool { return false }

	if Blocks(nil, driver, unsetCredential{}, noLogin, nil) != true {
		t.Error("no credential and no login must mark the top bar")
	}
	if Blocks(nil, driver, unsetCredential{}, nil, nil) != true {
		t.Error("no credential and no login check must mark the top bar")
	}
	if Blocks(nil, driver, unsetCredential{}, login, nil) {
		t.Error("a login in the agent home must not mark the top bar")
	}
}
