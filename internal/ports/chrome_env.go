package ports

import (
	"slices"
	"strings"
)

// SandboxMarkerEnv is the sandbox marker (ADR 0024): in the env of a
// sandboxed agent session it names the sandbox driver. A tool that launches
// Chrome adds --no-sandbox when it is non-empty.
const SandboxMarkerEnv = "HARNESS_SANDBOX"

// ChromiumTmpdirEnv is the env var Chromium on macOS reads for its temp dir
// instead of TMPDIR. A sandboxed seatbelt session carries it, set to its own
// temp dir.
const ChromiumTmpdirEnv = "MAC_CHROMIUM_TMPDIR"

// PlaywrightSocketsEnv is the env var Playwright reads for the directory of
// its unix sockets. The browser MCP server gets it in its own env, under
// seatbelt, set to the session's temp dir (ADR 0025).
const PlaywrightSocketsEnv = "PWTEST_SOCKETS_DIR"

// PinChromeEnv answers final with the two sandbox-marker variables exactly as
// declared says: whatever final carries of them (a server env that leaked in)
// is dropped, then declared's entries are added; the browser server's own
// env is set apart, so nothing of PWTEST_SOCKETS_DIR reaches claude from the
// server env either. A variable declared does not
// name is absent from the answer, not empty: Chromium checks that
// MAC_CHROMIUM_TMPDIR is set, not that it has a value.
func PinChromeEnv(final, declared []string) []string {
	out := slices.DeleteFunc(slices.Clone(final), isChromeEnv)
	for _, e := range declared {
		if isChromeEnv(e) {
			out = append(out, e)
		}
	}
	return out
}

func isChromeEnv(e string) bool {
	return strings.HasPrefix(e, SandboxMarkerEnv+"=") || strings.HasPrefix(e, ChromiumTmpdirEnv+"=") || strings.HasPrefix(e, PlaywrightSocketsEnv+"=")
}
