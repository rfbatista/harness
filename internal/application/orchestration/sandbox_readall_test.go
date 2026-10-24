package orchestration

import (
	"path/filepath"
	"slices"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// startWithReadAll starts a session of mode under the setting.
func (w *sandboxWorld) startWithReadAll(t *testing.T, mode string, on bool) (*domain.Session, ports.SandboxPolicy) {
	t.Helper()
	w.readAll = on
	return w.start(t, mode)
}

func TestReadAllRepos_EveryModeReadsEveryCheckoutOwnIncludedAndWritesOnlyItsOwn(t *testing.T) {
	for _, mode := range []string{"implement", "design", "", "architect", "product"} {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			w.ownTempDir()
			common := w.linkWorktree("a")
			sess, p := w.startWithReadAll(t, mode, true)
			for _, id := range []string{"r1", "r2", "r3"} {
				if !slices.Contains(p.Readable, w.repos[id]) {
					t.Errorf("%s not readable: %v", id, p.Readable)
				}
				if slices.Contains(p.Writable, w.repos[id]) || slices.Contains(p.Hidden, w.repos[id]) {
					t.Errorf("%s writable or hidden: %v / %v", id, p.Writable, p.Hidden)
				}
			}
			if mode != "product" && !slices.Contains(p.Writable, sess.WorkingDir) {
				t.Errorf("own worktree not writable: %v", p.Writable)
			}
			if mode != "product" && !slices.Contains(p.Writable, common) {
				t.Errorf("git dir not writable: %v", p.Writable)
			}
			// The setting adds nothing writable: the same paths as with it off.
			w2 := newSandboxWorld(t)
			w2.ownTempDir()
			w2.linkWorktree("a")
			_, off := w2.startWithReadAll(t, mode, false)
			if len(p.Writable) != len(off.Writable) {
				t.Errorf("writable on %v, off %v", p.Writable, off.Writable)
			}
			if !slices.Contains(p.Hidden, w.wsRoot) {
				t.Errorf("workspaces root not hidden: %v", p.Hidden)
			}
			if mode != "product" && slices.Contains(p.Readable, filepath.Join(common, "worktrees", "own")) {
				t.Errorf("own entry read-only")
			}
			if mode != "product" && !slices.Contains(p.Readable, filepath.Join(common, "worktrees", "a")) {
				t.Errorf("other worktree's entry not read-only: %v", p.Readable)
			}
			if got := w.svc.sessions.Get(sess.ID).Sandbox; got == nil || got.Visibility != domain.SandboxAllReposReadOnly {
				t.Errorf("facts = %+v", got)
			}
		})
	}
}

func TestReadAllRepos_OffTheOwnCheckoutAndTheOthersStayHiddenAsBefore(t *testing.T) {
	for _, mode := range []string{"implement", "design", ""} {
		w := newSandboxWorld(t)
		w.ownTempDir()
		_, off := w.startWithReadAll(t, mode, false)
		for _, id := range []string{"r1", "r2", "r3"} {
			if slices.Contains(off.Readable, w.repos[id]) || !slices.Contains(off.Hidden, w.repos[id]) {
				t.Errorf("%q off: %s readable or not hidden: %v / %v", mode, id, off.Readable, off.Hidden)
			}
		}
		w2 := newSandboxWorld(t)
		w2.ownTempDir()
		_, on := w2.startWithReadAll(t, mode, true)
		if len(on.Readable) != len(off.Readable)+3 {
			t.Errorf("%q: on reads %v, off %v: the setting adds exactly the three checkouts", mode, on.Readable, off.Readable)
		}
	}
}

func TestReadAllRepos_ASessionStartedAfterTheSettingChangesFollowsIt(t *testing.T) {
	w := newSandboxWorld(t)
	w.svc.Transcripts = fakeTranscripts{}
	sess, p := w.startWithReadAll(t, "implement", false)
	if slices.Contains(p.Readable, w.repos["r2"]) {
		t.Fatal("off: r2 readable")
	}
	if _, err := w.svc.EndInteractive(t.Context(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	w.readAll = true // a resume starts under the current rules
	_, spec, err := w.svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(spec.Sandbox.Readable, w.repos["r2"]) {
		t.Errorf("resume after the switch: %v", spec.Sandbox.Readable)
	}
}

func TestReadAllRepos_TheHookGetsTheSessionsProjectAndUnsetMeansOn(t *testing.T) {
	w := newSandboxWorld(t)
	var asked []string
	w.svc.Sandbox.ReadAllRepos = func(id string) bool { asked = append(asked, id); return false }
	w.start(t, "implement")
	if !slices.Equal(asked, []string{"p1"}) {
		t.Errorf("asked for %v", asked)
	}

	w = newSandboxWorld(t)
	w.svc.Sandbox.ReadAllRepos = nil
	_, p := w.start(t, "implement")
	if !slices.Contains(p.Readable, w.repos["r2"]) {
		t.Errorf("no hook is on: %v", p.Readable)
	}
}

func TestReadAllRepos_AOneShotReadsEveryCheckoutOnlyWithTheSettingOn(t *testing.T) {
	checkouts := []string{"/opt/a", "/Volumes/x/b", "/home/p/c"}
	for _, on := range []bool{true, false} {
		w := newSandboxWorld(t)
		w.readAll = on
		w.wsRoot = "/Volumes/ws"
		w.svc.Sandbox.Checkouts = func() []string { return checkouts }
		wrap, err := w.svc.Sandbox.OneShotWrap(nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := wrap(commandIn("/tmp")); err != nil {
			t.Fatal(err)
		}
		p := w.driver.Calls()[0].Policy
		for _, c := range checkouts {
			if got := slices.Contains(p.Readable, c); got != on {
				t.Errorf("on=%v: %s readable = %v", on, c, got)
			}
			if got := slices.Contains(p.Hidden, c); got == on {
				t.Errorf("on=%v: %s hidden = %v", on, c, got)
			}
			if slices.Contains(p.Writable, c) {
				t.Errorf("%s writable to a one-shot", c)
			}
		}
		if !slices.Contains(p.Hidden, "/Volumes/ws") {
			t.Errorf("workspaces root not hidden: %v", p.Hidden)
		}
	}
}

func TestBuildSandboxPolicy_ReadAllReposCoversRepositoriesUnderAndOutsideTheHome(t *testing.T) {
	s := SandboxSession{
		Mode: domain.SessionModeImplement, Worktree: "/ws/r/wt", GitCommonDir: "/home/p/r/.git", ReadAllRepos: true,
		Repositories: []string{"/home/p/r", "/home/p/o", "/Volumes/ext/x", "/opt/y"}, WorkspacesRoot: "/ws",
		UserHome: "/home/p",
	}
	p := BuildSandboxPolicy(s)
	for _, r := range s.Repositories {
		if !slices.Contains(p.Readable, r) || slices.Contains(p.Hidden, r) || slices.Contains(p.Writable, r) {
			t.Errorf("%s: readable %v hidden %v writable %v", r, p.Readable, p.Hidden, p.Writable)
		}
	}
	if !slices.Equal(p.Hidden, []string{"/ws"}) {
		t.Errorf("hidden = %v, want only the workspaces root", p.Hidden)
	}
	if !slices.Contains(p.Writable, "/home/p/r/.git") || !slices.Contains(p.Writable, "/ws/r/wt") {
		t.Errorf("writable = %v", p.Writable)
	}
	s.ReadAllRepos = false
	off := BuildSandboxPolicy(s)
	if !slices.Equal(off.Hidden, []string{"/home/p/r", "/home/p/o", "/Volumes/ext/x", "/opt/y", "/ws"}) || slices.Contains(off.Readable, "/home/p/r") || slices.Contains(off.Readable, "/opt/y") {
		t.Errorf("off: hidden %v readable %v", off.Hidden, off.Readable)
	}
}

func TestReadAllRepos_FactsOffRecordTheModesOwnVisibilityAndOnRecordAllRepos(t *testing.T) {
	own := map[string]string{"implement": domain.SandboxOwnRepo, "design": domain.SandboxOwnRepo, "": domain.SandboxOwnRepo, "architect": domain.SandboxAllReposReadOnly, "product": domain.SandboxAllReposReadOnly}
	for mode, want := range own {
		w := newSandboxWorld(t)
		sess, _ := w.startWithReadAll(t, mode, false)
		if got := w.svc.sessions.Get(sess.ID).Sandbox; got == nil || got.Visibility != want {
			t.Errorf("%q off: facts %+v, want %q", mode, got, want)
		}
	}
}

func TestReadAllRepos_AResumeAfterAChangeRecordsTheNewValueAndARunningSessionKeepsItsRecord(t *testing.T) {
	w := newSandboxWorld(t)
	w.svc.Transcripts = fakeTranscripts{}
	sess, _ := w.startWithReadAll(t, "implement", false)
	w.readAll = true // a running session keeps what it recorded
	if got := w.svc.sessions.Get(sess.ID).Sandbox; got == nil || got.Visibility != domain.SandboxOwnRepo {
		t.Fatalf("running session's facts changed: %+v", got)
	}
	if _, err := w.svc.EndInteractive(t.Context(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	if got := w.svc.sessions.Get(sess.ID).Sandbox; got == nil || got.Visibility != domain.SandboxAllReposReadOnly {
		t.Errorf("after resuming with the setting on: %+v", got)
	}
	if _, err := w.svc.EndInteractive(t.Context(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	w.readAll = false
	if _, _, err := w.svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	if got := w.svc.sessions.Get(sess.ID).Sandbox; got == nil || got.Visibility != domain.SandboxOwnRepo {
		t.Errorf("after resuming with the setting off: %+v", got)
	}
}
