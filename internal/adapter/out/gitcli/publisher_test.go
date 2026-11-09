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
	if err := p.FetchBranch(ctx, repo, "main", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "log", "-1", "--format=%s", "refs/remotes/origin/main")); got != "upstream moved" {
		t.Fatalf("origin/main after fetch is at %q", got)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "log", "-1", "--format=%s", "main")); got != "init" {
		t.Fatalf("the local main moved to %q; a fetch must only touch origin/main", got)
	}

	if err := p.FetchBranch(ctx, repo, "no-such-branch", nil); !errors.Is(err, ports.ErrRemoteBranchNotFound) {
		t.Fatalf("fetch of a missing branch = %v, want ErrRemoteBranchNotFound", err)
	}
}

func TestPublisher_PushPublishesTheBranchAndLeavesTheCheckoutAlone(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, worktree, origin := publishRepo(t)
	statusBefore := gitOut(t, repo, "status", "--porcelain")
	headBefore := gitOut(t, repo, "symbolic-ref", "HEAD")

	if err := p.Push(ctx, repo, "agent/x", nil); err != nil {
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
	if err := p.Push(ctx, repo, "agent/x", nil); err != nil {
		t.Fatalf("pushing again: %v", err)
	}
}

func TestPublisher_PushNeverForces(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, worktree, origin := publishRepo(t)
	if err := p.Push(ctx, repo, "agent/x", nil); err != nil {
		t.Fatal(err)
	}
	pushed := strings.TrimSpace(gitOut(t, origin, "rev-parse", "refs/heads/agent/x"))
	// History rewritten locally: the push is no longer a fast-forward.
	gitIn(t, worktree, "reset", "-q", "--hard", "HEAD~1")
	gitIn(t, worktree, "commit", "-q", "--allow-empty", "-m", "rewritten")

	err := p.Push(ctx, repo, "agent/x", nil)
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

func TestPublisher_OriginBranchesAndFetchOrigin(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, _, origin := publishRepo(t)

	// A repository never fetched has no fetch time.
	never := initRepo(t)
	if got, at, err := p.OriginBranches(ctx, never); err != nil || len(got) != 0 || !at.IsZero() {
		t.Fatalf("never fetched = %v %v %v", got, at, err)
	}

	got, at, err := p.OriginBranches(ctx, repo)
	if err != nil || strings.Join(got, ",") != "main" || at.IsZero() {
		t.Fatalf("branches = %v at %v err %v, want [main] and a fetch time", got, at, err)
	}
	mainTip := gitTrim(t, repo, "rev-parse", "main")
	checkedOut := gitTrim(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	readme, _ := os.ReadFile(filepath.Join(repo, "README.md"))

	// Origin gains a branch (and a HEAD alias) and loses another, elsewhere.
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", origin, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	gitIn(t, other, "checkout", "-q", "-B", "main", "origin/main")
	gitIn(t, other, "push", "-q", "origin", "main:release/1.0", "main:old")
	gitIn(t, repo, "fetch", "-q", "origin")
	gitIn(t, other, "push", "-q", "origin", "--delete", "old")
	gitIn(t, other, "branch", "feature", "main")
	gitIn(t, other, "push", "-q", "origin", "feature")
	gitIn(t, repo, "remote", "set-head", "origin", "main")

	if err := p.FetchOrigin(ctx, repo, nil); err != nil {
		t.Fatal(err)
	}
	got, after, err := p.OriginBranches(ctx, repo)
	if err != nil || strings.Join(got, ",") != "feature,main,release/1.0" {
		t.Fatalf("after the fetch = %v err %v: HEAD is excluded, old is pruned, by name", got, err)
	}
	if after.Before(at) {
		t.Fatalf("fetch time went back: %v then %v", at, after)
	}
	if gitTrim(t, repo, "rev-parse", "main") != mainTip || gitTrim(t, repo, "rev-parse", "--abbrev-ref", "HEAD") != checkedOut {
		t.Fatal("a fetch moved a local branch or the checkout")
	}
	if now, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(now) != string(readme) {
		t.Fatal("a fetch touched the working tree")
	}
}

func TestPublisher_FetchOriginWithoutACredentialIsRefusedAsAuth(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "remote", "add", "origin", "https://127.0.0.1:1/nothing.git")
	err := NewBranchPublisher().FetchOrigin(context.Background(), repo, nil)
	if err == nil {
		t.Fatal("a fetch from nowhere succeeded")
	}
	if errors.Is(err, ports.ErrRemoteAuthRejected) {
		t.Fatalf("an unreachable origin is no auth refusal: %v", err)
	}
}

func TestPublisher_RemoteURLReadsAnyNamedRemote(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, _, origin := publishRepo(t)
	if got, err := p.RemoteURL(ctx, repo, "origin"); err != nil || got != origin {
		t.Fatalf("origin = %q, %v; want %q", got, err, origin)
	}
	if got, err := p.RemoteURL(ctx, repo, "nope"); err != nil || got != "" {
		t.Fatalf("unknown remote = %q, %v; want \"\", nil", got, err)
	}
}

func TestPublisher_PushToANamedRemoteUnderAnotherBranchName(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, wt, _ := publishRepo(t)
	mirror := filepath.Join(t.TempDir(), "mirror.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", mirror).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	gitIn(t, repo, "remote", "add", "mirror", mirror)
	head := strings.TrimSpace(gitOut(t, wt, "rev-parse", "HEAD"))

	if err := p.PushTo(ctx, repo, "agent/x", "", "mirror", "review/x", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOut(t, mirror, "rev-parse", "refs/heads/review/x")); got != head {
		t.Errorf("mirror review/x = %s, want %s", got, head)
	}
	if out := gitOut(t, repo, "for-each-ref", "refs/remotes/origin/agent"); strings.TrimSpace(out) != "" {
		t.Errorf("origin got agent/x: %s", out)
	}
	if up := strings.TrimSpace(gitOut(t, repo, "for-each-ref", "--format=%(upstream)", "refs/heads/agent/x")); up != "" {
		t.Errorf("a push to another remote or name set an upstream: %q", up)
	}
}

func TestPublisher_PushToNeverForces(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, wt, origin := publishRepo(t)
	if err := p.PushTo(ctx, repo, "agent/x", "", "origin", "agent/x", nil); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "reset", "-q", "--hard", "HEAD~1")
	if err := os.WriteFile(filepath.Join(wt, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-q", "-m", "diverge")
	before := strings.TrimSpace(gitOut(t, origin, "rev-parse", "refs/heads/agent/x"))
	err := p.PushTo(ctx, repo, "agent/x", "", "origin", "agent/x", nil)
	if !errors.Is(err, ports.ErrPushRejected) {
		t.Fatalf("diverged push = %v, want ErrPushRejected", err)
	}
	if got := strings.TrimSpace(gitOut(t, origin, "rev-parse", "refs/heads/agent/x")); got != before {
		t.Errorf("origin moved: the push was forced")
	}
}

func TestPublisher_CommitsToPushCountsAgainstTheRemoteTrackingRefOrTheBase(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, _, _ := publishRepo(t)
	// remote branch unknown: against the base (main), two commits
	n, known, err := p.CommitsToPush(ctx, repo, "agent/x", "origin", "agent/x", "main")
	if err != nil || known || n != 2 {
		t.Fatalf("unknown remote branch = %d known=%v, %v; want 2 false", n, known, err)
	}
	if err := p.PushTo(ctx, repo, "agent/x", "", "origin", "agent/x", nil); err != nil {
		t.Fatal(err)
	}
	n, known, err = p.CommitsToPush(ctx, repo, "agent/x", "origin", "agent/x", "main")
	if err != nil || !known || n != 0 {
		t.Fatalf("after the push = %d known=%v, %v; want 0 true", n, known, err)
	}
}

func TestPublisher_CommitsToPushWithNoBaseCountsWhatTheRemoteLacks(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, _, _ := publishRepo(t)
	// origin holds main only: agent/x has its two commits, and main's are there.
	n, known, err := p.CommitsToPush(ctx, repo, "agent/x", "origin", "agent/x", "")
	if err != nil || known || n != 2 {
		t.Fatalf("no base = %d known=%v, %v; want 2 false", n, known, err)
	}
}

func TestPublisher_PushToWithACommitPushesThatCommitAndNotTheHead(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, wt, origin := publishRepo(t)
	pinned := strings.TrimSpace(gitOut(t, wt, "rev-parse", "HEAD~1"))
	if err := p.PushTo(ctx, repo, "agent/x", pinned, "origin", "review/x", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOut(t, origin, "rev-parse", "refs/heads/review/x")); got != pinned {
		t.Errorf("origin holds %s, want the pinned %s", got, pinned)
	}
}

func TestPublisher_RemoteURLIsThePushurlWhenThereIsOne(t *testing.T) {
	ctx := context.Background()
	p := NewBranchPublisher()
	repo, _, origin := publishRepo(t)
	gitIn(t, repo, "config", "remote.origin.pushurl", "/somewhere/else.git")
	if got, err := p.RemoteURL(ctx, repo, "origin"); err != nil || got != "/somewhere/else.git" {
		t.Fatalf("url = %q, %v; want the pushurl (the url is %s)", got, err, origin)
	}
}
