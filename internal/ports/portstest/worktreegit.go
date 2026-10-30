package portstest

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// WorktreeGitTarget is where the runner recorded the fixture: its repository
// and one workspace per branch of WorktreeGitBranches (by branch name).
type WorktreeGitTarget struct {
	RepositoryID string
	// NoRemoteRepositoryID is the fixture's repository with no origin.
	NoRemoteRepositoryID string
	// NoRemoteWorkspaceID is a workspace of that repository.
	NoRemoteWorkspaceID string
	Workspaces          map[string]string
	// TaskID names a task with one running session per workspace (Sessions by
	// branch) and one session with no workspace. TaskOrder is the branches of
	// its sessions in the order the task page lists them.
	TaskID    string
	Sessions  map[string]string
	TaskOrder []string
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

	t.Run("a push lifts unknown on a repository never fetched", func(t *testing.T) {
		f2 := NewWorktreeGitFixture(t)
		p2, at2 := newPort(t, f2)
		before, err := p2.GitState(ctx, at2.Workspaces["nopush"])
		if err != nil || before.PushedAt != nil || before.FetchedAt != nil {
			t.Fatalf("before the push: %+v, %v", before, err)
		}
		if st, _ := p2.GitState(ctx, at2.Workspaces["insync"]); st.Upstream.State != domain.UpstreamUnknown || st.PushedAt != nil {
			t.Fatalf("a branch nobody pushed through the server is unknown: %+v", st)
		}
		start := time.Now().Add(-time.Minute)
		st, err := p2.PushBranch(ctx, at2.Workspaces["nopush"], before.Head)
		if err != nil {
			t.Fatal(err)
		}
		if st.Upstream.State != domain.UpstreamInSync || st.FetchedAt != nil || st.PushedAt == nil || st.PushedAt.Before(start) {
			t.Errorf("after the push: upstream %s, fetched_at %v, pushed_at %v; want in_sync, no fetch, pushed now", st.Upstream.State, st.FetchedAt, st.PushedAt)
		}
		at2.PassInterval()
		if again, _ := p2.GitState(ctx, at2.Workspaces["nopush"]); again.Upstream.State != domain.UpstreamInSync || again.PushedAt == nil {
			t.Errorf("the pushed time is kept: %+v", again)
		}
		// The other branches have neither time.
		at2.PassInterval()
		if other, _ := p2.GitState(ctx, at2.Workspaces["ahead"]); other.PushedAt != nil || other.Upstream.State != domain.UpstreamUnknown {
			t.Errorf("another workspace: %+v", other)
		}
	})

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

	// The fetch is the port's: no credential is needed for a file remote.
	fetchedBefore := time.Now().Add(-time.Minute)
	fetchedAt, err := p.FetchRemote(ctx, at.RepositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if fetchedAt.Before(fetchedBefore) || fetchedAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("fetch answered %v", fetchedAt)
	}
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
			if b := state(t, branch).Base; b.Ref != f.BaseRef || b.Gone || b.Ahead != ahead || b.Behind != 0 {
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

	t.Run("a base that no longer resolves keeps its name and says so", func(t *testing.T) {
		f.Git(t, f.RepoDir, "branch", "-m", "main", "trunk") // the workspaces were cut from "main"
		defer f.Git(t, f.RepoDir, "branch", "-m", "trunk", "main")
		at.PassInterval()
		st := state(t, "ahead")
		if st.Base.Ref != f.BaseRef || !st.Base.Gone || st.Base.Ahead != 0 || st.Base.Behind != 0 {
			t.Errorf("base = %+v, want the name %s, gone, 0/0", st.Base, f.BaseRef)
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

	t.Run("a task's states come in one read, in the session list's order", func(t *testing.T) {
		at.PassInterval()
		states, err := p.TaskGitStates(ctx, at.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		want := at.TaskOrder
		if len(states) != len(want) {
			t.Fatalf("task states = %d, want %d", len(states), len(want))
		}
		for i, branch := range want {
			st := states[i]
			if st.WorkspaceID != at.Workspaces[branch] || st.SessionID != at.Sessions[branch] || st.Branch != branch {
				t.Errorf("state %d = workspace %s session %s branch %s, want %s", i, st.WorkspaceID, st.SessionID, st.Branch, branch)
			}
			if one := state(t, branch); one.Upstream.State != st.Upstream.State || one.Gone != st.Gone || one.Uncommitted != st.Uncommitted {
				t.Errorf("%s: the task read and the workspace read differ: %+v / %+v", branch, st, one)
			}
		}
		for _, st := range states {
			if st.Gone != (st.Branch == "gone") {
				t.Errorf("%s: gone = %v", st.Branch, st.Gone)
			}
		}
		if len(want) != len(WorktreeGitBranches) {
			t.Errorf("the task order names %d branches, want all %d, the gone one included", len(want), len(WorktreeGitBranches))
		}
		if one := state(t, "ahead"); one.SessionID != at.Sessions["ahead"] {
			t.Errorf("a workspace read names its session %q, want %q", one.SessionID, at.Sessions["ahead"])
		}
	})

	t.Run("a task read needs a known task", func(t *testing.T) {
		_, err := p.TaskGitStates(ctx, "")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.TaskGitStates(ctx, "no-such-task")
		wantCode(t, err, "TASK_NOT_FOUND")
	})

	t.Run("a fetch moves the fetch age and the counts it answers", func(t *testing.T) {
		// origin moves again behind the repository's back; nothing shows until a fetch.
		f.PushFromOtherClone(t, "insync", "more-remote.txt")
		at.PassInterval()
		if st := state(t, "insync"); st.Upstream.State != domain.UpstreamAhead {
			t.Fatalf("before the fetch: %s", st.Upstream.State) // ahead by its own extra commit only
		}
		again, err := p.FetchRemote(ctx, at.RepositoryID)
		if err != nil {
			t.Fatal(err)
		}
		if again.Before(fetchedAt) {
			t.Errorf("fetched_at went back: %v then %v", fetchedAt, again)
		}
		at.PassInterval()
		st := state(t, "insync")
		if st.Upstream.State != domain.UpstreamDiverged {
			t.Errorf("after the fetch: %s, want diverged (a local commit and a remote one)", st.Upstream.State)
		}
		if st.FetchedAt == nil || st.FetchedAt.Before(fetchedAt) {
			t.Errorf("fetch age = %v after %v", st.FetchedAt, fetchedAt)
		}
	})

	t.Run("a fetch needs a repository with an origin", func(t *testing.T) {
		_, err := p.FetchRemote(ctx, "")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.FetchRemote(ctx, "no-such-repository")
		wantCode(t, err, "REPOSITORY_NOT_FOUND")
		_, err = p.FetchRemote(ctx, at.NoRemoteRepositoryID)
		wantCode(t, err, "NO_REMOTE")
	})

	t.Run("a push the remote refuses for what it carries is PUSH_REJECTED", func(t *testing.T) {
		// A hook declining the branch.
		defer f.RefuseInOrigin(t, "nopush")()
		head := state(t, "nopush").Head
		_, err := p.PushBranch(ctx, at.Workspaces["nopush"], head)
		wantCode(t, err, "PUSH_REJECTED")
		if got := f.OriginRev(t, "nopush"); got != "" {
			t.Errorf("origin holds nopush at %s after a refused push", got)
		}
		if st, _ := detailsOf(t, err)["state"].(map[string]any); st["workspace_id"] != at.Workspaces["nopush"] {
			t.Errorf("details = %v, want the state", detailsOf(t, err))
		}

		// Never forced: a branch diverged from its upstream is refused, and origin keeps its commit.
		before := f.OriginRev(t, "diverged")
		_, err = p.PushBranch(ctx, at.Workspaces["diverged"], state(t, "diverged").Head)
		wantCode(t, err, "PUSH_REJECTED")
		if got := f.OriginRev(t, "diverged"); got != before {
			t.Errorf("origin's diverged moved from %s to %s: the push was forced", before, got)
		}
		details := detailsOf(t, err)
		st, _ := details["state"].(map[string]any)
		if up, _ := st["upstream"].(map[string]any); up["state"] != domain.UpstreamDiverged {
			t.Errorf("details state = %v, want the re-read state, diverged", details)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "reject") {
			t.Errorf("the message does not carry git's reason: %v", err)
		}
	})

	t.Run("a push of a branch whose head moved is PUSH_STALE and pushes nothing", func(t *testing.T) {
		before := f.OriginRev(t, "ahead")
		_, err := p.PushBranch(ctx, at.Workspaces["ahead"], "0000000000000000000000000000000000000000")
		wantCode(t, err, "PUSH_STALE")
		if st, _ := detailsOf(t, err)["state"].(map[string]any); st["head"] != f.Heads["ahead"] {
			t.Errorf("details = %v, want the state with the head now", detailsOf(t, err))
		}
		if got := f.OriginRev(t, "ahead"); got != before {
			t.Errorf("origin's ahead moved from %s to %s", before, got)
		}
	})

	t.Run("a push publishes the branch and sets its upstream", func(t *testing.T) {
		// Never pushed: origin gets the branch, and it tracks it.
		head := state(t, "nopush").Head
		st, err := p.PushBranch(ctx, at.Workspaces["nopush"], head)
		if err != nil {
			t.Fatal(err)
		}
		if st.Upstream.State != domain.UpstreamInSync || st.Upstream.Ref == nil || *st.Upstream.Ref != "origin/nopush" || st.Upstream.Remote == nil || *st.Upstream.Remote != "origin" {
			t.Errorf("after the first push: %+v", st.Upstream)
		}
		if got := f.OriginRev(t, "nopush"); got != head {
			t.Errorf("origin holds nopush at %q, want %s", got, head)
		}
		if again := state(t, "nopush"); again.Upstream.State != domain.UpstreamInSync {
			t.Errorf("the state read after the push = %s", again.Upstream.State)
		}

		// Ahead, with uncommitted work: the commits go, the worktree stays as it was.
		wt := f.Worktrees["ahead"]
		statusBefore := f.Git(t, wt, "status", "--porcelain")
		head = state(t, "ahead").Head
		st, err = p.PushBranch(ctx, at.Workspaces["ahead"], head)
		if err != nil {
			t.Fatal(err)
		}
		if st.Upstream.State != domain.UpstreamInSync || st.Uncommitted != (domain.Uncommitted{Tracked: 2, Untracked: 1, Staged: 1}) {
			t.Errorf("after pushing ahead: %+v, uncommitted %+v", st.Upstream, st.Uncommitted)
		}
		if got := f.OriginRev(t, "ahead"); got != head {
			t.Errorf("origin holds ahead at %q, want %s", got, head)
		}
		if after := f.Git(t, wt, "status", "--porcelain"); after != statusBefore || f.Git(t, wt, "rev-parse", "HEAD") != head {
			t.Errorf("the push touched the worktree:\n%s\nthen\n%s", statusBefore, after)
		}

		// A branch whose upstream was deleted is published again.
		st, err = p.PushBranch(ctx, at.Workspaces["deleted"], state(t, "deleted").Head)
		if err != nil || st.Upstream.State != domain.UpstreamInSync {
			t.Errorf("deleted upstream pushed again = %+v, %v", st, err)
		}
	})

	t.Run("a push needs a worktree on disk and a repository with an origin", func(t *testing.T) {
		_, err := p.PushBranch(ctx, at.Workspaces["gone"], f.Heads["gone"])
		wantCode(t, err, "WORKSPACE_MISSING")
		_, err = p.PushBranch(ctx, at.NoRemoteWorkspaceID, "abc")
		wantCode(t, err, "NO_REMOTE")
		_, err = p.PushBranch(ctx, "", "abc")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.PushBranch(ctx, at.Workspaces["ahead"], "")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.PushBranch(ctx, "no-such-workspace", "abc")
		wantCode(t, err, "WORKSPACE_NOT_FOUND")
	})

	t.Run("bad requests", func(t *testing.T) {
		_, err := p.GitState(ctx, "")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.GitState(ctx, "no-such-workspace")
		wantCode(t, err, "WORKSPACE_NOT_FOUND")
	})
}
