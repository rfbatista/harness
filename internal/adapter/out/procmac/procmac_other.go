//go:build !darwin

// Package procmac reads the macOS process table in process. Elsewhere the
// vigia is not wired and this table reads nothing.
package procmac

import (
	"context"

	"operators-mcp/internal/ports"
)

var _ ports.ProcessTable = Table{}

// Table is empty off macOS.
type Table struct{}

// Read answers no processes.
func (Table) Read(context.Context) ([]ports.ProcessInfo, error) { return nil, nil }
