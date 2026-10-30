package app

import (
	"path/filepath"
	"strings"
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

// A token saver's binary dir, which goes on a session's PATH, sits under the
// tools dir the standing allowlist already reads: nothing else is added.
func TestATokenSaversBinaryDirIsReadableUnderTheStandingAllow(t *testing.T) {
	list := sandboxAllowlist(allowSources{Home: "/home/p", ToolsDir: "/data/tools", Getenv: func(string) string { return "" }})
	dir := "/data/tools/rtk/v0.37.0"
	for _, p := range list {
		if rel, err := filepath.Rel(p.Path, dir); err == nil && !strings.HasPrefix(rel, "..") {
			if p.Write {
				t.Errorf("%s is writable", p.Path)
			}
			return
		}
	}
	t.Errorf("%s is not readable under %v", dir, list)
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

func TestTheSandboxAllowlistHoldsUvAndPodmanAtTheirDefaultPlaces(t *testing.T) {
	home := "/home/p"
	list := sandboxAllowlist(allowSources{
		Home:   home,
		Getenv: func(k string) string { return map[string]string{"TMPDIR": "/var/folders/xx/T/"}[k] },
	})
	for path, wantWrite := range map[string]bool{
		filepath.Join(home, ".cache", "uv"):                  true,
		filepath.Join(home, ".local", "share", "uv"):         true,
		filepath.Join(home, ".config", "containers"):         false,
		filepath.Join(home, ".local", "share", "containers"): false,
		"/var/folders/xx/T/podman":                           true,
	} {
		found, write := allowed(list, path)
		if !found || write != wantWrite {
			t.Errorf("%s: found %v write %v, want write %v", path, found, write, wantWrite)
		}
	}
	for _, p := range list {
		if p.Path == filepath.Join(home, ".ssh") || p.Path == home {
			t.Errorf("allowlist reaches %s", p.Path)
		}
	}
}

func TestTheSandboxAllowlistFollowsWhereUvAndPodmanAreMoved(t *testing.T) {
	env := map[string]string{
		"UV_CACHE_DIR": "/w/uvcache", "UV_TOOL_DIR": "/w/uvtools", "UV_PYTHON_INSTALL_DIR": "/w/uvpy",
		"XDG_CACHE_HOME": "/x/cache", "XDG_DATA_HOME": "/x/data", "XDG_CONFIG_HOME": "/x/config",
		"XDG_RUNTIME_DIR": "/run/user/1", "CONTAINERS_CONF": "/w/containers.conf",
	}
	list := sandboxAllowlist(allowSources{Home: "/home/p", Getenv: func(k string) string { return env[k] }})
	for path, wantWrite := range map[string]bool{
		"/w/uvcache": true, "/w/uvtools": true, "/w/uvpy": true,
		"/x/cache/uv": true, "/x/data/uv": true,
		"/x/config/containers": false, "/x/data/containers": false,
		"/w/containers.conf": false, "/run/user/1/podman": true,
	} {
		found, write := allowed(list, path)
		if !found || write != wantWrite {
			t.Errorf("%s: found %v write %v, want write %v", path, found, write, wantWrite)
		}
	}
}
