package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// pathWorld is what a session's PATH is built from in these tests.
type pathWorld struct {
	shims, saver, kc, dev string // shims is filled from the session's env
	rtk                   string
}

// wantOrder checks path is the edit-proxy shim dir first, then rtk's dir,
// the keychain shim dir, the developer bin, then the server's own PATH.
func (p pathWorld) wantOrder(t *testing.T, path string) {
	t.Helper()
	parts := filepath.SplitList(path)
	head := []string{p.saver, p.kc, p.dev}
	if len(parts) < 1+len(head) {
		t.Fatalf("PATH %q is too short", path)
	}
	if _, err := os.Readlink(filepath.Join(parts[0], "sed")); err != nil {
		t.Errorf("PATH %q: first entry %q is not the edit-proxy shim dir", path, parts[0])
	}
	if !slices.Equal(parts[1:1+len(head)], head) {
		t.Errorf("PATH %q: after the shims want %v", path, head)
	}
	if rest := strings.Join(parts[1+len(head):], string(os.PathListSeparator)); rest != os.Getenv("PATH") {
		t.Errorf("PATH %q: the rest is %q, want the server's %q", path, rest, os.Getenv("PATH"))
	}
}

// newPathWorld sets svc up with shims (the test binary stands in for the
// shim binary), a keychain shim dir, a developer bin and rtk installed and
// on for p1.
func newPathWorld(t *testing.T, svc *Service) pathWorld {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := pathWorld{kc: t.TempDir(), dev: "/dev/bin/usr/bin"}
	svc.ShimBinary, svc.KeychainShimDir = bin, p.kc
	svc.DeveloperBin = func() (string, error) { return p.dev, nil }
	savers := withTokenSavers(t, svc)
	p.rtk = savers.install(t, domain.TokenSaverRTK, "rtk")
	p.saver = filepath.Dir(p.rtk)
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverRTK, domain.ActivationOn)
	return p
}

func pathEntries(env []string) []string {
	var out []string
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return out
}

func TestAHeadlessSessionKeepsRTKOnItsPathBehindTheShims(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	p := newPathWorld(t, svc)
	_, launch := startHeadless(t, svc, w.task.ID)
	p.wantOrder(t, launch.Path)
	// The CLI process itself finds rtk in its installed place and tee and
	// sed in the shim dir, not in the saver's.
	if launch.Resolved["rtk"] != p.rtk {
		t.Errorf("command -v rtk = %q, want %q", launch.Resolved["rtk"], p.rtk)
	}
	for _, name := range []string{"tee", "sed"} {
		if got := filepath.Dir(launch.Resolved[name]); got == p.saver {
			t.Errorf("command -v %s resolves into the saver dir", name)
		}
	}
}

func TestAnInteractiveSessionKeepsOnePathWithRTKBehindTheShimsAndAResumeToo(t *testing.T) {
	svc, _ := newInteractiveService(t)
	w := withShells(t, svc)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	p := newPathWorld(t, svc)
	ctx := context.Background()

	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	if got := pathEntries(launch.Spec.Env); len(got) != 1 {
		t.Fatalf("PATH entries = %v, want exactly one", got)
	}
	p.wantOrder(t, specEnv(launch.Spec)["PATH"])
	if got := specEnv(launch.Spec)[ports.SaverBinDirsEnv]; got != p.saver {
		t.Errorf("%s = %q, want %q", ports.SaverBinDirsEnv, got, p.saver)
	}

	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.EndInteractive(ctx, sess.ID, 0, false) })
	if got := pathEntries(spec.Env); len(got) != 1 {
		t.Fatalf("resumed PATH entries = %v, want exactly one", got)
	}
	p.wantOrder(t, specEnv(spec)["PATH"])
}

func TestASessionWithoutShimsKeepsRTKFirstAndOnePath(t *testing.T) {
	svc, _ := newInteractiveService(t)
	w := withShells(t, svc)
	p := newPathWorld(t, svc)
	svc.ShimBinary = ""
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	if got := pathEntries(launch.Spec.Env); len(got) != 1 {
		t.Fatalf("PATH entries = %v, want exactly one", got)
	}
	parts := filepath.SplitList(specEnv(launch.Spec)["PATH"])
	if !slices.Equal(parts[:3], []string{p.saver, p.kc, p.dev}) {
		t.Errorf("PATH = %v, want saver, keychain shim, developer bin first", parts)
	}
}

func TestRTKIsOnThePathWithTheSandboxOnAndOff(t *testing.T) {
	for _, off := range []bool{false, true} {
		w := newSandboxWorld(t)
		w.off = off
		w.svc.Shells = &fakeShells{dir: t.TempDir()}
		p := newPathWorld(t, w.svc)
		_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", RunsOn: domain.RunnerTUI})
		if err != nil {
			t.Fatal(err)
		}
		if got := pathEntries(spec.Env); len(got) != 1 {
			t.Fatalf("off=%v: PATH entries = %v, want exactly one", off, got)
		}
		p.wantOrder(t, specEnv(spec)["PATH"])
	}
}

func TestASaverDirHoldingTeeOrSedNeverShadowsTheShims(t *testing.T) {
	svc, _ := newInteractiveService(t)
	w := withShells(t, svc)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	p := newPathWorld(t, svc)
	for _, name := range ports.ShellShimCommands {
		if err := os.WriteFile(filepath.Join(p.saver, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	parts := filepath.SplitList(specEnv(launch.Spec)["PATH"])
	shims := specEnv(launch.Spec)[ports.ShimDirEnv]
	if i, j := slices.Index(parts, shims), slices.Index(parts, p.saver); i != 0 || j <= i {
		t.Errorf("PATH = %v: shim dir at %d, saver dir at %d, want the shims first", parts, i, j)
	}
}

func TestWithoutAnActiveSaverThePathIsWhatItWas(t *testing.T) {
	svc, _ := newInteractiveService(t)
	w := withShells(t, svc)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	p := newPathWorld(t, svc)
	svc.TokenSavers = nil
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	if got := pathEntries(launch.Spec.Env); len(got) != 1 {
		t.Fatalf("PATH entries = %v, want exactly one", got)
	}
	parts := filepath.SplitList(specEnv(launch.Spec)["PATH"])
	if !slices.Equal(parts[1:3], []string{p.kc, p.dev}) || slices.Contains(parts, p.saver) {
		t.Errorf("PATH = %v", parts)
	}
	if _, ok := specEnv(launch.Spec)[ports.SaverBinDirsEnv]; ok {
		t.Error("the saver dirs variable is set without a saver")
	}
}

// A login shell whose profile resets PATH still finds rtk where it is
// installed, right behind the shims: command -v in the session's own
// process resolves rtk to the saver dir and tee and sed to the shim dir.
func TestALoginShellKeepsRTKOnThePathBehindTheShims(t *testing.T) {
	svc := newLoginHostedService(t, scriptAgent{script: `printf '%s' "$PATH" > path; command -v rtk > rtk; command -v tee > tee; command -v sed > sed; printf 'ready\r\n'; cat`})
	w := withShells(t, svc)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	p := newPathWorld(t, svc)
	for _, name := range ports.ShellShimCommands {
		if err := os.WriteFile(filepath.Join(p.saver, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID,
		RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 80, Rows: 24}})
	waitFile(t, filepath.Join(sess.WorkingDir, "sed"))
	read := func(name string) string {
		return strings.TrimSpace(string(waitFile(t, filepath.Join(sess.WorkingDir, name))))
	}
	parts := filepath.SplitList(read("path"))
	if len(parts) < 4 || parts[1] != p.saver || parts[2] != p.kc || parts[3] != p.dev {
		t.Errorf("PATH = %v, want the shims, rtk's dir, the keychain shim, the developer bin first", parts[:min(len(parts), 4)])
	}
	shims := specEnv(launch.Spec)[ports.ShimDirEnv]
	if got := read("rtk"); got != p.rtk {
		t.Errorf("command -v rtk = %q, want %q", got, p.rtk)
	}
	for _, name := range []string{"tee", "sed"} {
		if got := read(name); got != filepath.Join(shims, name) {
			t.Errorf("command -v %s = %q, want the shim in %q", name, got, shims)
		}
	}
	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
}
