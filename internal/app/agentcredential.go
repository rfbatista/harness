package app

import (
	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/application/agentcredential"
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

// optionalAgentCredential is the credential when the graph has one; graphs
// built without it (some tests) serve none.
type optionalAgentCredential struct {
	fx.In

	Service *agentcredential.Service `optional:"true"`
}

// port is the service as the driving port, nil (untyped) when absent.
func (o optionalAgentCredential) port() ports.AgentCredential {
	if o.Service == nil {
		return nil
	}
	return o.Service
}
