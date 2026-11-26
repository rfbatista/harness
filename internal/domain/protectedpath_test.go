package domain

import (
	"slices"
	"testing"
)

func TestAProtectedGlobMatchesPathsRelativeToTheRepositoryRoot(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		// a root-only name: only at the root
		{"check.sh", "check.sh", true},
		{"check.sh", "scripts/check.sh", false},
		{"Jenkinsfile", "Jenkinsfile", true},
		{"check.sh", "check.sh.bak", false},
		// ** crosses directories, in front, in the middle and at the end
		{".github/workflows/**", ".github/workflows/ci.yml", true},
		{".github/workflows/**", ".github/workflows/deep/er/ci.yml", true},
		{".github/workflows/**", ".github/ISSUE_TEMPLATE/bug.md", false},
		{"**/check.sh", "check.sh", true},
		{"**/check.sh", "a/b/check.sh", true},
		{"a/**/z.txt", "a/z.txt", true},
		{"a/**/z.txt", "a/b/c/z.txt", true},
		{"a/**/z.txt", "b/a/z.txt", false},
		// a leading directory protects everything under it
		{".circleci", ".circleci/config.yml", true},
		{".circleci/", ".circleci/config.yml", true},
		{"ci", "ci/build/x.sh", true},
		{"ci", "ci2/x.sh", false},
		// * and ? stay within one segment
		{"*.yml", "ci.yml", true},
		{"*.yml", "dir/ci.yml", false},
		{"/check.sh", "check.sh", true}, // a leading slash means the root too
		{"gates/*.json", "gates/a.json", true},
		{"gates/*.json", "gates/sub/a.json", false},
		{"", "check.sh", false},
	}
	for _, c := range cases {
		if got := MatchProtectedPath(c.glob, c.path); got != c.want {
			t.Errorf("MatchProtectedPath(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestTheFirstMatchingGlobNamesTheRule(t *testing.T) {
	globs := []string{"check.sh", ".github/workflows/**"}
	if g, ok := ProtectedGlob(globs, ".github/workflows/ci.yml"); !ok || g != ".github/workflows/**" {
		t.Errorf("got %q %v", g, ok)
	}
	if _, ok := ProtectedGlob(globs, "main.go"); ok {
		t.Error("an unprotected path matched")
	}
	if _, ok := ProtectedGlob(globs, "./check.sh"); !ok {
		t.Error("a ./ prefix escaped the rule")
	}
}

func TestShellWriteTargetsAreTheFilesARedirectWritesTo(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{"echo hi > check.sh", []string{"check.sh"}},
		{"echo hi >> check.sh", []string{"check.sh"}},
		{"echo hi >check.sh", []string{"check.sh"}},
		{"cat <<EOF > ci/build.sh\nx\nEOF", []string{"ci/build.sh"}},
		{`printf x > "my file.txt"`, []string{"my file.txt"}},
		{"echo hi > a.txt && echo yo >| b.txt", []string{"a.txt", "b.txt"}},
		{"go test ./... 2> err.log", []string{"err.log"}},
		{"echo hi &> all.log", []string{"all.log"}},
		{"echo hi > /dev/null", nil},
		{"echo hi 2>&1", nil},
		{"echo '> not.txt'", nil},
		{"git diff", nil},
	}
	for _, c := range cases {
		if got := ShellWriteTargets(c.cmd); !slices.Equal(got, c.want) {
			t.Errorf("ShellWriteTargets(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestAScopeWithAnInvalidGlobIsRefusedWithACodedError(t *testing.T) {
	if err := ScopeGlobError([]string{"check.sh", "ci/**"}); err != nil {
		t.Errorf("valid scope: %v", err)
	}
	err := ScopeGlobError([]string{"check.sh", "[unclosed"})
	se, ok := err.(*StructuredError)
	if !ok || se.Code != "INVALID_GLOB" {
		t.Errorf("invalid scope: %v", err)
	}
	if err := ScopeGlobError([]string{" "}); err == nil {
		t.Error("an empty glob was accepted")
	}
	if ScopeGlobError(nil) != nil {
		t.Error("no scope is valid")
	}
}
