package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// fakeBranchLists plays the server's Branches lists for the task page's e2e:
// the first paint's seed (ports.SessionBranchLists, read in process), the
// answer of GET /api/session_branches, and the feed's session_branches key.
type fakeBranchLists struct {
	mu    sync.Mutex
	lists map[string][]domain.SessionBranch
	order []string
	host  *fakeGitHost
}

func (f *fakeBranchLists) SessionBranches(_ context.Context, id string) ([]domain.SessionBranch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists[id], nil
}

func (f *fakeBranchLists) TaskSessionBranches(_ context.Context, _ string) ([]domain.SessionBranches, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.SessionBranches{}
	for _, id := range f.order {
		out = append(out, domain.SessionBranches{SessionID: id, Branches: f.lists[id]})
	}
	return out, nil
}

// change replaces a session's list and tells the project feed, as the server does.
func (f *fakeBranchLists) change(id string, list []domain.SessionBranch) {
	f.mu.Lock()
	f.lists[id] = list
	f.mu.Unlock()
	msg, _ := json.Marshal(map[string]any{"session_branches": domain.SessionBranches{SessionID: id, Branches: list}})
	f.host.mu.Lock()
	defer f.host.mu.Unlock()
	for _, ch := range f.host.streams {
		ch <- string(msg)
	}
}

func (f *fakeBranchLists) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/session_branches" {
		f.mu.Lock()
		list := f.lists[r.URL.Query().Get("session_id")]
		f.mu.Unlock()
		if list == nil {
			list = []domain.SessionBranch{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"branches": list})
		return
	}
	f.host.ServeHTTP(w, r)
}

func branch(name string, state domain.SessionBranchState, base *domain.BranchBase, pushed ...domain.SessionBranchPush) domain.SessionBranch {
	b := domain.SessionBranch{Name: name, IsSessionBranch: state == domain.SessionBranchSession, State: state, Base: base, Pushed: pushed, RequestIDs: []string{}}
	if b.Pushed == nil {
		b.Pushed = []domain.SessionBranchPush{}
	}
	if state == domain.SessionBranchUnapproved {
		at := time.Now()
		b.FlaggedAt = &at
	}
	return b
}

func branchesServer(t *testing.T, lists map[string][]domain.SessionBranch) (*fakeBranchLists, string) {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	w := board()
	w.sessions[0].WorkspaceID, w.sessions[0].RepositoryID, w.sessions[0].Branch = "ws-1", "r-harness", "agent/sync-1234"
	w.sessions[1].WorkspaceID, w.sessions[1].RepositoryID, w.sessions[1].Branch, w.sessions[1].AgentID = "ws-2", "r-kit", "agent/docs-77aa", "backend"
	host := newFakeGitHost(
		wireState("ws-1", "r-harness", "s1", "agent/sync-1234",
			map[string]any{"ref": "main", "ahead": 3, "behind": 8}, upstreamOf("ahead", "origin/agent/sync-1234", 3, 0), [3]int{3, 2, 1}, 12*time.Minute),
		wireState("ws-2", "r-kit", "s2", "agent/docs-77aa",
			map[string]any{"ref": "main", "ahead": 5, "behind": 0}, upstreamOf("none", "", nil, nil), [3]int{1, 0, 0}, 90*time.Minute),
	)
	fake := &fakeBranchLists{lists: lists, order: []string{"s1", "s2"}, host: host}
	mux := http.NewServeMux()
	mux.Handle("/api/", fake)
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env,
		WorktreeGit: host, SessionBranches: fake, Now: func() time.Time { return now },
	}, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	return fake, srv.URL
}

// The Branches section of the Git popover shows each state with its counts and
// pushes, the task page's session rows carry "N branches" and a flag for an
// unapproved branch, and both follow the feed (spec "the Branches list and
// detection" stories 1 to 5, 13 to 15).
func TestTheBranchesSectionAndTheRowLinkShowEachStateAndFollowTheFeed(t *testing.T) {
	pushedAt := time.Now().Add(-time.Hour)
	fake, url := branchesServer(t, map[string][]domain.SessionBranch{
		"s1": {
			branch("agent/sync-1234", domain.SessionBranchSession, &domain.BranchBase{Ref: "main", Ahead: 3, Behind: 8},
				domain.SessionBranchPush{Remote: "origin", RemoteBranch: "agent/sync-1234", At: pushedAt, By: domain.PushedByPerson}),
			branch("feature-x", domain.SessionBranchApproved, &domain.BranchBase{Ref: "agent/sync-1234", Ahead: 2, Behind: 0},
				domain.SessionBranchPush{Remote: "fork", RemoteBranch: "feature-x", At: pushedAt, By: domain.PushedByAgent}),
			branch("nope", domain.SessionBranchDenied, nil),
			branch("old", domain.SessionBranchDeleted, nil),
			branch("sneaky", domain.SessionBranchUnapproved, nil),
		},
		"s2": {branch("agent/docs-77aa", domain.SessionBranchSession, &domain.BranchBase{Ref: "main", Ahead: 5, Behind: 0})},
	})
	b := newGitBrowser(t)
	const link1 = `[data-session-id="s1"] .branch-link`
	const link2 = `[data-session-id="s2"] .branch-link`
	b.run("open the task page", chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url+taskFeedPage),
		waitPoll(`document.querySelector('[data-ssr]') === null && `+shownJS(link1)+` && `+shownJS(link2)))

	var l1, l2, flag1, flag2 string
	b.run("read the links",
		chromedp.Evaluate(textJS(link1), &l1), chromedp.Evaluate(textJS(link2), &l2),
		chromedp.Evaluate(`document.querySelector('`+link1+`').hasAttribute('data-flagged') + ''`, &flag1),
		chromedp.Evaluate(`document.querySelector('`+link2+`').hasAttribute('data-flagged') + ''`, &flag2))
	if !strings.HasPrefix(l1, "5 branches") || !strings.Contains(l1, "unapproved") || flag1 != "true" {
		t.Errorf("session 1 link = %q flagged=%s, want 5 branches with the unapproved flag", l1, flag1)
	}
	if l2 != "1 branch" || flag2 != "false" {
		t.Errorf("session 2 link = %q flagged=%s, want 1 branch and no flag", l2, flag2)
	}

	// The link opens the popover at that session's section.
	b.run("follow the link", chromedp.Evaluate(clickJS(link1), nil),
		waitPoll(`document.getElementById('task-git').matches(':popover-open')`),
		waitPoll(shownJS(`#task-git [data-branches-for="s1"]`)))
	section := `#task-git [data-branches-for="s1"]`
	var items, text string
	b.run("read the section",
		chromedp.Evaluate(`[...document.querySelectorAll('`+section+` .item')].map(i => i.dataset.state + (i.hasAttribute('data-flagged') ? '!' : '')).join(',')`, &items),
		chromedp.Evaluate(textJS(section), &text))
	if items != "session,approved,denied,deleted,unapproved!" {
		t.Errorf("rows = %q", items)
	}
	for _, want := range []string{"5 branches", "agent/sync-1234", "session branch", "↑3", "↓8", "main", "pushed to origin/agent/sync-1234 by you",
		"feature-x", "approved", "↑2", "pushed to fork/feature-x by the agent", "nope", "denied", "old", "deleted", "sneaky", "unapproved"} {
		if !strings.Contains(text, want) {
			t.Errorf("section %q lacks %q", text, want)
		}
	}

	// A request answered or a finding found while the page is open shows with no reload.
	fake.change("s2", []domain.SessionBranch{
		branch("agent/docs-77aa", domain.SessionBranchSession, &domain.BranchBase{Ref: "main", Ahead: 5, Behind: 0}),
		branch("surprise", domain.SessionBranchUnapproved, nil),
	})
	b.run("the row link follows the feed", waitPoll(`(`+textJS(link2)+`).startsWith('2 branches') && document.querySelector('`+link2+`').hasAttribute('data-flagged')`))
	b.run("so does the section", waitPoll(`(`+textJS(`#task-git [data-branches-for="s2"]`)+`).includes('surprise')`))
	fake.change("s2", []domain.SessionBranch{branch("agent/docs-77aa", domain.SessionBranchSession, &domain.BranchBase{Ref: "main", Ahead: 5, Behind: 0})})
	b.run("and the flag goes", waitPoll(`(`+textJS(link2)+`) === '1 branch' && !document.querySelector('`+link2+`').hasAttribute('data-flagged')`))
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JS errors: %v", errs)
	}
}
