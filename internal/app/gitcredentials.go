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
	"operators-mcp/internal/application/gitcredentials"
	"operators-mcp/internal/ports"
)

// GitCredentialsModule wires the git credentials context: its stores, the
// secret box keyed from HARNESS_SECRET_KEY or the data dir's secret.key, and
// its bus subscriptions. It provides *gitcredentials.Service.
var GitCredentialsModule = fx.Module("gitcredentials",
	fx.Provide(newGitCredentialsService),
	fx.Invoke(registerGitCredentials),
)

type gitCredentialsParams struct {
	fx.In

	Cfg          Config
	DB           *gorm.DB
	Projects     ports.ProjectRepository
	Repositories ports.RepositoryRepository
	Git          ports.BranchPublisher
	Cat          catalog.Catalog
}

// registerGitCredentials checks git on start and sweeps what a previous run
// left: operation directories, and the git directories of sessions that are
// no longer running.
func registerGitCredentials(lc fx.Lifecycle, svc *gitcredentials.Service, sessions ports.SessionRepository) {
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		if err := gitcli.CheckVersion(); err != nil {
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

func newGitCredentialsService(p gitCredentialsParams) (*gitcredentials.Service, error) {
	dataDir, err := secretsDir(p.Cfg)
	if err != nil {
		return nil, err
	}
	keyFile := filepath.Join(dataDir, "secret.key")
	box, err := secretbox.New(os.Getenv(secretbox.EnvKey), keyFile)
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
		box, nil)
	files := gitcli.NewAuthFiles(filepath.Join(dataDir, "git"), gitcli.MachineExcludesFile())
	svc.Files = files
	svc.Tester = &gitcli.Probe{Files: files, GitHub: githubapi.New().User}
	svc.Subscribe(p.Cat.Bus)
	return svc, nil
}

// secretsDir is the data dir: where the database is, or a fresh temporary
// directory for an in-memory one. secret.key and git/ go there.
func secretsDir(cfg Config) (string, error) {
	if cfg.DBPath == "" || cfg.DBPath == ":memory:" {
		return os.MkdirTemp("", "harness-secrets-")
	}
	return filepath.Dir(cfg.DBPath), nil
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
