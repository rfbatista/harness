//go:build !windows

package ptyunix

import (
	"testing"

	"operators-mcp/internal/ports/runtimetest"
)

func TestPTYConformance(t *testing.T) { runtimetest.PTYConformance(t, New()) }
