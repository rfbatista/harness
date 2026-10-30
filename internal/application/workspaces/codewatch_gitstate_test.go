package workspaces_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// countingReader counts the git runs a state read makes.
type countingReader struct {
	ports.GitWorktreeReader
	runs atomic.Int64
}

func (c *countingReader) WorktreeStatus(ctx context.Context, dir string) (*ports.GitWorktreeStatus, error) {
	c.runs.Add(1)
	return c.GitWorktreeReader.WorktreeStatus(ctx, dir)
}

func (c *countingReader) BranchUpstream(ctx context.Context, root, branch string) (ports.GitBranchUpstream, error) {
	c.runs.Add(1)
	return c.GitWorktreeReader.BranchUpstream(ctx, root, branch)
}

func (c *countingReader) Divergence(ctx context.Context, dir, base, branch string) (int, int, error) {
	c.runs.Add(1)
	return c.GitWorktreeReader.Divergence(ctx, dir, base, branch)
}

func (c *countingReader) CurrentBranch(ctx context.Context, dir string) (string, error) {
	c.runs.Add(1)
	return c.GitWorktreeReader.CurrentBranch(ctx, dir)
}

func (c *countingReader) ResolveCommit(ctx context.Context, dir, ref string) (string, error) {
	c.runs.Add(1)
	return c.GitWorktreeReader.ResolveCommit(ctx, dir, ref)
}

type watchedGit struct {
	f       *portstest.WorktreeGitFixture
	svc     ports.WorktreeGit
	at      portstest.WorktreeGitTarget
	feed    *workspacestest.FeedRecorder
	watched workspacestest.Watched
	reads   *countingReader
}

func newWatchedGit(t *testing.T) *watchedGit {
	t.Helper()
	w := &watchedGit{f: portstest.NewWorktreeGitFixture(t), reads: &countingReader{}}
	svc, at, feed := workspacestest.NewWorktreeGitWithFeed(t, w.f,
		workspacestest.WithCodeWatcher(&w.watched),
		workspacestest.WithGitReader(func(r ports.GitWorktreeReader) ports.GitWorktreeReader {
			w.reads.GitWorktreeReader = r
			return w.reads
		}))
	w.svc, w.at, w.feed = svc, at, feed
	if _, err := svc.FetchRemote(context.Background(), at.RepositoryID); err != nil {
		t.Fatal(err)
	}
	// The first look only records.
	w.watched.Poll()
	return w
}

// The watcher's change of a running worktree is announced as code_changed and,
// in the same tick, the git state it left.
func TestARunningWorktreeChangeIsAnnouncedWithItsGitState(t *testing.T) {
	w := newWatchedGit(t)
	before := len(w.feed.Kinds())
	if err := os.WriteFile(filepath.Join(w.f.Worktrees["ahead"], "another.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.watched.Poll()
	got := w.feed.Kinds()[before:]
	if !slices.Equal(got, []string{"code_changed", "git_state"}) {
		t.Fatalf("announced %v, want a code_changed and then its git_state", got)
	}
	states := w.feed.GitStates()
	last := states[len(states)-1]
	if last.WorkspaceID != w.at.Workspaces["ahead"] || last.SessionID != w.at.Sessions["ahead"] || last.ProjectID != "git-project" ||
		last.Uncommitted != (domain.Uncommitted{Tracked: 2, Untracked: 2, Staged: 1}) {
		t.Errorf("announced %+v", last.GitState)
	}

	// Nothing moved: nothing is announced.
	w.watched.Poll()
	if got := w.feed.Kinds()[before:]; len(got) != 2 {
		t.Errorf("an unchanged worktree announced %v", got)
	}
}

// The state moving without the code (a push seen from the worktree) is
// announced on its own, and a push or fetch is not a code change.
func TestAStateThatMovesAloneIsAnnouncedAlone(t *testing.T) {
	w := newWatchedGit(t)
	before := len(w.feed.Kinds())
	w.f.Git(t, w.f.Worktrees["ahead"], "push", "-q", "origin", "ahead")
	w.watched.Poll()
	got := w.feed.Kinds()[before:]
	if !slices.Equal(got, []string{"git_state"}) {
		t.Fatalf("announced %v, want only a git_state", got)
	}
	states := w.feed.GitStates()
	if st := states[len(states)-1]; st.WorkspaceID != w.at.Workspaces["ahead"] || st.Upstream.State != domain.UpstreamInSync {
		t.Errorf("announced %+v, want ahead in sync after its push", st.GitState)
	}
}

// At most one pair per workspace per interval.
func TestARunningWorktreeIsAnnouncedAtMostOncePerInterval(t *testing.T) {
	w := newWatchedGit(t)
	before := len(w.feed.Kinds())
	path := filepath.Join(w.f.Worktrees["ahead"], "burst.txt")
	for i, content := range []string{"1\n", "22\n", "333\n"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			w.watched.Poll()
		} else {
			w.watched.PollNow()
		}
	}
	if got := w.feed.Kinds()[before:]; len(got) != 2 {
		t.Errorf("three changes in one interval announced %v, want one pair", got)
	}
}

// A read of a running session's git state costs no git run: the watcher's
// poll keeps it, and confirms it while nothing moves.
func TestAReadOfARunningSessionsGitStateRunsNoGit(t *testing.T) {
	w := newWatchedGit(t)
	ctx := context.Background()
	for range 3 {
		w.at.PassInterval()
		w.watched.PollNow() // the watcher confirms the state each interval
		runs := w.reads.runs.Load()
		for _, branch := range portstest.WorktreeGitBranches {
			if branch == "gone" {
				continue
			}
			if _, err := w.svc.GitState(ctx, w.at.Workspaces[branch]); err != nil {
				t.Fatal(err)
			}
		}
		if got := w.reads.runs.Load() - runs; got != 0 {
			t.Fatalf("reading the running sessions' states ran git %d times, want none", got)
		}
	}
}

// The staged files of a running session's working diff come from the
// watcher's look: the diff runs no git for them. Without a watcher it does.
func TestTheWorkingDiffOfARunningSessionTakesItsStagedFilesFromTheWatcher(t *testing.T) {
	f := portstest.NewWorktreeGitFixture(t)
	var (
		watched workspacestest.Watched
		code    workspacestest.CodeHandle
	)
	_, at, _ := workspacestest.NewWorktreeGitWithFeed(t, f, workspacestest.WithCodeWatcher(&watched), workspacestest.WithCode(&code))
	ctx := context.Background()
	read := func() *domain.CodeDiff {
		t.Helper()
		d, err := code.Code.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: at.Workspaces["ahead"], Scope: "working"})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	// Nobody has looked yet: git lists them.
	if d := read(); d.Sections == nil || d.Sections.Staged != 1 || code.StagedRuns.Load() != 1 {
		t.Fatalf("unwatched: sections %+v, %d git runs for the staged files, want 1 staged and 1 run", d.Sections, code.StagedRuns.Load())
	}
	watched.Poll() // the first look records the status
	runs := code.StagedRuns.Load()
	d := read()
	if code.StagedRuns.Load() != runs {
		t.Errorf("a watched worktree's diff ran git for its staged files")
	}
	staged := map[string]bool{}
	for _, fd := range d.Files {
		staged[fd.Path] = fd.Staged
	}
	if d.Sections == nil || d.Sections.Staged != 1 || !staged["staged.txt"] || staged["tracked.txt"] {
		t.Errorf("staged from the watcher: sections %+v, files %v", d.Sections, staged)
	}
}
