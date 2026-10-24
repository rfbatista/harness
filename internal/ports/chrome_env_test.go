package ports

import (
	"slices"
	"testing"
)

func TestPinChromeEnvKeepsOnlyWhatIsDeclared(t *testing.T) {
	final := []string{"A=1", "HARNESS_SANDBOX=leak", "MAC_CHROMIUM_TMPDIR=/leak", "B=2"}
	got := PinChromeEnv(final, []string{"HARNESS_SANDBOX=seatbelt", "TMPDIR=/x"})
	want := []string{"A=1", "B=2", "HARNESS_SANDBOX=seatbelt"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := PinChromeEnv(final, nil); !slices.Equal(got, []string{"A=1", "B=2"}) {
		t.Errorf("nothing declared: got %v", got)
	}
	if len(final) != 4 || final[1] != "HARNESS_SANDBOX=leak" {
		t.Errorf("input was changed: %v", final)
	}
}
