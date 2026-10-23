// Package keychainpath puts the security shim's directory on an agent
// process's PATH (ADR 0022): right after the edit-proxy shims, before the
// developer bin and the rest.
package keychainpath

import (
	"os"
	"strings"

	"operators-mcp/internal/ports"
)

// Env answers the entries to append to env so the process finds the shim dir
// first after its edit-proxy shims: PATH, and the dir under
// ports.KeychainShimDirEnv for a login shell to put back. env's last PATH is
// the base (the server's own when it has none). No dir, no entries.
func Env(env []string, dir string) []string {
	if dir == "" {
		return nil
	}
	path, shims := os.Getenv("PATH"), ""
	for _, kv := range env {
		switch name, v, _ := strings.Cut(kv, "="); name {
		case "PATH":
			path = v
		case ports.ShimDirEnv:
			shims = v
		}
	}
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, string(os.PathListSeparator))
	}
	at := 0
	if shims != "" && len(parts) > 0 && parts[0] == shims {
		at = 1
	}
	parts = append(parts[:at], append([]string{dir}, parts[at:]...)...)
	return []string{"PATH=" + strings.Join(parts, string(os.PathListSeparator)), ports.KeychainShimDirEnv + "=" + dir}
}
