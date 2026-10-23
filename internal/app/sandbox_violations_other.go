//go:build !darwin && !linux

package app

import "operators-mcp/internal/ports"

// platformSandboxViolations is nil where violations are not collected.
func platformSandboxViolations() ports.SandboxViolationSource { return nil }
