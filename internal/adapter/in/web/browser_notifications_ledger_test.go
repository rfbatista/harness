package web

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// Gaps the ledger's browser tests leave: every outcome word on the page,
// revision 5 rows beside revision 6, the compact row under 44rem, the fold's
// edges (an open cause between two, a day boundary, no session), filters with
// a tab and a cursor, a failed page load with Reload, and an open cause that
// is read but still counted.
//
// The page reads its rows through GET (where the fake adds the revision 6
// fields) whenever a who or kind filter or the Needs you tab is asked for,
// so these tests ask for one and need no stream push.

// ledgerPush is a branch request from devops's session sess, with its
// revision 6 fields, resolved at resolved (nil: open) with outcome.
func ledgerPush(extra map[string]map[string]any, id, sess string, created time.Time, resolved *time.Time, outcome, object string) *domain.Notification {
	n := &domain.Notification{ID: id, ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromAgent, Kind: domain.NotificationBranchRequest, Level: domain.NotificationAction, Title: "devops wants to push " + object, Link: "/projects/p1", CreatedAt: created, ResolvedAt: resolved}
	fields := map[string]any{"actor": map[string]any{"kind": "agent", "name": "devops", "mode": "operate", "session_id": sess}, "verb": "wants to push", "object": map[string]any{"text": object, "code": true}}
	if outcome != "" {
		fields["outcome"] = outcome
	}
	extra[id] = fields
	return n
}

const (
	standings = `[...document.querySelectorAll('main .notification')].map(r => r.querySelector('.when .standing')?.textContent.trim() ?? '').join('|')`
	groupHead = `[...document.querySelectorAll('main .group > .ghead')].map(h => h.textContent.replace(/\s+/g, ' ').trim()).join('|')`
)

// Every outcome the contract lists reads as its word and time on the page, a
// row settled before outcomes were recorded reads Settled, and none of them
// claims to need the person.
func TestNotificationsPageReadsEveryOutcomeAsWords(t *testing.T) {
	outcomes := []string{"approved", "changes_requested", "denied", "expired", "withdrawn", "answered", "dismissed", "acknowledged", "repaired", "resumed", "ended", "done", "reopened", ""}
	words := []string{"Approved", "Changes requested", "Denied", "Expired", "Withdrawn", "Answered", "Dismissed", "Acknowledged", "Repaired", "Resumed", "Ended", "Done", "Reopened", "Settled"}
	extra := map[string]map[string]any{}
	now := time.Now()
	var list []*domain.Notification
	for i, o := range outcomes {
		at := now.Add(-time.Duration(10+i) * time.Minute)
		// Each from its own session, so no two fold into a stack.
		list = append(list, ledgerPush(extra, "o-"+strconv.Itoa(i), "s-"+strconv.Itoa(i), at, &at, o, "agent/x"+strconv.Itoa(i)))
	}
	h := newInboxHarness(t, list)
	h.inbox.extra = extra
	var got string
	var needsYou int
	h.run(t, "page", h.openReady("/notifications?tab=all&actor=devops"), railPoll(fmt.Sprintf(`document.querySelectorAll('main .notification').length === %d`, len(outcomes))), chromedp.Evaluate(standings, &got), chromedp.Evaluate(`document.querySelectorAll('main .notification[data-level=action]:not([data-state=settled])').length`, &needsYou))
	for i, s := range strings.Split(got, "|") {
		if !strings.HasPrefix(s, words[i]) || (words[i] != "Settled" && !strings.HasPrefix(s, words[i]+" ")) {
			t.Errorf("row %d: standing %q, want %q and a time", i, s, words[i])
		}
	}
	if needsYou != 0 || strings.Contains(got, "Needs you") {
		t.Errorf("settled rows claim to need the person: %q (%d open)", got, needsYou)
	}
	h.noErrors(t)
}

// A revision 5 row (no actor, verb or outcome) reads as the harness with its
// title as the what, beside a revision 6 row, on the client-read page.
func TestNotificationsPageReadsRevision5BesideRevision6(t *testing.T) {
	now := time.Now()
	extra := map[string]map[string]any{}
	list := []*domain.Notification{
		ledgerPush(extra, "new", "s1", now.Add(-time.Minute), nil, "", "agent/x → origin/feat/y"),
		{ID: "old", ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromSystem, Kind: domain.NotificationSessionWaiting, Level: domain.NotificationAction, Title: "A session waits for approval", Link: "/projects/p1", CreatedAt: now.Add(-2 * time.Minute)},
	}
	h := newInboxHarness(t, list)
	h.inbox.extra = extra
	const rows = `[...document.querySelectorAll('main .notification')].map(r => r.querySelector('.who .name').textContent.trim() + ': ' + r.querySelector('.what .title').innerText.replace(/\s+/g, ' ').trim()).join('|')`
	var got string
	h.run(t, "page", h.openReady("/notifications?tab=needs"), railPoll(`document.querySelectorAll('main .notification').length === 2`), chromedp.Evaluate(rows, &got))
	if want := "devops: wants to push agent/x → origin/feat/y|harness: A session waits for approval"; got != want {
		t.Errorf("rows:\n got %s\nwant %s", got, want)
	}
	h.noErrors(t)
}

// Under 44rem the row is compact: who and when share the first line and the
// what sits under them. Wide, the three are columns of one line.
func TestNotificationsPageRowIsCompactUnder44rem(t *testing.T) {
	extra := map[string]map[string]any{}
	list := []*domain.Notification{ledgerPush(extra, "p", "s1", time.Now().Add(-time.Minute), nil, "", "agent/x → origin/feat/y")}
	h := newInboxHarness(t, list)
	h.inbox.extra = extra
	// who, what and when: the one line they share, and whether the what is beneath who.
	const layout = `(() => { const r = document.querySelector('main .notification'); const b = (s) => r.querySelector(s).getBoundingClientRect(); const w = b('.who'), x = b('.what'), n = b('.when'); return JSON.stringify({ whoWhenLine: Math.abs(w.top - n.top) < 6, whatBelow: x.top >= w.bottom - 1, whatBeside: Math.abs(x.top - w.top) < 6 && x.left >= w.right - 1, overflow: document.documentElement.scrollWidth > innerWidth }); })()`
	var wide, narrow string
	h.run(t, "wide", chromedp.EmulateViewport(1600, 900), h.openReady("/notifications?tab=needs"), railPoll(`!!document.querySelector('main .notification .who')`), chromedp.Evaluate(layout, &wide))
	if !strings.Contains(wide, `"whatBeside":true`) || !strings.Contains(wide, `"whatBelow":false`) {
		t.Errorf("wide row: %s", wide)
	}
	h.run(t, "narrow", chromedp.EmulateViewport(560, 900), h.openReady("/notifications?tab=needs"), railPoll(`!!document.querySelector('main .notification .who')`), chromedp.Evaluate(layout, &narrow))
	if !strings.Contains(narrow, `"whoWhenLine":true`) || !strings.Contains(narrow, `"whatBelow":true`) || !strings.Contains(narrow, `"overflow":false`) {
		t.Errorf("narrow row: %s", narrow)
	}
	h.noErrors(t)
}

// An open cause between two settled pushes of one session breaks their run;
// settled pushes across midnight, and settled items with no session, never
// fold.
func TestNotificationsPageFoldEdges(t *testing.T) {
	now := time.Now()
	extra := map[string]map[string]any{}
	min := func(m int) *time.Time { t := now.Add(-time.Duration(m) * time.Minute); return &t }
	// Today: a settled, an open, then two settled: the first stands alone, the last two fold.
	list := []*domain.Notification{
		ledgerPush(extra, "a", "s1", *min(10), min(9), "approved", "agent/x"),
		ledgerPush(extra, "open", "s1", *min(20), nil, "", "agent/z"),
		ledgerPush(extra, "b", "s1", *min(30), min(29), "approved", "agent/x"),
		ledgerPush(extra, "c", "s1", *min(40), min(39), "denied", "agent/x"),
	}
	// Across midnight: two settled pushes of another session, one just before and one just after, never in one stack.
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if now.Sub(midnight) > 3*time.Hour { // after midnight must sort below the pushes of the last hour
		after, before := midnight.Add(time.Minute), midnight.Add(-time.Minute)
		list = append(list,
			ledgerPush(extra, "after", "s2", after, &after, "approved", "agent/m"),
			ledgerPush(extra, "before", "s2", before, &before, "approved", "agent/m"))
	}
	// No session: three settled restarts from "restart" never fold.
	for i := range 3 {
		at := now.Add(-time.Duration(50+i) * time.Minute)
		id := "r" + strconv.Itoa(i)
		list = append(list, &domain.Notification{ID: id, ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromSystem, Kind: domain.NotificationSessionsInterrupted, Level: domain.NotificationAction, Title: "restart interrupted 2 sessions", Link: "/projects/p1", CreatedAt: at, ResolvedAt: &at})
		extra[id] = map[string]any{"actor": map[string]any{"kind": "system", "name": "restart"}, "verb": "interrupted 2 sessions", "object": map[string]any{"text": "sessions", "code": false}, "outcome": "resumed"}
	}
	h := newInboxHarness(t, list)
	h.inbox.extra = extra
	// With both kinds on show the page reads its rows through GET: ask for all of one kind.
	const stacks = `[...document.querySelectorAll('main details.stack')].map(d => d.querySelectorAll(':scope > ul > li').length).join(',')`
	var s string
	var rows int
	h.run(t, "pushes", h.openReady("/notifications?tab=all&kind=branch_request"), railPoll(`document.querySelectorAll('main .notification').length >= 4`), chromedp.Evaluate(stacks, &s))
	// Only the two settled pushes after the open cause fold (b and c); a is alone, open is pinned, and the pair across midnight is two rows.
	if s != "2" {
		t.Errorf("stacks of the pushes: %q, want one of 2 (an open cause breaks the run, midnight splits the day)", s)
	}
	var needs string
	h.run(t, "needs", chromedp.Evaluate(`document.querySelectorAll('main .group:first-of-type details.stack').length + '|' + document.querySelectorAll('main .group:first-of-type .notification').length`, &needs))
	if needs != "0|1" {
		t.Errorf("Needs you: stacks|rows %q, want 0|1", needs)
	}
	h.run(t, "restarts", h.openReady("/notifications?tab=all&kind=sessions_interrupted"), railPoll(`document.querySelectorAll('main .notification').length === 3`), chromedp.Evaluate(`document.querySelectorAll('main details.stack').length`, &rows))
	if rows != 0 {
		t.Errorf("settled restarts with no session folded into %d stacks", rows)
	}
	h.noErrors(t)
}

// Settled pushes of one session on two calendar days fold on neither: Today
// and Yesterday each hold their own.
func TestNotificationsPageDoesNotFoldAcrossDays(t *testing.T) {
	now := time.Now()
	extra := map[string]map[string]any{}
	today := now.Add(-time.Minute)
	yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 12, 0, 0, 0, now.Location())
	yesterday2 := yesterday.Add(-time.Hour)
	list := []*domain.Notification{
		ledgerPush(extra, "t", "s1", today, &today, "approved", "agent/x"),
		ledgerPush(extra, "y1", "s1", yesterday, &yesterday, "approved", "agent/x"),
		ledgerPush(extra, "y2", "s1", yesterday2, &yesterday2, "denied", "agent/x"),
	}
	h := newInboxHarness(t, list)
	h.inbox.extra = extra
	var heads, stacks string
	h.run(t, "page", h.openReady("/notifications?tab=all&actor=devops"), railPoll(`document.querySelectorAll('main .notification').length >= 3`), chromedp.Evaluate(groupHead, &heads), chromedp.Evaluate(`[...document.querySelectorAll('main .group')].map(g => g.querySelectorAll('details.stack').length).join(',')`, &stacks))
	// Today holds the one; Yesterday folds its own two.
	if heads != "Today 1|Yesterday 2" || stacks != "0,1" {
		t.Errorf("groups %q, stacks per group %q; want Today 1|Yesterday 2 and 0,1", heads, stacks)
	}
	h.noErrors(t)
}

// The who and kind filters combine with a tab and the cursor: Needs you ·
// devops · branch requests pages through its 50 and holds nothing else.
func TestNotificationsPageFiltersCombineWithTabAndCursor(t *testing.T) {
	now := time.Now()
	extra := map[string]map[string]any{}
	var list []*domain.Notification
	want := 0
	for i := range 260 {
		at := now.Add(-time.Duration(i+1) * time.Minute)
		id := fmt.Sprintf("n%03d", i)
		n := ledgerPush(extra, id, "s-"+id, at, nil, "", "agent/x")
		switch i % 4 {
		case 1: // another agent
			extra[id]["actor"] = map[string]any{"kind": "agent", "name": "go-developer", "mode": "implementing", "session_id": "s-" + id}
		case 2: // another kind
			n.Kind = domain.NotificationServiceRequest
		case 3: // settled
			n.ResolvedAt = &at
			extra[id]["outcome"] = "approved"
		default:
			want++
		}
		list = append(list, n)
	}
	h := newInboxHarness(t, list)
	h.inbox.extra = extra
	const count = `document.querySelectorAll('main .notification').length`
	const strangers = `[...document.querySelectorAll('main .notification')].filter(r => r.dataset.kind !== 'branch_request' || r.querySelector('.who .name').textContent.trim() !== 'devops' || r.dataset.state === 'settled').length`
	var first, last, odd int
	var more bool
	h.run(t, "first page", h.openReady("/notifications?tab=needs&actor=devops&kind=branch_request"), railPoll(count+` === 50`), chromedp.Evaluate(count, &first), chromedp.Evaluate(`!!document.querySelector('main .more')?.checkVisibility()`, &more))
	if first != 50 || !more {
		t.Errorf("first page: %d rows, Load more shown %v", first, more)
	}
	for range 4 {
		var shown bool
		h.run(t, "load more", chromedp.Evaluate(`!!document.querySelector('main button.more')?.checkVisibility()`, &shown))
		if !shown {
			break
		}
		h.run(t, "click", jsClick(`main button.more`), chromedp.Sleep(300*time.Millisecond))
	}
	h.run(t, "all pages", railPoll(fmt.Sprintf(`%s === %d`, count, want)), chromedp.Evaluate(count, &last), chromedp.Evaluate(strangers, &odd))
	if last != want || odd != 0 {
		t.Errorf("after paging: %d rows (want %d), %d rows that are not an open devops branch request", last, want, odd)
	}
	h.noErrors(t)
}

// A page that cannot read its rows says so in plain words, never as a code,
// and Reload reads them again.
func TestNotificationsPageSaysAFailedLoadInPlainWordsAndReloads(t *testing.T) {
	extra := map[string]map[string]any{}
	list := []*domain.Notification{ledgerPush(extra, "p", "s1", time.Now().Add(-time.Minute), nil, "", "agent/x")}
	h := newInboxHarness(t, list)
	h.inbox.extra = extra
	h.inbox.mu.Lock()
	h.inbox.failGet = true
	h.inbox.mu.Unlock()
	const alert = `(document.querySelector('main [role=alert]')?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	var msg string
	var rows int
	h.run(t, "failing", chromedp.Navigate(h.srv.URL+"/notifications?tab=needs"), railPoll(`!!document.querySelector('main [role=alert]')?.checkVisibility()`), chromedp.Evaluate(alert, &msg))
	if msg == "" || strings.Contains(msg, "sql") || strings.Contains(msg, "500") || strings.Contains(msg, "BAD_RESPONSE") || !strings.Contains(msg, "Reload") {
		t.Errorf("the banner reads %q: want plain words and a Reload action", msg)
	}
	h.inbox.mu.Lock()
	h.inbox.failGet = false
	h.inbox.mu.Unlock()
	h.run(t, "reload", jsClick(`main [role=alert] button`), railPoll(`document.querySelectorAll('main .notification').length === 1`), chromedp.Evaluate(`document.querySelectorAll('main .notification').length`, &rows), chromedp.Evaluate(alert, &msg))
	if rows != 1 || msg != "" {
		t.Errorf("after Reload: %d rows, banner %q", rows, msg)
	}
	h.noErrors(t)
}

// Open is not unread: a read open cause still counts on the bell and the
// title; Mark all read takes the unread count to zero and leaves the open one.
func TestNotificationsOpenIsNotUnread(t *testing.T) {
	now := time.Now()
	list := []*domain.Notification{
		{ID: "open", ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromSystem, Kind: domain.NotificationReviewRequested, Level: domain.NotificationAction, Title: "Review requested", Link: "/projects/p1", CreatedAt: now.Add(-time.Minute)},
	}
	for i := range 3 {
		list = append(list, &domain.Notification{ID: "info-" + strconv.Itoa(i), ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromAgent, Kind: domain.NotificationAgent, Level: domain.NotificationInfo, Title: "Note " + strconv.Itoa(i), Link: "/projects/p1", CreatedAt: now.Add(-time.Duration(i+2) * time.Minute)})
	}
	h := newInboxHarness(t, list)
	var badge, title, tabs string
	h.run(t, "ready", h.openReady("/notifications?tab=all"), chromedp.Title(&title), chromedp.Evaluate(bellBadge, &badge), chromedp.Evaluate(`document.querySelector('main .toolbar .sum').textContent.trim()`, &tabs))
	if badge != "1" || !strings.HasPrefix(title, "(1) ") || tabs != "1 need you · 4 unread" {
		t.Errorf("before: badge %q, title %q, summary %q", badge, title, tabs)
	}
	h.run(t, "mark all", jsClick(`main .toolbar button`), railPoll(`document.querySelector('main .toolbar .sum').textContent.trim() === '1 need you · 0 unread'`), chromedp.Title(&title), chromedp.Evaluate(bellBadge, &badge))
	if badge != "1" || !strings.HasPrefix(title, "(1) ") {
		t.Errorf("after Mark all read: badge %q, title %q: reading must not settle a cause", badge, title)
	}
	h.noErrors(t)
}
