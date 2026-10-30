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

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
)

// syncHost stands in for the server's git state routes (GET
// /api/worktree_git_state, POST /api/fetch_remote and /api/push_branch) until
// the server serves them: the web module's own HTTP gateway talks to it as it
// will to the server. A test sets it in newCodeWorld's deps (w.sync) and moves
// the world with its fields; the feed's git_state echo is w.feed.send.
type syncHost struct {
	mu     sync.Mutex
	states map[string]map[string]any // by workspace id, in the contract's wire shape

	// afterFetch is merged into the states of the fetched repository (the remote moved).
	afterFetch map[string]any
	fetchTook  time.Duration
	// pushRefusal is the code the next push answers with instead of pushing.
	pushRefusal string
	// fetchRefusal likewise for the next fetch ("GIT_CREDENTIAL_MISSING" names the host).
	fetchRefusal string

	fetches []string
	pushes  []map[string]any
}

func syncUpstream(state, ref string, ahead, behind any) map[string]any {
	m := map[string]any{"state": state, "ref": nil, "remote": nil, "ahead": ahead, "behind": behind}
	if ref != "" {
		m["ref"], m["remote"] = ref, "origin"
	}
	return m
}

// syncState is a GitState as the contract writes it, on the fixture's worktree.
func (w *codeWorld) syncState(upstream map[string]any, fetchedAgo time.Duration, base [2]int) map[string]any {
	fetched := any(nil)
	if fetchedAgo >= 0 {
		fetched = time.Now().Add(-fetchedAgo).UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"workspace_id": w.at.WorkspaceID, "repository_id": w.at.RepositoryID, "session_id": "s1", "branch": w.f.Branch,
		"head": "ab6f51b9d3c0e1f2a3b4c5d6e7f8091a2b3c4d5e", "gone": false,
		"base":        map[string]any{"ref": w.f.BaseRef, "ahead": base[0], "behind": base[1]},
		"upstream":    upstream,
		"fetched_at":  fetched,
		"uncommitted": map[string]any{"tracked": 1, "untracked": 2, "staged": 0},
		"read_at":     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func newSyncHost(states ...map[string]any) *syncHost {
	h := &syncHost{states: map[string]map[string]any{}, fetchTook: 400 * time.Millisecond}
	for _, s := range states {
		h.states[s["workspace_id"].(string)] = s
	}
	return h
}

func (h *syncHost) touch(s map[string]any, fields map[string]any) {
	for k, v := range fields {
		s[k] = v
	}
	s["read_at"] = time.Now().UTC().Format(time.RFC3339Nano)
}

func (h *syncHost) refuse(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "code": code, "details": details})
}

func (h *syncHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/worktree_git_state":
		h.mu.Lock()
		defer h.mu.Unlock()
		s, ok := h.states[r.URL.Query().Get("workspace_id")]
		if !ok {
			h.refuse(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "WORKSPACE_NOT_FOUND: no such workspace", nil)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"state": s})
	case "/api/fetch_remote":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		time.Sleep(h.fetchTook)
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.fetchRefusal != "" {
			code := h.fetchRefusal
			h.fetchRefusal = ""
			h.refuse(w, http.StatusBadRequest, code, code+": no stored credential resolves for the origin", map[string]any{"host": "github.com", "kind": "https_token"})
			return
		}
		h.fetches = append(h.fetches, body["repository_id"])
		at := time.Now().UTC().Format(time.RFC3339)
		for _, s := range h.states {
			if s["repository_id"] == body["repository_id"] {
				h.touch(s, map[string]any{"fetched_at": at})
				if h.afterFetch != nil {
					h.touch(s, h.afterFetch)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"fetched_at": at})
	case "/api/push_branch":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		h.mu.Lock()
		defer h.mu.Unlock()
		ws := fmt.Sprint(body["workspace_id"])
		s := h.states[ws]
		if h.pushRefusal != "" {
			code := h.pushRefusal
			h.pushRefusal = ""
			h.touch(s, map[string]any{"upstream": syncUpstream("diverged", "origin/"+fmt.Sprint(s["branch"]), 3, 1)})
			h.refuse(w, http.StatusConflict, code, code+": ! [rejected] (non-fast-forward)", map[string]any{"state": s})
			return
		}
		h.pushes = append(h.pushes, body)
		h.touch(s, map[string]any{"upstream": syncUpstream("in_sync", "origin/"+fmt.Sprint(s["branch"]), 0, 0)})
		_ = json.NewEncoder(w).Encode(map[string]any{"state": s})
	default:
		http.NotFound(w, r)
	}
}

func (h *syncHost) pushed() []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]map[string]any(nil), h.pushes...)
}

func (h *syncHost) fetched() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.fetches...)
}

// syncWorld is a running session on the code fixture whose worktree's git
// state is the one given.
func syncWorld(t *testing.T, state func(w *codeWorld) map[string]any, extra ...func(w *codeWorld, d *Deps)) *codeWorld {
	t.Helper()
	return newCodeWorld(t, func(w *codeWorld, d *Deps) {
		withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID })(w, d)
		w.sync = newSyncHost(state(w))
		for _, e := range extra {
			e(w, d)
		}
	})
}

func aheadOfItsRemote(w *codeWorld) map[string]any {
	return w.syncState(syncUpstream("ahead", "origin/"+w.f.Branch, 3, 0), 12*time.Minute, [2]int{3, 0})
}

func codeURL(w *codeWorld) string {
	return w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code"
}

const chipSelector = `section[aria-label=Code] .code-source-bar .sync-slot > button.sync-state`

// chipJS reads the source bar's chip as the person does: its state, its
// words and what follows the commit.
const chipJS = `(() => { const b = document.querySelector('` + chipSelector + `'); return b ? b.dataset.state + '|' + b.innerText.replace(/\s+/g, ' ').trim() : '' })()`

func pollFor(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(8*time.Second))
}

func evalString(js string, out *string) chromedp.Action { return chromedp.Evaluate(js, out) }

// TestTheCodeTabsSourceBarShowsTheSyncChipAndItsPanel: the chip follows the
// commit; it opens the panel (both comparisons, the fetch age, Fetch and Push),
// which traps focus and closes on Escape; Fetch reads the remote and the new
// counts show; Push asks first, is refused with its reason, and then goes.
func TestTheCodeTabsSourceBarShowsTheSyncChipAndItsPanel(t *testing.T) {
	w := syncWorld(t, aheadOfItsRemote)
	ctx, errs := browser(t)

	var chip, afterCommit, panelText, inFetch, fetched, fetchBtn, question, refusal, afterPush string
	var trapped, closed, refocused bool
	var shot []byte
	panelOpen := `!!document.querySelector('section[aria-label=Code] .sync-panel:popover-open')`
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(codeURL(w)),
		pollFor(`document.querySelectorAll('[data-ssr]').length === 0 && `+chipJS+` !== ''`),
		evalString(chipJS, &chip),
		evalString(`(() => { const kids = [...document.querySelector('section[aria-label=Code] .code-source-bar .origin').children].filter(c => c.tagName !== 'TEMPLATE'); return kids[kids.findIndex(c => c.classList.contains('commit')) + 1].className })()`, &afterCommit),
		chromedp.Evaluate(`document.querySelector('`+chipSelector+`').click()`, nil),
		pollFor(panelOpen),
		evalString(`document.querySelector('section[aria-label=Code] .sync-panel').innerText.replace(/\s+/g, ' ')`, &panelText),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),
		// focus stays inside: Tab round the panel more times than it has controls
		chromedp.KeyEvent(kb.Tab), chromedp.KeyEvent(kb.Tab), chromedp.KeyEvent(kb.Tab), chromedp.KeyEvent(kb.Tab), chromedp.KeyEvent(kb.Tab),
		chromedp.Evaluate(`!!document.activeElement.closest('.sync-panel')`, &trapped),
		chromedp.KeyEvent(kb.Escape),
		pollFor(`!`+panelOpen),
		chromedp.Evaluate(`document.activeElement === document.querySelector('`+chipSelector+`')`, &refocused),
		chromedp.Evaluate(`!`+panelOpen, &closed),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	saveShot("sync-panel-code.jpg", shot)
	if !strings.HasPrefix(chip, "ahead|") || !strings.Contains(chip, "origin") || !strings.Contains(chip, "3 ahead") || !strings.Contains(chip, "0 behind") {
		t.Errorf("chip = %q, want the remote and its counts", chip)
	}
	if afterCommit != "sync-slot" {
		t.Errorf("the chip does not follow the commit: %q", afterCommit)
	}
	for _, want := range []string{"vs base " + w.f.BaseRef, "vs upstream origin/" + w.f.Branch, "Remote last fetched 12 min ago", "Fetch", "Push 3 commits"} {
		if !strings.Contains(panelText, want) {
			t.Errorf("panel lacks %q: %s", want, panelText)
		}
	}
	if !trapped {
		t.Error("Tab left the panel")
	}
	if !closed || !refocused {
		t.Errorf("Escape: closed %v, focus back on the chip %v", closed, refocused)
	}

	// Fetch: the chip and the panel say Fetching… until the new counts are in.
	w.sync.mu.Lock()
	w.sync.afterFetch = map[string]any{"upstream": syncUpstream("diverged", "origin/"+w.f.Branch, 3, 2)}
	w.sync.mu.Unlock()
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('`+chipSelector+`').click()`, nil),
		pollFor(panelOpen),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-fetch]').click()`, nil),
		pollFor(`document.querySelector('section[aria-label=Code] .sync-panel [data-fetch]').textContent.includes('Fetching')`),
		evalString(chipJS, &inFetch),
		pollFor(`document.querySelector('`+chipSelector+`').dataset.state === 'diverged'`),
		evalString(chipJS, &fetched),
		evalString(`document.querySelector('section[aria-label=Code] .sync-panel .fresh').innerText.replace(/\s+/g, ' ')`, &fetchBtn),
	)
	if err != nil {
		t.Fatalf("fetch: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(inFetch, "Fetching") {
		t.Errorf("the chip while fetching = %q", inFetch)
	}
	if !strings.HasPrefix(fetched, "diverged|") || !strings.Contains(fetched, "2 behind") {
		t.Errorf("after the fetch the chip = %q", fetched)
	}
	if !strings.Contains(fetchBtn, "just now") {
		t.Errorf("the age line after the fetch = %q", fetchBtn)
	}
	if got := w.sync.fetched(); len(got) != 1 || got[0] != w.at.RepositoryID {
		t.Errorf("fetches = %v", got)
	}

	// Push is not offered when diverged; back to ahead, it asks, is refused, then pushes.
	w.sync.mu.Lock()
	w.sync.touch(w.sync.states[w.at.WorkspaceID], map[string]any{"upstream": syncUpstream("ahead", "origin/"+w.f.Branch, 3, 0)})
	w.sync.pushRefusal = "PUSH_REJECTED"
	w.sync.mu.Unlock()
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-fetch]').click()`, nil),
		pollFor(`document.querySelector('`+chipSelector+`').dataset.state === 'diverged' || document.querySelector('`+chipSelector+`').dataset.state === 'ahead'`),
	)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	w.sync.mu.Lock()
	w.sync.afterFetch = map[string]any{"upstream": syncUpstream("ahead", "origin/"+w.f.Branch, 3, 0)}
	w.sync.mu.Unlock()
	err = chromedp.Run(ctx,
		pollFor(`!!document.querySelector('section[aria-label=Code] .sync-panel [data-push]')`),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-push]').click()`, nil),
		pollFor(`!!document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push]')`),
		evalString(`document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push]').innerText.replace(/\s+/g, ' ')`, &question),
		chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Code] .sync-panel [data-confirm=push] button')].find(b => b.textContent.trim() === 'Cancel').click()`, nil),
		pollFor(`!document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push]') && !!document.querySelector('section[aria-label=Code] .sync-panel [data-push]')`),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-push]').click()`, nil),
		pollFor(`!!document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push]')`),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push] [data-variant=primary]').click()`, nil),
		pollFor(`!!document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push-refused]')`),
		evalString(`document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push-refused]').innerText.replace(/\s+/g, ' ')`, &refusal),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push-refused] button').click()`, nil),
		pollFor(`!document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push-refused]')`),
	)
	if err != nil {
		var dump string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel').innerText`, &dump))
		t.Fatalf("push: %v\npanel: %s\nJS errors: %v", err, dump, errs.all())
	}
	if !strings.Contains(question, "Push 3 commits to origin/"+w.f.Branch+"?") || !strings.Contains(question, "never a force push") {
		t.Errorf("the push question = %q", question)
	}
	if !strings.Contains(refusal, "The remote refused the push") || !strings.Contains(refusal, "PUSH_REJECTED") {
		t.Errorf("the refusal = %q", refusal)
	}
	if n := len(w.sync.pushed()); n != 0 {
		t.Fatalf("%d pushes went through before one was allowed", n)
	}
	// The refusal re-read the state: diverged now, so nothing to push; put it back and push for real.
	w.sync.mu.Lock()
	w.sync.touch(w.sync.states[w.at.WorkspaceID], map[string]any{"upstream": syncUpstream("ahead", "origin/"+w.f.Branch, 3, 0)})
	w.sync.mu.Unlock()
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-fetch]').click()`, nil),
		pollFor(`document.querySelector('`+chipSelector+`').dataset.state === 'ahead' && !!document.querySelector('section[aria-label=Code] .sync-panel [data-push]')`),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-push]').click()`, nil),
		pollFor(`!!document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push]')`),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-confirm=push] [data-variant=primary]').click()`, nil),
		pollFor(`document.querySelector('`+chipSelector+`').dataset.state === 'in-sync'`),
		evalString(chipJS, &afterPush),
	)
	if err != nil {
		var dump string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel').innerText`, &dump))
		t.Fatalf("second push: %v\npanel: %s\nJS errors: %v", err, dump, errs.all())
	}
	pushes := w.sync.pushed()
	if len(pushes) != 1 || pushes[0]["workspace_id"] != w.at.WorkspaceID || pushes[0]["expected_head"] != "ab6f51b9d3c0e1f2a3b4c5d6e7f8091a2b3c4d5e" {
		t.Errorf("pushes = %v, want one at the head the person saw", pushes)
	}
	if !strings.Contains(afterPush, "in sync") {
		t.Errorf("after the push the chip = %q", afterPush)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestTheSyncChipSaysEachStateInWords: not pushed, upstream deleted, not
// fetched and in sync each read as words (never colour alone), and a missing
// credential names the host in the panel's banner.
func TestTheSyncChipSaysEachStateInWords(t *testing.T) {
	cases := []struct {
		name     string
		upstream func(w *codeWorld) map[string]any
		fetched  time.Duration
		state    string
		word     string
	}{
		{"never pushed", func(*codeWorld) map[string]any { return syncUpstream("none", "", nil, nil) }, 5 * time.Minute, "none", "not pushed"},
		{"upstream deleted", func(w *codeWorld) map[string]any { return syncUpstream("gone", "origin/"+w.f.Branch, nil, nil) }, 5 * time.Minute, "gone", "upstream deleted"},
		{"never fetched", func(w *codeWorld) map[string]any { return syncUpstream("unknown", "origin/"+w.f.Branch, nil, nil) }, -1, "unknown", "not fetched"},
		{"in sync", func(w *codeWorld) map[string]any { return syncUpstream("in_sync", "origin/"+w.f.Branch, 0, 0) }, 5 * time.Minute, "in-sync", "in sync"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := syncWorld(t, func(w *codeWorld) map[string]any { return w.syncState(c.upstream(w), c.fetched, [2]int{3, 0}) })
			ctx, errs := browser(t)
			var chip string
			err := chromedp.Run(ctx,
				chromedp.EmulateViewport(1440, 900),
				chromedp.Navigate(codeURL(w)),
				pollFor(`document.querySelectorAll('[data-ssr]').length === 0 && `+chipJS+` !== ''`),
				evalString(chipJS, &chip),
			)
			if err != nil {
				t.Fatalf("%v\nJS errors: %v", err, errs.all())
			}
			if !strings.HasPrefix(chip, c.state+"|") || !strings.Contains(chip, c.word) {
				t.Errorf("chip = %q, want state %s and the words %q", chip, c.state, c.word)
			}
		})
	}
}

// TestAFetchWithoutACredentialNamesTheHostInThePanel: the banner names the
// host and links to the credentials page, with nothing else.
func TestAFetchWithoutACredentialNamesTheHostInThePanel(t *testing.T) {
	w := syncWorld(t, aheadOfItsRemote)
	w.sync.fetchRefusal = "GIT_CREDENTIAL_MISSING"
	ctx, errs := browser(t)
	var banner, href string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(codeURL(w)),
		pollFor(`document.querySelectorAll('[data-ssr]').length === 0 && `+chipJS+` !== ''`),
		chromedp.Evaluate(`document.querySelector('`+chipSelector+`').click()`, nil),
		pollFor(`!!document.querySelector('section[aria-label=Code] .sync-panel:popover-open')`),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .sync-panel [data-fetch]').click()`, nil),
		pollFor(`!!document.querySelector('section[aria-label=Code] .sync-panel .banner')`),
		evalString(`document.querySelector('section[aria-label=Code] .sync-panel .banner').innerText.replace(/\s+/g, ' ')`, &banner),
		evalString(`document.querySelector('section[aria-label=Code] .sync-panel .banner a').getAttribute('href')`, &href),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(banner, "github.com") || !strings.Contains(banner, "Git credentials") || !strings.HasPrefix(href, "/git-credentials") {
		t.Errorf("banner = %q, link %q", banner, href)
	}
}

func withHistory(w *codeWorld, d *Deps) {
	d.History = gitHistory{
		dirs:       map[string]string{w.at.RepositoryID: w.f.RepoDir},
		workspaces: map[string]testWorkspace{w.at.WorkspaceID: {repo: w.at.RepositoryID, dir: w.f.Worktree, branch: w.f.Branch, base: w.f.BaseRef}},
	}
}

// TestTheHistoryHeadShowsBothComparisonsAndOpensTheSamePanel: "vs main" and
// "vs upstream" with Fetch and how old the fetch is, the chip following the
// feed, and the uncommitted row's list linking to Code ▸ Working.
func TestTheHistoryHeadShowsBothComparisonsAndOpensTheSamePanel(t *testing.T) {
	w := syncWorld(t, aheadOfItsRemote, withHistory)
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/history"
	head := `main > header.toolbar .git-head`

	var line, panel, afterEcho, afterFetch, openLink, rows string
	var files, hrefsOK int
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		pollFor(`!!document.querySelector('`+head+` .sync-slot > button.sync-state') && document.querySelector('`+head+` .sync-slot').checkVisibility()`),
		evalString(`document.querySelector('`+head+`').innerText.replace(/\s+/g, ' ')`, &line),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),
		chromedp.Evaluate(`document.querySelector('`+head+` .sync-slot > button.sync-state').click()`, nil),
		pollFor(`!!document.querySelector('`+head+` .sync-panel:popover-open')`),
		evalString(`document.querySelector('`+head+` .sync-panel').innerText.replace(/\s+/g, ' ')`, &panel),
		chromedp.KeyEvent(kb.Escape),
		pollFor(`!document.querySelector('`+head+` .sync-panel:popover-open')`),
		// the feed tells a new state: the remote moved
		chromedp.ActionFunc(func(ctx context.Context) error { return w.feed.connected().Do(ctx) }),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	saveShot("history-head-sync.jpg", shot)
	for _, want := range []string{"vs " + w.f.BaseRef, "vs upstream", "origin", "Fetch"} {
		if !strings.Contains(line, want) {
			t.Errorf("head lacks %q: %s", want, line)
		}
	}
	if !strings.Contains(line, "fetched 12 min ago") && !strings.Contains(line, "fetched 13 min ago") {
		t.Errorf("head lacks the fetch age: %s", line)
	}
	if !strings.Contains(panel, "vs upstream") || !strings.Contains(panel, "Push 3 commits") {
		t.Errorf("the panel from the History head = %q", panel)
	}

	behind := w.syncState(syncUpstream("behind", "origin/"+w.f.Branch, 0, 4), 1*time.Minute, [2]int{3, 0})
	b, _ := json.Marshal(map[string]any{"git_state": behind})
	w.feed.send(string(b))
	err = chromedp.Run(ctx,
		pollFor(`document.querySelector('`+head+` .sync-slot > button.sync-state').dataset.state === 'behind'`),
		evalString(`document.querySelector('`+head+`').innerText.replace(/\s+/g, ' ')`, &afterEcho),
		// Fetch beside the line
		chromedp.Evaluate(`document.querySelector('`+head+` button[data-fetch]').click()`, nil),
		pollFor(`document.querySelector('`+head+` button[data-fetch]').textContent.includes('Fetching')`),
		pollFor(`document.querySelector('`+head+`').innerText.includes('fetched just now')`),
		evalString(`document.querySelector('`+head+`').innerText.replace(/\s+/g, ' ')`, &afterFetch),
	)
	if err != nil {
		t.Fatalf("feed and fetch: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(afterEcho, "4 behind") {
		t.Errorf("after the feed's state the head reads %q", afterEcho)
	}
	if !strings.Contains(afterFetch, "fetched just now") || len(w.sync.fetched()) != 1 {
		t.Errorf("after Fetch the head reads %q (%d fetches)", afterFetch, len(w.sync.fetched()))
	}

	// The uncommitted row opens the shared list, which links to Code ▸ Working; there is no second diff.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('nav[aria-label=Commits] a[data-working]')?.click()`, nil),
		pollFor(`location.pathname.endsWith('/history/working') && document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('article[aria-label="Uncommitted changes"] .diff-summary .changed a').length > 0`),
		evalString(`document.querySelector('article[aria-label="Uncommitted changes"] > div a[href*="scope=working"]').textContent`, &openLink),
		chromedp.Evaluate(`document.querySelectorAll('article[aria-label="Uncommitted changes"] section.file, article[aria-label="Uncommitted changes"] .diff-view').length`, &files),
		chromedp.Evaluate(`[...document.querySelectorAll('article[aria-label="Uncommitted changes"] .diff-summary .changed a')].filter(a => a.getAttribute('href').includes('/tasks/t1/sessions/s1/code/changes?scope=working#diff-')).length`, &hrefsOK),
		evalString(`[...document.querySelectorAll('article[aria-label="Uncommitted changes"] .diff-summary .changed a .name')].map(n => n.textContent).join(',')`, &rows),
	)
	if err != nil {
		t.Fatalf("working row: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(openLink, "Open in Code") || !strings.Contains(openLink, "Working") {
		t.Errorf("link = %q", openLink)
	}
	if files != 0 {
		t.Errorf("the uncommitted changes render %d diff elements: the diff is the Code tab's", files)
	}
	if hrefsOK == 0 || rows != "README.md,new.txt,nonl.txt" {
		t.Errorf("rows %q, %d linking to the Code tab's Working scope", rows, hrefsOK)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
