package workspaces_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// listing is a WorktreeManager that only lists branches, and counts the reads.
type listing struct {
	ports.WorktreeManager
	branches []domain.GitBranch
	reads    int
}

func (l *listing) ListBranches(string) ([]domain.GitBranch, error) {
	l.reads++
	return l.branches, nil
}

// branchPublisher is a BranchPublisher that records the branches fetched, in
// order, and fails or hangs the one-branch fetch on request.
type branchPublisher struct {
	ports.BranchPublisher
	origin string

	mu       sync.Mutex
	fetched  []string
	fetchErr error
	hang     bool
	running  int
	overlap  bool
}

func (p *branchPublisher) OriginURL(context.Context, string) (string, error) { return p.origin, nil }

func (p *branchPublisher) FetchBranch(ctx context.Context, _, branch string, _ []string) error {
	p.mu.Lock()
	p.fetched = append(p.fetched, branch)
	p.running++
	if p.running > 1 {
		p.overlap = true
	}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.running--
		p.mu.Unlock()
	}()
	if p.hang {
		<-ctx.Done()
		return ctx.Err()
	}
	time.Sleep(20 * time.Millisecond)
	return p.fetchErr
}

func (p *branchPublisher) FetchOrigin(context.Context, string, []string) error {
	p.mu.Lock()
	p.running++
	if p.running > 1 {
		p.overlap = true
	}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.running--
		p.mu.Unlock()
	}()
	time.Sleep(50 * time.Millisecond)
	return nil
}

func (p *branchPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.fetched)
}

type baseRig struct {
	prep    *workspaces.BaseBranches
	list    *listing
	git     *branchPublisher
	auth    *gitcredentialstest.Fake
	fetcher *workspaces.OriginFetcher
}

func newBaseRig(t *testing.T) *baseRig {
	t.Helper()
	r := &baseRig{
		list: &listing{branches: []domain.GitBranch{
			{Name: "main", IsHead: true}, {Name: "agent/other-1234"},
			{Name: "origin/feature-x", Remote: true}, {Name: "upstream/y", Remote: true},
		}},
		git:  &branchPublisher{origin: "file:///origin.git"},
		auth: gitcredentialstest.New(),
	}
	r.fetcher = workspaces.NewOriginFetcher(r.git, r.auth).WithTimeout(200 * time.Millisecond)
	r.prep = workspaces.NewBaseBranches(r.list, r.git, r.fetcher)
	return r
}

func (r *baseRig) prepare(base string) error {
	return r.prep.PrepareBase(context.Background(), "repo-1", "/checkout", base)
}

func TestAnEmptyBaseReadsNothingAndFetchesNothing(t *testing.T) {
	r := newBaseRig(t)
	if err := r.prepare("  "); err != nil {
		t.Fatal(err)
	}
	if r.list.reads != 0 || r.git.count() != 0 {
		t.Fatalf("reads=%d fetches=%d, want none", r.list.reads, r.git.count())
	}
}

func TestALocalBaseAndAnotherRemotesRefAreCutAsTheyAreWithNoFetch(t *testing.T) {
	r := newBaseRig(t)
	for _, base := range []string{"main", "agent/other-1234", "upstream/y"} {
		if err := r.prepare(base); err != nil {
			t.Fatalf("%s: %v", base, err)
		}
	}
	if r.git.count() != 0 {
		t.Fatalf("fetched %v for a base that is not origin's", r.git.fetched)
	}
	if r.list.reads != 3 {
		t.Fatalf("the branch list was read %d times for 3 starts, want once each", r.list.reads)
	}
}

func TestAnOriginBaseIsFetchedAsOneBranch(t *testing.T) {
	r := newBaseRig(t)
	if err := r.prepare("origin/feature-x"); err != nil {
		t.Fatal(err)
	}
	if got := r.git.fetched; len(got) != 1 || got[0] != "feature-x" {
		t.Fatalf("fetched %v, want [feature-x]", got)
	}
}

func TestABaseThatNamesNoListedBranchIsRefusedWithItsName(t *testing.T) {
	r := newBaseRig(t)
	for _, base := range []string{"mian", "origin/not-listed", "nowhere/x", "v1.0"} {
		err := r.prepare(base)
		if errs.Code(err) != domain.BaseBranchNotFound {
			t.Fatalf("%s: err = %v, want BASE_BRANCH_NOT_FOUND", base, err)
		}
		var de *domain.DetailedError
		if !errors.As(err, &de) || de.Details.(map[string]string)["base_branch"] != base {
			t.Fatalf("%s: details = %v", base, err)
		}
		if msg := err.Error(); !contains(msg, base) {
			t.Fatalf("message %q does not name %s", msg, base)
		}
	}
	if r.git.count() != 0 {
		t.Fatalf("fetched %v for unlisted bases", r.git.fetched)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAnOriginBaseWithNoOriginRemoteIsNoRemote(t *testing.T) {
	r := newBaseRig(t)
	r.git.origin = ""
	if err := r.prepare("origin/feature-x"); errs.Code(err) != domain.CodeNoRemote {
		t.Fatalf("err = %v, want NO_REMOTE", err)
	}
	if r.git.count() != 0 {
		t.Fatal("fetched with no origin")
	}
}

func TestABranchGoneFromOriginIsRefusedAndTheMessageNamesTheBaseAndTheFix(t *testing.T) {
	r := newBaseRig(t)
	r.git.fetchErr = ports.ErrRemoteBranchNotFound
	err := r.prepare("origin/feature-x")
	if errs.Code(err) != domain.RemoteBranchNotFound {
		t.Fatalf("err = %v, want REMOTE_BRANCH_NOT_FOUND", err)
	}
	var de *domain.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("no details: %v", err)
	}
	want := map[string]string{"base_branch": "origin/feature-x", "remote": "origin", "branch": "feature-x"}
	for k, v := range want {
		if de.Details.(map[string]string)[k] != v {
			t.Fatalf("details = %v, want %v", de.Details, want)
		}
	}
	if de.Message != "origin/feature-x is not on origin any more. Fetch from origin to refresh the list." {
		t.Fatalf("message = %q", de.Message)
	}
}

func TestAFailedFetchOfTheBaseIsFetchFailedWithGitsReason(t *testing.T) {
	r := newBaseRig(t)
	r.git.fetchErr = errors.New("git fetch: exit status 128: fatal: unable to access 'https://user:tok_secret@example.com/x.git/': Could not resolve host")
	err := r.prepare("origin/feature-x")
	if errs.Code(err) != domain.CodeFetchFailed {
		t.Fatalf("err = %v, want FETCH_FAILED", err)
	}
	if !contains(err.Error(), "origin/feature-x") || !contains(err.Error(), "Could not resolve host") || contains(err.Error(), "tok_secret") {
		t.Fatalf("message = %q, want the base, git's reason and no secret", err.Error())
	}
}

func TestACredentialProblemRefusesTheStartWithTheCredentialCodes(t *testing.T) {
	r := newBaseRig(t)
	r.git.origin = "https://example.com/acme/widgets.git"
	if err := r.prepare("origin/feature-x"); errs.Code(err) != domain.CodeGitCredentialMissing {
		t.Fatalf("err = %v, want GIT_CREDENTIAL_MISSING for an https origin with nothing stored", err)
	}
	if r.git.count() != 0 {
		t.Fatal("fetched without a credential")
	}

	r.git.origin = "file:///origin.git"
	r.git.fetchErr = ports.ErrRemoteAuthRejected
	if err := r.prepare("origin/feature-x"); errs.Code(err) != domain.CodeGitCredentialRejected {
		t.Fatalf("err = %v, want GIT_CREDENTIAL_REJECTED", err)
	}
}

func TestAHungRemoteEndsAtTheBoundAsAFetchFailure(t *testing.T) {
	r := newBaseRig(t)
	r.git.hang = true
	start := time.Now()
	err := r.prepare("origin/feature-x")
	if errs.Code(err) != domain.CodeFetchFailed {
		t.Fatalf("err = %v, want FETCH_FAILED", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the fetch was not bounded")
	}
}

// A start from a remote base waits for a running fetch of the repository
// instead of refusing, and the two never run side by side.
func TestAStartDuringARepositoryFetchWaitsForIt(t *testing.T) {
	r := newBaseRig(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = r.fetcher.Fetch(context.Background(), "repo-1", "/checkout", "file:///origin.git")
	}()
	time.Sleep(10 * time.Millisecond)
	if err := r.prepare("origin/feature-x"); err != nil {
		t.Fatalf("a start during a fetch was refused: %v", err)
	}
	wg.Wait()
	if r.git.overlap {
		t.Fatal("the base fetch ran while a repository fetch was running")
	}
	if r.git.count() != 1 {
		t.Fatalf("fetched %v", r.git.fetched)
	}
}
