package app

import (
	"os"

	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/app/runtime"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/ports"
)

// The person's own sandbox configuration (the Agent isolation page): the
// store over the settings, and its binding to the orchestration. The
// platform's driver and the rest of the Sandbox are wired where the
// orchestration is built.

func newSandboxSettings(cat catalog.Catalog) *sandboxsettings.Store {
	home, _ := os.UserHomeDir()
	return &sandboxsettings.Store{Settings: cat.Settings, Home: home}
}

// bindSandboxSettings makes the orchestration's Sandbox follow the store: the
// switch and the extra paths are read at each session start. A service
// without a Sandbox has nothing to bind.
func bindSandboxSettings(svc *orchestration.Service, store *sandboxsettings.Store) {
	if svc.Sandbox == nil {
		return
	}
	svc.Sandbox.Enabled = store.Enabled
	svc.Sandbox.Extra = store.Extra
}

// sandboxDriver is the driver the orchestration runs agents under, nil when
// the platform has none. It reads the service each time, so it follows
// whatever wired the Sandbox.
func sandboxDriver(svc *orchestration.Service) func() ports.SandboxDriver {
	return func() ports.SandboxDriver {
		if svc.Sandbox == nil {
			return nil
		}
		return svc.Sandbox.Driver
	}
}

// platformSandboxDriver is the platform's driver, which the runtime stack
// also uses to confine the sessions it hosts; nil where there is none.
func platformSandboxDriver() ports.SandboxDriver { return runtime.PlatformSandboxDriver() }
