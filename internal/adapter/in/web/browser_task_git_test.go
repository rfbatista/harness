package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeGitHost plays the server's git state for the task page's e2e: the first
// paint's seed (the same states, in the contract's wire shape), the project
// feed's git_state events, and the fetch and push routes. It is the stand-in
// until the server serves them; the sync module's own HTTP gateway talks to it
// exactly as it will to the server.
type fakeGitHost struct {
	ports.WorktreeGit // the methods the page does not call
	mu                sync.Mutex
	states            map[string]map[string]any // by workspace id, wire shape
	order             []string
	fetches           []string
	pushes            []map[string]any
	remotes           []string // the repositories' remotes, origin first; nil is newFakeGitHost's origin and fork
	streams           []chan string
}

func upstreamOf(state, ref string, ahead, behind any) map[string]any {
	m := map[string]any{"state": state, "ref": nil, "remote": nil, "ahead": ahead, "behind": behind}
	if ref != "" {
		m["ref"], m["remote"] = ref, "origin"
	}
	return m
}

// wireState is a GitState as the contract writes it.
func wireState(ws, repo, session, branch string, base, upstream map[string]any, dirty [3]int, fetchedAgo time.Duration) map[string]any {
	return map[string]any{
		"workspace_id": ws, "repository_id": repo, "session_id": session, "branch": branch,
		"head": "ab6f51b9d3c0e1f2a3b4c5d6e7f8091a2b3c4d5e", "gone": false,
		"base": base, "upstream": upstream,
		"fetched_at":  time.Now().Add(-fetchedAgo).UTC().Format(time.RFC3339),
		"uncommitted": map[string]any{"tracked": dirty[0], "untracked": dirty[1], "staged": dirty[2]},
		"read_at":     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func newFakeGitHost(states ...map[string]any) *fakeGitHost {
	f := &fakeGitHost{states: map[string]map[string]any{}, remotes: []string{"origin", "fork"}}
	for _, s := range states {
		id := s["workspace_id"].(string)
		f.states[id] = s
		f.order = append(f.order, id)
	}
	return f
}

// TaskGitStates is the ports.WorktreeGit the page paints its first state from:
// the states the host holds, decoded from their wire shape, in their order.
func (f *fakeGitHost) TaskGitStates(_ context.Context, _ string) ([]domain.GitState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	states := []domain.GitState{}
	for _, id := range f.order {
		raw, err := json.Marshal(f.states[id])
		if err != nil {
			return nil, err
		}
		var s domain.GitState
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	return states, nil
}

// change replaces a state's fields and tells the feed, as the code watcher would.
func (f *fakeGitHost) change(ws string, fields map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.update(ws, fields)
}

// update must be called with the lock held.
func (f *fakeGitHost) update(ws string, fields map[string]any) {
	s := f.states[ws]
	for k, v := range fields {
		s[k] = v
	}
	s["read_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	msg, _ := json.Marshal(map[string]any{"git_state": s})
	for _, ch := range f.streams {
		ch <- string(msg)
	}
}

func (f *fakeGitHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/worktree_git_state":
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"state": f.states[r.URL.Query().Get("workspace_id")]})
	case "/api/fetch_remote":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		time.Sleep(300 * time.Millisecond) // the network
		f.mu.Lock()
		defer f.mu.Unlock()
		f.fetches = append(f.fetches, body["repository_id"])
		at := time.Now().UTC().Format(time.RFC3339)
		for _, id := range f.order {
			if f.states[id]["repository_id"] == body["repository_id"] {
				f.update(id, map[string]any{"fetched_at": at})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"fetched_at": at})
	case "/api/push_branch":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pushes = append(f.pushes, body)
		ws := fmt.Sprint(body["workspace_id"])
		branch := fmt.Sprint(f.states[ws]["branch"])
		if body["branch"] == nil && body["remote"] == nil {
			f.update(ws, map[string]any{"upstream": upstreamOf("in_sync", "origin/"+branch, 0, 0)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"state": f.states[ws]})
	case "/api/repository_remotes":
		f.mu.Lock()
		defer f.mu.Unlock()
		remotes := []map[string]string{}
		for _, name := range f.remotes {
			remotes = append(remotes, map[string]string{"name": name, "url": "https://example.test/" + name + ".git"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"remotes": remotes})
	case "/api/push_preview":
		q := r.URL.Query()
		branch, remote := q.Get("branch"), q.Get("remote")
		against, commits := "base", 3
		if remote == "origin" {
			against, commits = "remote", 2
		}
		remoteBranch := q.Get("remote_branch")
		if remoteBranch == "" {
			remoteBranch = branch
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"head": "head-of-" + branch, "commits": commits, "commits_against": against, "remote_branch": remoteBranch})
	case "/api/events":
		w.Header().Set("Content-Type", "text/event-stream")
		ch := make(chan string, 8)
		f.mu.Lock()
		f.streams = append(f.streams, ch)
		f.mu.Unlock()
		fmt.Fprint(w, ": open\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case msg := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeGitHost) pushed() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.pushes...)
}

func (f *fakeGitHost) fetched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.fetches...)
}

// taskGitServer serves the board world with two worktrees on the feed task: an
// agent whose branch is 3 ahead of its remote, and one never pushed.
func taskGitServer(t *testing.T) (*fakeGitHost, string) {
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
	mux := http.NewServeMux()
	mux.Handle("/api/", host)
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env,
		WorktreeGit: host, Now: func() time.Time { return now },
	}, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() }) // the feed's stream stays open
	return host, srv.URL
}

const (
	taskFeedPage = "/projects/p1/tasks/t-feed"
	// shownJS is true when the element exists and takes space.
	shownFn = `const shown = (sel) => { const el = document.querySelector(sel); return !!el && el.checkVisibility() };`
)

func shownJS(sel string) string {
	return `(() => { ` + shownFn + ` return shown(` + jsString(sel) + `) })()`
}

// gitBrowser is one person's headless Chrome for these tests.
type gitBrowser struct {
	t    *testing.T
	ctx  context.Context
	errs *errorLog
}

func newGitBrowser(t *testing.T) *gitBrowser {
	ctx, errs := browser(t)
	return &gitBrowser{t: t, ctx: ctx, errs: errs}
}

func (b *gitBrowser) jsErrors() []string { return b.errs.all() }

// run runs one step and names it when it fails.
func (b *gitBrowser) run(step string, actions ...chromedp.Action) {
	b.t.Helper()
	if err := chromedp.Run(b.ctx, actions...); err != nil {
		var page string
		_ = chromedp.Run(b.ctx, chromedp.Evaluate(`document.querySelector('main')?.innerText.slice(0, 3000) ?? ''`, &page))
		b.t.Fatalf("%s: %v\npage:\n%s\nJS errors: %v", step, err, page, b.errs.all())
	}
}

// waitPoll polls js every 100ms for up to 10s.
func waitPoll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingInterval(100*time.Millisecond), chromedp.WithPollingTimeout(10*time.Second))
}

func textJS(sel string) string {
	return `(document.querySelector(` + jsString(sel) + `)?.innerText ?? '').replace(/\s+/g, ' ').trim()`
}

func clickJS(sel string) string { return `document.querySelector(` + jsString(sel) + `).click()` }

// TestTheTaskPageShowsEachWorktreesRemoteStateAndTheGitPopover covers stories
// 7 and 8: the chip on every session row after the badges, kept live by the
// feed, and the Git popover from the title row with Push behind a question and
// one Fetch per repository.
func TestTheTaskPageShowsEachWorktreesRemoteStateAndTheGitPopover(t *testing.T) {
	host, url := taskGitServer(t)
	b := newGitBrowser(t)

	const row1 = `[data-session-id="s1"] .sync-state`
	const row2 = `[data-session-id="s2"] .sync-state`
	b.run("open the task page", chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url+taskFeedPage),
		waitPoll(`document.querySelector('[data-ssr]') === null && `+shownJS(row1)+` && `+shownJS(row2)))

	var chip1, chip2, state1, state2, title1 string
	b.run("read the chips",
		chromedp.Evaluate(textJS(row1), &chip1), chromedp.Evaluate(textJS(row2), &chip2),
		chromedp.Evaluate(`document.querySelector('`+row1+`').dataset.state`, &state1),
		chromedp.Evaluate(`document.querySelector('`+row2+`').dataset.state`, &state2),
		chromedp.Evaluate(`document.querySelector('`+row1+`').title`, &title1))
	if chip1 != "origin ↑3 ahead ↓0 behind" || state1 != "ahead" {
		t.Errorf("row 1 chip = %q (%s), want origin ↑3 ahead ↓0 behind (ahead)", chip1, state1)
	}
	if chip2 != "not pushed" || state2 != "none" {
		t.Errorf("row 2 chip = %q (%s), want not pushed (none)", chip2, state2)
	}
	if !strings.Contains(title1, "origin/agent/sync-1234") || !strings.Contains(title1, "fetched 12 min ago") {
		t.Errorf("row 1 chip title = %q, want the upstream ref and the fetch age", title1)
	}
	var order string
	b.run("the chip follows the pull request badge",
		chromedp.Evaluate(`(() => { const who = document.querySelector('[data-session-id="s1"] .who'); const kids = [...who.children]; return kids.findIndex(k => k.contains(who.querySelector('.sync-state'))) === kids.length - 1 ? 'last' : 'not last' })()`, &order))
	if order != "last" {
		t.Errorf("the chip is %s in the row's first line", order)
	}

	// The feed keeps the chip live.
	host.change("ws-1", map[string]any{"upstream": upstreamOf("behind", "origin/agent/sync-1234", 0, 2)})
	b.run("the chip follows a git_state", waitPoll(`document.querySelector('`+row1+`').dataset.state === 'behind'`))
	host.change("ws-1", map[string]any{"upstream": upstreamOf("ahead", "origin/agent/sync-1234", 3, 0)})
	b.run("and back", waitPoll(`document.querySelector('`+row1+`').dataset.state === 'ahead'`))

	// The Git button sits after Feature and Reviews in the title row.
	var buttons string
	b.run("the title row", waitPoll(shownJS(`#task-git-button`)),
		chromedp.Evaluate(`[...document.querySelectorAll('.toolbar .actions > *, .toolbar .actions > span > button')].map(e => e.id).filter(Boolean).join(',')`, &buttons))
	if !strings.Contains(buttons, "task-git-button") {
		t.Fatalf("the title row's buttons: %q", buttons)
	}
	var follows bool
	b.run("after Feature and Reviews", chromedp.Evaluate(`(() => { const all = [...document.querySelectorAll('.toolbar .actions button')]; const at = (id) => all.findIndex(b => b.id === id); return at('task-git-button') > at('task-feature-button') && at('task-git-button') > at('task-reviews-button') })()`, &follows))
	if !follows {
		t.Errorf("the Git button comes after Feature and Reviews")
	}

	// Open the popover.
	b.run("open the popover", chromedp.Evaluate(clickJS(`#task-git-button`), nil),
		waitPoll(`document.getElementById('task-git').matches(':popover-open')`))
	var rows, popRow1, popRow2, foot string
	var fetchLabels, pushRows string
	b.run("read the popover",
		chromedp.Evaluate(`document.querySelectorAll('#task-git .row').length + ''`, &rows),
		chromedp.Evaluate(textJS(`#task-git .row[data-workspace-id="ws-1"]`), &popRow1),
		chromedp.Evaluate(textJS(`#task-git .row[data-workspace-id="ws-2"]`), &popRow2),
		chromedp.Evaluate(`[...document.querySelectorAll('#task-git [data-fetch]')].map(b => b.textContent.trim()).join('|')`, &fetchLabels),
		chromedp.Evaluate(`[...document.querySelectorAll('#task-git .row')].filter(r => { const p = r.querySelector('[data-push]'); return p && p.checkVisibility() }).map(r => r.dataset.workspaceId).join(',')`, &pushRows),
		chromedp.Evaluate(textJS(`#task-git .pop-foot .fresh`), &foot))
	if rows != "2" {
		t.Errorf("popover rows = %s, want 2", rows)
	}
	for _, want := range []string{"Reviewer", "agent/sync-1234", "base", "↑3 ahead", "↓8 behind", "upstream", "origin", "3 changed · 2 new", "Push", "Code"} {
		if !strings.Contains(popRow1, want) {
			t.Errorf("row 1 %q lacks %q", popRow1, want)
		}
	}
	for _, want := range []string{"Backend dev", "agent/docs-77aa", "not pushed", "1 changed", "Code"} {
		if !strings.Contains(popRow2, want) {
			t.Errorf("row 2 %q lacks %q", popRow2, want)
		}
	}
	// Row 2 is unpushed with commits: Push is offered on both.
	if pushRows != "ws-1,ws-2" {
		t.Errorf("Push offered on %q, want both worktrees", pushRows)
	}
	if fetchLabels != "↻ Fetch harness|↻ Fetch harnesskit" {
		t.Errorf("fetch buttons = %q", fetchLabels)
	}
	if foot != "Fetched 1 h ago" {
		t.Errorf("the foot's age = %q, want the oldest fetch (1 h ago)", foot)
	}
	var href string
	b.run("the Code link", chromedp.Evaluate(`document.querySelector('#task-git .row[data-workspace-id="ws-1"] .actions a').getAttribute('href')`, &href))
	if href != "/projects/p1/tasks/t-feed/sessions/s1/code" {
		t.Errorf("Code link = %q", href)
	}

	// Push asks first, naming the remote branch and the count; nothing was pushed yet.
	b.run("ask to push", chromedp.Evaluate(clickJS(`#task-git .row[data-workspace-id="ws-1"] [data-push]`), nil),
		waitPoll(shownJS(`#task-git .row[data-workspace-id="ws-1"] [data-confirm="push"]`)))
	var question string
	b.run("the question", chromedp.Evaluate(textJS(`#task-git .row[data-workspace-id="ws-1"] [data-confirm="push"] h3`), &question))
	if question != "Push 3 commits to origin/agent/sync-1234?" {
		t.Errorf("question = %q", question)
	}
	if n := len(host.pushed()); n != 0 {
		t.Errorf("%d pushes before the person confirmed", n)
	}
	b.run("confirm", chromedp.Evaluate(clickJS(`#task-git [data-push-confirm]`), nil),
		waitPoll(`document.querySelector('`+row1+`').dataset.state === 'in-sync'`))
	if p := host.pushed(); len(p) != 1 || p[0]["workspace_id"] != "ws-1" || p[0]["expected_head"] != "ab6f51b9d3c0e1f2a3b4c5d6e7f8091a2b3c4d5e" {
		t.Errorf("pushes = %v, want one for ws-1 at the head the person saw", p)
	}
	b.run("Push is gone for the pushed branch", waitPoll(`(() => { const p = document.querySelector('#task-git .row[data-workspace-id="ws-1"] [data-push]'); return !p || !p.checkVisibility() })()`))

	// One Fetch per repository: Fetching… while it runs, then the new age.
	var label string
	b.run("fetch the second repository", chromedp.Evaluate(clickJS(`#task-git [data-fetch][data-repository-id="r-kit"]`), nil),
		waitPoll(`document.querySelector('#task-git [data-repository-id="r-kit"]').textContent.trim() === 'Fetching…'`),
		waitPoll(`document.querySelector('#task-git [data-repository-id="r-kit"]').textContent.trim() === '↻ Fetch harnesskit'`),
		chromedp.Evaluate(textJS(`#task-git .pop-foot .fresh`), &foot),
		chromedp.Evaluate(`document.querySelector('#task-git [data-repository-id="r-harness"]').textContent.trim()`, &label))
	if f := host.fetched(); len(f) != 1 || f[0] != "r-kit" {
		t.Errorf("fetches = %v, want only r-kit", f)
	}
	if foot != "Fetched 12 min ago" {
		t.Errorf("after the fetch the oldest age = %q, want 12 min ago (the other repository)", foot)
	}
	if label != "↻ Fetch harness" {
		t.Errorf("the other repository's button = %q", label)
	}

	// Escape closes it, and the open state is not remembered.
	b.run("Escape", chromedp.KeyEvent(kb.Escape), waitPoll(`!document.getElementById('task-git').matches(':popover-open')`))
	var expanded string
	b.run("closed", chromedp.Evaluate(`document.getElementById('task-git-button').getAttribute('aria-expanded')`, &expanded))
	if expanded != "false" {
		t.Errorf("aria-expanded after Escape = %q", expanded)
	}
	b.run("Close button", chromedp.Evaluate(clickJS(`#task-git-button`), nil), waitPoll(`document.getElementById('task-git').matches(':popover-open')`),
		chromedp.Evaluate(clickJS(`#task-git .pop-head .button`), nil), waitPoll(`!document.getElementById('task-git').matches(':popover-open')`))
	b.run("reload: closed", chromedp.Navigate(url+taskFeedPage), waitPoll(shownJS(`#task-git-button`)))
	var open bool
	b.run("not stored", chromedp.Evaluate(`document.getElementById('task-git').matches(':popover-open')`, &open))
	if open {
		t.Errorf("the popover opened by itself on load")
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestTheGitButtonIsAbsentWithoutAWorktree is story 8's last clause: a task
// whose sessions have no worktree has no Git button.
func TestTheGitButtonIsAbsentWithoutAWorktree(t *testing.T) {
	_, url := taskGitServer(t)
	b := newGitBrowser(t)
	b.run("open a task whose sessions have no worktree", chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url+"/projects/p1/tasks/t-docs"),
		waitPoll(`document.querySelector('[data-ssr]') === null && document.querySelector('.toolbar h1') !== null`))
	var shown bool
	b.run("the button", chromedp.Evaluate(`(() => { const b = document.getElementById('task-git-button'); return !!b && b.checkVisibility() })()`, &shown))
	if shown {
		t.Errorf("the Git button shows for a task without a worktree")
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestTheGitPopoverIsABottomSheetOnAPhone is story 9 for the popover.
func TestTheGitPopoverIsABottomSheetOnAPhone(t *testing.T) {
	_, url := taskGitServer(t)
	b := newGitBrowser(t)
	b.run("open at phone width", chromedp.EmulateViewport(390, 844), chromedp.Navigate(url+taskFeedPage),
		waitPoll(shownJS(`#task-git-button`)))
	b.run("open the popover", chromedp.Evaluate(clickJS(`#task-git-button`), nil), waitPoll(`document.getElementById('task-git').matches(':popover-open')`))
	var sheet bool
	var box struct{ Left, Width, Bottom, Viewport float64 }
	b.run("measure",
		chromedp.Evaluate(`document.getElementById('task-git').hasAttribute('data-sheet')`, &sheet),
		chromedp.Evaluate(`(() => { const r = document.getElementById('task-git').getBoundingClientRect(); return { Left: r.left, Width: r.width, Bottom: r.bottom, Viewport: innerHeight } })()`, &box))
	if !sheet {
		t.Errorf("the popover has no data-sheet below 48rem")
	}
	if box.Left > 1 || box.Width < 388 || box.Bottom < box.Viewport-1 {
		t.Errorf("the sheet is at %+v, want full width on the bottom edge", box)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// The first paint carries the chips: with scripts off the session rows already
// read "origin ↑3 ahead ↓0 behind" and "not pushed", and once the browser takes
// over the chip is where it was, the same width, and the row's first line does
// not truncate (story 7, no flash or layout shift at hydration).
func TestTheSessionRowsChipIsPaintedBeforeScriptsAndStaysPutOnHydration(t *testing.T) {
	_, url := taskGitServer(t)
	b := newGitBrowser(t)
	const row1 = `[data-session-id="s1"] .sync-state`
	const row2 = `[data-session-id="s2"] .sync-state`
	const measure = `(() => { const c = document.querySelector('` + row1 + `').getBoundingClientRect(); const who = document.querySelector('[data-session-id="s1"] .who'); const row = document.querySelector('[data-session-id="s1"]').getBoundingClientRect(); return [Math.round(c.left - who.getBoundingClientRect().left), Math.round(c.top - row.top), Math.round(c.width), Math.round(row.height), who.scrollWidth <= who.clientWidth ? 1 : 0] })()`

	var ssrText1, ssrText2 string
	var before []int
	b.run("scripts off", emulation.SetScriptExecutionDisabled(true), chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url+taskFeedPage),
		chromedp.WaitVisible(row1, chromedp.ByQuery), chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(textJS(row1), &ssrText1), chromedp.Evaluate(textJS(row2), &ssrText2), chromedp.Evaluate(measure, &before))
	if ssrText1 != "origin ↑3 ahead ↓0 behind" || ssrText2 != "not pushed" {
		t.Errorf("first paint chips = %q and %q", ssrText1, ssrText2)
	}
	var after []int
	var liveText1 string
	b.run("scripts on", emulation.SetScriptExecutionDisabled(false), chromedp.Navigate(url+taskFeedPage),
		waitPoll(`document.querySelector('[data-ssr]') === null && !!document.querySelector('`+row1+`')`), chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(textJS(row1), &liveText1), chromedp.Evaluate(measure, &after))
	if liveText1 != ssrText1 {
		t.Errorf("the browser's chip reads %q, the server's %q", liveText1, ssrText1)
	}
	if len(before) != 5 || len(after) != 5 {
		t.Fatalf("measure: %v %v", before, after)
	}
	for i, name := range []string{"left", "top", "width", "row height"} {
		if d := before[i] - after[i]; d < -2 || d > 2 {
			t.Errorf("the chip moved on hydration: %s %d to %d", name, before[i], after[i])
		}
	}
	if before[4] != 1 || after[4] != 1 {
		t.Errorf("the row's first line truncates (before %d, after %d)", before[4], after[4])
	}
}
