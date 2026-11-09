package workspaces_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// A push announces the state it left, whole, to the workspace's project.
func TestAPushAnnouncesTheStateItLeft(t *testing.T) {
	f := portstest.NewWorktreeGitFixture(t)
	svc, at, feed := workspacestest.NewWorktreeGitWithFeed(t, f)
	ctx := context.Background()
	// The repository was fetched, so the state after a push has counts: on a
	// never fetched one an upstream is unknown, by the contract's rule.
	if _, err := svc.FetchRemote(ctx, at.RepositoryID); err != nil {
		t.Fatal(err)
	}
	fetchEvents := len(feed.GitStates())
	st, err := svc.GitState(ctx, at.Workspaces["nopush"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PushBranch(ctx, at.Workspaces["nopush"], st.Head); err != nil {
		t.Fatal(err)
	}
	announced := feed.GitStates()[fetchEvents:]
	if len(announced) != 1 {
		t.Fatalf("announced %d states by the push, want 1", len(announced))
	}
	if c := announced[0]; c.ProjectID != "git-project" || c.WorkspaceID != at.Workspaces["nopush"] || c.SessionID != at.Sessions["nopush"] ||
		c.Upstream.State != domain.UpstreamInSync || c.Upstream.Ref == nil || *c.Upstream.Ref != "origin/nopush" {
		t.Errorf("announced %+v", c.GitState)
	}
	// A refused push announces nothing.
	if _, err := svc.PushBranch(ctx, at.Workspaces["diverged"], "abc"); errs.Code(err) != domain.CodePushStale {
		t.Fatalf("err = %v", err)
	}
	if len(feed.GitStates()) != fetchEvents+1 {
		t.Errorf("a refused push announced a state")
	}
}

// pushingPublisher is real git for reads and a slow or hung push for the rest.
type pushingPublisher struct {
	*gitcli.BranchPublisher
	hang          bool
	running, peak atomic.Int32
	pushes        atomic.Int32
}

func (p *pushingPublisher) Push(ctx context.Context, root, branch string, env []string) error {
	p.pushes.Add(1)
	n := p.running.Add(1)
	defer p.running.Add(-1)
	for cur := p.peak.Load(); n > cur && !p.peak.CompareAndSwap(cur, n); cur = p.peak.Load() {
	}
	if p.hang {
		<-ctx.Done()
		return ctx.Err()
	}
	time.Sleep(40 * time.Millisecond)
	return p.BranchPublisher.Push(ctx, root, branch, env)
}

// Two pushes of one workspace run one at a time, and the second, which saw
// the same head, finds the branch already published and pushes nothing new.
func TestTwoPushesOfOneWorkspaceRunOneAtATime(t *testing.T) {
	f := portstest.NewWorktreeGitFixture(t)
	pub := &pushingPublisher{BranchPublisher: gitcli.NewBranchPublisher()}
	svc, at, _ := workspacestest.NewWorktreeGitWithFeed(t, f, workspacestest.WithPublisher(pub))
	head := f.Heads["ahead"]
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.PushBranch(context.Background(), at.Workspaces["ahead"], head); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if pub.peak.Load() != 1 || pub.pushes.Load() != 2 {
		t.Errorf("%d pushes ran at once (of %d), want 1 at a time", pub.peak.Load(), pub.pushes.Load())
	}
}

// A push to a remote that hangs is cut at its bound, and says so.
func TestAPushToAHungRemoteIsCutAtItsBound(t *testing.T) {
	f := portstest.NewWorktreeGitFixture(t)
	pub := &pushingPublisher{BranchPublisher: gitcli.NewBranchPublisher(), hang: true}
	svc, at, _ := workspacestest.NewWorktreeGitWithFeed(t, f, workspacestest.WithPublisher(pub), workspacestest.WithRemoteTimeout(100*time.Millisecond))
	start := time.Now()
	_, err := svc.PushBranch(context.Background(), at.Workspaces["ahead"], f.Heads["ahead"])
	if errs.Code(err) != domain.CodePushFailed {
		t.Fatalf("err = %v, want PUSH_FAILED", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the push held the caller for %v", took)
	}
}

var _ ports.BranchPublisher = (*pushingPublisher)(nil)
