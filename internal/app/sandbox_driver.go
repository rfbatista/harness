package app

import "operators-mcp/internal/ports"

// platformSandboxDriver builds the platform's sandbox driver, or answers nil
// when the platform has none or its tool is missing (the session start then
// refuses, ADR 0017). Each platform's file sets it.
var platformSandboxDriver = func() ports.SandboxDriver { return nil }
