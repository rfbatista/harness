package orchestration

import (
	"os"
	"path/filepath"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Builtin is the allowlist a session gets without the person adding anything,
// for the Agent isolation page. It reads the same fields a session start
// reads and runs the same BuildSandboxPolicy, so what the page lists cannot
// drift from what the policy holds: the fixed paths, the repositories each
// mode sees, and the standing toolchain and cache allowlist. The paths a
// session gets from its own env (its PATH shims and git material) are made
// per session and are not listed.
func (b *Sandbox) Builtin() []ports.SandboxBuiltinGroup {
	if b == nil {
		return nil
	}
	const (
		shell    = "<the session's virtual shell>"
		worktree = "<the session's own worktree>"
		gitDir   = "<the git directory it commits into>"
		repos    = "<every repository of the project>"
	)
	temp := os.TempDir()
	if b.TempDir != nil {
		temp = b.TempDir("")
	}
	temp = filepath.Clean(temp)
	session := func(mode domain.SessionMode, readAll bool) ports.SandboxPolicy {
		return BuildSandboxPolicy(SandboxSession{
			Mode: mode, Worktree: worktree, GitCommonDir: gitDir, Repositories: []string{repos}, ReadAllRepos: readAll,
			VirtualShell: shell, TempDir: temp, AgentHome: b.Home, UserHome: b.UserHome, ConfigHome: b.ConfigHome,
		})
	}
	entries := func(p ports.SandboxPolicy, labels map[string]string, prefix string) []ports.SandboxBuiltinEntry {
		var out []ports.SandboxBuiltinEntry
		for _, path := range slices.Concat(p.Writable, p.Readable) {
			if label, ok := labels[path]; ok {
				out = append(out, ports.SandboxBuiltinEntry{Label: prefix + label, Path: pathOf(label, path), Write: slices.Contains(p.Writable, path)})
			}
		}
		return out
	}

	labels := map[string]string{
		b.Home: "Agent Claude home",
		shell:  "The session's virtual shell",
		temp:   "Temp dir",
	}
	for _, p := range gitConfigPaths(b.UserHome, b.ConfigHome) {
		labels[p] = "Your git config"
	}
	fixed := entries(session(domain.SessionModeImplement, false), labels, "")
	byMode := []struct {
		title string
		mode  domain.SessionMode
	}{
		{"Implement, design and default sessions: ", domain.SessionModeImplement},
		{"Architect sessions: ", domain.SessionModeArchitect},
		{"Product sessions: ", domain.SessionModeProduct},
	}
	var modes []ports.SandboxBuiltinEntry
	for _, m := range byMode {
		modes = append(modes, entries(session(m.mode, false), map[string]string{
			worktree: "own worktree",
			gitDir:   "its git directory",
			repos:    "every repository of the project",
		}, m.title)...)
	}

	// With the project's setting on (the default), every mode and every one-shot
	// also reads the main checkout of every repository, the session's own
	// included; the builder grants it, this lists what it grants.
	var readAll []ports.SandboxBuiltinEntry
	for _, m := range byMode {
		readAll = append(readAll, entries(session(m.mode, true), map[string]string{
			repos: "every repository of the project, its own checkout included",
		}, m.title)...)
	}
	oneShot := BuildSandboxPolicy(SandboxSession{OneShot: true, Repositories: []string{repos}, ReadAllRepos: true, TempDir: temp})
	readAll = append(readAll, entries(oneShot, map[string]string{
		repos: "every repository of the project, its own checkout included",
	}, "One-shot calls: ")...)

	var standing []ports.SandboxBuiltinEntry
	if b.Allow != nil {
		for _, p := range b.Allow("") {
			standing = append(standing, ports.SandboxBuiltinEntry{Path: p.Path, Write: p.Write})
		}
	}
	return []ports.SandboxBuiltinGroup{
		{Title: "Every session", Entries: fixed},
		{Title: "Repositories by mode", Entries: modes},
		{Title: "Repositories when agents can read every repository (default)", Entries: readAll},
		{Title: "Tools, toolchains and caches", Entries: standing},
	}
}

// pathOf is the path to show for an entry: the real one, or none when the
// label names something that is per session.
func pathOf(label, path string) string {
	if label == "The session's virtual shell" || path == "" || path[0] == '<' {
		return ""
	}
	return path
}
