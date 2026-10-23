//go:build linux

package app

import (
	"operators-mcp/internal/adapter/out/bwrap"
	"operators-mcp/internal/ports"
)

func init() {
	platformSandboxDriver = func() ports.SandboxDriver {
		d, err := bwrap.New()
		if err != nil {
			return nil // no bwrap: no driver
		}
		return d
	}
}
