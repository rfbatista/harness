package domain

import (
	"slices"
	"testing"
)

func TestEveryLockfileMapsToTheManifestBesideIt(t *testing.T) {
	cases := []struct{ lock, manifest string }{
		{"go.sum", "go.mod"},
		{"services/api/go.sum", "services/api/go.mod"},
		{"go.work.sum", "go.work"},
		{"package-lock.json", "package.json"},
		{"web/npm-shrinkwrap.json", "web/package.json"},
		{"yarn.lock", "package.json"},
		{"pnpm-lock.yaml", "package.json"},
		{"Cargo.lock", "Cargo.toml"},
		{"poetry.lock", "pyproject.toml"},
		{"uv.lock", "pyproject.toml"},
		{"Pipfile.lock", "Pipfile"},
		{"Gemfile.lock", "Gemfile"},
		{"composer.lock", "composer.json"},
	}
	for _, c := range cases {
		if got, ok := LockfileManifest(c.lock); !ok || got != c.manifest {
			t.Errorf("LockfileManifest(%q) = %q, %v; want %q", c.lock, got, ok, c.manifest)
		}
	}
	for _, not := range []string{"go.mod", "main.go", "lock.txt", "package.json", "README.md"} {
		if _, ok := LockfileManifest(not); ok {
			t.Errorf("%q is not a lockfile", not)
		}
	}
}

func TestTheAddedDependenciesAreReadFromTheAddedLinesOfALockfileDiff(t *testing.T) {
	goSum := `--- a/go.sum
+++ b/go.sum
@@ -1,2 +1,4 @@
 github.com/a/b v1.0.0 h1:old=
+github.com/c/d v0.3.1 h1:abc=
+github.com/c/d v0.3.1/go.mod h1:def=
-github.com/e/f v1.0.0 h1:gone=
`
	if got, want := AddedDependencies("go.sum", goSum), []string{"github.com/c/d@v0.3.1"}; !slices.Equal(got, want) {
		t.Errorf("go.sum: %v, want %v", got, want)
	}
	cargo := "+++ b/Cargo.lock\n@@\n+[[package]]\n+name = \"serde\"\n+version = \"1.0.1\"\n [[package]]\n name = \"old\"\n"
	if got, want := AddedDependencies("Cargo.lock", cargo), []string{"serde"}; !slices.Equal(got, want) {
		t.Errorf("Cargo.lock: %v, want %v", got, want)
	}
	npm := "+++ b/package-lock.json\n+    \"node_modules/left-pad\": {\n+      \"version\": \"1.3.0\",\n-    \"node_modules/gone\": {\n"
	if got, want := AddedDependencies("package-lock.json", npm), []string{"left-pad"}; !slices.Equal(got, want) {
		t.Errorf("package-lock.json: %v, want %v", got, want)
	}
	if got := AddedDependencies("go.sum", " unchanged\n-removed\n"); len(got) != 0 {
		t.Errorf("a diff that adds nothing listed %v", got)
	}
	if got := AddedDependencies("unknown.lock", "+whatever\n"); len(got) != 0 {
		t.Errorf("an unknown lockfile listed %v", got)
	}
}
