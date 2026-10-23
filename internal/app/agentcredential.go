package app

import (
	"context"
	"os"

	goruntime "runtime"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/adapter/out/keychainshim"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/app/runtime"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/application/keychainpath"
	"operators-mcp/internal/application/orchestration"
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
//
// Which one agents use is decided at each start (ADR 0022): a login in the
// agent home wins, the pasted credential is the fallback. The `security` shim
// keeps claude's login out of the person's keychain.
func bindAgentCredential(svc *agentcredential.Service, u gitCredentialUsers, home claudehome.Home, kc *keychainshim.Dir) {
	if u.Orch != nil {
		u.Orch.AgentCredential = svc.Env
		u.Orch.AgentLogin = home.HasLogin
		u.Orch.KeychainShimDir = kc.Path()
	}
}

// newKeychainShim makes the dir holding the `security` shim, on macOS, and
// removes it when the server stops.
func newKeychainShim(lc fx.Lifecycle, cfg Config) (*keychainshim.Dir, error) {
	d, err := keychainshim.New(cfg.ShimBinary, goruntime.GOOS)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		d.Close()
		return nil
	}})
	return d, nil
}

// withKeychainShim is env with the shim dir on its PATH, the env of the
// login, status and logout commands and of a one-shot.
func withKeychainShim(env []string, dir string) []string {
	return append(env, keychainpath.Env(env, dir)...)
}

// oauthParts are what the sign-in flow runs on; the orchestration's sandbox is
// absent in graphs built without one.
type oauthParts struct {
	fx.In

	Bin  ports.ManagedClaudeBin
	Home claudehome.Home
	Orch *orchestration.Service `optional:"true"`
	KC   *keychainshim.Dir
}

// sandbox is the orchestration's sandbox step, nil without one.
func (p oauthParts) sandbox() *orchestration.Sandbox {
	if p.Orch == nil {
		return nil
	}
	return p.Orch.Sandbox
}

// newAgentOAuth is the Sign in with Claude flow: `claude auth login` of the
// managed claude, run in the agent Claude home under the sandbox driver with
// the agent-home-only policy, so claude keeps the login there and never
// reaches the person's keychain. Stopping the server kills the process that
// runs.
func newAgentOAuth(lc fx.Lifecycle, p oauthParts) *agentoauth.Service {
	svc := agentoauth.New(agentoauth.Deps{Bin: p.Bin, ConfigDir: p.Home.Path(), PTY: ptyunix.New(), Wrap: loginWrap(p.sandbox(), p.Bin, p.KC.Path()),
		Env: func() []string { return withKeychainShim(os.Environ(), p.KC.Path()) }})
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		svc.Close()
		return nil
	}})
	return svc
}

// loginWrap confines the login under the platform's sandbox driver, whether
// or not the agent sandbox is switched on: claude's install dir is the one
// thing it may read beyond its temp dir and the agent home. Nil without a
// driver.
// readable are further directories it may read, the security shim's.
func loginWrap(sb *orchestration.Sandbox, bin ports.ManagedClaudeBin, readable ...string) agentoauth.Wrap {
	if sb == nil || sb.Driver == nil {
		return nil
	}
	return func(c ports.SandboxCommand) (ports.SandboxCommand, error) {
		// Read at each login: the managed claude may have been installed or
		// switched since the server started.
		allow := sandboxAllowlist(allowSources{ClaudeDirs: append(runtime.ClaudeInstallDirs(bin.Bin()), readable...)})
		return sb.LoginWrap(allow)(c)
	}
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

// auth is the sign-in service as the port that reads and ends the login, nil
// (untyped) when absent.
func (o optionalAgentCredential) auth() ports.AgentAuth {
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
