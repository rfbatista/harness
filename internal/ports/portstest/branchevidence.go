package portstest

import (
	"context"
	"path/filepath"
	"testing"

	"operators-mcp/internal/infra/pagetime"
	"operators-mcp/internal/ports"
)

// GitBranchEvidenceConformance runs the branch snapshot and evidence reads of
// the worktree git port (ADR 0070) against the adapter newEvidence builds,
// over the worktree git fixture and its bare origin.
func GitBranchEvidenceConformance(t *testing.T, newEvidence func() ports.GitBranchEvidence) {
	ctx := context.Background()
	f := NewWorktreeGitFixture(t)
	ev := newEvidence()
	real := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			return p
		}
		return r
	}

	t.Run("the snapshot names branches, remote-tracking refs, remotes and worktrees", func(t *testing.T) {
		snap, err := ev.BranchSnapshot(ctx, f.RepoDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range append([]string{"main"}, WorktreeGitBranches...) {
			if len(snap.Branches[b]) != 40 {
				t.Errorf("branch %s head = %q", b, snap.Branches[b])
			}
		}
		if snap.Branches["ahead"] != f.Heads["ahead"] {
			t.Errorf("ahead head = %s, want %s", snap.Branches["ahead"], f.Heads["ahead"])
		}
		if len(snap.RemoteRefs["origin/main"]) != 40 {
			t.Errorf("remote refs = %v", snap.RemoteRefs)
		}
		urls := map[string]string{}
		for _, r := range snap.Remotes {
			urls[r.Name] = r.URL
		}
		if urls["origin"] != f.OriginDir {
			t.Errorf("remotes = %v, want origin at %s", snap.Remotes, f.OriginDir)
		}
		at := map[string]string{}
		for _, w := range snap.Worktrees {
			at[w.Dir] = w.Branch
		}
		if at[real(f.Worktrees["ahead"])] != "ahead" || at[real(f.RepoDir)] != "main" {
			t.Errorf("worktrees = %+v", snap.Worktrees)
		}
		if _, there := at[real(f.Worktrees["gone"])]; there {
			t.Errorf("a removed worktree is listed: %+v", snap.Worktrees)
		}
	})

	t.Run("a created, renamed and deleted branch show in the next snapshot", func(t *testing.T) {
		f.Git(t, f.RepoDir, "branch", "made", "main")
		f.Git(t, f.RepoDir, "branch", "-m", "made", "renamed")
		snap, _ := ev.BranchSnapshot(ctx, f.RepoDir)
		if _, ok := snap.Branches["made"]; ok || len(snap.Branches["renamed"]) != 40 {
			t.Errorf("after the rename: %v", snap.Branches)
		}
		f.Git(t, f.RepoDir, "branch", "-D", "renamed")
		snap, _ = ev.BranchSnapshot(ctx, f.RepoDir)
		if _, ok := snap.Branches["renamed"]; ok {
			t.Error("a deleted branch is still in the snapshot")
		}
	})

	t.Run("the HEAD reflog of a worktree is its own, newest first", func(t *testing.T) {
		wt := f.Worktrees["insync"]
		f.Git(t, wt, "checkout", "-q", "-b", "sneaky")
		log, err := ev.HEADReflog(ctx, wt)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) == 0 || log[0].Message != "checkout: moving from insync to sneaky" || log[0].At.IsZero() || len(log[0].New) != 40 {
			t.Fatalf("reflog = %+v", log)
		}
		main, err := ev.HEADReflog(ctx, f.RepoDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range main {
			if e.Message == "checkout: moving from insync to sneaky" {
				t.Error("the main checkout's reflog holds another worktree's move")
			}
		}
		f.Git(t, wt, "checkout", "-q", "insync")
	})

	t.Run("a push and a fetch of a remote-tracking ref are told apart", func(t *testing.T) {
		f.Git(t, f.Worktrees["nopush"], "push", "-q", "origin", "nopush")
		f.PushFromOtherClone(t, "behind", "elsewhere.txt")
		f.Fetch(t)
		ups, err := ev.RemoteRefUpdates(ctx, f.RepoDir)
		if err != nil {
			t.Fatal(err)
		}
		newest := map[string]ports.GitRemoteRefUpdate{}
		for _, u := range ups { // newest first
			if _, seen := newest[u.Remote+"/"+u.Branch]; !seen {
				newest[u.Remote+"/"+u.Branch] = u
			}
		}
		if u := newest["origin/nopush"]; !u.Push || len(u.New) != 40 || u.At.IsZero() {
			t.Errorf("nopush = %+v, want a push", u)
		}
		if u, ok := newest["origin/behind"]; !ok || u.Push {
			t.Errorf("behind = %+v, want a fetch", u)
		}
	})

	t.Run("the reads cost the same number of git runs however many branches there are", func(t *testing.T) {
		runs := func() int {
			c, timing := pagetime.Start(ctx)
			if _, err := ev.BranchSnapshot(c, f.RepoDir); err != nil {
				t.Fatal(err)
			}
			return int(timing.Report(0).GitCalls)
		}
		few := runs()
		for _, n := range []string{"bulk-a", "bulk-b", "bulk-c", "bulk-d", "bulk-e", "bulk-f"} {
			f.Git(t, f.RepoDir, "branch", n, "main")
		}
		if many := runs(); many != few {
			t.Errorf("%d git runs with few branches, %d with more", few, many)
		}
	})
}
