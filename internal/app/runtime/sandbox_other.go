//go:build !darwin && !linux

package runtime

import "operators-mcp/internal/ports"

// PlatformSandboxDriver is nil where no sandbox driver exists.
func PlatformSandboxDriver() ports.SandboxDriver { return nil }
