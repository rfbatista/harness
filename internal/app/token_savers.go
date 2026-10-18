package app

import (
	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/capabilities"
)

// TokenSaversModule lets the capabilities context install token savers: from
// the releases the harness pins (or Config.TokenSaverReleaseBase), under a
// tools/ directory next to the database (ADR 0012).
var TokenSaversModule = fx.Module("tokensavers",
	fx.Invoke(useTokenSavers),
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
