package app

import (
	"path/filepath"
	"testing"

	"operators-mcp/internal/adapter/out/agents/claudehome"
)

// Token savers run from tools/ in another working directory (a checkout, a
// worktree), so a relative data dir would point nowhere there.
func TestDataDirIsAbsoluteForARelativeDatabasePath(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, db := range []string{"data.db", "var/data.db"} {
		dir, err := dataDir(Config{DBPath: db}, "tools")
		if err != nil {
			t.Fatalf("dataDir(%q): %v", db, err)
		}
		want, _ := filepath.Abs(filepath.Join(filepath.Dir(db), "tools"))
		if dir != want {
			t.Errorf("dataDir(%q) = %q, want %q", db, dir, want)
		}
	}
}

// A named data dir holds the data wherever the database is.
func TestDataDirIsTheNamedOneWhateverTheDatabase(t *testing.T) {
	root := t.TempDir()
	for _, db := range []string{":memory:", "elsewhere/data.db"} {
		cfg := Config{DBPath: db, DataDir: root}
		if dir, err := dataDir(cfg, "tools"); err != nil || dir != filepath.Join(root, "tools") {
			t.Errorf("dataDir with db %q = %q (%v), want %q", db, dir, err, filepath.Join(root, "tools"))
		}
		if dir, err := secretsDir(cfg); err != nil || dir != root {
			t.Errorf("secretsDir with db %q = %q (%v), want %q", db, dir, err, root)
		}
	}
}

// Agents share one Claude home in the data dir; transcripts are read there
// first and then in the person's own ~/.claude, which is never the home.
func TestAgentClaudeHomeIsInTheDataDirAndLegacyIsThePersonsClaudeDir(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dir, err := agentClaudeHomeDir(Config{DataDir: root})
	if err != nil || dir != filepath.Join(root, "claude-home") {
		t.Fatalf("agent home = %q (%v)", dir, err)
	}
	h := claudehome.NewHome(dir)
	tr := newClaudeTranscripts(h)
	if tr.Dir != dir || tr.Legacy != filepath.Join(home, ".claude") {
		t.Errorf("transcripts = %+v", tr)
	}
	if r := newClaudeTranscriptReader(h); r.Dir != dir || r.Legacy != tr.Legacy {
		t.Errorf("reader = %+v", r)
	}
}
