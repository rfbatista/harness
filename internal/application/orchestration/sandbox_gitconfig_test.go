package orchestration

import (
	"path/filepath"
	"slices"
	"testing"

	"operators-mcp/internal/ports"
)

// The person's git config is read-only in every session mode and one-shot;
// their ssh folder is never a path of the policy.
func TestSandbox_ThePersonsGitConfigIsReadOnlyInEveryModeAndNeverTheirSSHFolder(t *testing.T) {
	for _, mode := range []string{"implement", "design", "", "architect", "product"} {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			home, xdg := t.TempDir(), t.TempDir()
			w.svc.Sandbox.UserHome, w.svc.Sandbox.ConfigHome = home, xdg
			_, p := w.start(t, mode)
			for _, want := range []string{filepath.Join(home, ".gitconfig"), filepath.Join(home, ".config", "git"), filepath.Join(xdg, "git")} {
				if !slices.Contains(p.Readable, want) || slices.Contains(p.Writable, want) {
					t.Errorf("%s is not read-only: readable %v writable %v", want, p.Readable, p.Writable)
				}
			}
			for _, path := range slices.Concat(p.Readable, p.Writable) {
				if path == filepath.Join(home, ".ssh") {
					t.Errorf("policy names ~/.ssh")
				}
			}
		})
	}
}

func TestOneShotWrap_ThePersonsGitConfigIsReadOnlyToo(t *testing.T) {
	sb, d, _ := oneShotSandbox(t)
	home := t.TempDir()
	sb.UserHome = home
	wrap, _ := sb.OneShotWrap(nil)
	if _, err := wrap(commandIn(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	p := d.Calls()[0].Policy
	if !slices.Contains(p.Readable, filepath.Join(home, ".gitconfig")) || !slices.Contains(p.Readable, filepath.Join(home, ".config", "git")) {
		t.Errorf("readable = %v", p.Readable)
	}
}

func TestTheBuiltinAllowlistShowsThePersonsGitConfig(t *testing.T) {
	b := &Sandbox{Home: "/data/agent-home", UserHome: "/home/p", ConfigHome: "/xdg"}
	fixed := builtinEntries(t, b.Builtin(), "Every session")
	for _, path := range []string{"/home/p/.gitconfig", "/home/p/.config/git", "/xdg/git"} {
		if !slices.ContainsFunc(fixed, func(e ports.SandboxBuiltinEntry) bool { return e.Path == path && !e.Write }) {
			t.Errorf("fixed paths %+v lack read-only %s", fixed, path)
		}
	}
}
