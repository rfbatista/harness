package app

import (
	"operators-mcp/internal/adapter/out/seatbelt"
	"operators-mcp/internal/ports"
)

// platformSandboxDriver is Seatbelt on macOS.
func platformSandboxDriver() ports.SandboxDriver { return seatbelt.New() }

// platformSandboxViolations follows the unified log on macOS.
func platformSandboxViolations() ports.SandboxViolationSource { return seatbelt.NewCollector() }
