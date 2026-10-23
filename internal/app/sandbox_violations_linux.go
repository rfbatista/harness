package app

import "operators-mcp/internal/ports"

// platformSandboxViolations is nil: violations are not collected on Linux.
func platformSandboxViolations() ports.SandboxViolationSource { return nil }
