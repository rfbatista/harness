package workspaces_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/githubapi/githubapitest"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeGit is a scripted ports.BranchPublisher: a checkout whose origin and
// refs the test sets.
type fakeGit struct {
	mu       sync.Mutex
	origin   string
	refs     map[string]bool
	subjects []string
	fetchErr error
	pushErr  error
	// onFetch runs on each fetch, e.g. to move the refs as origin moved.
	onFetch func(branch string)
	fetches []string
	pushes  []string
	envs    [][]string // the environment of each fetch and push
}

func (g *fakeGit) OriginURL(context.Context, string) (string, error) { return g.origin, nil }

func (g *fakeGit) HasRef(_ context.Context, _ string, ref string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.refs[ref]
}

func (g *fakeGit) FetchBranch(_ context.Context, _ string, branch string, env []string) error {
	g.mu.Lock()
	g.fetches = append(g.fetches, branch)
	g.envs = append(g.envs, env)
	on := g.onFetch
	g.mu.Unlock()
	if on != nil {
		on(branch)
	}
	return g.fetchErr
}

func (g *fakeGit) Push(_ context.Context, _ string, branch string, env []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.envs = append(g.envs, env)
	if g.pushErr != nil {
		return g.pushErr
	}
	g.pushes = append(g.pushes, branch)
	g.refs["refs/remotes/origin/"+branch] = true
	return nil
}

func (g *fakeGit) Subjects(_ context.Context, _, base, head string, limit int) ([]string, error) {
	return g.subjects[:min(limit, len(g.subjects))], nil
}

func (g *fakeGit) setRef(ref string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refs[ref] = ok
}

// fakeHistory is a scripted ports.GitHistory: how far head is ahead of each
// base ref, and the worktree's changes.
type fakeHistory struct {
	mu         sync.Mutex
	ahead      map[string]int // by base ref
	changes    int
	missing    bool
	rootBranch string
}

func (h *fakeHistory) Log(context.Context, string, ports.HistoryQuery) ([]domain.Commit, error) {
	return nil, nil
}

func (h *fakeHistory) Show(context.Context, string, string) (*domain.CommitDetail, error) {
	return nil, nil
}

func (h *fakeHistory) Status(_ context.Context, dir string) (*domain.WorktreeStatus, error) {
	if dir == "/checkout" {
		return &domain.WorktreeStatus{Branch: h.rootBranch, Changes: []domain.WorkingChange{}}, nil
	}
	if h.missing {
		return nil, &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "gone"}
	}
	st := &domain.WorktreeStatus{Changes: []domain.WorkingChange{}}
	for i := range h.changes {
		st.Changes = append(st.Changes, domain.WorkingChange{Path: fmt.Sprintf("f%d", i)})
	}
	return st, nil
}

func (h *fakeHistory) Divergence(_ context.Context, _, base, _ string) (int, int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	n, ok := h.ahead[base]
	if !ok {
		return 0, 0, errors.New("unknown revision " + base)
	}
	return n, 0, nil
}

func (h *fakeHistory) setAhead(base string, n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ahead[base] = n
}

type fakeTickets map[string]*domain.Ticket

func (f fakeTickets) GetTicket(_ context.Context, id string) (*domain.Ticket, error) {
	if t, ok := f[id]; ok {
		return t, nil
	}
	return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "no"}
}

func (f fakeTickets) ListTickets(context.Context, string) ([]*domain.Ticket, error) { return nil, nil }

// feedRecorder is a ports.ProjectChangeSink that keeps what was announced.
type feedRecorder struct {
	mu      sync.Mutex
	changes []ports.ProjectChange
}

func (f *feedRecorder) AnnounceChange(c ports.ProjectChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, c)
}

func (f *feedRecorder) pullRequests() []domain.PullRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.PullRequest
	for _, c := range f.changes {
		if c.PullRequest != nil {
			out = append(out, *c.PullRequest)
		}
	}
	return out
}

var widgets = domain.GitRemote{Host: "github.com", Owner: "acme", Repo: "widgets"}

type prFixture struct {
	svc     *workspaces.PullRequests
	deps    workspaces.PullRequestDeps
	ws      *domain.Workspace
	git     *fakeGit
	history *fakeHistory
	host    *githubapitest.FakeHost
	auth    *gitcredentialstest.Fake
	key     *domain.GitCredential // the ssh key origin pushes with
	token   *domain.GitCredential // the GitHub token
	store   *sqlite.PullRequestRepository
	feed    *feedRecorder
	bus     *eventbus.Bus
	now     time.Time
	nowMu   sync.Mutex
}

func (f *prFixture) clock() time.Time {
	f.nowMu.Lock()
	defer f.nowMu.Unlock()
	return f.now
}

func (f *prFixture) advance(d time.Duration) {
	f.nowMu.Lock()
	defer f.nowMu.Unlock()
	f.now = f.now.Add(d)
}

// fixtureToken is the GitHub token the fixture stores.
const fixtureToken domain.Secret = "ghp_fixtureToken0001"

// newPRFixture: workspace "agent/x" cut from origin/main in project p1, two
// commits ahead of origin/main, one uncommitted file, origin on GitHub over
// ssh, an ssh key and a GitHub token stored, and a session on a task.
func newPRFixture(t *testing.T) *prFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	repo, err := repos.Create("p1", "Widgets", "", "git@github.com:acme/widgets.git", "/checkout")
	if err != nil {
		t.Fatal(err)
	}
	wsRepo := sqlite.NewWorkspaceRepository(db)
	ws, err := wsRepo.Create(domain.Workspace{RepositoryID: repo.ID, Name: "x", Branch: "agent/x", Path: "/wt", BaseRef: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sqlite.NewSessionRepository(db)
	if _, err := sessions.Create(&domain.Session{ID: "s1", ProjectID: "p1", TicketID: "t1", WorkspaceID: ws.ID, Branch: "agent/x", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	f := &prFixture{
		ws: ws,
		git: &fakeGit{origin: "git@github.com:acme/widgets.git", subjects: []string{"second change", "first change"},
			refs: map[string]bool{"refs/heads/main": true, "refs/remotes/origin/main": true, "refs/heads/agent/x": true}},
		history: &fakeHistory{ahead: map[string]int{"refs/remotes/origin/main": 2, "refs/heads/main": 2}, changes: 1, rootBranch: "main"},
		host:    githubapitest.New(),
		auth:    gitcredentialstest.New(),
		store:   sqlite.NewPullRequestRepository(db),
		feed:    &feedRecorder{},
		bus:     eventbus.New(),
		now:     time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
	}
	ctx := context.Background()
	if f.key, err = f.auth.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "key", Kind: domain.GitSSHKey, Host: "github.com", Generate: true}); err != nil {
		t.Fatal(err)
	}
	if f.token, err = f.auth.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "token", Kind: domain.GitHTTPSToken, Host: "github.com", Token: fixtureToken}); err != nil {
		t.Fatal(err)
	}
	f.deps = workspaces.PullRequestDeps{
		Workspaces: wsRepo, Repositories: repos, History: f.history, Git: f.git, Host: f.host, Store: f.store, Auth: f.auth,
		Sessions: sessions, Tickets: fakeTickets{"t1": {ID: "t1", ProjectID: "p1", Title: "Create pull request", Description: "I need a way to create a pull request from the ui"}},
		Feed: f.feed, Now: f.clock,
	}
	f.svc = workspaces.NewPullRequests(f.deps)
	f.svc.Subscribe(f.bus)
	t.Cleanup(f.svc.Close)
	return f
}

// get reads the page and waits for the background host work it started.
func (f *prFixture) get(t *testing.T) *ports.PullRequestView {
	t.Helper()
	v, err := f.svc.PullRequest(context.Background(), f.ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.Wait()
	return v
}

func (f *prFixture) create(req ports.CreatePullRequest) (*domain.PullRequest, error) {
	if req.WorkspaceID == "" {
		req.WorkspaceID = f.ws.ID
	}
	return f.svc.CreatePullRequest(context.Background(), req)
}

func TestDraftSuggestsFromTheTaskAndTheCommits(t *testing.T) {
	f := newPRFixture(t)
	v := f.get(t)
	d := v.Draft
	if v.PullRequest != nil {
		t.Fatalf("no PR yet, got %+v", v.PullRequest)
	}
	if d.WorkspaceID != f.ws.ID || d.RepositoryID != f.ws.RepositoryID || d.HeadBranch != "agent/x" || d.BaseBranch != "main" {
		t.Fatalf("draft = %+v", d)
	}
	if d.Title != "Create pull request" {
		t.Fatalf("title = %q", d.Title)
	}
	wantBody := "I need a way to create a pull request from the ui\n\n- second change\n- first change"
	if d.Body != wantBody {
		t.Fatalf("body = %q\nwant %q", d.Body, wantBody)
	}
	if d.Ahead != 2 || d.UncommittedChanges != 1 {
		t.Fatalf("ahead %d uncommitted %d", d.Ahead, d.UncommittedChanges)
	}
	if d.Remote == nil || *d.Remote != widgets {
		t.Fatalf("remote = %+v", d.Remote)
	}
	if d.Blockers == nil || len(d.Blockers) != 0 {
		t.Fatalf("blockers = %#v, want []", d.Blockers)
	}
}

func TestDraftTitleFallsBackToTheNewestCommitSubject(t *testing.T) {
	f := newPRFixture(t)
	// No session runs in the workspace.
	deps := f.deps
	deps.Sessions, deps.Tickets = nil, nil
	f.svc = workspaces.NewPullRequests(deps)
	t.Cleanup(f.svc.Close)
	d := f.get(t).Draft
	if d.Title != "second change" || d.Body != "- second change\n- first change" {
		t.Fatalf("title %q body %q", d.Title, d.Body)
	}
}

func TestDraftBaseFallsBackToTheCheckedOutBranch(t *testing.T) {
	for name, baseRef := range map[string]string{"empty": "", "a commit": "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "a tag": "v1"} {
		t.Run(name, func(t *testing.T) {
			f := newPRFixture(t)
			f.history.rootBranch = "develop"
			f.git.setRef("refs/remotes/origin/develop", true)
			f.history.setAhead("refs/remotes/origin/develop", 3)
			w, err := f.deps.Workspaces.Create(domain.Workspace{RepositoryID: f.ws.RepositoryID, Name: "y-" + strings.ReplaceAll(name, " ", "-"), Branch: "agent/y", Path: "/wt2", BaseRef: baseRef})
			if err != nil {
				t.Fatal(err)
			}
			v, err := f.svc.PullRequest(context.Background(), w.ID)
			if err != nil {
				t.Fatal(err)
			}
			if v.Draft.BaseBranch != "develop" || v.Draft.Ahead != 3 {
				t.Fatalf("base %q ahead %d", v.Draft.BaseBranch, v.Draft.Ahead)
			}
		})
	}
}

func TestDraftAheadComparesWithTheLocalBaseWhenOriginsIsUnknown(t *testing.T) {
	f := newPRFixture(t)
	f.git.setRef("refs/remotes/origin/main", false)
	f.history.setAhead("refs/heads/main", 5)
	d := f.get(t).Draft
	if d.Ahead != 5 {
		t.Fatalf("ahead = %d, want 5 against the local main", d.Ahead)
	}
	if strings.Join(d.Blockers, ",") != domain.CodePRBaseNotFound {
		t.Fatalf("blockers = %v, want the base unknown on origin", d.Blockers)
	}
}

func TestDraftBlockers(t *testing.T) {
	cases := map[string]struct {
		arrange func(f *prFixture)
		want    string
		remote  *domain.GitRemote
	}{
		"no origin": {func(f *prFixture) { f.git.origin = "" }, "PR_NO_REMOTE", nil},
		"origin not on GitHub": {func(f *prFixture) { f.git.origin = "git@gitlab.com:acme/widgets.git" }, "PR_NO_REMOTE",
			&domain.GitRemote{Host: "gitlab.com", Owner: "acme", Repo: "widgets"}},
		"no stored ssh key": {func(f *prFixture) {
			_ = f.auth.DeleteGitCredential(context.Background(), f.key.ID)
		}, "GIT_CREDENTIAL_MISSING", &widgets},
		"no stored GitHub token": {func(f *prFixture) {
			_ = f.auth.DeleteGitCredential(context.Background(), f.token.ID)
		}, "GIT_CREDENTIAL_MISSING", &widgets},
		"nothing ahead": {func(f *prFixture) { f.history.setAhead("refs/remotes/origin/main", 0) }, "PR_NO_COMMITS", &widgets},
		"an open PR stored": {func(f *prFixture) {
			_, _ = f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1, State: domain.PullRequestOpen})
		}, "PR_ALREADY_EXISTS", &widgets},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPRFixture(t)
			c.arrange(f)
			d := f.get(t).Draft
			if got := strings.Join(d.Blockers, ","); got != c.want {
				t.Fatalf("blockers = %q, want %q", got, c.want)
			}
			if (d.Remote == nil) != (c.remote == nil) || (d.Remote != nil && *d.Remote != *c.remote) {
				t.Fatalf("remote = %+v, want %+v", d.Remote, c.remote)
			}
		})
	}
}

func TestDraftOfAWorktreeGoneFromDiskIsNotBlocked(t *testing.T) {
	f := newPRFixture(t)
	f.history.missing = true
	d := f.get(t).Draft
	if d.UncommittedChanges != 0 || len(d.Blockers) != 0 {
		t.Fatalf("draft = %+v", d)
	}
}

func TestPullRequestRefusesBadInput(t *testing.T) {
	f := newPRFixture(t)
	if _, err := f.svc.PullRequest(context.Background(), ""); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("no id = %v", err)
	}
	if _, err := f.svc.PullRequest(context.Background(), "nope"); errs.Code(err) != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("unknown = %v", err)
	}
}

// GET answers before the host does: a host that never answers does not hold it.
func TestPullRequestNeverWaitsOnTheHost(t *testing.T) {
	f := newPRFixture(t)
	saved, _ := f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1, State: domain.PullRequestOpen})
	f.host.Add(widgets, ports.HostPullRequest{Number: 1, Head: "agent/x", Base: "main", State: domain.PullRequestMerged})
	f.host.Gate = make(chan struct{}) // never opens

	done := make(chan *ports.PullRequestView)
	go func() {
		v, _ := f.svc.PullRequest(context.Background(), f.ws.ID)
		done <- v
	}()
	select {
	case v := <-done:
		if v.PullRequest == nil || v.PullRequest.Number != saved.Number || v.PullRequest.State != domain.PullRequestOpen {
			t.Fatalf("answered %+v, want the stored record as stored", v.PullRequest)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GET waited on the host")
	}
	// Close cancels the background call stuck on the host.
	closed := make(chan struct{})
	go func() { f.svc.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the background host call")
	}
}

func TestPullRequestRefreshesAStoredOpenPRInTheBackground(t *testing.T) {
	f := newPRFixture(t)
	_, _ = f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1,
		Title: "Create pull request", HeadBranch: "agent/x", BaseBranch: "main", State: domain.PullRequestOpen})
	f.host.Add(widgets, ports.HostPullRequest{Number: 1, Head: "agent/x", Base: "main", Title: "Create pull request", State: domain.PullRequestOpen})

	f.get(t)
	if got := f.feed.pullRequests(); len(got) != 0 {
		t.Fatalf("nothing changed, yet announced %+v", got)
	}

	f.host.SetState(widgets, 1, domain.PullRequestMerged)
	f.advance(31 * time.Second)
	if v := f.get(t); v.PullRequest.State != domain.PullRequestOpen {
		t.Fatalf("GET answered %s; it answers as stored, the refresh comes on the feed", v.PullRequest.State)
	}
	got := f.feed.pullRequests()
	if len(got) != 1 || got[0].State != domain.PullRequestMerged || got[0].ProjectID != "p1" || got[0].WorkspaceID != f.ws.ID {
		t.Fatalf("announced %+v, want one merged", got)
	}
	f.advance(31 * time.Second)
	if v := f.get(t); v.PullRequest.State != domain.PullRequestMerged {
		t.Fatalf("second GET = %s, want merged", v.PullRequest.State)
	}
	if _, gets, _ := f.host.Calls(); gets != 2 {
		t.Fatalf("host Get calls = %d; a merged PR is not refreshed again", gets)
	}
}

func TestPullRequestAsksTheHostAtMostEvery30Seconds(t *testing.T) {
	f := newPRFixture(t)
	_, _ = f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1, State: domain.PullRequestOpen})
	f.host.Add(widgets, ports.HostPullRequest{Number: 1, Head: "agent/x", State: domain.PullRequestOpen})
	f.get(t)
	f.advance(10 * time.Second)
	f.get(t)
	f.advance(19 * time.Second)
	f.get(t)
	if _, gets, _ := f.host.Calls(); gets != 1 {
		t.Fatalf("host Get calls within 30 s = %d, want 1", gets)
	}
	f.advance(2 * time.Second)
	f.get(t)
	if _, gets, _ := f.host.Calls(); gets != 2 {
		t.Fatalf("host Get calls after 30 s = %d, want 2", gets)
	}
}

func TestPullRequestIgnoresHostErrorsInTheBackground(t *testing.T) {
	f := newPRFixture(t)
	_, _ = f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1, State: domain.PullRequestOpen})
	f.host.GetErr = errors.New("connection reset")
	v := f.get(t)
	if v.PullRequest.State != domain.PullRequestOpen || len(f.feed.pullRequests()) != 0 {
		t.Fatalf("state %s, announced %v", v.PullRequest.State, f.feed.pullRequests())
	}
}

func TestPullRequestAdoptsAnOpenPROfAPushedBranchInTheBackground(t *testing.T) {
	f := newPRFixture(t)
	f.host.Add(widgets, ports.HostPullRequest{Number: 9, Head: "agent/x", Base: "main", Title: "Opened by hand", State: domain.PullRequestOpen})

	f.get(t) // not pushed: the host is not asked
	if _, _, finds := f.host.Calls(); finds != 0 {
		t.Fatalf("asked the host about an unpushed branch (%d finds)", finds)
	}

	f.git.setRef("refs/remotes/origin/agent/x", true)
	f.advance(31 * time.Second)
	f.get(t)
	got := f.feed.pullRequests()
	if len(got) != 1 || got[0].Number != 9 || got[0].Title != "Opened by hand" || got[0].HeadBranch != "agent/x" {
		t.Fatalf("announced %+v, want the adopted #9", got)
	}
	f.advance(31 * time.Second)
	if v := f.get(t); v.PullRequest == nil || v.PullRequest.Number != 9 {
		t.Fatalf("GET after adoption = %+v", v.PullRequest)
	}
}

func TestCreatePushesAndOpensThePullRequest(t *testing.T) {
	f := newPRFixture(t)
	pr, err := f.create(ports.CreatePullRequest{Title: "  Create pull request  ", Body: "Why", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.git.fetches, ",") != "main" || strings.Join(f.git.pushes, ",") != "agent/x" {
		t.Fatalf("fetches %v pushes %v", f.git.fetches, f.git.pushes)
	}
	want := domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1,
		URL: "https://github.com/acme/widgets/pull/1", Title: "Create pull request", HeadBranch: "agent/x", BaseBranch: "main",
		Draft: true, State: domain.PullRequestOpen}
	pr.CreatedAt, pr.UpdatedAt = time.Time{}, time.Time{}
	if *pr != want {
		t.Fatalf("created %+v\nwant    %+v", *pr, want)
	}
	if stored := f.store.LatestForWorkspace(f.ws.ID); stored == nil || stored.Number != 1 {
		t.Fatalf("stored %+v", stored)
	}
	if got := f.feed.pullRequests(); len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("announced %+v", got)
	}
	host, _ := f.host.Get(context.Background(), widgets, fixtureToken, 1)
	if host.Title != "Create pull request" || host.Base != "main" || !host.Draft {
		t.Fatalf("opened on the host as %+v", host)
	}
	// Every host call carried the stored token; fetch and push ran with an
	// operation's credentials, released once done.
	for _, tok := range f.host.Tokens() {
		if tok != fixtureToken {
			t.Fatalf("a host call carried %q", tok.Reveal())
		}
	}
	if len(f.git.envs) != 2 || len(f.git.envs[0]) == 0 || !strings.HasPrefix(f.git.envs[0][0], gitcredentialstest.EnvKey+"=op-") {
		t.Fatalf("fetch and push env %v", f.git.envs)
	}
	if f.auth.Broker.Operations() != 0 {
		t.Fatal("the operation's files outlived the create")
	}
	if list, _ := f.auth.ListGitCredentials(context.Background()); list.Credentials[0].LastUsedAt == nil || list.Credentials[1].LastUsedAt == nil {
		t.Fatalf("last_used_at not recorded: %+v %+v", list.Credentials[0], list.Credentials[1])
	}
}

func TestCreateUsesTheBaseAskedFor(t *testing.T) {
	f := newPRFixture(t)
	f.git.setRef("refs/remotes/origin/release", true)
	f.history.setAhead("refs/remotes/origin/release", 4)
	pr, err := f.create(ports.CreatePullRequest{Title: "T", BaseBranch: "release"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.BaseBranch != "release" || f.git.fetches[0] != "release" {
		t.Fatalf("base %q fetched %v", pr.BaseBranch, f.git.fetches)
	}
}

func TestCreateRefusals(t *testing.T) {
	cases := map[string]struct {
		arrange func(f *prFixture)
		req     ports.CreatePullRequest
		code    string
		reason  string
	}{
		"empty title":       {nil, ports.CreatePullRequest{Title: "   "}, "INVALID_INPUT", ""},
		"no such workspace": {nil, ports.CreatePullRequest{WorkspaceID: "nope", Title: "T"}, "WORKSPACE_NOT_FOUND", ""},
		"no origin":         {func(f *prFixture) { f.git.origin = "" }, ports.CreatePullRequest{Title: "T"}, "PR_NO_REMOTE", ""},
		"not GitHub":        {func(f *prFixture) { f.git.origin = "https://gitlab.com/acme/widgets" }, ports.CreatePullRequest{Title: "T"}, "PR_NO_REMOTE", "gitlab.com"},
		"no stored ssh key": {func(f *prFixture) {
			_ = f.auth.DeleteGitCredential(context.Background(), f.key.ID)
		}, ports.CreatePullRequest{Title: "T"}, "GIT_CREDENTIAL_MISSING", "ssh_key for github.com"},
		"no stored GitHub token": {func(f *prFixture) {
			_ = f.auth.DeleteGitCredential(context.Background(), f.token.ID)
		}, ports.CreatePullRequest{Title: "T"}, "GIT_CREDENTIAL_MISSING", "https_token for github.com"},
		"store locked": {func(f *prFixture) { f.auth.Lock() }, ports.CreatePullRequest{Title: "T"}, "GIT_CREDENTIAL_STORE_LOCKED", ""},
		"origin refuses the key": {func(f *prFixture) {
			f.git.pushErr = fmt.Errorf("git push: %w: git@github.com: Permission denied (publickey).", ports.ErrRemoteAuthRejected)
		}, ports.CreatePullRequest{Title: "T"}, "GIT_CREDENTIAL_REJECTED", "Permission denied (publickey)"},
		"origin refuses the key on fetch": {func(f *prFixture) {
			f.git.fetchErr = fmt.Errorf("git fetch: %w: Permission denied (publickey).", ports.ErrRemoteAuthRejected)
		}, ports.CreatePullRequest{Title: "T"}, "GIT_CREDENTIAL_REJECTED", "Permission denied"},
		"GitHub refuses the token": {func(f *prFixture) { f.host.Reject(fixtureToken) },
			ports.CreatePullRequest{Title: "T"}, "GIT_CREDENTIAL_REJECTED", "Bad credentials"},
		"base not on origin": {func(f *prFixture) {
			f.git.fetchErr = fmt.Errorf("%w: main", ports.ErrRemoteBranchNotFound)
		}, ports.CreatePullRequest{Title: "T"}, "PR_BASE_NOT_FOUND", "main"},
		"origin moved, nothing ahead": {func(f *prFixture) {
			f.git.onFetch = func(string) { f.history.setAhead("refs/remotes/origin/main", 0) }
		}, ports.CreatePullRequest{Title: "T"}, "PR_NO_COMMITS", ""},
		"push rejected": {func(f *prFixture) {
			f.git.pushErr = errors.New("git push: exit status 1: ! [rejected] agent/x -> agent/x (non-fast-forward)")
		}, ports.CreatePullRequest{Title: "T"}, "PR_PUSH_FAILED", "non-fast-forward"},
		"host refuses": {func(f *prFixture) {
			f.host.OpenErr = domain.PullRequestError(domain.CodePRCreateFailed, "Validation Failed; base invalid")
		}, ports.CreatePullRequest{Title: "T"}, "PR_CREATE_FAILED", "Validation Failed"},
		"host breaks": {func(f *prFixture) { f.host.OpenErr = errors.New("connection reset") },
			ports.CreatePullRequest{Title: "T"}, "PR_CREATE_FAILED", "connection reset"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPRFixture(t)
			if c.arrange != nil {
				c.arrange(f)
			}
			_, err := f.create(c.req)
			if errs.Code(err) != c.code {
				t.Fatalf("err = %v, want %s", err, c.code)
			}
			var se *domain.StructuredError
			errors.As(err, &se)
			if !strings.HasPrefix(se.Message, c.code+": ") || !strings.Contains(se.Message, c.reason) {
				t.Fatalf("message %q should start with %s and carry %q", se.Message, c.code, c.reason)
			}
			if f.store.LatestForWorkspace(f.ws.ID) != nil || len(f.feed.pullRequests()) != 0 {
				t.Fatal("a refused create stored or announced a PR")
			}
		})
	}
}

func TestCreateWithAWorktreeGoneFromDiskStillPushes(t *testing.T) {
	f := newPRFixture(t)
	f.history.missing = true
	if _, err := f.create(ports.CreatePullRequest{Title: "T"}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAdoptsAPROpenedOutsideTheHarness(t *testing.T) {
	f := newPRFixture(t)
	f.host.Add(widgets, ports.HostPullRequest{Number: 7, Head: "agent/x", Base: "main", Title: "By hand", State: domain.PullRequestOpen})
	_, err := f.create(ports.CreatePullRequest{Title: "T"})
	assertAlreadyExists(t, err, 7)
	if len(f.git.pushes) != 0 {
		t.Fatal("pushed although a PR already stood open")
	}
	if stored := f.store.LatestForWorkspace(f.ws.ID); stored == nil || stored.Number != 7 || stored.Title != "By hand" {
		t.Fatalf("stored %+v", stored)
	}
	if got := f.feed.pullRequests(); len(got) != 1 || got[0].Number != 7 {
		t.Fatalf("announced %+v", got)
	}
}

func TestCreateAgainAnswersTheStoredOpenPR(t *testing.T) {
	f := newPRFixture(t)
	if _, err := f.create(ports.CreatePullRequest{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.create(ports.CreatePullRequest{Title: "T"})
	assertAlreadyExists(t, err, 1)
	if opens, _, _ := f.host.Calls(); opens != 1 {
		t.Fatalf("opens = %d", opens)
	}
}

func TestCreateAfterTheStoredPRWasMergedOpensANewOne(t *testing.T) {
	f := newPRFixture(t)
	if _, err := f.create(ports.CreatePullRequest{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	f.host.SetState(widgets, 1, domain.PullRequestMerged)
	pr, err := f.create(ports.CreatePullRequest{Title: "Follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 2 {
		t.Fatalf("new PR #%d", pr.Number)
	}
	if first := f.feed.pullRequests(); len(first) != 3 || first[1].Number != 1 || first[1].State != domain.PullRequestMerged {
		t.Fatalf("announced %+v; the merge found on the way is announced too", first)
	}
}

func TestCreateAdoptsWhenTheHostSaysItAlreadyExists(t *testing.T) {
	f := newPRFixture(t)
	// The host has it, but FindOpen did not see it yet (a race).
	f.host.OpenErr = fmt.Errorf("%w: A pull request already exists for acme:agent/x.", ports.ErrPullRequestExists)
	gotFirst := false
	f.git.onFetch = func(string) {
		if !gotFirst {
			gotFirst = true
			f.host.Add(widgets, ports.HostPullRequest{Number: 3, Head: "agent/x", Base: "main", State: domain.PullRequestOpen})
		}
	}
	_, err := f.create(ports.CreatePullRequest{Title: "T"})
	assertAlreadyExists(t, err, 3)
}

func TestConcurrentCreatesOpenOnePR(t *testing.T) {
	f := newPRFixture(t)
	f.host.Gate = make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := f.create(ports.CreatePullRequest{Title: "T"})
			results <- err
		}()
	}
	// Let both in, then release the host for good.
	time.Sleep(50 * time.Millisecond)
	close(f.host.Gate)
	var ok, exists int
	for range 2 {
		switch err := <-results; errs.Code(err) {
		case "":
			ok++
		case domain.CodePRAlreadyExists:
			exists++
		default:
			t.Fatalf("unexpected %v", err)
		}
	}
	if ok != 1 || exists != 1 {
		t.Fatalf("ok %d exists %d", ok, exists)
	}
	if opens, _, _ := f.host.Calls(); opens != 1 {
		t.Fatalf("opens = %d", opens)
	}
}

func TestProjectDeletedRemovesItsPullRequests(t *testing.T) {
	f := newPRFixture(t)
	if _, err := f.create(ports.CreatePullRequest{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	if err := f.bus.Publish(context.Background(), domain.ProjectDeleted{ProjectID: "p1"}); err != nil {
		t.Fatal(err)
	}
	if f.store.LatestForWorkspace(f.ws.ID) != nil {
		t.Fatal("the record outlived its project")
	}
}

func assertAlreadyExists(t *testing.T, err error, number int) {
	t.Helper()
	if errs.Code(err) != domain.CodePRAlreadyExists {
		t.Fatalf("err = %v, want PR_ALREADY_EXISTS", err)
	}
	var de *domain.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("no details on %v", err)
	}
	details, _ := de.Details.(map[string]any)
	pr, _ := details["pull_request"].(*domain.PullRequest)
	if pr == nil || pr.Number != number {
		t.Fatalf("details = %#v, want pull_request #%d", de.Details, number)
	}
}

func TestStoredPullRequestsReadsTheStoreOnly(t *testing.T) {
	f := newPRFixture(t)
	w2, err := f.deps.Workspaces.Create(domain.Workspace{RepositoryID: f.ws.RepositoryID, Name: "other", Branch: "agent/y", Path: "/wt2"})
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	_, _ = f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1, State: domain.PullRequestClosed, CreatedAt: t0})
	_, _ = f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 2, State: domain.PullRequestOpen, CreatedAt: t0.Add(time.Hour)})
	// Git and the host would fail the call if it reached them.
	f.git.origin = "unused"
	f.host.Gate = make(chan struct{})

	got, err := f.svc.StoredPullRequests(context.Background(), []string{f.ws.ID, w2.ID, "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 2 || got[0].WorkspaceID != f.ws.ID {
		t.Fatalf("got %+v, want the newest of the workspace that has one", got)
	}
	f.svc.Wait()
	if opens, gets, finds := f.host.Calls(); opens+gets+finds != 0 {
		t.Fatalf("host calls %d %d %d", opens, gets, finds)
	}
	if len(f.git.fetches)+len(f.git.pushes) != 0 {
		t.Fatal("git was asked")
	}

	empty, err := f.svc.StoredPullRequests(context.Background(), nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("no ids = %#v, %v; want an empty, non-nil list", empty, err)
	}
}

func TestRefreshNowAsksTheHostDespiteTheThrottle(t *testing.T) {
	f := newPRFixture(t)
	_, _ = f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1, HeadBranch: "agent/x", BaseBranch: "main", State: domain.PullRequestOpen})
	f.host.Add(widgets, ports.HostPullRequest{Number: 1, Head: "agent/x", Base: "main", State: domain.PullRequestOpen})
	f.get(t) // uses up this 30 s slot

	f.host.SetState(widgets, 1, domain.PullRequestMerged)
	if err := f.svc.RefreshNow(context.Background(), f.ws.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.feed.pullRequests(); len(got) != 1 || got[0].State != domain.PullRequestMerged {
		t.Fatalf("announced %+v by the time RefreshNow returned", got)
	}
	if v, _ := f.svc.PullRequest(context.Background(), f.ws.ID); v.PullRequest.State != domain.PullRequestMerged {
		t.Fatalf("stored %s", v.PullRequest.State)
	}

	// Nothing stored: it adopts the pushed branch's open PR.
	g := newPRFixture(t)
	g.git.setRef("refs/remotes/origin/agent/x", true)
	g.host.Add(widgets, ports.HostPullRequest{Number: 4, Head: "agent/x", Base: "main", State: domain.PullRequestOpen})
	if err := g.svc.RefreshNow(context.Background(), g.ws.ID); err != nil {
		t.Fatal(err)
	}
	if got := g.feed.pullRequests(); len(got) != 1 || got[0].Number != 4 {
		t.Fatalf("adopted %+v", got)
	}

	if err := g.svc.RefreshNow(context.Background(), "nope"); errs.Code(err) != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("unknown workspace = %v", err)
	}
}

// A refusal names the credential the host refused, so the person can fix
// that one.
func TestRejectedNamesTheRefusedCredential(t *testing.T) {
	for name, c := range map[string]struct {
		arrange func(f *prFixture)
		want    func(f *prFixture) string
	}{
		"the push key": {func(f *prFixture) {
			f.git.pushErr = fmt.Errorf("git push: %w: Permission denied (publickey).", ports.ErrRemoteAuthRejected)
		}, func(f *prFixture) string { return f.key.ID }},
		"the GitHub token": {func(f *prFixture) { f.host.Reject(fixtureToken) }, func(f *prFixture) string { return f.token.ID }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPRFixture(t)
			c.arrange(f)
			_, err := f.create(ports.CreatePullRequest{Title: "T"})
			var de *domain.DetailedError
			if !errors.As(err, &de) {
				t.Fatalf("no details on %v", err)
			}
			d, _ := de.Details.(map[string]any)
			if d["host"] != "github.com" || d["credential_id"] != c.want(f) {
				t.Fatalf("details %v, want host github.com and credential %s", d, c.want(f))
			}
		})
	}
}

// Without a stored GitHub token the background refresh does not ask the
// host at all: there is no other token to try.
func TestTheBackgroundRefreshNeedsAStoredToken(t *testing.T) {
	f := newPRFixture(t)
	_, _ = f.store.Save(&domain.PullRequest{WorkspaceID: f.ws.ID, RepositoryID: f.ws.RepositoryID, ProjectID: "p1", Number: 1, HeadBranch: "agent/x", BaseBranch: "main", State: domain.PullRequestOpen})
	f.host.Add(widgets, ports.HostPullRequest{Number: 1, Head: "agent/x", Base: "main", State: domain.PullRequestOpen})
	if err := f.auth.DeleteGitCredential(context.Background(), f.token.ID); err != nil {
		t.Fatal(err)
	}
	f.get(t)
	if opens, gets, finds := f.host.Calls(); opens+gets+finds != 0 {
		t.Fatalf("the host was asked without a stored token: %d %d %d", opens, gets, finds)
	}
	if err := f.svc.RefreshNow(context.Background(), f.ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, gets, _ := f.host.Calls(); gets != 0 {
		t.Fatal("RefreshNow asked the host without a stored token")
	}
}
