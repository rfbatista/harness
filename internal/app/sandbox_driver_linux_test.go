//go:build linux

package app

import "testing"

func TestLinuxRegistersTheBubblewrapDriverOrNoneWithoutBwrap(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if d := platformSandboxDriver(); d != nil {
		t.Fatalf("driver without bwrap = %v", d)
	}
}
