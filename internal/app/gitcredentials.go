package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/githubapi"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/gitcredentials"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// GitCredentialsModule wires the git credentials context: its stores, the
// secret box keyed from HARNESS_SECRET_KEY or the data dir's secret.key, and
// its bus subscriptions. It provides *gitcredentials.Service.
var GitCredentialsModule = fx.Module("gitcredentials",
	fx.Provide(newSecretBox),
	fx.Provide(newGitCredentialsService),
	fx.Provide(newAgentCredential, newKeychainShim),
	fx.Provide(newAgentOAuth),
	fx.Invoke(registerGitCredentials, bindGitCredentials),
	fx.Invoke(bindAgentCredential),
)

// gitCredentialUsers are what runs git in session worktrees, when the graph
// has them.
type gitCredentialUsers struct {
	fx.In

	Orch *orchestration.Service `optional:"true"`
	Apps *apps.Service          `optional:"true"`
}

// bindGitCredentials gives sessions and their app runs the stored
// credentials: their processes get a git directory of their own.
func bindGitCredentials(svc *gitcredentials.Service, u gitCredentialUsers) {
	if u.Orch != nil {
		u.Orch.GitAuth = svc
	}
	if u.Apps != nil {
		u.Apps.GitAuth = svc
	}
}

type gitCredentialsParams struct {
	fx.In

	Cfg          Config
	DB           *gorm.DB
	Projects     ports.ProjectRepository
	Repositories ports.RepositoryRepository
	Git          ports.BranchPublisher
	Cat          catalog.Catalog
	Box          *secretbox.Box
}

// registerGitCredentials checks git on start and sweeps what a previous run
// left: operation directories, and the git directories of sessions that are
// no longer running.
func registerGitCredentials(lc fx.Lifecycle, cfg Config, svc *gitcredentials.Service, sessions ports.SessionRepository) {
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		if err := gitcli.CheckVersion(cfg.GitBin); err != nil {
			slog.Error("git credentials", "err", err)
		}
		return svc.Sweep(ctx, func(id string) (string, bool) {
			sess := sessions.Get(id)
			if sess == nil || sess.Status.IsTerminal() {
				return "", false
			}
			return sess.RepositoryID, true
		})
	}})
}

// newSecretBox is the one box that seals every secret the harness keeps
// (git credentials, the agent credential): keyed from HARNESS_SECRET_KEY or
// the data dir's secret.key. One box, because two would both try to create
// the key file.
func newSecretBox(cfg Config) (*secretbox.Box, error) {
	dataDir, err := secretsDir(cfg)
	if err != nil {
		return nil, err
	}
	return secretbox.New(os.Getenv(secretbox.EnvKey), filepath.Join(dataDir, "secret.key"))
}

func newGitCredentialsService(p gitCredentialsParams) (*gitcredentials.Service, error) {
	dataDir, err := secretsDir(p.Cfg)
	if err != nil {
		return nil, err
	}
	svc := gitcredentials.New(
		gitcredentials.Stores{
			Credentials: sqlite.NewGitCredentialRepository(p.DB),
			Identity:    sqlite.NewGitIdentityRepository(p.DB),
			Pins:        sqlite.NewRepositoryCredentialRepository(p.DB),
		},
		gitcredentials.Readers{Repositories: p.Repositories, Projects: p.Projects, Origins: p.Git},
		p.Box, nil)
	broker, err := gitcli.NewAuthBroker(filepath.Join(dataDir, "git"), gitcli.MachineExcludesFile(p.Cfg.GitBin))
	if err != nil {
		return nil, err
	}
	broker.IncludeFiles = personalGitConfigs()
	svc.Broker = broker
	svc.Tester = &gitcli.Probe{Broker: broker, GitHub: githubapi.New().User, Git: p.Cfg.GitBin}
	svc.Subscribe(p.Cat.Bus)
	return svc, nil
}

// secretsDir is the data dir, absolute (Config.DataDir, or the database's
// folder), or a fresh temporary directory when there is none. secret.key
// and git/ go there. Absolute, because sessions run git from their
// worktrees, never from the server's working directory.
func secretsDir(cfg Config) (string, error) {
	root, err := cfg.dataRoot()
	if err != nil || root != "" {
		return root, err
	}
	return os.MkdirTemp("", "harness-secrets-")
}

// optionalGitCredentials is the credentials context when
// GitCredentialsModule is in the graph; graphs built without it (some tests)
// serve none.
type optionalGitCredentials struct {
	fx.In

	Service *gitcredentials.Service `optional:"true"`
}

// port is the service as the driving port, nil (untyped) when absent.
func (o optionalGitCredentials) port() ports.GitCredentials {
	if o.Service == nil {
		return nil
	}
	return o.Service
}

// personalGitConfigs are the person's own git config files a session's global
// config includes first: the XDG one, then ~/.gitconfig (git's own order).
func personalGitConfigs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	return []string{filepath.Join(xdg, "git", "config"), filepath.Join(home, ".gitconfig")}
}
