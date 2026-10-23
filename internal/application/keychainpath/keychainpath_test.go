package keychainpath

import (
	"slices"
	"testing"
)

func TestTheShimDirGoesAfterTheEditProxyShimsAndBeforeTheRest(t *testing.T) {
	env := []string{"A=1", "PATH=/shims:/dev/bin:/usr/bin", "HARNESS_SHIM_DIR=/shims", "PATH=/shims:/dev/bin:/usr/bin:/bin"}
	got := Env(env, "/kc")
	want := []string{"PATH=/shims:/kc:/dev/bin:/usr/bin:/bin", "HARNESS_KEYCHAIN_SHIM_DIR=/kc"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWithoutEditProxyShimsTheShimDirIsFirst(t *testing.T) {
	got := Env([]string{"PATH=/dev/bin:/usr/bin", "HARNESS_SHIM_DIR="}, "/kc")
	if got[0] != "PATH=/kc:/dev/bin:/usr/bin" {
		t.Fatalf("got %v", got)
	}
}

func TestWithNoPathInTheEnvTheInheritedOneFollows(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	if got := Env(nil, "/kc"); got[0] != "PATH=/kc:/usr/bin:/bin" {
		t.Fatalf("got %v", got)
	}
}

func TestNoDirAddsNothing(t *testing.T) {
	if got := Env([]string{"PATH=/x"}, ""); got != nil {
		t.Fatalf("got %v", got)
	}
}
