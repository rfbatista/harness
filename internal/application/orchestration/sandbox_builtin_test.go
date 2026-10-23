package orchestration

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func builtinEntries(t *testing.T, groups []ports.SandboxBuiltinGroup, title string) []ports.SandboxBuiltinEntry {
	t.Helper()
	for _, g := range groups {
		if g.Title == title {
			return g.Entries
		}
	}
	t.Fatalf("no group %q in %+v", title, groups)
	return nil
}

func TestTheBuiltinAllowlistListsTheFixedPathsAndTheStandingAllowlist(t *testing.T) {
	b := &Sandbox{Home: "/data/agent-home", Allow: func(string) []ports.SandboxPath {
		return []ports.SandboxPath{{Path: "/home/p/go", Write: true}, {Path: "/home/p/.rustup"}}
	}}
	groups := b.Builtin()

	fixed := builtinEntries(t, groups, "Every session")
	want := []ports.SandboxBuiltinEntry{
		{Label: "Agent Claude home", Path: "/data/agent-home", Write: true},
		{Label: "The session's virtual shell", Write: true},
		{Label: "Temp dir", Path: filepath.Clean(os.TempDir()), Write: true},
	}
	for _, w := range want {
		if !slices.Contains(fixed, w) {
			t.Errorf("fixed paths %+v lack %+v", fixed, w)
		}
	}

	standing := builtinEntries(t, groups, "Tools, toolchains and caches")
	if len(standing) != 2 || standing[0].Path != "/home/p/go" || !standing[0].Write || standing[1].Path != "/home/p/.rustup" || standing[1].Write {
		t.Errorf("standing = %+v", standing)
	}
}

func TestTheBuiltinAllowlistNamesWhatEachModeSeesAsTheBuilderDecidesIt(t *testing.T) {
	b := &Sandbox{Home: "/data/agent-home"}
	modes := builtinEntries(t, b.Builtin(), "Repositories by mode")
	access := func(prefix, what string) string {
		for _, e := range modes {
			if strings.HasPrefix(e.Label, prefix) && strings.HasSuffix(e.Label, what) {
				if e.Write {
					return "write"
				}
				return "read"
			}
		}
		return "none"
	}
	for _, c := range []struct {
		mode               domain.SessionMode
		prefix             string
		worktree, git, all string
	}{
		{domain.SessionModeImplement, "Implement", "write", "write", "none"},
		{domain.SessionModeArchitect, "Architect", "write", "write", "read"},
		{domain.SessionModeProduct, "Product", "read", "none", "read"},
	} {
		// What the builder does with real paths.
		p := BuildSandboxPolicy(SandboxSession{Mode: c.mode, Worktree: "/w", GitCommonDir: "/g", Repositories: []string{"/r"}})
		real := func(path string) string {
			switch {
			case slices.Contains(p.Writable, path):
				return "write"
			case slices.Contains(p.Readable, path):
				return "read"
			}
			return "none"
		}
		for what, path := range map[string]string{"own worktree": "/w", "its git directory": "/g", "every repository of the project": "/r"} {
			if got := access(c.prefix, what); got != real(path) {
				t.Errorf("%v %s: the page says %s, the policy gives %s", c.mode, what, got, real(path))
			}
		}
	}
}

func TestTheBuiltinAllowlistOfNoSandboxIsEmpty(t *testing.T) {
	var b *Sandbox
	if got := b.Builtin(); got != nil {
		t.Fatalf("Builtin = %+v", got)
	}
}
