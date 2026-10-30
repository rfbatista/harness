package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// realGit is the acceptance world of ticket 09: the task page and the Code tab
// over the real worktree git service, real git and the fixture's bare origin,
// served through the real /api routes. Only the feed's wire is the test's (the
// service's announcements are pumped onto a fake /api/events).
type realGit struct {
	f    *portstest.WorktreeGitFixture
	at   portstest.WorktreeGitTarget
	feed *fakeFeed
	url  string
	task string
}

func newRealGit(t *testing.T) *realGit {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	f := portstest.NewWorktreeGitFixture(t)
	var code workspacestest.CodeHandle
	svc, at, rec := workspacestest.NewWorktreeGitWithFeed(t, f, workspacestest.WithCode(&code))
	r := &realGit{f: f, at: at, feed: newFakeFeed(), task: at.TaskID}

	w := board()
	w.tickets[0].ID = at.TaskID
	w.repos = []*domain.Repository{{ID: at.RepositoryID, ProjectID: "p1", Name: "fixture", RootDir: f.RepoDir}}
	w.sessions = nil
	for _, branch := range portstest.WorktreeGitBranches {
		w.sessions = append(w.sessions, &domain.Session{
			ID: at.Sessions[branch], ProjectID: "p1", TicketID: at.TaskID, RepositoryID: at.RepositoryID,
			WorkspaceID: at.Workspaces[branch], Branch: branch, AgentID: "reviewer", Task: "work on " + branch,
			Status: domain.SessionRunning, Interactive: true, UpdatedAt: now,
		})
	}
	history := gitHistory{dirs: map[string]string{at.RepositoryID: f.RepoDir}, workspaces: map[string]testWorkspace{}}
	for _, branch := range portstest.WorktreeGitBranches {
		if branch != "gone" {
			history.workspaces[at.Workspaces[branch]] = testWorkspace{repo: at.RepositoryID, dir: f.Worktrees[branch], branch: branch, base: f.BaseRef}
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/api/events", r.feed)
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Code: code.Code, WorktreeGit: svc})))
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env,
		WorktreeGit: svc, Code: code.Code, Workspaces: fakeWorkspaces{base: f.BaseRef}, History: history, Now: time.Now,
	}, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	r.url = srv.URL

	// What the service announces reaches the page's feed as the server's does.
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		seen := 0
		for {
			select {
			case <-done:
				return
			case <-time.After(40 * time.Millisecond):
			}
			states := rec.GitStates()
			for ; seen < len(states); seen++ {
				if msg, err := json.Marshal(ports.ProjectChange{GitState: &states[seen]}); err == nil {
					r.feed.send(string(msg))
				}
			}
		}
	}()
	return r
}

func (r *realGit) taskPage() string { return r.url + "/projects/p1/tasks/" + r.task }

// rowChip is a session row's chip selector, by branch.
func (r *realGit) rowChip(branch string) string {
	return `[data-session-id="` + r.at.Sessions[branch] + `"] .sync-state`
}

func (r *realGit) popRow(branch string) string {
	return `#task-git .row[data-workspace-id="` + r.at.Workspaces[branch] + `"]`
}

// The task page reads the real worktrees: before any fetch every upstream that
// exists is "not fetched"; the popover's Fetch turns each into its counts; Push
// publishes the ahead branch to the real bare origin behind a question; and a
// hook that declines it is a refusal that names the reason, nothing pushed.
func TestRealGitTaskPageFetchPushAndRefusals(t *testing.T) {
	r := newRealGit(t)
	b := newGitBrowser(t)

	b.run("open the task page", chromedp.EmulateViewport(1280, 900), chromedp.Navigate(r.taskPage()),
		waitPoll(`document.querySelector('[data-ssr]') === null && `+shownJS(r.rowChip("ahead"))))
	chips := func() map[string]string {
		out := map[string]string{}
		for _, branch := range portstest.WorktreeGitBranches {
			var s string
			b.run("read "+branch, chromedp.Evaluate(`(`+shownJS(r.rowChip(branch))+` ? `+textJS(r.rowChip(branch))+` : '')`, &s))
			out[branch] = s
		}
		return out
	}
	before := chips()
	want := map[string]string{"nopush": "not pushed", "insync": "origin not fetched", "ahead": "origin not fetched", "behind": "origin not fetched", "diverged": "origin not fetched", "deleted": "origin not fetched", "gone": ""}
	for branch, w := range want {
		if before[branch] != w {
			t.Errorf("before a fetch, %s chip = %q, want %q", branch, before[branch], w)
		}
	}

	b.run("open the popover", chromedp.Evaluate(clickJS(`#task-git-button`), nil),
		waitPoll(`document.getElementById('task-git').matches(':popover-open')`))
	b.run("fetch", chromedp.Evaluate(clickJS(`#task-git [data-fetch]`), nil),
		waitPoll(`(`+textJS(r.rowChip("ahead"))+`).includes('↑1') && (`+textJS(r.rowChip("behind"))+`).includes('↓1') && (`+textJS(r.rowChip("diverged"))+`).includes('↓1') && (`+textJS(r.rowChip("deleted"))+`).includes('deleted')`))
	after := chips()
	for branch, w := range map[string]string{
		"insync": "origin in sync", "ahead": "origin ↑1 ahead ↓0 behind", "behind": "origin ↑0 ahead ↓1 behind",
		"diverged": "origin ↑1 ahead ↓1 behind", "deleted": "origin upstream deleted",
	} {
		if !strings.Contains(after[branch], strings.TrimPrefix(w, "origin ")) {
			t.Errorf("after the fetch, %s chip = %q, want %q", branch, after[branch], w)
		}
	}
	if !strings.Contains(r.f.Git(t, r.f.RepoDir, "branch", "-r"), "origin/ahead") {
		t.Error("the fetch did not run in the repository")
	}

	// The gone worktree reads "worktree gone", offers no Push and opens Code on the branch.
	var goneText, goneHref string
	var gonePush bool
	b.run("the gone row", chromedp.Evaluate(textJS(r.popRow("gone")), &goneText),
		chromedp.Evaluate(`(() => { const p = document.querySelector('`+r.popRow("gone")+` [data-push]'); return !!p && p.checkVisibility() })()`, &gonePush),
		chromedp.Evaluate(`document.querySelector('`+r.popRow("gone")+` .actions a').getAttribute('href')`, &goneHref))
	if !strings.Contains(goneText, "worktree gone") || strings.Contains(goneText, "clean") || gonePush {
		t.Errorf("gone row = %q, push offered %v", goneText, gonePush)
	}
	if want := "/projects/p1/tasks/" + r.task + "/sessions/" + r.at.Sessions["gone"] + "/code/changes?scope=branch"; goneHref != want {
		t.Errorf("gone Code link = %q, want %q", goneHref, want)
	}

	// A hook on origin declines the push: the strip names git's reason, the
	// branch stays ahead, origin is untouched.
	remove := r.f.RefuseInOrigin(t, "ahead")
	originBefore := r.f.OriginRev(t, "ahead")
	b.run("ask to push", chromedp.Evaluate(clickJS(r.popRow("ahead")+` [data-push]`), nil),
		waitPoll(shownJS(r.popRow("ahead")+` [data-confirm="push"]`)),
		chromedp.Evaluate(clickJS(`#task-git [data-push-confirm]`), nil),
		waitPoll(shownJS(r.popRow("ahead")+` [data-refusal="push"]`)))
	var refusal string
	b.run("the refusal", chromedp.Evaluate(textJS(r.popRow("ahead")+` [data-refusal="push"]`), &refusal))
	if !strings.Contains(refusal, "The remote refused the push") || !strings.Contains(refusal, "declined by the fixture's hook") || !strings.Contains(refusal, "PUSH_REJECTED") {
		t.Errorf("refusal = %q", refusal)
	}
	if r.f.OriginRev(t, "ahead") != originBefore {
		t.Error("a refused push moved origin")
	}
	var state string
	b.run("still ahead", chromedp.Evaluate(`document.querySelector('`+r.rowChip("ahead")+`').dataset.state`, &state))
	if state != "ahead" {
		t.Errorf("after the refusal the chip is %q", state)
	}

	// Without the hook the same push goes through, to the real origin.
	remove()
	b.run("dismiss and push again",
		chromedp.Evaluate(clickJS(r.popRow("ahead")+` [data-refusal="push"] .button`), nil),
		waitPoll(shownJS(r.popRow("ahead")+` [data-push]`)),
		chromedp.Evaluate(clickJS(r.popRow("ahead")+` [data-push]`), nil),
		waitPoll(shownJS(r.popRow("ahead")+` [data-confirm="push"]`)),
		chromedp.Evaluate(clickJS(`#task-git [data-push-confirm]`), nil),
		waitPoll(`document.querySelector('`+r.rowChip("ahead")+`').dataset.state === 'in-sync'`))
	if got, want := r.f.OriginRev(t, "ahead"), r.f.Git(t, r.f.Worktrees["ahead"], "rev-parse", "HEAD"); got != want {
		t.Errorf("origin's ahead = %s, want the worktree's HEAD %s", got, want)
	}

	// A fetch that cannot reach origin is a refusal in the panel; counts stay.
	moved := r.f.OriginDir + ".away"
	if err := os.Rename(r.f.OriginDir, moved); err != nil {
		t.Fatal(err)
	}
	b.run("fetch with origin away", chromedp.Evaluate(clickJS(`#task-git [data-fetch]`), nil),
		waitPoll(shownJS(`#task-git [data-refusal="fetch"]`)))
	var popup string
	b.run("the refusal", chromedp.Evaluate(textJS(`#task-git [data-refusal="fetch"]`), &popup))
	if !strings.Contains(popup, "The fetch failed") || !strings.Contains(popup, "FETCH_FAILED") {
		t.Errorf("a failed fetch reads %q", popup)
	}
	_ = os.Rename(moved, r.f.OriginDir)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// The Code tab over the real worktree: the chip after the commit in the source
// bar and its panel read the real counts and Fetch updates them; the changed
// files carry the staged flag and the rename the real diff reports; and a
// file written to the worktree appears in the list when the feed says the code
// changed, without a reload.
func TestRealGitCodeTabChipPanelAndChangedFiles(t *testing.T) {
	r := newRealGit(t)
	b := newGitBrowser(t)
	wt := r.f.Worktrees["ahead"]
	r.f.Git(t, wt, "mv", "README.md", "GUIDE.md")
	session := r.at.Sessions["ahead"]
	const bar = `section[aria-label=Code] .code-source-bar`
	chip := bar + ` .sync-slot > button.sync-state`

	b.run("open the Code tab", chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(r.url+"/projects/p1/tasks/"+r.task+"/sessions/"+session+"/code/changes"),
		waitPoll(`document.querySelectorAll('[data-ssr]').length === 0 && `+shownJS(chip)))
	var state, text string
	b.run("the chip", chromedp.Evaluate(`document.querySelector('`+chip+`').dataset.state`, &state),
		chromedp.Evaluate(textJS(chip), &text))
	if state != "unknown" || !strings.Contains(text, "not fetched") {
		t.Errorf("before a fetch the chip is %q (%s)", text, state)
	}

	b.run("open the panel", chromedp.Evaluate(clickJS(chip), nil),
		waitPoll(`!!document.querySelector('`+bar+` .sync-panel:popover-open')`))
	var panel string
	b.run("read the panel", chromedp.Evaluate(textJS(bar+` .sync-panel`), &panel))
	for _, want := range []string{"vs base main", "vs upstream origin/ahead", "Remote not fetched yet", "Fetch"} {
		if !strings.Contains(panel, want) {
			t.Errorf("the panel %q lacks %q", panel, want)
		}
	}
	b.run("fetch from the panel", chromedp.Evaluate(clickJS(bar+` .sync-panel [data-fetch]`), nil),
		waitPoll(`document.querySelector('`+chip+`').dataset.state === 'ahead'`),
		waitPoll(`(`+textJS(bar+` .sync-panel .fresh`)+`).includes('Remote last fetched')`))
	b.run("the counts", chromedp.Evaluate(textJS(chip), &text))
	if !strings.Contains(text, "↑1") || !strings.Contains(text, "↓0") {
		t.Errorf("after the fetch the chip is %q", text)
	}
	b.run("close the panel", chromedp.Evaluate(`document.querySelector('`+bar+` .sync-panel').hidePopover()`, nil))

	// The Working scope lists what the real diff says: a staged edit, a rename.
	b.run("the Working scope", chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Code] .diff-summary .head .segmented a')].find(a => a.textContent.trim().startsWith('Working')).click()`, nil),
		waitPoll(`document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length >= 4`))
	var labels []string
	b.run("the rows", chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a')].map(a => a.getAttribute('aria-label'))`, &labels))
	joined := strings.Join(labels, "\n")
	for _, want := range []string{"staged.txt, modified", ", staged", "GUIDE.md, renamed", "untracked.txt, untracked, new"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the rows lack %q:\n%s", want, joined)
		}
	}

	// A file the agent writes shows up when the feed says the code changed.
	if err := os.WriteFile(wt+"/late.txt", []byte("late\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.feed.send(`{"code_changed":{"workspace_id":"` + r.at.Workspaces["ahead"] + `","head":"` + r.f.Heads["ahead"] + `"}}`)
	b.run("the list follows", waitPoll(`[...document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a .name')].some(n => n.textContent === 'late.txt')`))
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// The History head over the real worktree: before the browser holds a state the
// server's own paint says both comparisons, and after it the head is the
// browser's with the chip that opens the panel; Fetch there moves the counts.
func TestRealGitHistoryHead(t *testing.T) {
	r := newRealGit(t)
	b := newGitBrowser(t)
	url := r.url + "/projects/p1/tasks/" + r.task + "/sessions/" + r.at.Sessions["ahead"] + "/history"
	const head = `.git-head`
	chip := head + ` .sync-slot > button.sync-state`

	b.run("open the history", chromedp.EmulateViewport(1440, 900), chromedp.Navigate(url), waitPoll(shownJS(chip)))
	var line string
	b.run("the head", chromedp.Evaluate(textJS(head), &line))
	for _, want := range []string{"ahead", "vs main", "vs upstream", "origin", "not fetched yet", "Fetch"} {
		if !strings.Contains(line, want) {
			t.Errorf("the head %q lacks %q", line, want)
		}
	}
	b.run("fetch from the head", chromedp.Evaluate(clickJS(head+` [data-fetch]`), nil),
		waitPoll(`document.querySelector('`+chip+`').dataset.state === 'ahead'`),
		waitPoll(`(`+textJS(head)+`).includes('fetched just now')`))
	b.run("the head after", chromedp.Evaluate(textJS(head), &line))
	if !strings.Contains(line, "↑1") {
		t.Errorf("after the fetch the head reads %q", line)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
