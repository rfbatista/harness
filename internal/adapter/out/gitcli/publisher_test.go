package gitcli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// publishRepo is a checkout whose origin is a local bare repository holding
// main, plus a session's worktree on agent/x with two commits ahead of main.
// The checkout keeps main checked out with an uncommitted edit, which a
// publish must leave alone.
func publishRepo(t *testing.T) (repo, worktree, origin string) {
	t.Helper()
	repo = initRepo(t)
	origin = filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", origin).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	gitIn(t, repo, "remote", "add", "origin", origin)
	gitIn(t, repo, "push", "-q", "origin", "main")
	gitIn(t, repo, "fetch", "-q", "origin")

	worktree = filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "agent/x", worktree, "main")
	for _, msg := range []string{"first change", "second change"} {
		if err := os.WriteFile(filepath.Join(worktree, strings.ReplaceAll(msg, " ", "-")+".txt"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, worktree, "add", ".")
		gitIn(t, worktree, "commit", "-q", "-m", msg)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("edited, not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, worktree, origin
}

func TestPublisher_OriginURL(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	bare := initRepo(t)
	if got, err := p.OriginURL(ctx, bare); err != nil || got != "" {
		t.Fatalf("no origin = %q, %v; want \"\", nil", got, err)
	}
	repo, _, origin := publishRepo(t)
	if got, err := p.OriginURL(ctx, repo); err != nil || got != origin {
		t.Fatalf("origin = %q, %v; want %q", got, err, origin)
	}
}

func TestPublisher_HasRefAndFetchBranch(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, _, origin := publishRepo(t)
	if !p.HasRef(ctx, repo, "refs/remotes/origin/main") {
		t.Fatal("origin/main should resolve")
	}
	if p.HasRef(ctx, repo, "refs/remotes/origin/agent/x") {
		t.Fatal("agent/x is not pushed yet")
	}

	// Origin moves on main from elsewhere; a fetch brings only that ref.
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", origin, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	gitIn(t, other, "commit", "-q", "--allow-empty", "-m", "upstream moved")
	gitIn(t, other, "push", "-q", "origin", "main")
	if err := p.FetchBranch(ctx, repo, "main"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "log", "-1", "--format=%s", "refs/remotes/origin/main")); got != "upstream moved" {
		t.Fatalf("origin/main after fetch is at %q", got)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "log", "-1", "--format=%s", "main")); got != "init" {
		t.Fatalf("the local main moved to %q; a fetch must only touch origin/main", got)
	}

	if err := p.FetchBranch(ctx, repo, "no-such-branch"); !errors.Is(err, ports.ErrRemoteBranchNotFound) {
		t.Fatalf("fetch of a missing branch = %v, want ErrRemoteBranchNotFound", err)
	}
}

func TestPublisher_PushPublishesTheBranchAndLeavesTheCheckoutAlone(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, worktree, origin := publishRepo(t)
	statusBefore := gitOut(t, repo, "status", "--porcelain")
	headBefore := gitOut(t, repo, "symbolic-ref", "HEAD")

	if err := p.Push(ctx, repo, "agent/x"); err != nil {
		t.Fatal(err)
	}
	local := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "HEAD"))
	remote := strings.TrimSpace(gitOut(t, origin, "rev-parse", "refs/heads/agent/x"))
	if local != remote {
		t.Fatalf("origin has agent/x at %s, the branch is at %s", remote, local)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "config", "--get", "branch.agent/x.remote")); got != "origin" {
		t.Fatalf("upstream remote = %q", got)
	}
	if !p.HasRef(ctx, repo, "refs/remotes/origin/agent/x") {
		t.Fatal("the remote-tracking ref was not updated")
	}
	if got := gitOut(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Fatalf("the checkout's status changed:\n%s\nwas\n%s", got, statusBefore)
	}
	if got := gitOut(t, repo, "symbolic-ref", "HEAD"); got != headBefore {
		t.Fatalf("the checkout's branch changed to %s", got)
	}
	// A second push with nothing new is fine.
	if err := p.Push(ctx, repo, "agent/x"); err != nil {
		t.Fatalf("pushing again: %v", err)
	}
}

func TestPublisher_PushNeverForces(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, worktree, origin := publishRepo(t)
	if err := p.Push(ctx, repo, "agent/x"); err != nil {
		t.Fatal(err)
	}
	pushed := strings.TrimSpace(gitOut(t, origin, "rev-parse", "refs/heads/agent/x"))
	// History rewritten locally: the push is no longer a fast-forward.
	gitIn(t, worktree, "reset", "-q", "--hard", "HEAD~1")
	gitIn(t, worktree, "commit", "-q", "--allow-empty", "-m", "rewritten")

	err := p.Push(ctx, repo, "agent/x")
	if err == nil {
		t.Fatal("a non-fast-forward push was accepted")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("the error does not carry git's reason: %v", err)
	}
	if got := strings.TrimSpace(gitOut(t, origin, "rev-parse", "refs/heads/agent/x")); got != pushed {
		t.Fatal("origin's branch was overwritten")
	}
}

func TestPublisher_SubjectsAreTheCommitsAheadNewestFirst(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, _, _ := publishRepo(t)
	got, err := p.Subjects(ctx, repo, "refs/remotes/origin/main", "agent/x", 10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "second change|first change" {
		t.Fatalf("subjects = %q", got)
	}
	if got, _ := p.Subjects(ctx, repo, "main", "agent/x", 1); len(got) != 1 || got[0] != "second change" {
		t.Fatalf("limited subjects = %q", got)
	}
	if got, err := p.Subjects(ctx, repo, "main", "main", 10); err != nil || len(got) != 0 {
		t.Fatalf("nothing ahead = %q, %v", got, err)
	}
}
