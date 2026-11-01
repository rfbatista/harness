package app

import (
	"context"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/agents/claudetext"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/app/runtime"
	"operators-mcp/internal/application/agentready"
	"operators-mcp/internal/application/archassistant"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/sandboxsettings"
)

// assistantTools are the only built-in tools the architecture assistant's
// claude may use (ADR 0060): it reads code and writes nothing.
var assistantTools = []string{"Read", "Grep", "Glob"}

// newTurnStreamer is the claude driver of the architecture assistant: llmkit's
// resumable read-only turn, on the same managed claude, agent Claude home,
// credential, LLM proxy and readiness gate as the one-shots. With the sandbox
// on, each turn runs under the deny-writes profile for its checkout.
func newTurnStreamer(cfg Config, p oneShotParts) claudetext.TurnStreamer {
	bin := oneShotBin(cfg, p)
	one := claudetext.TurnStreamer(oneShotClaude(bin, p.Home.Path(), cfg.llmProxyURL()))
	if p.Orch != nil && p.Credential != nil {
		one = claudetext.ConfinedTurn(one, readOnlyTurnPrep(bin, p))
	}
	return claudetext.GatedTurn(one, requireConfinement(p.Ready.check(), turnConfined(p)))
}

// turnConfined says whether a turn can be confined: the graph has the sandbox
// and the credential, and the platform has a driver. The deny-writes profile
// applies whatever the agent sandbox switch says (ADR 0060, like ADR 0052).
func turnConfined(p oneShotParts) func() bool {
	return func() bool {
		return p.Orch != nil && p.Credential != nil && p.Orch.Sandbox != nil && p.Orch.Sandbox.Driver != nil
	}
}

// requireConfinement is the readiness check of the assistant: it refuses while
// a turn cannot be confined, so the assistant never runs with the tool
// allowlist as its only guard, and then asks the readiness gate. A nil check is
// always ready.
func requireConfinement(check func(context.Context) error, confined func() bool) func(context.Context) error {
	return func(ctx context.Context) error {
		if !confined() {
			return agentready.ReadOnlySandboxMissing()
		}
		if check == nil {
			return nil
		}
		return check(ctx)
	}
}

// readOnlyTurnPrep prepares each turn: the env entries of the agent credential
// and, with the sandbox on, the deny-writes wrap for the turn's checkout.
func readOnlyTurnPrep(bin string, p oneShotParts) func(dir string) ([]string, llmkit.WrapFunc, error) {
	env := oneShotEnv(p.Credential.Env, p.Home.HasLogin, p.KC.Path())
	allow := sandboxAllowlist(allowSources{ClaudeDirs: append(runtime.ClaudeInstallDirs(bin), p.KC.Path())})
	return func(dir string) ([]string, llmkit.WrapFunc, error) {
		wrap, err := p.Orch.Sandbox.ReadOnlyWrap(dir, allow)
		if err != nil {
			return nil, nil, err
		}
		return env(), wrap, nil
	}
}

// assistantParts are what the assistant reads besides the catalog, each when
// the graph has it.
type assistantParts struct {
	fx.In

	Plan    *planning.Service      `optional:"true"`
	Sandbox *sandboxsettings.Store `optional:"true"`
}

// newArchitectureAssistant is the architecture assistant over the read-only
// turn, the record service, the catalogs and the project's documents. It is nil
// when the graph keeps no architecture records.
func newArchitectureAssistant(cat catalog.Catalog, turn claudetext.TurnStreamer, in assistantParts, p oneShotParts) *archassistant.Service {
	if cat.RepositoryArchitectures == nil || in.Plan == nil || in.Sandbox == nil {
		return nil
	}
	return archassistant.New(archassistant.Deps{
		Turn:          assistantTurn{turn},
		Settings:      cat.Settings,
		Repos:         cat.Projects,
		Architectures: cat.RepositoryArchitectures,
		Agents:        cat.Agents,
		MCP:           cat.Capabilities,
		Sandbox:       in.Sandbox,
		Documents:     in.Plan,
		Contexts:      cat.Architecture,
		Peek:          requireConfinement(p.Ready.peek(), turnConfined(p)),
	})
}

// assistantTurn adapts llmkit's turn to the assistant's port.
type assistantTurn struct{ one claudetext.TurnStreamer }

func (a assistantTurn) Run(ctx context.Context, c archassistant.TurnCall, cb archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
	req := claude.TurnRequest{
		System: c.System, Input: c.Input, Model: c.Model, Attribution: c.Attribution,
		ResumeID: c.ResumeID, Dir: c.Dir, Tools: assistantTools,
	}
	res, err := a.one.TurnStream(ctx, req, claude.TurnCallbacks{
		OnText: cb.OnText,
		OnTool: func(name string, input []byte) {
			cb.OnTool(name, claudetext.RelativeTo(c.Dir, claudetext.ToolTarget(name, input)))
		},
	})
	return archassistant.TurnResult{SessionID: res.SessionID, Text: res.Text}, err
}

func (a assistantTurn) DeleteTranscript(sessionID string) { _ = a.one.DeleteTranscript(sessionID) }

var _ archassistant.Turn = assistantTurn{}
