//go:build !windows

package term

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/runtimetest"
)

var terminalSeq atomic.Int64

// shellTerminal runs script with /bin/sh on a real terminal host and returns
// the terminal a pane attaches to.
func shellTerminal(t *testing.T, script string, w, h int) ports.Terminal {
	t.Helper()
	host := termhost.New(shell.Direct{}, ptyunix.New(), runtimetest.Script{})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	id := fmt.Sprintf("t%d", terminalSeq.Add(1))
	if err := host.Spawn(context.Background(), id, runtimetest.Spec(id, t.TempDir(), script), ports.TermSize{Cols: w, Rows: h}, nil); err != nil {
		t.Fatal(err)
	}
	term, err := host.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	return term
}
