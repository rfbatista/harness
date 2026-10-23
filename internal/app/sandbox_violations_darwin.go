package app

import (
	"operators-mcp/internal/adapter/out/seatbelt"
	"operators-mcp/internal/ports"
)

// platformSandboxViolations follows the unified log on macOS.
func platformSandboxViolations() ports.SandboxViolationSource { return seatbelt.NewCollector() }
