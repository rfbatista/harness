package app

import (
	"os"
	"path/filepath"

	"go.uber.org/fx"
	"gorm.io/gorm"

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

func newGitCredentialsService(p gitCredentialsParams) (*gitcredentials.Service, error) {
	keyFile, err := secretKeyPath(p.Cfg)
	if err != nil {
		return nil, err
	}
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
	svc.Subscribe(p.Cat.Bus)
	return svc, nil
}

// secretKeyPath is secret.key next to the database, or in a fresh temporary
// directory for an in-memory one.
func secretKeyPath(cfg Config) (string, error) {
	if cfg.DBPath == "" || cfg.DBPath == ":memory:" {
		dir, err := os.MkdirTemp("", "harness-secrets-")
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "secret.key"), nil
	}
	return filepath.Join(filepath.Dir(cfg.DBPath), "secret.key"), nil
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
