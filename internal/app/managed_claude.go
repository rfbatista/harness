package app

import (
	"context"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/ports"
)

// The managed claude (ADR 0019): the official native build, installed under
// <data dir>/claude, with a stable current path. Which binary sessions run
// is decided elsewhere; ClaudeBin resolves it, the -claude.bin override first.

// newManagedClaude builds the install service; stopping the server cancels
// the install that runs and waits for it.
func newManagedClaude(lc fx.Lifecycle, cfg Config) (*managedclaude.Service, error) {
	dir, err := dataDir(cfg, "claude")
	if err != nil {
		return nil, err
	}
	svc := managedclaude.New(managedclaude.Deps{
		Downloads: releases.NewClient(),
		Dir:       dir,
		BaseURL:   cfg.ManagedClaudeReleaseBase,
	})
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		svc.Close()
		return nil
	}})
	return svc, nil
}

// newManagedClaudeBin is the binary agents resolve to: the override when
// -claude.bin / $CLAUDE_BIN is set, else the managed current path. It also
// says whether that binary is installed.
func newManagedClaudeBin(cfg Config, svc *managedclaude.Service) ports.ManagedClaudeBin {
	return managedclaude.Resolver{Override: cfg.ClaudeBin, Managed: svc}
}
