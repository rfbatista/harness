package bwrap

import (
	"errors"
	"slices"
	"testing"

	"operators-mcp/internal/ports"
)

// pairs finds the consecutive argv items flag, a, b.
func has(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		if slices.Equal(args[i:i+len(seq)], seq) {
			return true
		}
	}
	return false
}

func index(args []string, seq ...string) int {
	for i := 0; i+len(seq) <= len(args); i++ {
		if slices.Equal(args[i:i+len(seq)], seq) {
			return i
		}
	}
	return -1
}

var policy = ports.SandboxPolicy{
	WorkingDir: "/home/me/wt",
	Readable:   []string{"/home/me/repos/other"},
	Writable:   []string{"/home/me/wt", "/home/me/repos/proj/.git", "/home/me/wt/sub"},
}

func TestRender_SystemDirsAreReadOnlyAndTheNetworkIsShared(t *testing.T) {
	args := Render(policy, "/home/me")
	if !has(args, "--ro-bind-try", "/usr", "/usr") {
		t.Fatalf("/usr not read-only bound: %v", args)
	}
	for _, a := range []string{"--unshare-net", "--unshare-all"} {
		if slices.Contains(args, a) {
			t.Fatalf("network must stay shared, got %s", a)
		}
	}
	for _, want := range []string{"--die-with-parent"} {
		if !slices.Contains(args, want) {
			t.Fatalf("missing %s: %v", want, args)
		}
	}
	if !has(args, "--dev", "/dev") || !has(args, "--proc", "/proc") {
		t.Fatalf("missing /dev or /proc: %v", args)
	}
	if !has(args, "--chdir", "/home/me/wt") {
		t.Fatalf("missing chdir: %v", args)
	}
}

func TestRender_HomeIsHiddenThenPolicyPathsAreBoundReadOnlyOrReadWrite(t *testing.T) {
	args := Render(policy, "/home/me")
	home := index(args, "--tmpfs", "/home/me")
	ro := index(args, "--ro-bind-try", "/home/me/repos/other", "/home/me/repos/other")
	rw := index(args, "--bind-try", "/home/me/repos/proj/.git", "/home/me/repos/proj/.git")
	if home < 0 || ro < home || rw < home {
		t.Fatalf("home tmpfs must come first (home=%d ro=%d rw=%d): %v", home, ro, rw, args)
	}
	// A nested path is bound after its parent so it is not hidden by it.
	if index(args, "--bind-try", "/home/me/wt/sub", "/home/me/wt/sub") < index(args, "--bind-try", "/home/me/wt", "/home/me/wt") {
		t.Fatalf("child bound before parent: %v", args)
	}
}

func TestRender_TempIsFreshAndPolicyPathsInItSurvive(t *testing.T) {
	p := ports.SandboxPolicy{WorkingDir: "/tmp/s", Writable: []string{"/tmp/s"}}
	args := Render(p, "/home/me")
	if index(args, "--tmpfs", "/tmp") < 0 || index(args, "--tmpfs", "/tmp") > index(args, "--bind-try", "/tmp/s", "/tmp/s") {
		t.Fatalf("/tmp tmpfs must precede the policy bind: %v", args)
	}
}

func TestRender_APathIsOneArgumentSoSpacesAndDashesCannotInjectFlags(t *testing.T) {
	p := ports.SandboxPolicy{WorkingDir: "/w", Writable: []string{"/w/a b --bind /etc /x", "/w/-weird"}}
	args := Render(p, "/home/me")
	if !has(args, "--bind-try", "/w/a b --bind /etc /x", "/w/a b --bind /etc /x") {
		t.Fatalf("path not kept as a single argument: %v", args)
	}
	if slices.Contains(args, "/x") {
		t.Fatalf("path split into arguments: %v", args)
	}
}

func TestRender_EmptyPathsAreSkipped(t *testing.T) {
	args := Render(ports.SandboxPolicy{WorkingDir: "/w", Readable: []string{""}}, "/home/me")
	if slices.Contains(args, "") {
		t.Fatalf("empty argument: %v", args)
	}
}

func TestDriverWrap_PutsTheCommandAfterTheSeparatorAndKeepsDirAndEnv(t *testing.T) {
	d := &Driver{bin: "/usr/bin/bwrap", home: "/home/me"}
	got, err := d.Wrap(policy, ports.SandboxCommand{Path: "claude", Args: []string{"-p", "x"}, Dir: "/home/me/wt", Env: []string{"A=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/usr/bin/bwrap" || got.Dir != "/home/me/wt" || !slices.Equal(got.Env, []string{"A=1"}) {
		t.Fatalf("wrapped command = %+v", got)
	}
	tail := got.Args[len(got.Args)-4:]
	if !slices.Equal(tail, []string{"--", "claude", "-p", "x"}) {
		t.Fatalf("command tail = %v", tail)
	}
}

func TestDriver_ReportsViolationCollectionUnavailableAndNamesItself(t *testing.T) {
	d := &Driver{bin: "bwrap"}
	if d.ViolationsAvailable() {
		t.Fatal("violation collection is not available on Linux")
	}
	if d.Name() != "bubblewrap" {
		t.Fatalf("name = %q", d.Name())
	}
}

func TestNew_AMissingBwrapMakesTheDriverUnavailable(t *testing.T) {
	d, err := lookup(func(string) (string, error) { return "", errors.New("not found") })
	if d != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("driver=%v err=%v", d, err)
	}
	d, err = lookup(func(string) (string, error) { return "/usr/bin/bwrap", nil })
	if err != nil || d == nil {
		t.Fatalf("driver=%v err=%v", d, err)
	}
}
