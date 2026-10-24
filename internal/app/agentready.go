package app

import (
	"context"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/agentready"
	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/ports"
)

// agentReadinessParts are what the gate reads, each when the graph has it:
// what is absent is not enforced.
type agentReadinessParts struct {
	fx.In

	Orch       *orchestration.Service   `optional:"true"`
	Credential *agentcredential.Service `optional:"true"`
	Managed    *managedclaude.Service   `optional:"true"`
	Bin        ports.ManagedClaudeBin   `optional:"true"`
	Sandbox    *sandboxsettings.Store   `optional:"true"`
	Home       claudehome.Home          `optional:"true"`
}

// newAgentReadiness is the check every agent start and one-shot goes through:
// the agent credential, the managed claude (installed on the first start that
// finds it missing) and, with the sandbox on, its driver.
func newAgentReadiness(p agentReadinessParts) *agentready.Gate {
	g := &agentready.Gate{}
	if p.Credential != nil {
		// A login in the agent home satisfies the requirement as a pasted
		// credential does (ADR 0022).
		g.Credential = func() bool { return p.Home.HasLogin() || p.Credential.Status().Set }
	}
	if p.Bin != nil {
		g.Bin = p.Bin
		if p.Managed != nil {
			g.Managed = p.Managed
		}
	}
	if p.Orch != nil && p.Sandbox != nil {
		g.SandboxOn = p.Sandbox.Enabled
		g.SandboxDriver = func() bool { return p.Orch.Sandbox == nil || p.Orch.Sandbox.Driver != nil }
	}
	return g
}

// bindAgentReadiness makes session starts and resumes go through the gate.
func bindAgentReadiness(svc *orchestration.Service, g *agentready.Gate) {
	svc.Ready = g
}

// optionalAgentReadiness is the gate when the graph has one.
type optionalAgentReadiness struct {
	fx.In

	Gate *agentready.Gate `optional:"true"`
}

// check is the gate's Check, nil when the graph has no gate.
func (o optionalAgentReadiness) check() func(context.Context) error {
	if o.Gate == nil {
		return nil
	}
	return o.Gate.Check
}

// peek is the gate's Peek, nil when the graph has no gate: the state of the
// readiness, read without starting an install.
func (o optionalAgentReadiness) peek() func(context.Context) error {
	if o.Gate == nil {
		return nil
	}
	return o.Gate.Peek
}
