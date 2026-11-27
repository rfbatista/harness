package domain

import (
	"slices"
	"testing"
)

func TestACommandsWritesAreFollowedThroughItsOwnCdPushdAndPopd(t *testing.T) {
	cases := []struct {
		name, cmd  string
		paths      []string
		unresolved []string
	}{
		{"no cd", "echo x > a.txt", []string{"/w/a.txt"}, nil},
		{"an absolute target ignores the directory", "cd sub && echo x > /abs/f", []string{"/abs/f"}, nil},
		{"cd into a folder", "cd .github/workflows && echo bad > ci.yml", []string{"/w/.github/workflows/ci.yml"}, nil},
		{"cd up then a name", "cd sub && echo bad > ../check.sh", []string{"/w/check.sh"}, nil},
		{"pushd", "pushd sub >/dev/null; echo bad > ../check.sh", []string{"/w/check.sh"}, nil},
		{"pushd then popd", "pushd sub; popd; echo x > b.txt", []string{"/w/b.txt"}, nil},
		{"cd inside a subshell stays in it", "(cd sub; echo a > in.txt); echo b > out.txt", []string{"/w/sub/in.txt", "/w/out.txt"}, nil},
		{"cd with an option", "cd -P sub && echo x > f", []string{"/w/sub/f"}, nil},
		{"cd to a variable cannot be resolved", "cd $D && echo x > check.sh", nil, []string{"check.sh"}},
		{"cd to a substitution", "cd \"$(mktemp -d)\" && echo x > f", nil, []string{"f"}},
		{"cd to the previous directory", "cd - && echo x > f", nil, []string{"f"}},
		{"a bare cd goes home", "cd && echo x > f", nil, []string{"f"}},
		{"cd to a tilde path", "cd ~/x && echo x > f", nil, []string{"f"}},
		{"after an unresolved cd an absolute target still resolves", "cd $D; echo x > /w/check.sh", []string{"/w/check.sh"}, nil},
		{"a cd that is no command name is not a cd", "echo cd sub > f", []string{"/w/f"}, nil},
		{"glued forms", "echo x>a.txt; (echo y)>b.txt", []string{"/w/a.txt", "/w/b.txt"}, nil},
		{"a heredoc body is not read", "cat <<EOF > a.txt\necho z > b.txt\nEOF\n", []string{"/w/a.txt"}, nil},
		{"a here-string word is not a file", "cat <<< 'x' > a.txt", []string{"/w/a.txt"}, nil},
	}
	for _, c := range cases {
		got := ShellWritesFrom("/w", c.cmd)
		if !slices.Equal(got.Paths, c.paths) || !slices.Equal(got.Unresolved, c.unresolved) {
			t.Errorf("%s: ShellWritesFrom(%q) = paths %q unresolved %q, want %q %q", c.name, c.cmd, got.Paths, got.Unresolved, c.paths, c.unresolved)
		}
	}
}
