package workspaces_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func (f *prFixture) bases(t *testing.T) *domain.PullRequestBases {
	t.Helper()
	b, err := f.svc.PullRequestBases(context.Background(), f.ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPullRequestBasesAreOriginsBranchesAsLastFetched(t *testing.T) {
	f := newPRFixture(t)
	fetched := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	f.git.originBranches, f.git.fetchedAt = []string{"develop", "main"}, fetched

	b := f.bases(t)
	if b.WorkspaceID != f.ws.ID || b.Default != "main" || !slices.Equal(b.Branches, []string{"develop", "main"}) ||
		b.FetchedAt == nil || !b.FetchedAt.Equal(fetched) {
		t.Fatalf("bases = %+v", b)
	}
	if f.git.originFetches != 0 || len(f.git.fetches) != 0 {
		t.Fatal("a read fetched")
	}
	// The default is the draft's base, whatever origin lists.
	if draft := f.get(t).Draft; draft.BaseBranch != b.Default {
		t.Fatalf("default %q, draft base %q", b.Default, draft.BaseBranch)
	}
}

func TestPullRequestBasesWithoutAFetchTimeOrAnOriginAreEmptyNotAnError(t *testing.T) {
	f := newPRFixture(t)
	f.git.originBranches = []string{"main"}
	if b := f.bases(t); b.FetchedAt != nil || !slices.Equal(b.Branches, []string{"main"}) {
		t.Fatalf("never fetched: %+v", b)
	}
	f.git.origin = ""
	if b := f.bases(t); b.Branches == nil || len(b.Branches) != 0 || b.FetchedAt != nil {
		t.Fatalf("no origin: %+v", b)
	}
	if _, err := f.svc.PullRequestBases(context.Background(), "nope"); errs.Code(err) != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("unknown workspace: %v", err)
	}
	if _, err := f.svc.PullRequestBases(context.Background(), " "); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("no workspace: %v", err)
	}
}

func TestRefreshFetchesOriginWithTheStoredCredentialAndAnswersTheNewList(t *testing.T) {
	f := newPRFixture(t)
	f.git.originBranches, f.git.fetchedAt = []string{"main"}, time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	later := time.Date(2026, 10, 26, 9, 5, 0, 0, time.UTC)
	f.git.onFetchOrigin = func() {
		f.git.mu.Lock()
		defer f.git.mu.Unlock()
		f.git.originBranches, f.git.fetchedAt = []string{"main", "release/2"}, later
	}
	b, err := f.svc.RefreshPullRequestBases(context.Background(), f.ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.git.originFetches != 1 || len(f.git.envs) != 1 || len(f.git.envs[0]) == 0 {
		t.Fatalf("fetched %d times with env %v: the stored credential's environment is the only way in", f.git.originFetches, f.git.envs)
	}
	if !slices.Equal(b.Branches, []string{"main", "release/2"}) || b.FetchedAt == nil || !b.FetchedAt.Equal(later) || b.Default != "main" {
		t.Fatalf("bases = %+v", b)
	}
	if !slices.Equal(f.bases(t).Branches, b.Branches) {
		t.Fatal("GET and the refresh disagree")
	}
}

func TestRefreshFailuresAreCodedAndKeepThePreviousList(t *testing.T) {
	cases := map[string]struct {
		arrange func(f *prFixture)
		code    string
		reason  string
	}{
		"no origin":  {func(f *prFixture) { f.git.origin = "" }, "PR_NO_REMOTE", ""},
		"not GitHub": {func(f *prFixture) { f.git.origin = "https://gitlab.com/acme/widgets" }, "PR_NO_REMOTE", "gitlab.com"},
		"no stored ssh key": {func(f *prFixture) {
			_ = f.auth.DeleteGitCredential(context.Background(), f.key.ID)
		}, "GIT_CREDENTIAL_MISSING", "ssh_key for github.com"},
		"store locked": {func(f *prFixture) { f.auth.Lock() }, "GIT_CREDENTIAL_STORE_LOCKED", ""},
		"origin refuses the key": {func(f *prFixture) {
			f.git.fetchOriginErr = fmt.Errorf("git fetch: %w: Permission denied (publickey).", ports.ErrRemoteAuthRejected)
		}, "GIT_CREDENTIAL_REJECTED", "Permission denied (publickey)"},
		"fetch fails": {func(f *prFixture) {
			f.git.fetchOriginErr = errors.New("git fetch: exit status 128: fatal: unable to access: Could not resolve host")
		}, "PR_FETCH_FAILED", "Could not resolve host"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPRFixture(t)
			f.git.originBranches, f.git.fetchedAt = []string{"main", "develop"}, time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
			if c.code == "PR_NO_REMOTE" {
				f.git.originBranches = nil
			}
			c.arrange(f)
			_, err := f.svc.RefreshPullRequestBases(context.Background(), f.ws.ID)
			if errs.Code(err) != c.code {
				t.Fatalf("err = %v, want %s", err, c.code)
			}
			var se *domain.StructuredError
			if errors.As(err, &se); c.reason != "" && !strings.Contains(se.Message, c.reason) {
				t.Fatalf("message %q lacks %q", se.Message, c.reason)
			}
			if c.code != "PR_NO_REMOTE" && c.code != "GIT_CREDENTIAL_STORE_LOCKED" {
				if got, _ := f.svc.PullRequestBases(context.Background(), f.ws.ID); !slices.Equal(got.Branches, []string{"main", "develop"}) {
					t.Fatalf("the previous list is gone: %+v", got)
				}
			}
		})
	}
}

func TestRefreshesOfOneRepositoryRunOneAfterTheOther(t *testing.T) {
	f := newPRFixture(t)
	var running, peak atomic.Int32
	f.git.onFetchOrigin = func() {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		running.Add(-1)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.svc.RefreshPullRequestBases(context.Background(), f.ws.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 1 || f.git.originFetches != 4 {
		t.Fatalf("peak %d concurrent fetches over %d", peak.Load(), f.git.originFetches)
	}
}
