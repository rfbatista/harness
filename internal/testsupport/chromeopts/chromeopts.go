// Package chromeopts builds the chromedp exec allocator options the harness's
// browser tests share.
package chromeopts

import (
	"os"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/ports"
)

// Options are chromedp's defaults and extra. Inside an agent sandbox (the
// sandbox marker is set, ADR 0024) they also carry --no-sandbox: Chrome's own
// sandbox cannot nest inside the agent's. A non-empty MAC_CHROMIUM_TMPDIR
// counts too: a session started before the server set the marker has it
// alone. Elsewhere Chrome keeps its sandbox.
func Options(extra ...chromedp.ExecAllocatorOption) []chromedp.ExecAllocatorOption {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, extra...)
	if os.Getenv(ports.SandboxMarkerEnv) != "" || os.Getenv(ports.ChromiumTmpdirEnv) != "" {
		opts = append(opts, chromedp.NoSandbox)
	}
	return opts
}
