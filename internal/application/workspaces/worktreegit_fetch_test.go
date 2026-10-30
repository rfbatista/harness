package workspaces_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// A fetch announces the state of every workspace of the repository, once
// each, whole, to the project the repository is in.
func TestAFetchAnnouncesTheGitStateOfEveryWorkspaceOfTheRepository(t *testing.T) {
	f := portstest.NewWorktreeGitFixture(t)
	svc, at, feed := workspacestest.NewWorktreeGitWithFeed(t, f)
	if _, err := svc.FetchRemote(context.Background(), at.RepositoryID); err != nil {
		t.Fatal(err)
	}
	announced := feed.GitStates()
	if len(announced) != len(portstest.WorktreeGitBranches) {
		t.Fatalf("announced %d states, want one per workspace (%d)", len(announced), len(portstest.WorktreeGitBranches))
	}
	byBranch := map[string]ports.GitStateChange{}
	for _, c := range announced {
		if c.ProjectID != "git-project" {
			t.Errorf("%s announced to project %q", c.Branch, c.ProjectID)
		}
		if _, twice := byBranch[c.Branch]; twice {
			t.Errorf("%s announced twice", c.Branch)
		}
		byBranch[c.Branch] = c
	}
	for branch, want := range map[string]string{"behind": domain.UpstreamBehind, "diverged": domain.UpstreamDiverged, "deleted": domain.UpstreamGone, "nopush": domain.UpstreamNone} {
		c, ok := byBranch[branch]
		switch {
		case !ok:
			t.Errorf("%s was not announced", branch)
		case c.Upstream.State != want || c.FetchedAt == nil || c.WorkspaceID != at.Workspaces[branch] || c.SessionID != at.Sessions[branch]:
			t.Errorf("%s announced as %+v, want upstream %s with the fetch age and its ids", branch, c.GitState, want)
		}
	}
}

// A failed fetch announces nothing.
func TestAFailedFetchAnnouncesNothing(t *testing.T) {
	r := newFetchRig(t)
	r.git.fetchErr = errors.New("git fetch: exit status 128: fatal: unable to access")
	_, err := r.svc.FetchRemote(context.Background(), r.repoID)
	if errs.Code(err) != domain.CodeFetchFailed {
		t.Fatalf("err = %v, want FETCH_FAILED", err)
	}
	if got := r.feed.GitStates(); len(got) != 0 {
		t.Errorf("announced %d states after a failed fetch", len(got))
	}
}

// The pull request base refresh and the git state's fetch are one operation:
// a refresh announces states, and the two never run side by side.
func TestThePullRequestBaseRefreshAndTheFetchAreTheSameOperation(t *testing.T) {
	f := newPRFixture(t)
	fetcher := workspaces.NewOriginFetcher(f.git, f.auth)
	var fetched []string
	fetcher.OnFetched(func(_ context.Context, repositoryID string) { fetched = append(fetched, repositoryID) })
	f.deps.Fetcher = fetcher
	svc := workspaces.NewPullRequests(f.deps)
	t.Cleanup(svc.Close)

	if _, err := svc.RefreshPullRequestBases(context.Background(), f.ws.ID); err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 1 || fetched[0] != f.ws.RepositoryID {
		t.Errorf("a base refresh told the fetch's observers %v, want the repository once", fetched)
	}

	// A refresh and a direct fetch of the same checkout run one at a time.
	var running, peak atomic.Int32
	f.git.onFetchOrigin = func() {
		n := running.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = svc.RefreshPullRequestBases(context.Background(), f.ws.ID) }()
	go func() {
		defer wg.Done()
		_ = fetcher.Fetch(context.Background(), f.ws.RepositoryID, "/checkout", f.git.origin)
	}()
	wg.Wait()
	if peak.Load() != 1 {
		t.Errorf("%d fetches of one checkout ran at once, want 1", peak.Load())
	}
}

// A fetch to a remote that hangs is cut at its bound.
func TestAFetchToAHungRemoteIsCutAtItsBound(t *testing.T) {
	r := newFetchRig(t)
	r.git.hang = true
	start := time.Now()
	_, err := r.svc.FetchRemote(context.Background(), r.repoID)
	if errs.Code(err) != domain.CodeFetchFailed {
		t.Fatalf("err = %v, want FETCH_FAILED", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the fetch held the caller for %v", took)
	}
}

// The credential codes are the pull requests' own.
func TestAFetchRefusesWithTheCredentialCodes(t *testing.T) {
	ctx := context.Background()
	r := newFetchRig(t)
	r.git.origin = "https://example.com/acme/widgets.git"

	_, err := r.svc.FetchRemote(ctx, r.repoID)
	if errs.Code(err) != "GIT_CREDENTIAL_MISSING" {
		t.Fatalf("no credential stored: %v", err)
	}
	var detailed *domain.DetailedError
	if !errors.As(err, &detailed) || detailsOf(detailed)["host"] != "example.com" || detailsOf(detailed)["kind"] != string(domain.GitHTTPSToken) {
		t.Errorf("details = %+v, want host and kind", err)
	}

	cred, err := r.auth.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "t", Kind: domain.GitHTTPSToken, Host: "example.com", Token: "tok_secretvalue123"})
	if err != nil {
		t.Fatal(err)
	}
	r.git.fetchErr = fmt.Errorf("git fetch: %w: authentication failed", ports.ErrRemoteAuthRejected)
	_, err = r.svc.FetchRemote(ctx, r.repoID)
	if errs.Code(err) != "GIT_CREDENTIAL_REJECTED" {
		t.Fatalf("host refused: %v", err)
	}
	if !errors.As(err, &detailed) || detailsOf(detailed)["credential_id"] != cred.ID || detailsOf(detailed)["host"] != "example.com" {
		t.Errorf("details = %+v, want host and credential_id", err)
	}
	if msg := err.Error(); containsSecret(msg) {
		t.Errorf("the error carries the secret: %s", msg)
	}

	r.git.fetchErr = nil
	r.auth.Lock()
	if _, err = r.svc.FetchRemote(ctx, r.repoID); errs.Code(err) != "GIT_CREDENTIAL_STORE_LOCKED" {
		t.Errorf("locked store: %v", err)
	}
}

func containsSecret(s string) bool { return strings.Contains(s, "tok_secretvalue123") }

func detailsOf(e *domain.DetailedError) map[string]any {
	m, _ := e.Details.(map[string]any)
	return m
}

// fetchRig is a WorktreeGit over a fake remote, for the fetch's failures.
type fetchRig struct {
	svc    *workspaces.WorktreeGit
	git    *hangingPublisher
	auth   *gitcredentialstest.Fake
	feed   *workspacestest.FeedRecorder
	repoID string
}

// hangingPublisher is a BranchPublisher whose fetch fails, or hangs until its
// context ends.
type hangingPublisher struct {
	ports.BranchPublisher
	origin   string
	fetchErr error
	hang     bool
}

func (p *hangingPublisher) OriginURL(context.Context, string) (string, error) { return p.origin, nil }

func (p *hangingPublisher) FetchOrigin(ctx context.Context, _ string, _ []string) error {
	if p.hang {
		<-ctx.Done()
		return ctx.Err()
	}
	return p.fetchErr
}

func newFetchRig(t *testing.T) *fetchRig {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	repo, err := repos.Create("git-project", "widgets", "", "https://example.com/acme/widgets.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &fetchRig{git: &hangingPublisher{origin: "file:///origin.git"}, auth: gitcredentialstest.New(), feed: &workspacestest.FeedRecorder{}, repoID: repo.ID}
	r.svc = workspaces.NewWorktreeGit(workspaces.WorktreeGitDeps{
		Workspaces: sqlite.NewWorkspaceRepository(db), Repositories: repos, Git: gitcli.NewCode(), Files: filesystem.NewWorktreeFiles(),
		Publisher: r.git, Fetcher: workspaces.NewOriginFetcher(r.git, r.auth).WithTimeout(100 * time.Millisecond), Feed: r.feed,
	})
	return r
}

// git's own words in a failure never carry the credential of an https origin.
func TestAFailedFetchNeverShowsTheCredentialOfItsOrigin(t *testing.T) {
	r := newFetchRig(t)
	r.git.fetchErr = errors.New("git fetch: exit status 128: fatal: unable to access 'https://user:tok_secretvalue123@example.com/acme/widgets.git/': Could not resolve host")
	_, err := r.svc.FetchRemote(context.Background(), r.repoID)
	if errs.Code(err) != domain.CodeFetchFailed || containsSecret(err.Error()) {
		t.Fatalf("err = %v, want FETCH_FAILED without the secret", err)
	}
}
