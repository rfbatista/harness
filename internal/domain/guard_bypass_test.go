package domain

import (
	"slices"
	"testing"
	"unicode/utf8"
)

// Adversarial tests of the Guard rule tables (seam A): shell forms that write a
// file, glob matching on odd paths, lockfile pairs, the test-file table.

func TestAShellWriteIsFoundWhateverFormTheRedirectTakes(t *testing.T) {
	cases := []struct {
		name, cmd string
		want      []string
	}{
		{"redirect glued to the last word", "echo x>check.sh", []string{"check.sh"}},
		{"redirect glued to a quoted word", `echo "x">check.sh`, []string{"check.sh"}},
		{"redirect after a subshell", "(echo x)>check.sh", []string{"check.sh"}},
		{"heredoc with the redirect glued to the delimiter", "cat <<EOF>check.sh\nx\nEOF", []string{"check.sh"}},
		{"redirect after a brace group", "{ echo x; }>check.sh", []string{"check.sh"}},
		{"redirect with a number glued to the word", "echo x 1>check.sh", []string{"check.sh"}},
		{"redirect to a quoted name", `echo x > 'check.sh'`, []string{"check.sh"}},
		{"redirect to a path with a dot segment", "echo x > ./sub/../check.sh", []string{"./sub/../check.sh"}},
		{"redirect to a name with a space", `echo x > "a b/check.sh"`, []string{"a b/check.sh"}},
		{"redirect to an escaped space", `echo x > a\ b.sh`, []string{"a b.sh"}},
		{"redirect to a variable", "F=check.sh; echo x > $F", nil},
		{"redirect after a pipe", "printf x | cat > check.sh", []string{"check.sh"}},
		{"redirect on the second command", "ls; echo x > check.sh", []string{"check.sh"}},
		{"redirect in the middle of arguments", "echo > check.sh x", []string{"check.sh"}},
		{"noclobber override", "echo x >| check.sh", []string{"check.sh"}},
		{"file descriptor opened for writing", "exec 3> check.sh", []string{"check.sh"}},
	}
	for _, c := range cases {
		got := ShellWriteTargets(c.cmd)
		if c.want == nil && len(got) > 0 {
			// A variable target cannot be resolved by a parser; it is documented
			// as a gap, so the case only asserts the parser does not panic.
			continue
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: ShellWriteTargets(%q) = %q, want %q", c.name, c.cmd, got, c.want)
		}
	}
}

func TestAShellWriteThroughAVariableOrCommandSubstitutionIsNotSeenByTheHook(t *testing.T) {
	// Not a pass/fail claim about the product: it pins the known gap so a change
	// in it shows up. The PATH shims and the sandbox are the other layers.
	for _, cmd := range []string{"F=check.sh; echo x > $F", `echo x > "$(echo check.sh)"`, "echo x > `echo check.sh`"} {
		if got := ShellWriteTargets(cmd); len(got) != 0 {
			t.Logf("%q now yields %q", cmd, got)
		}
	}
}

func TestAProtectedGlobMatchesOddPathsTheSameAsPlainOnes(t *testing.T) {
	cases := []struct {
		glob, rel string
		want      bool
	}{
		{"check.sh", "check.sh", true},
		{"check.sh", "./check.sh", true},
		{"check.sh", "sub/../check.sh", true},
		{"check.sh", "sub//../check.sh", true},
		{"check.sh", `.\check.sh`, true},
		{"check.sh", "sub/check.sh", false},
		{"check.sh", "../check.sh", false},
		{"check.sh", "/check.sh", false},
		{"check.sh", "CHECK.SH", false},
		{".github/workflows/**", ".github/workflows/ci.yml", true},
		{".github/workflows/**", ".github/workflows", true},
		{".github/workflows/**", ".github/workflows/sub/dir/x.yml", true},
		{".github/workflows/**", ".github/./workflows/ci.yml", true},
		{".github/workflows/**", ".github/other/../workflows/ci.yml", true},
		{".github/workflows/**", ".github/workflows2/ci.yml", false},
		{"my dir/check.sh", "my dir/check.sh", true},
		{"my dir/**", "my dir/a b.sh", true},
		{"**/check.sh", "a/b/check.sh", true},
		{"**/check.sh", "check.sh", true},
		{"**", "anything/at/all", true},
		{"*.lock", "yarn.lock", true},
		{"*.lock", "sub/yarn.lock", false},
		{"/check.sh", "check.sh", true},
		{"check.sh/", "check.sh", true},
		{"ci", "ci/build.sh", true},
		{"ci", "cinema/build.sh", false},
		{"", "check.sh", false},
		{"   ", "check.sh", false},
		{"check.sh", "", false},
		{"check.sh", ".", false},
		{"[", "check.sh", false},
	}
	for _, c := range cases {
		if got := MatchProtectedPath(c.glob, c.rel); got != c.want {
			t.Errorf("MatchProtectedPath(%q, %q) = %v, want %v", c.glob, c.rel, got, c.want)
		}
	}
}

func TestAMalformedGlobNeverPanicsOrMatches(t *testing.T) {
	for _, g := range []string{"[", "[a-", "a[", "\\", "**/**/**", "***", "?[", "[]]"} {
		for _, rel := range []string{"a", "a/b", "[", "\\"} {
			_ = MatchProtectedPath(g, rel)
		}
	}
	if MatchProtectedPath("[", "[") {
		t.Error("a malformed glob matched")
	}
}

func TestALockfileIsPairedWithTheManifestBesideIt(t *testing.T) {
	cases := []struct{ lock, manifest string }{
		{"go.sum", "go.mod"},
		{"sub/go.sum", "sub/go.mod"},
		{"./sub/go.sum", "sub/go.mod"},
		{"sub/../go.sum", "go.mod"},
		{"web/pnpm-lock.yaml", "web/package.json"},
		{"a b/yarn.lock", "a b/package.json"},
		{"Cargo.lock", "Cargo.toml"},
		{"deep/er/Gemfile.lock", "deep/er/Gemfile"},
		{"composer.lock", "composer.json"},
		{`sub\go.sum`, "sub/go.mod"},
	}
	for _, c := range cases {
		got, ok := LockfileManifest(c.lock)
		if !ok || got != c.manifest {
			t.Errorf("LockfileManifest(%q) = %q, %v; want %q", c.lock, got, ok, c.manifest)
		}
	}
	// A manifest in another folder is not the pair: a lockfile cannot ride on an
	// unrelated manifest change.
	if got, _ := LockfileManifest("sub/go.sum"); got == "go.mod" {
		t.Error("sub/go.sum paired with the root go.mod")
	}
	for _, rel := range []string{"go.mod", "package.json", "go.sum.bak", "my-go.sum", "GO.SUM", "package-lock.json.orig", "notes.txt", ""} {
		if m, ok := LockfileManifest(rel); ok {
			t.Errorf("LockfileManifest(%q) = %q, want no pair", rel, m)
		}
	}
}

func TestAddedDependenciesReadOnlyTheAddedLinesOfALockfile(t *testing.T) {
	gosum := "--- a/go.sum\n+++ b/go.sum\n" +
		"-example.com/old v1.0.0 h1:abc=\n" +
		"+example.com/new v1.2.3 h1:def=\n" +
		"+example.com/new v1.2.3/go.mod h1:ghi=\n" +
		" example.com/kept v1.0.0 h1:jkl=\n"
	if got := AddedDependencies("go.sum", gosum); !slices.Equal(got, []string{"example.com/new@v1.2.3"}) {
		t.Errorf("go.sum = %q", got)
	}
	if got := AddedDependencies("go.sum", "+++ b/go.sum\n"); len(got) != 0 {
		t.Errorf("the file header read as a dependency: %q", got)
	}
	if got := AddedDependencies("notes.txt", "+example.com/new v1.2.3 h1:def=\n"); got != nil {
		t.Errorf("a non-lockfile yielded %q", got)
	}
	// Windows line endings and a nested path.
	if got := AddedDependencies("sub/go.sum", "+example.com/x v0.1.0 h1:z=\r\n"); !slices.Equal(got, []string{"example.com/x@v0.1.0"}) {
		t.Errorf("CRLF go.sum = %q", got)
	}
	// The same dependency added twice is listed once.
	dup := "+example.com/x v0.1.0 h1:a=\n+example.com/x v0.1.0 h1:b=\n"
	if got := AddedDependencies("go.sum", dup); len(got) != 1 {
		t.Errorf("duplicate listed twice: %q", got)
	}
	npm := "+    \"node_modules/left-pad\": {\n+    \"node_modules/@scope/pkg\": {\n"
	if got := AddedDependencies("package-lock.json", npm); !slices.Equal(got, []string{"left-pad", "@scope/pkg"}) {
		t.Errorf("package-lock = %q", got)
	}
	// A lockfile change that only removes a dependency flags nothing.
	if got := AddedDependencies("go.sum", "-example.com/old v1.0.0 h1:abc=\n"); len(got) != 0 {
		t.Errorf("a removal flagged %q", got)
	}
}

func TestATestFileIsRecognisedWhereverItLives(t *testing.T) {
	yes := []string{
		"a_test.go", "pkg/a_test.go", "./pkg/a_test.go", "pkg/../a_test.go", "A_TEST.GO",
		"web/app.test.ts", "web/app.spec.js", "web/app.test.tsx",
		"tests/helper.go", "src/__tests__/x.js", "e2e/spec/x.rb", "pkg/testdata/golden.txt", "a b/tests/x.py",
		"src/fixtures/data.json", "test/anything.txt", "tests_folder/../tests/x.go",
		"test_x.py", "conftest.py", "FooTest.java", "FooTests.cs", `pkg\a_test.go`,
		"src/__mocks__/api.js", "src/testing/helper.go",
	}
	for _, p := range yes {
		if !IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = false", p)
		}
	}
	no := []string{
		"main.go", "pkg/a.go", "testing.go", "contest.go", "latest/x.go", "src/protest/x.go",
		"attest.go", "test.go", "pkg/tests.go", "README.md", "Makefile", "go.mod", "",
		"specification.md", "pkg/test_helpers.go",
	}
	for _, p := range no {
		if IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = true", p)
		}
	}
}

func TestAnUnusualTestFileLocationDoesNotLetNonTestCodeThrough(t *testing.T) {
	// A non-test file placed under a folder that merely contains "test" in the
	// name, or named like a test with another extension.
	for _, p := range []string{"contest/main.go", "latest.go", "src/tester/main.go", "src/unittest.go", "pkg/attest/x.go"} {
		if IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = true", p)
		}
	}
	// A sneaky path that climbs out of a tests folder is not a test file.
	if IsTestFile("tests/../main.go") {
		t.Error("tests/../main.go counted as a test file")
	}
	if IsTestFile("src/tests/../../main.go") {
		t.Error("src/tests/../../main.go counted as a test file")
	}
}

func TestAGateOutputTailNeverCutsARuneInHalf(t *testing.T) {
	g := GateResult{Output: "ééééééééééééé"} // 2 bytes per rune
	for n := 1; n <= len(g.Output); n++ {
		if tail := g.OutputTail(n); !utf8.ValidString(tail) {
			t.Fatalf("OutputTail(%d) = %q is not valid UTF-8", n, tail)
		}
	}
}

func TestASessionIsGatedByModeAndDiff(t *testing.T) {
	cases := []struct {
		mode SessionMode
		diff bool
		want bool
	}{
		{SessionModeImplement, false, true}, {SessionModeImplement, true, true},
		{SessionModeTest, false, true}, {SessionModeDefault, true, true}, {SessionModeDefault, false, false},
		{SessionModeArchitect, true, false}, {SessionModeReview, true, false}, {SessionMode("bogus"), true, false},
	}
	for _, c := range cases {
		if got := SessionGated(c.mode, c.diff); got != c.want {
			t.Errorf("SessionGated(%q, %v) = %v, want %v", c.mode, c.diff, got, c.want)
		}
	}
}
