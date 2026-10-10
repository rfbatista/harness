package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakePullRequests answers like the server's PullRequests port: views by
// workspace, the stored PRs, or the error it is set to.
type fakePullRequests struct {
	views  map[string]*ports.PullRequestView
	stored []*domain.PullRequest
	err    error
	asked  *[]string
}

func (f fakePullRequests) PullRequest(_ context.Context, id string) (*ports.PullRequestView, error) {
	if f.err != nil {
		return nil, f.err
	}
	if v, ok := f.views[id]; ok {
		return v, nil
	}
	return nil, errs.Newf("WORKSPACE_NOT_FOUND", "WORKSPACE_NOT_FOUND: workspace %s", id)
}

func (f fakePullRequests) CreatePullRequest(context.Context, ports.CreatePullRequest) (*domain.PullRequest, error) {
	return nil, errs.Newf("UNAVAILABLE", "pages only read")
}

func (f fakePullRequests) StoredPullRequests(_ context.Context, ids []string) ([]*domain.PullRequest, error) {
	if f.asked != nil {
		*f.asked = append(*f.asked, ids...)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.stored, nil
}

func agentDraft(blockers ...string) domain.PullRequestDraft {
	if blockers == nil {
		blockers = []string{}
	}
	return domain.PullRequestDraft{
		WorkspaceID: "ws-agent", RepositoryID: "r1", HeadBranch: "agent/add-sse-feed-1a2b", BaseBranch: "main",
		Title: "Add SSE feed", Ahead: 1, UncommittedChanges: 2, Remote: &domain.GitRemote{Host: "github.com", Owner: "acme", Repo: "harness"},
		Blockers: blockers,
	}
}

var agentPR = &domain.PullRequest{
	WorkspaceID: "ws-agent", RepositoryID: "r1", Number: 12, URL: "https://github.com/acme/harness/pull/12", Title: "Add SSE feed",
	HeadBranch: "agent/add-sse-feed-1a2b", BaseBranch: "main", State: domain.PullRequestOpen, CreatedAt: now,
}

// tagWith is the opening tag that carries marker.
func tagWith(body, marker string) string {
	i := strings.Index(body, marker)
	if i < 0 {
		return "(absent)"
	}
	start := strings.LastIndex(body[:i], "<")
	end := i + strings.Index(body[i:], ">")
	return body[start : end+1]
}

const sessionHistory = "/projects/p1/tasks/t-feed/sessions/s-agent/history"

func historyWith(t *testing.T, prs ports.PullRequests) string {
	t.Helper()
	w := historyWorld(t)
	w.prs = prs
	rec := get(t, newTestHandler(t, w), sessionHistory)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func TestSessionHistoryOffersAPullRequestAndSeedsThePanel(t *testing.T) {
	body := historyWith(t, fakePullRequests{views: map[string]*ports.PullRequestView{"ws-agent": {Draft: agentDraft()}}})
	for _, want := range []string{
		`data-seed="pull-request-seed" x-data="pullRequestPanel"`,
		`x-on:click="openForm"`,
		`>Create pull request</button>`,
		`id="pr-form"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(tagWith(body, `x-on:click="openForm"`), `aria-disabled="true"`) || strings.Contains(body, `class="[ pr-chip ]" href=`) {
		t.Error("nothing is in the way and there is no PR: an enabled button, no chip")
	}
	seed := seedOf(t, body, "pull-request-seed")
	draft, _ := seed["draft"].(map[string]any)
	if seed["project_id"] != "p1" || seed["workspace_id"] != "ws-agent" || draft["head_branch"] != "agent/add-sse-feed-1a2b" || seed["pull_request"] != nil {
		t.Errorf("seed = %v", seed)
	}
}

func TestSessionHistorySaysWhyAPullRequestCannotBeOpened(t *testing.T) {
	body := historyWith(t, fakePullRequests{views: map[string]*ports.PullRequestView{"ws-agent": {Draft: agentDraft("PR_NO_COMMITS", "GIT_CREDENTIAL_MISSING")}}})
	if !strings.Contains(tagWith(body, `x-on:click="openForm"`), `aria-disabled="true"`) {
		t.Error("the create button is disabled")
	}
	for _, want := range []string{
		`<li data-ssr>Nothing to propose yet: agent/add-sse-feed-1a2b has no commits ahead of main.`,
		`<li data-ssr>No credential stored for github.com can push this branch`,
		`<a href="/git-credentials?host=github.com">Add one in Git credentials</a>`,
		`aria-describedby="pr-reasons"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(tagWith(body, `aria-label="Pull request"`), "display: none") {
		t.Error("the reasons show on first paint")
	}
	if strings.Contains(body, "gh auth login") {
		t.Error("the gh sign-in copy is gone")
	}
}

func TestSessionHistoryShowsItsOpenPullRequestInsteadOfTheCreate(t *testing.T) {
	body := historyWith(t, fakePullRequests{views: map[string]*ports.PullRequestView{"ws-agent": {Draft: agentDraft(), PullRequest: agentPR}}})
	for _, want := range []string{
		`class="[ pr-chip ]" href="https://github.com/acme/harness/pull/12" target="_blank" rel="noopener"`,
		`data-tone="success"`,
		`>#12</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(tagWith(body, `x-on:click="openForm"`), `display: none`) {
		t.Error("no create while a PR is open")
	}

	merged := *agentPR
	merged.State = domain.PullRequestMerged
	body = historyWith(t, fakePullRequests{views: map[string]*ports.PullRequestView{"ws-agent": {Draft: agentDraft(), PullRequest: &merged}}})
	if !strings.Contains(body, `data-tone="signal"`) || strings.Contains(tagWith(body, `x-on:click="openForm"`), `display: none`) {
		t.Error("a merged PR shows beside a create")
	}
}

func TestSessionHistoryWithoutPullRequestsStillRenders(t *testing.T) {
	if body := historyWith(t, nil); strings.Contains(body, "pullRequestPanel") {
		t.Error("no reader: no panel")
	}
	if body := historyWith(t, fakePullRequests{views: map[string]*ports.PullRequestView{}}); strings.Contains(body, "pullRequestPanel") {
		t.Error("the workspace is gone: no PR action")
	}
	body := historyWith(t, fakePullRequests{err: errs.Newf("UNAVAILABLE", "store down")})
	if !strings.Contains(body, "pullRequestPanel") {
		t.Fatal("a failed read leaves the panel to read it itself")
	}
	if seed := seedOf(t, body, "pull-request-seed"); seed["draft"] != nil || seed["workspace_id"] != "ws-agent" {
		t.Errorf("seed = %v", seed)
	}
}

func TestTaskPageSeedsItsSessionsPullRequestsFromTheStore(t *testing.T) {
	w := historyWorld(t)
	var asked []string
	w.prs = fakePullRequests{stored: []*domain.PullRequest{agentPR}, asked: &asked}
	body := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed").Body.String()
	seed := seedOf(t, body, "pull-requests-seed")
	ids, _ := seed["workspace_ids"].([]any)
	prs, _ := seed["pull_requests"].([]any)
	if seed["project_id"] != "p1" || len(ids) != 1 || ids[0] != "ws-agent" || len(prs) != 1 {
		t.Errorf("seed = %v", seed)
	}
	if strings.Join(asked, ",") != "ws-agent" {
		t.Errorf("asked the store for %v: the task's sessions' workspaces only", asked)
	}
	for _, want := range []string{`x-data="pullRequestWatch" data-seed="pull-requests-seed"`, `x-data="pullRequestBadge(row.workspaceId)"`, `x-data="pullRequestBadge(ws)"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}

	w.prs = fakePullRequests{err: errs.Newf("UNAVAILABLE", "store down")}
	body = get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed").Body.String()
	if prs, _ := seedOf(t, body, "pull-requests-seed")["pull_requests"].([]any); prs == nil || len(prs) != 0 {
		t.Error("a failed read seeds none, never null")
	}
}
