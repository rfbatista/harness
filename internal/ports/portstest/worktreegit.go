package portstest

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// WorktreeGitTarget is where the runner recorded the fixture: its repository
// and one workspace per branch of WorktreeGitBranches (by branch name).
type WorktreeGitTarget struct {
	RepositoryID string
	Workspaces   map[string]string
	// PassInterval lets the watch interval go by on the clock the port's held
	// states age by, so the next read reads git again.
	PassInterval func()
}

// WorktreeGitConformance runs the worktree git state contract against the
// port newPort builds over the fixture.
func WorktreeGitConformance(t *testing.T, newPort func(t *testing.T, f *WorktreeGitFixture) (ports.WorktreeGit, WorktreeGitTarget)) {
	ctx := context.Background()
	f := NewWorktreeGitFixture(t)
	p, at := newPort(t, f)

	state := func(t *testing.T, branch string) *domain.GitState {
		t.Helper()
		st, err := p.GitState(ctx, at.Workspaces[branch])
		if err != nil {
			t.Fatalf("%s: %v", branch, err)
		}
		if st.WorkspaceID != at.Workspaces[branch] || st.RepositoryID != at.RepositoryID {
			t.Errorf("%s: workspace %q repository %q", branch, st.WorkspaceID, st.RepositoryID)
		}
		return st
	}
	wantUpstream := func(t *testing.T, branch, want string) *domain.GitState {
		t.Helper()
		st := state(t, branch)
		if st.Upstream.State != want {
			t.Errorf("%s: upstream state = %s, want %s", branch, st.Upstream.State, want)
		}
		return st
	}
	counts := func(t *testing.T, branch string, up domain.UpstreamState, ahead, behind int) {
		t.Helper()
		if up.Ahead == nil || up.Behind == nil || *up.Ahead != ahead || *up.Behind != behind {
			t.Errorf("%s: upstream counts = %v/%v, want %d/%d", branch, up.Ahead, up.Behind, ahead, behind)
		}
		if up.Ref == nil || *up.Ref != "origin/"+branch || up.Remote == nil || *up.Remote != "origin" {
			t.Errorf("%s: upstream ref %v remote %v", branch, up.Ref, up.Remote)
		}
	}

	t.Run("a repository never fetched says unknown where it has an upstream", func(t *testing.T) {
		for _, branch := range []string{"insync", "ahead", "behind", "diverged", "deleted", "gone"} {
			st := wantUpstream(t, branch, domain.UpstreamUnknown)
			if st.Upstream.Ahead != nil || st.Upstream.Behind != nil {
				t.Errorf("%s: unknown carries counts %v/%v", branch, st.Upstream.Ahead, st.Upstream.Behind)
			}
			if st.Upstream.Ref == nil || *st.Upstream.Ref != "origin/"+branch {
				t.Errorf("%s: unknown ref = %v", branch, st.Upstream.Ref)
			}
			if st.FetchedAt != nil {
				t.Errorf("%s: fetched_at = %v before any fetch", branch, st.FetchedAt)
			}
		}
		none := wantUpstream(t, "nopush", domain.UpstreamNone)
		if none.Upstream.Ref != nil || none.Upstream.Remote != nil || none.Upstream.Ahead != nil || none.Upstream.Behind != nil {
			t.Errorf("none carries %+v", none.Upstream)
		}
	})

	f.Fetch(t)
	fetchedBefore := time.Now().Add(-time.Minute)
	at.PassInterval()

	t.Run("every upstream state after a fetch", func(t *testing.T) {
		st := wantUpstream(t, "insync", domain.UpstreamInSync)
		counts(t, "insync", st.Upstream, 0, 0)
		if st.FetchedAt == nil || st.FetchedAt.Before(fetchedBefore) || st.FetchedAt.After(time.Now().Add(time.Minute)) {
			t.Errorf("fetched_at = %v after a fetch", st.FetchedAt)
		}
		st = wantUpstream(t, "ahead", domain.UpstreamAhead)
		counts(t, "ahead", st.Upstream, 1, 0)
		st = wantUpstream(t, "behind", domain.UpstreamBehind)
		counts(t, "behind", st.Upstream, 0, 1)
		st = wantUpstream(t, "diverged", domain.UpstreamDiverged)
		counts(t, "diverged", st.Upstream, 1, 1)
		st = wantUpstream(t, "deleted", domain.UpstreamGone)
		if st.Upstream.Ref == nil || *st.Upstream.Ref != "origin/deleted" || st.Upstream.Ahead != nil || st.Upstream.Behind != nil {
			t.Errorf("gone upstream = %+v", st.Upstream)
		}
		st = wantUpstream(t, "nopush", domain.UpstreamNone)
		if st.Upstream.Ref != nil || st.FetchedAt == nil {
			t.Errorf("none: ref %v, fetched_at %v (the repository was fetched)", st.Upstream.Ref, st.FetchedAt)
		}
	})

	t.Run("the branch, its head and its base comparison", func(t *testing.T) {
		for _, branch := range []string{"nopush", "insync", "ahead", "behind", "diverged"} {
			st := state(t, branch)
			if st.Branch != branch || st.Head != f.Heads[branch] || st.Gone {
				t.Errorf("%s: branch %q head %q gone %v", branch, st.Branch, st.Head, st.Gone)
			}
		}
		want := map[string]int{"nopush": 1, "insync": 0, "ahead": 1, "behind": 0, "diverged": 1}
		for branch, ahead := range want {
			if b := state(t, branch).Base; b.Ref != f.BaseRef || b.Ahead != ahead || b.Behind != 0 {
				t.Errorf("%s: base = %+v, want %s %d/0", branch, b, f.BaseRef, ahead)
			}
		}
	})

	t.Run("uncommitted files are counted tracked, untracked and staged", func(t *testing.T) {
		if u := state(t, "ahead").Uncommitted; u != (domain.Uncommitted{Tracked: 2, Untracked: 1, Staged: 1}) {
			t.Errorf("ahead uncommitted = %+v", u)
		}
		if u := state(t, "insync").Uncommitted; u != (domain.Uncommitted{}) {
			t.Errorf("a clean worktree = %+v", u)
		}
	})

	t.Run("a worktree that is gone answers from the repository", func(t *testing.T) {
		st := wantUpstream(t, "gone", domain.UpstreamInSync)
		if !st.Gone || st.Uncommitted != (domain.Uncommitted{}) || st.Branch != "gone" || st.Head != f.Heads["gone"] {
			t.Errorf("gone = gone %v, uncommitted %+v, branch %q head %q", st.Gone, st.Uncommitted, st.Branch, st.Head)
		}
		if st.Base.Ref != f.BaseRef || st.Base.Ahead != 1 || st.Base.Behind != 0 {
			t.Errorf("gone base = %+v", st.Base)
		}
	})

	t.Run("a base that no longer resolves has no counts", func(t *testing.T) {
		f.Git(t, f.RepoDir, "branch", "-m", "main", "trunk") // the workspaces were cut from "main"
		defer f.Git(t, f.RepoDir, "branch", "-m", "trunk", "main")
		at.PassInterval()
		st := state(t, "ahead")
		if st.Base.Ref != "" || st.Base.Ahead != 0 || st.Base.Behind != 0 {
			t.Errorf("base = %+v, want none", st.Base)
		}
		at.PassInterval()
	})

	t.Run("a state is read again once the watch interval has passed", func(t *testing.T) {
		wt := f.Worktrees["insync"]
		first := state(t, "insync")
		f.Git(t, wt, "commit", "-q", "--allow-empty", "-m", "more")
		if again := state(t, "insync"); again.Head != first.Head {
			t.Errorf("read within the interval answered a new head %s", again.Head)
		}
		at.PassInterval()
		later := state(t, "insync")
		if later.Head == first.Head || later.Upstream.State != domain.UpstreamAhead || later.ReadAt.Before(first.ReadAt) {
			t.Errorf("after the interval = head %s, upstream %s", later.Head, later.Upstream.State)
		}
	})

	t.Run("bad requests", func(t *testing.T) {
		_, err := p.GitState(ctx, "")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.GitState(ctx, "no-such-workspace")
		wantCode(t, err, "WORKSPACE_NOT_FOUND")
	})
}
