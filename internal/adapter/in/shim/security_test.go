//go:build !windows

package shim

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSecurity is a recorder standing in for /usr/bin/security: it logs its
// args and stdin, prints a marker and exits 7.
func fakeSecurity(t *testing.T) (path, log string) {
	t.Helper()
	dir := t.TempDir()
	path, log = filepath.Join(dir, "security"), filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\ncat >> '" + log + ".stdin'\necho real-out\necho real-err >&2\nexit 7\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, log
}

// runner runs the shim against one recorder, which it clears between calls.
type runner struct{ real, log string }

func newRunner(t *testing.T) runner {
	real, log := fakeSecurity(t)
	return runner{real, log}
}

func (r runner) run(t *testing.T, args []string, stdin string) (code int, out, errOut string, called bool, realStdin string) {
	t.Helper()
	real, log := r.real, r.log
	_ = os.Remove(log)
	_ = os.Remove(log + ".stdin")
	var o, e bytes.Buffer
	code = securityMain(args, strings.NewReader(stdin), &o, &e, real)
	_, err := os.Stat(log)
	b, _ := os.ReadFile(log + ".stdin")
	return code, o.String(), e.String(), err == nil, string(b)
}

func TestAClaudeCredentialsFindAndDeleteAreNotFoundAndReachNoKeychain(t *testing.T) {
	run := newRunner(t)
	for _, args := range [][]string{
		{"find-generic-password", "-a", "me", "-w", "-s", "Claude Code-credentials"},
		{"find-generic-password", "-s", "Claude Code-credentials-1a2b3c4d", "-w"},
		{"delete-generic-password", "-a", "me", "-s", "Claude Code-credentials"},
		{"-q", "find-generic-password", "-sClaude Code-credentials"},
	} {
		code, out, _, called, _ := run.run(t, args, "")
		if code != 44 || called || out != "" {
			t.Errorf("%v: code %d, real called %v, out %q", args, code, called, out)
		}
	}
}

func TestAClaudeCredentialsAddFailsSoClaudeKeepsItsLoginInTheFile(t *testing.T) {
	run := newRunner(t)
	code, _, _, called, _ := run.run(t, []string{"add-generic-password", "-U", "-a", "me", "-s", "Claude Code-credentials", "-X", "7b7d"}, "")
	if code == 0 || called {
		t.Fatalf("add: code %d, real called %v", code, called)
	}
}

func TestTheInteractiveFormIsAnsweredTheSameWay(t *testing.T) {
	run := newRunner(t)
	for line, want := range map[string]int{
		`add-generic-password -U -a "me" -s "Claude Code-credentials-1a2b3c4d" -X "7b7d"` + "\n": 1,
		`find-generic-password -s 'Claude Code-credentials' -w` + "\n":                           44,
		`delete-generic-password -a me -s Claude\ Code-credentials` + "\n":                       44,
		`add-generic-password -U -a me -s "Claude Code-credentials" -X 7b`:                       1, // no newline
	} {
		code, _, _, called, _ := run.run(t, []string{"-i"}, line)
		if code != want || called {
			t.Errorf("%q: code %d (want %d), real called %v", line, code, want, called)
		}
	}
}

func TestAnyOtherSecurityCallRunsTheRealOneWithItsOutputAndExitCode(t *testing.T) {
	run := newRunner(t)
	for _, args := range [][]string{
		{"find-generic-password", "-s", "Some other service", "-w"},
		{"add-generic-password", "-s", "Claude Codex-credentials-nope", "-a", "x"},
		{"find-generic-password", "-a", "Claude Code-credentials"}, // the account is not the service
		{"find-internet-password", "-s", "Claude Code-credentials"},
		{"list-keychains"},
		{"-v", "verify-cert", "-c", "x.pem"},
	} {
		code, out, errOut, called, _ := run.run(t, args, "")
		if code != 7 || !called || out != "real-out\n" || errOut != "real-err\n" {
			t.Errorf("%v: code %d, called %v, out %q, err %q", args, code, called, out, errOut)
		}
	}
}

func TestAnOtherInteractiveCallReachesTheRealOneWithItsWholeStdin(t *testing.T) {
	run := newRunner(t)
	stdin := "list-keychains\nfind-generic-password -s \"Claude Code-credentials\"\n"
	code, out, _, called, got := run.run(t, []string{"-i"}, stdin)
	if code != 7 || !called || out != "real-out\n" || got != stdin {
		t.Fatalf("code %d, called %v, out %q, stdin %q", code, called, out, got)
	}
}

func TestTheSecurityShimIsPartOfTheMultiCallBinary(t *testing.T) {
	if !Is("/tmp/shims/security") || !Is("sed") || Is("ls") {
		t.Fatal("Is does not know security")
	}
}
