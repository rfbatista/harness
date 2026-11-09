package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// pushableList is a session's list with one branch in every state.
func pushableList() []domain.SessionBranch {
	return []domain.SessionBranch{
		branch("agent/sync-1234", domain.SessionBranchSession, &domain.BranchBase{Ref: "main", Ahead: 3, Behind: 8}),
		branch("feature-x", domain.SessionBranchApproved, &domain.BranchBase{Ref: "agent/sync-1234", Ahead: 2, Behind: 0}),
		branch("nope", domain.SessionBranchDenied, nil),
		branch("old", domain.SessionBranchDeleted, nil),
		branch("sneaky", domain.SessionBranchUnapproved, nil),
	}
}

// openBranchesPopover opens the task page and its Git popover at the Branches of s1.
func openBranchesPopover(b *gitBrowser, url string) {
	b.run("open the task page", chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url+taskFeedPage),
		waitPoll(`document.querySelector('[data-ssr]') === null && `+shownJS(`[data-session-id="s1"] .branch-link`)))
	b.run("open the popover", chromedp.Evaluate(clickJS(`[data-session-id="s1"] .branch-link`), nil),
		waitPoll(shownJS(`#task-git [data-branches-for="s1"]`)))
}

const (
	s1Section   = `#task-git [data-branches-for="s1"]`
	featureItem = s1Section + ` .item[data-state="approved"]`
	sessionItem = s1Section + ` .item[data-state="session"]`
)

// setRemote picks a remote in the open confirmation, as a person does.
func setRemote(sel, remote string) string {
	return `(() => { const s = document.querySelector(` + jsString(sel+` select[data-branch-remote]`) + `); s.value = ` + jsString(remote) + `; s.dispatchEvent(new Event('change', { bubbles: true })); })()`
}

// Each row of the Branches section in state session or approved has Push; the
// confirmation names the remote, the remote branch and the commit count, with
// origin preselected; the remote picked changes it; and the push lands on that
// remote and shows on the row (spec "push a session's branches from the UI"
// stories 1 to 4 and 9).
func TestAnApprovedBranchIsPushedFromItsRowToTheRemoteThePersonPicks(t *testing.T) {
	fake, url := branchesServer(t, map[string][]domain.SessionBranch{"s1": pushableList(), "s2": {branch("agent/docs-77aa", domain.SessionBranchSession, nil)}})
	b := newGitBrowser(t)
	openBranchesPopover(b, url)

	var buttons string
	b.run("wait for the remotes",
		waitPoll(`document.querySelectorAll('`+s1Section+` [data-branch-push]').length === 2`),
		chromedp.Evaluate(`[...document.querySelectorAll('`+s1Section+` .item')].filter(i => i.querySelector('[data-branch-push]')).map(i => i.dataset.state).join(',')`, &buttons))
	if buttons != "session,approved" {
		t.Errorf("Push is on %q, want the session branch and the approved one", buttons)
	}

	b.run("ask the push question", chromedp.Evaluate(clickJS(featureItem+` [data-branch-push]`), nil),
		waitPoll(shownJS(featureItem+` [data-confirm="branch-push"]`)),
		waitPoll(`(`+textJS(featureItem+` [data-confirm="branch-push"]`)+`).includes('2 commits')`))
	var question, selected, options string
	b.run("read the question",
		chromedp.Evaluate(textJS(featureItem+` [data-confirm="branch-push"] h3`), &question),
		chromedp.Evaluate(`document.querySelector('`+featureItem+` select[data-branch-remote]').value`, &selected),
		chromedp.Evaluate(`[...document.querySelectorAll('`+featureItem+` select[data-branch-remote] option')].map(o => o.value).join(',')`, &options))
	if selected != "origin" || options != "origin,fork" {
		t.Errorf("picker = %q of %q, want origin preselected among origin,fork", selected, options)
	}
	if !strings.Contains(question, "origin/feature-x") || !strings.Contains(question, "2 commits") {
		t.Errorf("question = %q, want the remote, the remote branch and the commit count", question)
	}

	b.run("pick fork", chromedp.Evaluate(setRemote(featureItem, "fork"), nil),
		waitPoll(`(`+textJS(featureItem+` [data-confirm="branch-push"] h3`)+`).includes('fork/feature-x')`))
	b.run("read the question again", chromedp.Evaluate(textJS(featureItem+` [data-confirm="branch-push"] h3`), &question))
	if !strings.Contains(question, "3 commits") || !strings.Contains(question, "not known here") {
		t.Errorf("question = %q, want the count against the base for a remote branch not known here", question)
	}

	b.run("confirm", chromedp.Evaluate(clickJS(featureItem+` [data-branch-push-confirm]`), nil),
		waitPoll(`document.querySelector('`+featureItem+` [data-confirm="branch-push"]') === null`))
	got := onePush(t, fake.host)
	if got["workspace_id"] != "ws-1" || got["branch"] != "feature-x" || got["remote"] != "fork" || got["expected_head"] != "head-of-feature-x" || got["remote_branch"] != "feature-x" {
		t.Errorf("push body = %v", got)
	}
	b.run("the row shows where it went", waitPoll(`(`+textJS(featureItem)+`).includes('pushed to fork/feature-x by you')`))
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JS errors: %v", errs)
	}
}

func TestTheSessionBranchGoesToANonOriginRemoteFromTheSameList(t *testing.T) {
	fake, url := branchesServer(t, map[string][]domain.SessionBranch{"s1": pushableList(), "s2": {branch("agent/docs-77aa", domain.SessionBranchSession, nil)}})
	b := newGitBrowser(t)
	openBranchesPopover(b, url)
	b.run("ask on the session branch", waitPoll(shownJS(sessionItem+` [data-branch-push]`)),
		chromedp.Evaluate(clickJS(sessionItem+` [data-branch-push]`), nil),
		waitPoll(shownJS(sessionItem+` [data-confirm="branch-push"]`)),
		waitPoll(`(`+textJS(sessionItem+` [data-confirm="branch-push"]`)+`).includes('origin/agent/sync-1234')`),
		chromedp.Evaluate(setRemote(sessionItem, "fork"), nil),
		waitPoll(`(`+textJS(sessionItem+` [data-confirm="branch-push"]`)+`).includes('fork/agent/sync-1234')`),
		chromedp.Evaluate(clickJS(sessionItem+` [data-branch-push-confirm]`), nil),
		waitPoll(`document.querySelector('`+sessionItem+` [data-confirm="branch-push"]') === null`))
	got := onePush(t, fake.host)
	if got["branch"] != "agent/sync-1234" || got["remote"] != "fork" || got["expected_head"] != "head-of-agent/sync-1234" {
		t.Errorf("push body = %v", got)
	}
}

// onePush waits for the one push the fake host should have been asked for.
func onePush(t *testing.T, host *fakeGitHost) map[string]any {
	t.Helper()
	for range 100 {
		if got := host.pushed(); len(got) == 1 {
			return got[0]
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pushes = %v, want exactly one", host.pushed())
	return nil
}
