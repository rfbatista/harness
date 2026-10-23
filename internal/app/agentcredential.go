package app

import (
	"context"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/ports"
)

// The agent credential (ADR 0018): stored in its own table, sealed with the
// harness's secret box, and handed to each agent process as env. It is wired
// beside the git credentials because both share the secret box.

func newAgentCredential(db *gorm.DB, box *secretbox.Box) *agentcredential.Service {
	return agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil)
}

// bindAgentCredential gives sessions the credential in their env at each
// start and resume.
func bindAgentCredential(svc *agentcredential.Service, u gitCredentialUsers) {
	if u.Orch != nil {
		u.Orch.AgentCredential = svc.Env
	}
}

// newAgentOAuth is the Sign in with Claude flow: setup-token of the managed
// claude, run in the agent Claude home. Stopping the server kills the process
// that runs.
func newAgentOAuth(lc fx.Lifecycle, bin ports.ManagedClaudeBin, home claudehome.Home, cred *agentcredential.Service) *agentoauth.Service {
	svc := agentoauth.New(agentoauth.Deps{Bin: bin, ConfigDir: home.Path(), PTY: ptyunix.New(), Credential: cred})
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		svc.Close()
		return nil
	}})
	return svc
}

// optionalAgentCredential is the credential when the graph has one; graphs
// built without it (some tests) serve none.
type optionalAgentCredential struct {
	fx.In

	Service *agentcredential.Service `optional:"true"`
	OAuth   *agentoauth.Service      `optional:"true"`
}

// oauth is the sign-in flow as the driving port, nil (untyped) when absent.
func (o optionalAgentCredential) oauth() ports.AgentOAuth {
	if o.OAuth == nil {
		return nil
	}
	return o.OAuth
}

// port is the service as the driving port, nil (untyped) when absent.
func (o optionalAgentCredential) port() ports.AgentCredential {
	if o.Service == nil {
		return nil
	}
	return o.Service
}
