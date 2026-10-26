package workspaces_test

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func (f *mergeFixture) merger() *workspaces.Merges {
	return workspaces.NewMerges(workspaces.MergeDeps{
		Sessions: sqlite.NewSessionRepository(f.db), Workspaces: sqlite.NewWorkspaceRepository(f.db),
		Repositories: sqlite.NewRepositoryRepository(f.db), Git: gitcli.NewMerger(),
	})
}

func (f *mergeFixture) stored(branch string) *domain.Session {
	f.t.Helper()
	return sqlite.NewSessionRepository(f.db).Get(f.sessIDs[branch])
}

// snapshot is everything about the main checkout a merge into another branch
// must leave alone: branch, HEAD, status, the raw index and every file on disk.
func (f *mergeFixture) snapshot() string {
	f.t.Helper()
	var b strings.Builder
	for _, args := range [][]string{
		{"rev-parse", "--abbrev-ref", "HEAD"}, {"rev-parse", "HEAD"},
		{"status", "--porcelain=v2", "--branch", "--untracked-files=all"},
	} {
		b.WriteString(gitOut(f.t, f.repoDir, args...) + "\n")
	}
	idx, err := os.ReadFile(filepath.Join(f.repoDir, ".git", "index"))
	if err != nil {
		f.t.Fatal(err)
	}
	b.Write(idx)
	_ = filepath.WalkDir(f.repoDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if err == nil && !d.IsDir() {
			body, _ := os.ReadFile(p)
			b.WriteString(p + "\x00" + string(body) + "\n")
		}
		return nil
	})
	return b.String()
}

func (f *mergeFixture) dirtyMainCheckout() {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.repoDir, "README.md"), []byte("my edit\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repoDir, "untracked.txt"), []byte("u\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func TestMergeOffer_ListsEveryOtherLocalBranchWithItsState(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 2)
	gitRun(t, f.repoDir, "branch", "release", "main")
	gitRun(t, f.repoDir, "branch", "alpha", "main")
	f.dirtyMainCheckout()
	if err := os.WriteFile(filepath.Join(f.worktrees["feat"], "wip.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("UPDATE sessions SET task = 'Ship the thing' WHERE id = ?", f.sessIDs["feat"]).Error; err != nil {
		t.Fatal(err)
	}

	offer, err := f.merger().MergeOffer(context.Background(), f.sessIDs["feat"], "")
	if err != nil {
		t.Fatal(err)
	}
	want := []ports.MergeTarget{
		{Name: "main", Default: true, CheckedOut: true, Dirty: true},
		{Name: "alpha"},
		{Name: "release"},
	}
	if !slices.Equal(offer.Targets, want) {
		t.Fatalf("targets = %+v", offer.Targets)
	}
	// Dirty main is the default target and refuses; another target does not.
	if offer.Available || offer.Reason != domain.MergeCheckoutDirty || offer.Into != "main" || offer.Branch != "feat" ||
		offer.UncommittedChanges != 1 || offer.SuggestedMessage != "Ship the thing" {
		t.Fatalf("offer = %+v", offer)
	}
	offer, _ = f.merger().MergeOffer(context.Background(), f.sessIDs["feat"], "release")
	if !offer.Available || offer.Into != "release" || offer.Ahead != 2 || offer.Reason != "" {
		t.Fatalf("offer into release = %+v", offer)
	}
}

func TestMergeOffer_SaysWhyAMergeCannotBeAttempted(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	f.session(id, "idle", "main", 0)
	gitRun(t, f.repoDir, "branch", "release", "main")
	for _, c := range []struct{ branch, target, reason string }{
		{"feat", "nope", domain.MergeTargetNotFound},
		{"feat", "feat", domain.MergeTargetNotFound},
		{"idle", "release", domain.MergeAlreadyMerged},
		{"feat", "main", ""},
	} {
		offer, err := f.merger().MergeOffer(context.Background(), f.sessIDs[c.branch], c.target)
		if err != nil {
			t.Fatal(err)
		}
		if offer.Reason != c.reason || offer.Available != (c.reason == "") {
			t.Errorf("%s into %s: %+v, want reason %q", c.branch, c.target, offer, c.reason)
		}
	}
	gitRun(t, f.repoDir, "worktree", "remove", "--force", f.worktrees["feat"])
	gitRun(t, f.repoDir, "branch", "-D", "feat")
	if offer, _ := f.merger().MergeOffer(context.Background(), f.sessIDs["feat"], ""); offer.Available || offer.Reason != domain.MergeBranchNotFound {
		t.Errorf("a branch that is gone: %+v", offer)
	}
	if _, err := f.merger().MergeOffer(context.Background(), "nope", ""); errs.Code(err) != "SESSION_NOT_FOUND" {
		t.Errorf("unknown session: %v", err)
	}
}

func TestMergeOffer_ReadsNoMoreThanAFewGitRuns(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	for i := range 30 {
		gitRun(t, f.repoDir, "branch", "b"+string(rune('a'+i%26))+string(rune('a'+i/26)), "main")
	}
	start := time.Now()
	if _, err := f.merger().MergeOffer(context.Background(), f.sessIDs["feat"], ""); err != nil {
		t.Fatal(err)
	}
	// Four runs at about 65 ms each; one run per branch would be over two seconds.
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("the offer took %v for 30 branches", d)
	}
}

func TestMergeInto_ATargetThatIsNotCheckedOutLeavesTheMainCheckoutIdenticalAndIsRecorded(t *testing.T) {
	for _, method := range []domain.MergeMethod{domain.MergeMethodMerge, domain.MergeMethodSquash} {
		t.Run(string(method), func(t *testing.T) {
			f := newMergeFixture(t, "main")
			id := f.task(domain.TicketStatusReview)
			f.session(id, "feat", "main", 2)
			gitRun(t, f.repoDir, "branch", "release", "main")
			f.dirtyMainCheckout()
			before, branchTip := f.snapshot(), gitOut(t, f.repoDir, "rev-parse", "feat")

			res, err := f.merger().MergeInto(context.Background(), f.sessIDs["feat"],
				ports.MergeRequest{Target: "release", Method: method, Message: " Ship it "})
			if err != nil {
				t.Fatal(err)
			}
			if !res.Merged || res.Into != "release" || res.Method != method || res.Head != gitOut(t, f.repoDir, "rev-parse", "release") {
				t.Fatalf("result = %+v", res)
			}
			if f.snapshot() != before {
				t.Fatal("the main checkout changed")
			}
			if gitOut(t, f.repoDir, "rev-parse", "feat") != branchTip {
				t.Fatal("the session's branch moved")
			}
			if method == domain.MergeMethodSquash && gitOut(t, f.repoDir, "log", "-1", "--format=%B", "release") != "Ship it" {
				t.Fatal("squash message is not the trimmed message")
			}
			if got := f.stored("feat").LastMerge; got == nil || !got.Merged || got.Into != "release" || got.Method != method || got.Head != res.Head {
				t.Fatalf("recorded = %+v", got)
			}
		})
	}
}

func TestMergeInto_AnEmptyRequestIsTheDefaultBranchWithAMergeCommit(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	res, err := f.merger().MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Merged || res.Into != "main" || res.Method != domain.MergeMethodMerge ||
		len(strings.Fields(gitOut(t, f.repoDir, "rev-list", "--parents", "-n1", "main"))) != 3 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(f.repoDir, "feat.txt")); err != nil {
		t.Fatal("the branch's file did not follow into the checkout")
	}
	if gitOut(t, f.repoDir, "status", "--porcelain") != "" {
		t.Fatal("the checkout is not clean")
	}
}

func TestMergeInto_ADirtyCheckedOutTargetRefusesAndIsRecorded(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	f.dirtyMainCheckout()
	before := f.snapshot()
	_, err := f.merger().MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{Target: "main", Method: domain.MergeMethodSquash, Message: "m"})
	if errs.Code(err) != domain.MergeCheckoutDirty {
		t.Fatalf("err = %v", err)
	}
	if got := f.stored("feat").LastMerge; got == nil || got.Merged || got.Code != domain.MergeCheckoutDirty || got.Method != domain.MergeMethodSquash || got.Into != "main" {
		t.Fatalf("recorded = %+v", got)
	}
	if f.snapshot() != before {
		t.Fatal("the checkout changed")
	}
}

func TestMergeInto_RefusalsAreCodedAndInvalidRequestsAreNotRecorded(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	sid := f.sessIDs["feat"]
	for _, req := range []ports.MergeRequest{
		{Method: "rebase"},
		{Method: domain.MergeMethodSquash, Message: "  "},
	} {
		if _, err := f.merger().MergeInto(context.Background(), sid, req); errs.Code(err) != "INVALID_INPUT" {
			t.Errorf("%+v: %v", req, err)
		}
	}
	if f.stored("feat").LastMerge != nil {
		t.Fatal("an invalid request was recorded")
	}
	if _, err := f.merger().MergeInto(context.Background(), "nope", ports.MergeRequest{}); errs.Code(err) != "SESSION_NOT_FOUND" {
		t.Errorf("unknown session: %v", err)
	}
	for _, target := range []string{"nope", "feat"} {
		_, err := f.merger().MergeInto(context.Background(), sid, ports.MergeRequest{Target: target})
		if errs.Code(err) != domain.MergeTargetNotFound || !strings.HasPrefix(err.Error(), domain.MergeTargetNotFound) {
			t.Errorf("target %q: %v", target, err)
		}
		if got := f.stored("feat").LastMerge; got == nil || got.Code != domain.MergeTargetNotFound || got.Merged {
			t.Errorf("target %q: recorded %+v", target, got)
		}
	}
}

func TestMergeInto_AConflictIsRecordedAndChangesNothing(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	gitRun(t, f.repoDir, "branch", "release", "main")
	rel := filepath.Join(t.TempDir(), "rel")
	gitRun(t, f.repoDir, "worktree", "add", rel, "release")
	if err := os.WriteFile(filepath.Join(rel, "feat.txt"), []byte("zz"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, rel, "add", ".")
	gitRun(t, rel, "commit", "-m", "release has feat.txt too")
	gitRun(t, f.repoDir, "worktree", "remove", "--force", rel)
	before, refs := f.snapshot(), gitOut(t, f.repoDir, "for-each-ref")

	for _, method := range []domain.MergeMethod{domain.MergeMethodMerge, domain.MergeMethodSquash} {
		_, err := f.merger().MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{Target: "release", Method: method, Message: "m"})
		if errs.Code(err) != domain.MergeConflict || !strings.Contains(err.Error(), "feat.txt") {
			t.Fatalf("%s: %v", method, err)
		}
		if got := f.stored("feat").LastMerge; got == nil || got.Merged || got.Code != domain.MergeConflict || got.Method != method {
			t.Fatalf("%s: recorded %+v", method, got)
		}
	}
	if f.snapshot() != before || gitOut(t, f.repoDir, "for-each-ref") != refs {
		t.Fatal("something changed")
	}
}

// Two merges into one target at once run one after the other, so both land.
func TestMergeInto_TwoMergesIntoOneTargetAreSerialised(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "one", "main", 1)
	f.session(id, "two", "main", 1)
	gitRun(t, f.repoDir, "branch", "release", "main")
	m := f.merger()
	var wg sync.WaitGroup
	errsOut := make([]error, 2)
	for i, b := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errsOut[i] = m.MergeInto(context.Background(), f.sessIDs[b], ports.MergeRequest{Target: "release", Method: domain.MergeMethodSquash, Message: b})
		}()
	}
	wg.Wait()
	if errsOut[0] != nil || errsOut[1] != nil {
		t.Fatalf("errors: %v, %v", errsOut[0], errsOut[1])
	}
	files := gitOut(t, f.repoDir, "ls-tree", "--name-only", "release")
	if !strings.Contains(files, "one.txt") || !strings.Contains(files, "two.txt") {
		t.Fatalf("release holds %q", files)
	}
}

// The architect's tool keeps its behaviour: the default branch, --no-ff, and
// a refusal when the main checkout is on another branch.
func TestMergeSessionBranch_StillRefusesAWrongCheckedOutBranch(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	gitRun(t, f.repoDir, "checkout", "-q", "-b", "other")
	_, err := f.merger().MergeSessionBranch(context.Background(), f.sessIDs["feat"])
	if errs.Code(err) != domain.MergeWrongBranch {
		t.Fatalf("err = %v", err)
	}
	if got := f.stored("feat").LastMerge; got == nil || got.Code != domain.MergeWrongBranch || got.Method != domain.MergeMethodMerge {
		t.Fatalf("recorded %+v", got)
	}
	gitRun(t, f.repoDir, "checkout", "-q", "main")
	res, err := f.merger().MergeSessionBranch(context.Background(), f.sessIDs["feat"])
	if err != nil || !res.Merged || res.Method != domain.MergeMethodMerge || res.Into != "main" {
		t.Fatalf("result %+v, %v", res, err)
	}
}

// A squash leaves no ancestry, so the merge state counts a recorded squash
// into the default branch, and keeps counting it after a later refusal.
func TestMergeState_ASquashIntoTheDefaultBranchCountsAsMerged(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 2)
	gitRun(t, f.repoDir, "branch", "release", "main")
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1})

	// A squash, and a merge commit, into another target are not "merged".
	for _, method := range []domain.MergeMethod{domain.MergeMethodSquash, domain.MergeMethodMerge} {
		if _, err := f.merger().MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{Target: "release", Method: method, Message: "m"}); err != nil {
			if errs.Code(err) != domain.MergeAlreadyMerged {
				t.Fatal(err)
			}
		}
	}
	f.clock.advance(workspaces.MergeStateFreshFor + time.Second)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1})

	if _, err := f.merger().MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{Target: "main", Method: domain.MergeMethodSquash, Message: "Ship it"}); err != nil {
		t.Fatal(err)
	}
	if exec.Command("git", "-C", f.repoDir, "merge-base", "--is-ancestor", "feat", "main").Run() == nil {
		t.Fatal("a squash left git ancestry: the test would prove nothing")
	}
	f.clock.advance(workspaces.MergeStateFreshFor + time.Second)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})

	// A later refusal does not erase it.
	if _, err := f.merger().MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{Target: "nope"}); errs.Code(err) != domain.MergeTargetNotFound {
		t.Fatalf("err = %v", err)
	}
	f.clock.advance(workspaces.MergeStateFreshFor + time.Second)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}

func TestMergeState_AMergeCommitIntoTheDefaultBranchIsMergedByAncestryAsToday(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	if _, err := f.merger().MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{}); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(workspaces.MergeStateFreshFor + time.Second)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}

type mergeStateSink struct{ got []ports.ProjectChange }

func (c *mergeStateSink) AnnounceChange(ch ports.ProjectChange) { c.got = append(c.got, ch) }

// A merge that landed has the task's merge state read again at once.
func TestMergeInto_ALandedMergeTellsTheMergeStates(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	log := &mergeStateSink{}
	m := workspaces.NewMerges(workspaces.MergeDeps{
		Sessions: sqlite.NewSessionRepository(f.db), Workspaces: sqlite.NewWorkspaceRepository(f.db),
		Repositories: sqlite.NewRepositoryRepository(f.db), Git: gitcli.NewMerger(), States: log,
	})
	if _, err := m.MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{Target: "nope"}); err == nil || len(log.got) != 0 {
		t.Fatalf("a refusal told the states: %v %v", err, log.got)
	}
	if _, err := m.MergeInto(context.Background(), f.sessIDs["feat"], ports.MergeRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(log.got) != 1 || log.got[0].CodeChanged == nil || log.got[0].CodeChanged.WorkspaceID != f.wsIDs["feat"] || log.got[0].CodeChanged.ProjectID != f.projectID {
		t.Fatalf("told %+v", log.got)
	}
}

// A branch checked out in another session's worktree is no merge target: moving
// it would pull the branch from under that session.
func TestMergeInto_ABranchCheckedOutInAnotherWorktreeIsNoTarget(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "one", "main", 1)
	f.session(id, "two", "main", 1)
	offer, err := f.merger().MergeOffer(context.Background(), f.sessIDs["one"], "two")
	if err != nil {
		t.Fatal(err)
	}
	if offer.Available || offer.Reason != domain.MergeTargetNotFound {
		t.Fatalf("offer into another session's branch = %+v", offer)
	}
	for _, tg := range offer.Targets {
		if tg.Name == "two" {
			t.Fatalf("targets list %+v", offer.Targets)
		}
	}
	before := gitOut(t, f.repoDir, "rev-parse", "two")
	_, err = f.merger().MergeInto(context.Background(), f.sessIDs["one"], ports.MergeRequest{Target: "two"})
	if errs.Code(err) != domain.MergeTargetNotFound || gitOut(t, f.repoDir, "rev-parse", "two") != before {
		t.Fatalf("err = %v", err)
	}
}
