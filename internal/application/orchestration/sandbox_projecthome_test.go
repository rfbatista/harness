package orchestration

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// projectHomes makes the folder that holds the project Claude homes and the
// homes named, and tells the sandbox about it.
func (w *sandboxWorld) projectHomes(t *testing.T, ids ...string) string {
	t.Helper()
	root := t.TempDir()
	// The folder must not lie inside the temp dir a session is granted, as a
	// folder under the test's own temp dir would.
	t.Setenv("TMPDIR", t.TempDir())
	for _, id := range ids {
		if err := os.MkdirAll(filepath.Join(root, id), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w.svc.Sandbox.ProjectHomesRoot = root
	return root
}

func TestSandbox_ASessionReachesItsOwnProjectClaudeHomeAndNoOtherProjects(t *testing.T) {
	for _, mode := range []string{"implement", "design", "", "architect", "product"} {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			root := w.projectHomes(t, "p1", "p2")
			_, p := w.start(t, mode)

			own := filepath.Join(root, "p1")
			if !slices.Contains(p.Writable, own) {
				t.Errorf("own project Claude home %s not writable: %v", own, p.Writable)
			}
			other := filepath.Join(root, "p2")
			for _, list := range [][]string{p.Readable, p.Writable} {
				if slices.Contains(list, other) || slices.Contains(list, root) {
					t.Errorf("another project's Claude home is granted: %v", list)
				}
			}
			if !slices.Contains(p.Hidden, root) {
				t.Errorf("the project Claude homes folder %s is not hidden: %v", root, p.Hidden)
			}
		})
	}
}

func TestSandbox_AProjectWithoutAProjectClaudeHomeGetsTheSamePolicyAsToday(t *testing.T) {
	w := newSandboxWorld(t)
	_, before := w.start(t, "implement")

	w = newSandboxWorld(t)
	w.svc.Sandbox.ProjectHomesRoot = filepath.Join(t.TempDir(), "claude-homes")
	_, after := w.start(t, "implement")

	if len(after.Hidden) != len(before.Hidden) || len(after.Writable) != len(before.Writable) || len(after.Readable) != len(before.Readable) {
		t.Errorf("policy changed without a project Claude home: before %+v after %+v", before, after)
	}
}
