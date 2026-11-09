package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/infra/pagetime"
	"operators-mcp/internal/ports"
)

// evidenceRepo is a repository with a bare origin and one worktree.
func evidenceRepo(t *testing.T) (repo, origin, wt string) {
	t.Helper()
	repo = initRepo(t)
	origin = filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, repo, "init", "--bare", "-q", "-b", "main", origin)
	gitIn(t, repo, "remote", "add", "origin", origin)
	gitIn(t, repo, "push", "-q", "-u", "origin", "main")
	wt = filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "feature", wt, "main")
	return
}

func TestBranchEvidence_SnapshotNamesBranchesRemotesAndWorktrees(t *testing.T) {
	repo, origin, wt := evidenceRepo(t)
	gitIn(t, repo, "branch", "other")
	var ev ports.GitBranchEvidence = NewCode()
	snap, err := ev.BranchSnapshot(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"main", "feature", "other"} {
		if len(snap.Branches[b]) != 40 {
			t.Errorf("branch %s head = %q", b, snap.Branches[b])
		}
	}
	if len(snap.RemoteRefs["origin/main"]) != 40 {
		t.Errorf("remote refs = %v", snap.RemoteRefs)
	}
	if len(snap.Remotes) != 1 || snap.Remotes[0].Name != "origin" || snap.Remotes[0].URL != origin {
		t.Errorf("remotes = %v", snap.Remotes)
	}
	got := map[string]string{}
	for _, w := range snap.Worktrees {
		got[w.Dir] = w.Branch
	}
	real := func(p string) string { r, _ := filepath.EvalSymlinks(p); return r }
	if got[real(wt)] != "feature" || got[real(repo)] != "main" {
		t.Errorf("worktrees = %v", snap.Worktrees)
	}

	gitIn(t, repo, "branch", "-m", "other", "renamed")
	gitIn(t, repo, "branch", "-D", "renamed")
	snap, _ = ev.BranchSnapshot(context.Background(), repo)
	if _, ok := snap.Branches["other"]; ok {
		t.Error("deleted branch still in snapshot")
	}
	if _, ok := snap.Branches["renamed"]; ok {
		t.Error("deleted branch still in snapshot")
	}
}

func TestBranchEvidence_HEADReflogOfAWorktree(t *testing.T) {
	repo, _, wt := evidenceRepo(t)
	gitIn(t, wt, "checkout", "-q", "-b", "sneaky")
	log, err := NewCode().HEADReflog(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) == 0 || log[0].Message != "checkout: moving from feature to sneaky" {
		t.Errorf("reflog = %+v", log)
	}
	if log[0].At.IsZero() || len(log[0].New) != 40 {
		t.Errorf("entry = %+v", log[0])
	}
	main, err := NewCode().HEADReflog(context.Background(), repo)
	if err != nil || len(main) == 0 {
		t.Errorf("main checkout reflog = %v, %v", main, err)
	}
}

func TestBranchEvidence_RemoteRefUpdatesTellPushFromFetch(t *testing.T) {
	repo, origin, _ := evidenceRepo(t)
	gitIn(t, repo, "push", "-q", "origin", "feature")
	// another clone pushes a branch that this repository then fetches
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", "-q", origin, other)
	gitIn(t, other, "checkout", "-q", "-b", "theirs")
	if err := os.WriteFile(filepath.Join(other, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, other, "add", "x")
	gitIn(t, other, "commit", "-q", "-m", "x")
	gitIn(t, other, "push", "-q", "origin", "theirs")
	gitIn(t, repo, "fetch", "-q", "origin")

	ups, err := NewCode().RemoteRefUpdates(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ports.GitRemoteRefUpdate{}
	for _, u := range ups {
		by[u.Remote+"/"+u.Branch] = u
	}
	if u := by["origin/feature"]; !u.Push || len(u.New) != 40 || u.At.IsZero() {
		t.Errorf("feature = %+v", u)
	}
	if u, ok := by["origin/theirs"]; !ok || u.Push {
		t.Errorf("theirs = %+v (fetched, not pushed)", u)
	}
}

func TestBranchEvidence_SnapshotSpawnsDoNotGrowWithBranches(t *testing.T) {
	repo, _, _ := evidenceRepo(t)
	count := func() int {
		ctx, timing := pagetime.Start(context.Background())
		if _, err := NewCode().BranchSnapshot(ctx, repo); err != nil {
			t.Fatal(err)
		}
		return int(timing.Report(0).GitCalls)
	}
	few := count()
	for i := 0; i < 15; i++ {
		gitIn(t, repo, "branch", "b"+string(rune('a'+i)))
	}
	if many := count(); many != few {
		t.Errorf("spawns %d with few branches, %d with many", few, many)
	}
}
