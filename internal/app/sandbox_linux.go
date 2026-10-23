package app

import (
	"operators-mcp/internal/adapter/out/bwrap"
	"operators-mcp/internal/ports"
)

// platformSandboxDriver is bubblewrap on Linux, or nil when bwrap is missing
// (the session start then refuses, ADR 0017).
func platformSandboxDriver() ports.SandboxDriver {
	d, err := bwrap.New()
	if err != nil {
		return nil
	}
	return d
}

// platformSandboxViolations is nil: violations are not collected on Linux.
func platformSandboxViolations() ports.SandboxViolationSource { return nil }
