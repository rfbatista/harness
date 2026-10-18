package app

import (
	"context"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/codegraph"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/capabilities"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/ports"
)

// TokenSaversModule lets the capabilities context install token savers: from
// the releases the harness pins (or Config.TokenSaverReleaseBase), under a
// tools/ directory next to the database (ADR 0012). It also keeps each
// repository's codegraph index in its checkout (ADR 0014).
var TokenSaversModule = fx.Module("tokensavers",
	fx.Invoke(useTokenSavers),
	fx.Provide(newRepositoryIndexes),
	// Built even with no page to serve them: the subscriptions are the point.
	fx.Invoke(func(*workspaces.RepositoryIndexes) {}),
)

func useTokenSavers(cfg Config, db *gorm.DB, cat catalog.Catalog) error {
	dir, err := dataDir(cfg, "tools")
	if err != nil {
		return err
	}
	cat.Capabilities.UseTokenSavers(capabilities.TokenSaverDeps{
		Installations:  sqlite.NewTokenSaverInstallationRepository(db),
		Downloads:      releases.NewClient(),
		Dir:            dir,
		ReleaseBaseURL: cfg.TokenSaverReleaseBase,
	})
	return nil
}

// newRepositoryIndexes builds repository indexes as codegraph's activations,
// installs and sessions ending call for them; stopping the server kills the
// builds that run.
func newRepositoryIndexes(lc fx.Lifecycle, cat catalog.Catalog, orch *orchestration.Service) *workspaces.RepositoryIndexes {
	x := workspaces.NewRepositoryIndexes(workspaces.RepositoryIndexDeps{
		Projects:      cat.Projects,
		Sessions:      orch,
		Installations: cat.Capabilities,
		Activations:   cat.Capabilities,
		Indexer:       codegraph.NewIndexer(),
		Excludes:      gitcli.NewExcludes(),
	})
	x.Subscribe(cat.Bus)
	orch.IndexBuilds = x
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		x.Close()
		return nil
	}})
	return x
}

// optionalRepositoryIndexes is the repository indexes when
// TokenSaversModule is in the graph; graphs built without it (some tests)
// serve none.
type optionalRepositoryIndexes struct {
	fx.In

	Service *workspaces.RepositoryIndexes `optional:"true"`
}

// port is the service as the driving port, nil (untyped) when absent.
func (o optionalRepositoryIndexes) port() ports.RepositoryIndexes {
	if o.Service == nil {
		return nil
	}
	return o.Service
}
