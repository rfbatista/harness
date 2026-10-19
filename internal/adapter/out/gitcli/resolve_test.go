package gitcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func TestResolveGit(t *testing.T) {
	const real = "/Library/Developer/CommandLineTools/usr/bin/git"
	onPath := func(path string) func(string) (string, error) {
		return func(name string) (string, error) {
			if name != "git" {
				t.Fatalf("looked up %q on PATH, want git", name)
			}
			if path == "" {
				return "", errors.New("not found")
			}
			return path, nil
		}
	}
	xcrunFinds := func(path string) func() (string, error) {
		return func() (string, error) { return path, nil }
	}
	xcrunFails := func() (string, error) { return "", errors.New("xcrun: error: invalid active developer path") }
	xcrunUnused := func() (string, error) {
		t.Fatal("xcrun ran for a git that is not the shim")
		return "", nil
	}

	cases := []struct {
		name    string
		setting string
		path    func(string) (string, error)
		xcrun   func() (string, error)
		want    string
	}{
		{"the setting wins", "/opt/git/bin/git", onPath(shimGit), xcrunUnused, "/opt/git/bin/git"},
		{"a fake git first on PATH is kept", "", onPath("/tmp/fake/git"), xcrunUnused, "/tmp/fake/git"},
		{"the shim is replaced by the xcrun target", "", onPath(shimGit), xcrunFinds(real), real},
		{"the shim is kept when xcrun fails", "", onPath(shimGit), xcrunFails, shimGit},
		{"the shim is kept when xcrun names nothing", "", onPath(shimGit), xcrunFinds(""), shimGit},
		{"no git on PATH keeps the name", "", onPath(""), xcrunUnused, "git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveGit(c.setting, c.path, c.xcrun); got != c.want {
				t.Errorf("resolveGit = %q, want %q", got, c.want)
			}
		})
	}
}

// recordingGit is a git at a path off PATH that writes its subcommand to a
// log and fails; the log says which runs it served.
func recordingGit(t *testing.T) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log = filepath.Join(dir, "my-git"), filepath.Join(dir, "runs")
	script := "#!/bin/sh\nfor a; do case \"$a\" in -*|*=*) ;; *) echo \"$a\" >> " + log + "; exit 1 ;; esac; done\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func TestEveryAdapterRunsTheConfiguredGit(t *testing.T) {
	bin, log := recordingGit(t)
	opt := WithGit(bin)
	ctx := context.Background()
	repo := t.TempDir()

	_, _ = NewHistory(opt).Status(ctx, repo)
	_, _ = NewCode(opt).ResolveCommit(ctx, repo, "HEAD")
	_, _ = NewBranchPublisher(opt).OriginURL(ctx, repo)
	_ = NewBranchPublisher(opt).FetchBranch(ctx, repo, "main", nil)
	_, _ = NewWorktreeManager(opt).ListBranches(repo)
	_ = NewExcludes(opt).Exclude(repo, ".env", "env file")
	_ = CheckVersion(bin)

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the configured git never ran: %v", err)
	}
	got := strings.Fields(string(raw))
	for _, want := range []string{"status", "rev-parse", "config", "fetch", "for-each-ref", "check-ignore", "version"} {
		if !slices.Contains(got, want) {
			t.Errorf("git %s did not run the configured git; it ran %v", want, got)
		}
	}

	// Writing env files keeps git from tracking them through the same git.
	bin, log = recordingGit(t)
	_ = NewEnvFiles(WithGit(bin)).Write(repo, []*domain.EnvFile{{Path: ".env", Content: "A=1"}})
	if raw, err := os.ReadFile(log); err != nil || !slices.Contains(strings.Fields(string(raw)), "check-ignore") {
		t.Errorf("writing env files did not run the configured git (%v): it ran %q", err, raw)
	}
}
