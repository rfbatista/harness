package runtime

import (
	"operators-mcp/internal/adapter/out/bwrap"
	"operators-mcp/internal/ports"
)

// PlatformSandboxDriver is bubblewrap on Linux, or nil when bwrap is missing
// (the session start then refuses, ADR 0017).
func PlatformSandboxDriver() ports.SandboxDriver {
	d, err := bwrap.New()
	if err != nil {
		return nil
	}
	return d
}
