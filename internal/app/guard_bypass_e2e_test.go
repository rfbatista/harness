//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// Guard — protected paths hunted adversarially (seam B): odd shell forms
// through the PreToolUse hook and through the PATH shims, in a gated session
// with the default protected paths.

func TestOddShellFormsThroughTheBashHookAreRefusedWhenTheyWriteAProtectedPath(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	a := e.protectedSession(bin, domain.SessionModeImplement, false)
	write(t, filepath.Join(a.worktree, "check.sh"), "echo ok\n")
	write(t, filepath.Join(a.worktree, "sub", "keep.txt"), "x\n")
	write(t, filepath.Join(a.worktree, ".github", "workflows", "ci.yml"), "x\n")
	if err := os.Symlink("check.sh", filepath.Join(a.worktree, "alias.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".github/workflows", filepath.Join(a.worktree, "wf")); err != nil {
		t.Fatal(err)
	}
	hook := func(cwd, cmd string) string {
		return runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, bashInput(cwd, cmd))
	}
	forms := map[string]string{
		"a heredoc to the file":          "cat > check.sh <<EOF\nbad\nEOF",
		"a heredoc appended":             "cat >> check.sh <<'EOF'\nbad\nEOF",
		"a ./ path":                      "echo bad > ./check.sh",
		"a dot-dot path":                 "echo bad > sub/../check.sh",
		"an absolute path":               "echo bad > " + filepath.Join(a.worktree, "check.sh"),
		"a quoted path":                  `echo bad > "check.sh"`,
		"a path with spaces under a glob": `echo bad > ".github/workflows/my file.yml"`,
		"an escaped space":               `echo bad > .github/workflows/my\ file.yml`,
		"a symlink to the file":          "echo bad > alias.sh",
		"a symlink to the folder":        "echo bad > wf/new.yml",
		"stderr redirected":              "make 2> check.sh",
		"both streams":                   "make &> check.sh",
		"after a pipe":                   "echo bad | cat > check.sh",
		"after &&":                       "true && echo bad >> check.sh",
		"in a brace group":               "{ echo bad; } > check.sh",
		"in a subshell":                  "( echo bad ) > check.sh",
		"with noclobber":                 "echo bad >| check.sh",
		"a file descriptor":              "exec 3> check.sh",
		"a second target":                "echo a > ok.txt; echo b > check.sh",
	}
	for name, cmd := range forms {
		if out := hook(a.worktree, cmd); !strings.Contains(out, "PROTECTED_PATH") {
			t.Errorf("%s: %q through the Bash hook = %q", name, cmd, out)
		}
	}
	// Forms that write the file and give the parser no space to see the operator.
	glued := map[string]string{
		"a redirect glued to the word":   "echo bad>check.sh",
		"a glued quoted word":            `echo "bad">check.sh`,
		"a glued append":                 "echo bad>>check.sh",
		"a glued heredoc redirect":       "cat <<EOF>check.sh\nbad\nEOF",
		"a subshell then a glued redirect": "(echo bad)>check.sh",
	}
	for name, cmd := range glued {
		if out := hook(a.worktree, cmd); !strings.Contains(out, "PROTECTED_PATH") {
			t.Errorf("BYPASS %s: %q through the Bash hook = %q", name, cmd, out)
		}
	}
	// A command that changes directory first writes where it says, not where the
	// session started.
	cd := map[string]string{
		"cd into the protected folder": "cd .github/workflows && echo bad > ci.yml",
		"cd up then the file":          "cd sub && echo bad > ../check.sh",
		"pushd":                        "pushd sub >/dev/null; echo bad > ../check.sh",
	}
	for name, cmd := range cd {
		if out := hook(a.worktree, cmd); !strings.Contains(out, "PROTECTED_PATH") {
			t.Errorf("BYPASS %s: %q through the Bash hook = %q", name, cmd, out)
		}
	}
	// The hook runs from a subfolder: a relative path is that folder's.
	if out := hook(filepath.Join(a.worktree, "sub"), "echo bad > ../check.sh"); !strings.Contains(out, "PROTECTED_PATH") {
		t.Errorf("a ../ path from a subfolder = %q", out)
	}
	if out := hook(filepath.Join(a.worktree, ".github", "workflows"), "echo bad > ci.yml"); !strings.Contains(out, "PROTECTED_PATH") {
		t.Errorf("a bare name from the protected folder = %q", out)
	}
	// What is allowed stays allowed.
	for _, cmd := range []string{"echo ok > sub/new.txt", "cat check.sh", "echo ok > /dev/null", "echo '>' check.sh", "echo ok 2>&1", "grep x check.sh > out.txt"} {
		if out := hook(a.worktree, cmd); out != "" {
			t.Errorf("%q was refused: %q", cmd, out)
		}
	}
}

func TestOddShellFormsThroughThePathShimsAreRefusedAndLeaveTheProtectedFilesAsTheyWere(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	a := e.protectedSession(bin, domain.SessionModeImplement, true)
	wt := a.worktree
	setup := func() {
		for _, d := range []string{".github", ".circleci", "sub", "src", "check.sh", "other.sh", "alias.sh", "wf", "check.sh.bak"} {
			_ = os.RemoveAll(filepath.Join(wt, d))
		}
		write(t, filepath.Join(wt, "check.sh"), "echo ok\n")
		write(t, filepath.Join(wt, "other.sh"), "other\n")
		write(t, filepath.Join(wt, "sub", "keep.txt"), "keep\n")
		write(t, filepath.Join(wt, "src", "new.yml"), "new\n")
		write(t, filepath.Join(wt, "src", "deeper", "n.yml"), "deeper\n")
		write(t, filepath.Join(wt, ".github", "workflows", "ci.yml"), "ci\n")
		write(t, filepath.Join(wt, ".circleci", "config.yml"), "build\n")
		if err := os.Symlink("check.sh", filepath.Join(wt, "alias.sh")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(".github/workflows", filepath.Join(wt, "wf")); err != nil {
			t.Fatal(err)
		}

	}
	setup()
	forms := map[string]string{
		"tee -a":                       "printf 'bad\\n' | tee -a check.sh",
		"tee with another file first":  "printf 'bad\\n' | tee notes.txt check.sh",
		"tee through ./":               "printf 'bad\\n' | tee ./check.sh",
		"tee through dot-dot":          "printf 'bad\\n' | tee sub/../check.sh",
		"tee from a subfolder":         "cd sub && printf 'bad\\n' | tee ../check.sh",
		"tee from the protected dir":   "cd .circleci && printf 'bad\\n' | tee config.yml",
		"tee through a symlink":        "printf 'bad\\n' | tee alias.sh",
		"tee into a symlinked folder":  "printf 'bad\\n' | tee wf/ci.yml",
		"tee with a spaced name":       "printf 'bad\\n' | tee '.github/workflows/my file.yml'",
		"tee by absolute path":         "printf 'bad\\n' | tee " + filepath.Join(wt, "check.sh"),
		"cp -r into a protected dir":   "cp -r src .circleci/",
		"cp -r onto a protected dir":   "cp -r src/. .github/workflows/",
		"cp -R the same":               "cp -R src .github/workflows",
		"cp over the file":             "cp other.sh check.sh",
		"cp -f over the file":          "cp -f other.sh check.sh",
		"cp several into the folder":   "cp other.sh sub/keep.txt .circleci/",
		"cp into a symlinked folder":   "cp other.sh wf/",
		"mv over the file":             "mv other.sh check.sh",
		"mv -f over the file":          "mv -f other.sh check.sh",
		"mv into the folder":           "mv other.sh .circleci/",
		"mv a protected file away":     "mv check.sh sub/gone.sh",
		"mv a protected dir away":      "mv .circleci sub/cc2",
		"rm -rf the folder":            "rm -rf .github/workflows",
		"rm -r the folder":             "rm -r .circleci",
		"rm through a symlink":         "rm alias.sh && rm check.sh",
		"touch in a protected folder":  "touch .circleci/new.sh",
		"mkdir in a protected folder":  "mkdir .circleci/newdir",
		"sed -i in place":              "sed " + inPlace() + " 's/ok/bad/' check.sh",
		"sed -i through a symlink":     "sed " + inPlace() + " 's/ok/bad/' alias.sh",
		"sed -i with a backup suffix":  "sed -i.bak 's/ok/bad/' check.sh",
		"sed -i among several files":   "sed " + inPlace() + " 's/o/b/' sub/keep.txt check.sh",
	}
	for name, line := range forms {
		setup()
		r := a.inWorktree(t, line)
		if r.code == 0 {
			t.Errorf("BYPASS %s: %q exited 0", name, line)
			continue
		}
		if !strings.Contains(r.stderr, "PROTECTED_PATH") {
			t.Errorf("%s: %q = %+v, want a refusal with PROTECTED_PATH", name, line, r)
		}
		// Nothing the refused command named has changed or appeared.
		for rel, want := range map[string]string{
			"check.sh": "echo ok\n", "other.sh": "other\n", ".github/workflows/ci.yml": "ci\n", ".circleci/config.yml": "build\n",
			"sub/keep.txt": "keep\n", "src/new.yml": "new\n",
		} {
			if got := read(t, filepath.Join(wt, rel)); got != want {
				t.Errorf("%s: %s = %q after %q, want %q", name, rel, got, line, want)
			}
		}
		for _, rel := range []string{".circleci/new.sh", ".circleci/newdir", ".github/workflows/my file.yml", ".circleci/other.sh", ".circleci/deeper", "sub/gone.sh", "sub/cc2", "check.sh.bak"} {
			if got := read(t, filepath.Join(wt, rel)); got != "<missing>" {
				t.Errorf("%s: %s exists after %q: %q", name, rel, line, got)
			}
		}
	}

	// Commands on unprotected files still work.
	for _, line := range []string{"printf 'x\\n' | tee -a sub/keep.txt", "cp other.sh sub/o.sh", "mv sub/o.sh sub/o2.sh", "touch sub/t", "rm sub/t"} {
		if r := a.inWorktree(t, line); r.code != 0 {
			t.Errorf("%q = %+v", line, r)
		}
	}
}

func TestAProtectedFileReachedByAShimOnlyForAnAbsoluteProgramPathIsRecordedHere(t *testing.T) {
	// The shims stand first on PATH; a program called by its full path skips
	// them. The sandbox profile is what stops that, and it cannot run in the
	// agent sandbox (seatbelt does not nest), so this test only records what the
	// shim layer alone lets through, for the report.
	bin := buildServer(t)
	e := newShellE2E(t)
	a := e.protectedSession(bin, domain.SessionModeImplement, true)
	write(t, filepath.Join(a.worktree, "check.sh"), "echo ok\n")
	for _, line := range []string{
		"printf 'bad\\n' | /usr/bin/tee check.sh",
		"/bin/cp /bin/echo check.sh",
		"command -p tee check.sh </dev/null",
		"dd of=check.sh </dev/null 2>/dev/null",
		"ln -sf /dev/null check.sh",
		"git checkout HEAD~0 -- check.sh",
	} {
		r := a.inWorktree(t, line)
		t.Logf("%-48s exit %d, refused by the shims: %v", line, r.code, strings.Contains(r.stderr, "PROTECTED_PATH"))
	}
}
