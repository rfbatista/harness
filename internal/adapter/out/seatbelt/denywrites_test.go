package seatbelt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

func TestDenyWritesProfileAllowsEverythingButWritesUnderOnePath(t *testing.T) {
	d := &Driver{}
	root := "/tmp/harness-shell-1; (allow file-write* (subpath \"/\"))"
	w, err := d.WrapDenyWrites(root, ports.SandboxCommand{Path: "/bin/zsh", Args: []string{"-l"}, Dir: "/w", Env: []string{"A=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if w.Path != "sandbox-exec" || w.Dir != "/w" || len(w.Env) != 1 {
		t.Errorf("wrapped = %+v", w)
	}
	// ... -p <profile> -- /bin/zsh -l
	var profile string
	for i, a := range w.Args {
		if a == "-p" {
			profile = w.Args[i+1]
		}
	}
	if !strings.HasPrefix(profile, "(version 1)\n(allow default)\n") || !strings.Contains(profile, "(deny file-write*") {
		t.Errorf("profile = %q, want allow default then a write denial", profile)
	}
	if strings.Contains(profile, "file-read") {
		t.Errorf("the profile restricts reading: %q", profile)
	}
	if strings.Contains(profile, "harness-shell") || strings.Contains(profile, "allow file-write") {
		t.Errorf("the path reached the profile text: %q", profile)
	}
	var params []string
	for i, a := range w.Args {
		if a == "-D" {
			params = append(params, w.Args[i+1])
		}
	}
	if len(params) == 0 || !strings.HasSuffix(params[0], root) {
		t.Errorf("params = %q, want the path as a -D parameter", params)
	}
	if tail := w.Args[len(w.Args)-3:]; tail[0] != "--" || tail[1] != "/bin/zsh" || tail[2] != "-l" {
		t.Errorf("command tail = %q", tail)
	}
}

func TestDenyWritesRefusesARelativePath(t *testing.T) {
	if _, err := (&Driver{}).WrapDenyWrites("relative", ports.SandboxCommand{Path: "/bin/sh"}); err == nil {
		t.Error("a relative path was accepted")
	}
}

// Where sandbox-exec runs, a write under the path fails, everywhere else
// succeeds, and reading works.
func TestDenyWritesOnTheRealPlatform(t *testing.T) {
	d := requireSeatbelt(t)
	if err := exec.Command("sandbox-exec", "-p", "(version 1)(allow default)", "/usr/bin/true").Run(); err != nil {
		t.Skipf("sandbox-exec cannot run here (already sandboxed?): %v", err)
	}
	denied, open := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(denied, "doc.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `cat "$1/doc.md" && ! touch "$1/new" && ! echo x > "$1/doc.md" && ! mkdir "$1/sub" && ! rm "$1/doc.md" && touch "$2/ok" && echo done`
	w, err := d.WrapDenyWrites(denied, ports.SandboxCommand{Path: "/bin/sh", Args: []string{"-c", script, "sh", denied, open}, Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(w.Path, w.Args...).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "hello") || !strings.Contains(string(out), "done") {
		t.Fatalf("%v: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(denied, "new")); err == nil {
		t.Error("a file was created under the denied path")
	}
}
