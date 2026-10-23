package app

import (
	"path/filepath"
	"testing"

	"operators-mcp/internal/ports"
)

func allowed(list []ports.SandboxPath, path string) (found, write bool) {
	for _, p := range list {
		if p.Path == path {
			return true, p.Write
		}
	}
	return false, false
}

func TestTheSandboxAllowlistHoldsToolchainsCachesTokenSaversAndClaudesInstall(t *testing.T) {
	home := "/home/p"
	list := sandboxAllowlist(allowSources{
		Home: home, ToolsDir: "/data/tools", ClaudeDirs: []string{"/data/claude"},
		Getenv: func(string) string { return "" },
	})
	for path, wantWrite := range map[string]bool{
		"/data/tools":                                 false,
		"/data/claude":                                false,
		filepath.Join(home, "go"):                     true,
		filepath.Join(home, ".npm"):                   true,
		filepath.Join(home, ".cargo"):                 true,
		filepath.Join(home, ".cache", "pip"):          true,
		filepath.Join(home, ".config", "rtk"):         true,
		filepath.Join(home, ".local", "share", "rtk"): true,
		filepath.Join(home, ".rustup"):                false,
	} {
		found, write := allowed(list, path)
		if !found || write != wantWrite {
			t.Errorf("%s: found %v write %v, want write %v", path, found, write, wantWrite)
		}
	}
}

func TestTheSandboxAllowlistFollowsTheToolchainEnvAndNeverReachesTheClaudeHomeOfThePerson(t *testing.T) {
	home := "/home/p"
	list := sandboxAllowlist(allowSources{
		Home: home,
		Getenv: func(k string) string {
			return map[string]string{"GOPATH": "/work/gopath", "GOCACHE": "/work/gocache"}[k]
		},
	})
	for _, p := range []string{"/work/gopath", "/work/gocache"} {
		if f, w := allowed(list, p); !f || !w {
			t.Errorf("%s not allowed read-write", p)
		}
	}
	for _, p := range list {
		if p.Path == filepath.Join(home, ".claude") || p.Path == filepath.Join(home, ".claude.json") || p.Path == home {
			t.Errorf("allowlist reaches the person's Claude state: %s", p.Path)
		}
	}
}
