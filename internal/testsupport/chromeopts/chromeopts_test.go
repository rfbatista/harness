package chromeopts

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The options are opaque funcs, so the test counts them; that Chrome really
// starts with them is proved by the browser tests run in an agent sandbox.
func TestOptionsAddNoSandboxOnlyUnderTheMarker(t *testing.T) {
	base := len(chromedp.DefaultExecAllocatorOptions)
	t.Setenv("HARNESS_SANDBOX", "")
	t.Setenv("MAC_CHROMIUM_TMPDIR", "")
	if got := len(Options()); got != base {
		t.Errorf("no marker: %d options, want the %d defaults", got, base)
	}
	if got := len(Options(chromedp.Flag("a", true), chromedp.Flag("b", true))); got != base+2 {
		t.Errorf("no marker, two extras: %d options, want %d", got, base+2)
	}
	t.Setenv("HARNESS_SANDBOX", "seatbelt")
	if got := len(Options()); got != base+1 {
		t.Errorf("marker: %d options, want %d (the defaults and NoSandbox)", got, base+1)
	}
	if got := len(Options(chromedp.Flag("a", true))); got != base+2 {
		t.Errorf("marker and an extra: %d options, want %d", got, base+2)
	}
	t.Setenv("HARNESS_SANDBOX", "")
	t.Setenv("MAC_CHROMIUM_TMPDIR", "/tmp/x")
	if got := len(Options()); got != base+1 {
		t.Errorf("MAC_CHROMIUM_TMPDIR alone: %d options, want %d", got, base+1)
	}
}
