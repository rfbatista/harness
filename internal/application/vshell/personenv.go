package vshell

import (
	"path/filepath"
	"slices"
	"strings"

	"operators-mcp/internal/ports"
)

// personEnvRemoved is the one list of what a person's terminal does not get
// from the server's environment (ADR 0052), besides every HARNESS_* and
// CODING_POOL_* variable and the harness's shim directories on PATH: the
// variables the harness sets for agents. A variable the agent start code adds
// goes here too; a test fails until it does.
var personEnvRemoved = []string{
	"CLAUDE_CONFIG_DIR",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"ANTHROPIC_API_KEY",
	"DISABLE_AUTOUPDATER",
	ports.ChromiumTmpdirEnv,
	ports.PlaywrightSocketsEnv,
	"TERM",
}

// personEnvPrefixes are the families of variables the harness owns.
var personEnvPrefixes = []string{"HARNESS_", "CODING_POOL_"}

// shimDirVars name, in the server's environment, directories (or PATH-style
// lists of them) the harness put on PATH.
var shimDirVars = []string{ports.ShimDirEnv, ports.SaverBinDirsEnv, ports.KeychainShimDirEnv, ports.DeveloperBinEnv}

// shimDirPatterns match the directories the harness makes for its shims, for a
// PATH that carries one whose variable is not set.
var shimDirPatterns = []string{"harness-shims-*", "harness-keychain-shim-*"}

// PersonEnv is the environment of a person's terminal: the server's own with
// the harness's entries removed, and TERM set for the terminal. Variables the
// person's tools expect, SSH_AUTH_SOCK and LANG among them, stay; the login
// shell rebuilds PATH from the person's dotfiles.
func PersonEnv(server []string) []string {
	var shims []string
	for _, e := range server {
		name, v, _ := strings.Cut(e, "=")
		if slices.Contains(shimDirVars, name) {
			shims = append(shims, filepath.SplitList(v)...)
		}
	}
	out := make([]string, 0, len(server)+1)
	for _, e := range server {
		name, v, _ := strings.Cut(e, "=")
		switch {
		case slices.Contains(personEnvRemoved, name), hasAnyPrefix(name, personEnvPrefixes):
		case name == "PATH":
			out = append(out, "PATH="+strings.Join(withoutShimDirs(filepath.SplitList(v), shims), string(filepath.ListSeparator)))
		default:
			out = append(out, e)
		}
	}
	return append(out, "TERM=xterm-256color")
}

func hasAnyPrefix(s string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(s, p) })
}

func withoutShimDirs(path, shims []string) []string {
	return slices.DeleteFunc(slices.Clone(path), func(dir string) bool {
		if slices.Contains(shims, filepath.Clean(dir)) {
			return true
		}
		base := filepath.Base(dir)
		return slices.ContainsFunc(shimDirPatterns, func(p string) bool { ok, _ := filepath.Match(p, base); return ok })
	})
}
