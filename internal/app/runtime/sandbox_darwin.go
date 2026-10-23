package runtime

import (
	"operators-mcp/internal/adapter/out/seatbelt"
	"operators-mcp/internal/ports"
)

// PlatformSandboxDriver is Seatbelt on macOS.
func PlatformSandboxDriver() ports.SandboxDriver { return seatbelt.New() }
