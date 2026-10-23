//go:build !darwin && !linux

package app

import "operators-mcp/internal/ports"

// platformSandboxDriver is nil where no sandbox driver exists.
func platformSandboxDriver() ports.SandboxDriver { return nil }

// platformSandboxViolations is nil where violations are not collected.
func platformSandboxViolations() ports.SandboxViolationSource { return nil }
