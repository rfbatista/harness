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
	session := func(mode domain.SessionMode) ports.SandboxPolicy {
		return BuildSandboxPolicy(SandboxSession{
			Mode: mode, Worktree: worktree, GitCommonDir: gitDir, Repositories: []string{repos},
			VirtualShell: shell, TempDir: temp, AgentHome: b.Home,
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

	fixed := entries(session(domain.SessionModeImplement), map[string]string{
		b.Home: "Agent Claude home",
		shell:  "The session's virtual shell",
		temp:   "Temp dir",
	}, "")
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
		modes = append(modes, entries(session(m.mode), map[string]string{
			worktree: "own worktree",
			gitDir:   "its git directory",
			repos:    "every repository of the project",
		}, m.title)...)
	}

	var standing []ports.SandboxBuiltinEntry
	if b.Allow != nil {
		for _, p := range b.Allow("") {
			standing = append(standing, ports.SandboxBuiltinEntry{Path: p.Path, Write: p.Write})
		}
	}
	return []ports.SandboxBuiltinGroup{
		{Title: "Every session", Entries: fixed},
		{Title: "Repositories by mode", Entries: modes},
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
