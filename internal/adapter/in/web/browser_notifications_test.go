package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The notifications in the browser: the bell's count on the first paint, a
// live arrival, mark read with its echo before the answer, mark all read,
// the inbox page's filters and paging, the phone sheet and the keys.

// fakeInbox is the server's inbox as the web client sees it: the BFF port
// (ports.NotificationReader), GET /api/notifications, POST
// /api/notifications/read and the stream. A read commits, streams its echo,
// then answers; with hold set, the answer waits until release.
type fakeInbox struct {
	mu       sync.Mutex
	list     []*domain.Notification // newest first
	stream   *sseFeed
	failSeed bool
	hold     chan struct{}
	posts    []map[string]any
	next     int
}

func (f *fakeInbox) unread() int {
	n := 0
	for _, x := range f.list {
		if x.Unread() {
			n++
		}
	}
	return n
}

func (f *fakeInbox) page(q ports.NotificationQuery) *ports.NotificationPage {
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	var rest []*domain.Notification
	past := q.Before == ""
	for _, n := range f.list {
		if !past {
			past = n.ID == q.Before
			continue
		}
		if (q.ProjectID == "" || n.ProjectID == q.ProjectID) && (!q.UnreadOnly || n.Unread()) {
			rest = append(rest, n)
		}
	}
	p := &ports.NotificationPage{Notifications: []*domain.Notification{}, UnreadCount: f.unread()}
	if len(rest) > limit {
		p.NextBefore = rest[limit-1].ID
		rest = rest[:limit]
	}
	p.Notifications = append(p.Notifications, rest...)
	return p
}

func (f *fakeInbox) ListNotifications(_ context.Context, q ports.NotificationQuery) (*ports.NotificationPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSeed {
		return nil, fmt.Errorf("the inbox is down")
	}
	return f.page(q), nil
}

// add records a notification and streams it, as the server does.
func (f *fakeInbox) add(t *testing.T, n *domain.Notification) {
	t.Helper()
	f.mu.Lock()
	f.next++
	if n.ID == "" {
		n.ID = "new-" + strconv.Itoa(f.next)
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	f.list = append([]*domain.Notification{n}, f.list...)
	change := ports.NotificationChange{Changed: []*domain.Notification{n}, UnreadCount: f.unread()}
	f.mu.Unlock()
	f.stream.pushRaw(t, change)
}

func (f *fakeInbox) postsSeen() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.posts)
}

func (f *fakeInbox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/notifications":
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		f.mu.Lock()
		page := f.page(ports.NotificationQuery{ProjectID: q.Get("project_id"), UnreadOnly: q.Get("unread") == "true", Before: q.Get("before"), Limit: limit})
		f.mu.Unlock()
		writeJSON(w, page)
	case r.Method == http.MethodPost && r.URL.Path == "/api/notifications/read":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.posts = append(f.posts, body)
		ids, _ := body["ids"].([]any)
		project, _ := body["project_id"].(string)
		now := time.Now()
		changed := []*domain.Notification{}
		for _, n := range f.list {
			want := body["all"] == true && (project == "" || n.ProjectID == project)
			for _, id := range ids {
				want = want || id == n.ID
			}
			if want && n.ReadAt == nil {
				n.ReadAt = &now
				changed = append(changed, n)
			}
		}
		change := ports.NotificationChange{Changed: changed, UnreadCount: f.unread()}
		hold := f.hold
		f.mu.Unlock()
		if len(changed) > 0 {
			b, _ := json.Marshal(change)
			f.stream.mu.Lock()
			for _, c := range f.stream.conns {
				c <- b
			}
			f.stream.mu.Unlock()
		}
		if hold != nil {
			<-hold
		}
		writeJSON(w, map[string]any{"notifications": change.Changed, "unread_count": change.UnreadCount})
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// inboxWorld: two projects; three unread (two need the person), one read,
// one settled.
func inboxWorld() []*domain.Notification {
	now := time.Now()
	ago := func(m int) time.Time { return now.Add(-time.Duration(m) * time.Minute) }
	read := ago(30)
	return []*domain.Notification{
		{ID: "n-review", ProjectID: "p1", ProjectName: "harness", TaskID: "t-rev", TaskTitle: "Improve tasks dashboard ui", Source: domain.NotificationFromSystem, Kind: domain.NotificationReviewRequested, Level: domain.NotificationAction, Title: "Review requested: the dashboard plan", Body: "The architect asks you to look at the plan.", Link: "/projects/p1/tasks/t-rev", CreatedAt: ago(2)},
		{ID: "n-tui", ProjectID: "p2", ProjectName: "tui", Source: domain.NotificationFromAgent, Kind: domain.NotificationAgent, Level: domain.NotificationInfo, Title: "Release notes drafted", Link: "/projects/p2", CreatedAt: ago(5)},
		{ID: "n-wait", ProjectID: "p1", ProjectName: "harness", TaskID: "t-flaky", TaskTitle: "Fix flaky project documents test", Source: domain.NotificationFromSystem, Kind: domain.NotificationSessionWaiting, Level: domain.NotificationAction, Title: "A session waits for approval", Link: "/projects/p1/tasks/t-flaky", CreatedAt: ago(9)},
		{ID: "n-read", ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromSystem, Kind: domain.NotificationSessionFailed, Level: domain.NotificationInfo, Title: "A session failed", Link: "/projects/p1", CreatedAt: ago(40), ReadAt: &read},
		{ID: "n-settled", ProjectID: "p1", ProjectName: "harness", TaskID: "t-cur", TaskTitle: "Redesign sidebar", Source: domain.NotificationFromSystem, Kind: domain.NotificationTaskReview, Level: domain.NotificationAction, Title: "Redesign sidebar is in review", Link: "/projects/p1/tasks/t-cur", CreatedAt: ago(60), ResolvedAt: &read},
	}
}

type inboxHarness struct {
	*railHarness
	inbox *fakeInbox
}

func newInboxHarness(t *testing.T, list []*domain.Notification) *inboxHarness {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	inbox := &fakeInbox{list: list, stream: &sseFeed{}}
	deps := railWorld()
	deps.Projects = fakeProjects{[]*domain.Project{{ID: "p1", Name: "harness"}, {ID: "p2", Name: "tui"}}}
	deps.Notifications = inbox
	mux := http.NewServeMux()
	mux.Handle("/api/events", &sseFeed{})
	mux.Handle("/api/notifications/events", inbox.stream)
	mux.Handle("/api/notifications", inbox)
	mux.Handle("/api/notifications/read", inbox)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(deps, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return &inboxHarness{railHarness: &railHarness{ctx: ctx, srv: srv, feed: inbox.stream, errs: errs}, inbox: inbox}
}

const (
	bellBadge = `(() => { const b = document.querySelector('.topbar .bell .badge'); return b && b.checkVisibility() ? b.textContent.trim() : ''; })()`
	panelRows = `[...document.querySelectorAll('#notifications-panel .notification')].map(a => a.querySelector('.title').textContent.trim() + '|' + a.dataset.state).join('\n')`
	inboxLive = `document.querySelector('.notifications')?.dataset.feed === 'live' && !document.querySelector('main [data-ssr]')`
	pageRows  = `[...document.querySelectorAll('main .notification')].map(a => a.querySelector('.title').textContent.trim()).join('\n')`
)

// openReady loads a page and waits for the bell (and the inbox page, if any)
// to be live and following the stream.
func (h *inboxHarness) openReady(path string) chromedp.Action {
	return chromedp.Tasks{
		chromedp.Navigate(h.srv.URL + path),
		chromedp.Poll(inboxLive, nil, chromedp.WithPollingTimeout(10*time.Second)),
	}
}

// waitURL waits for the page to have navigated to want (a path and query)
// and come alive, reading it again across the navigation.
func (h *inboxHarness) waitURL(t *testing.T, want string) {
	t.Helper()
	var got string
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		var loc string
		var live bool
		if err := chromedp.Run(h.ctx, chromedp.Evaluate(`location.pathname + location.search`, &loc), chromedp.Evaluate(inboxLive, &live)); err == nil {
			if got = loc; got == want && live {
				return
			}
		}
	}
	t.Fatalf("the page is at %q, want %q", got, want)
}

// waitFollowers waits for the fake stream to have n followers: the browser
// sees a stream open before the fake has taken it on. The fake keeps the
// followers of pages gone, so count from before the navigation.
func (h *inboxHarness) waitFollowers(t *testing.T, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if h.inbox.stream.followers() >= n {
			return
		}
	}
	t.Fatalf("the stream has %d followers, want %d", h.inbox.stream.followers(), n)
}

// jsClick clicks without scrolling it into view first (chromedp.Click
// does, and a document scroll light-dismisses the panel, as for any popup).
func jsClick(sel string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`document.querySelector(%q).click(); true`, sel), nil)
}

func (h *inboxHarness) openPanel() chromedp.Action {
	return chromedp.Tasks{
		chromedp.Click(`.topbar .bell`, chromedp.ByQuery),
		railPoll(`document.querySelector('#notifications-panel').matches(':popover-open') && !!document.querySelector('#notifications-panel .notification')`),
	}
}

func TestNotificationsBellIsCountedOnTheFirstPaint(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	var badge, title, label, seed string
	h.run(t, "without script",
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/projects/p1"),
		chromedp.Evaluate(bellBadge, &badge),
		chromedp.Title(&title),
		chromedp.AttributeValue(`.topbar .bell`, "aria-label", &label, nil, chromedp.ByQuery),
		chromedp.AttributeValue(`.topbar .bell`, "href", &seed, nil, chromedp.ByQuery),
		emulation.SetScriptExecutionDisabled(false),
	)
	if badge != "3" || title != "(3) harness · harness" || label != "Notifications, 3 unread" || seed != "/notifications" {
		t.Errorf("first paint: badge %q, title %q, label %q, href %q", badge, title, label, seed)
	}
	h.run(t, "live", h.openReady("/projects/p1"), chromedp.Evaluate(bellBadge, &badge), chromedp.Title(&title))
	if badge != "3" || title != "(3) harness · harness" {
		t.Errorf("after hydration: badge %q, title %q", badge, title)
	}
	h.noErrors(t)
}

func TestNotificationsBellReadsTheCountWhenTheSeedFails(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	h.inbox.failSeed = true
	var badge string
	h.run(t, "no seed", h.openReady("/projects/p1"), railPoll(bellBadge+` === '3'`), chromedp.Evaluate(bellBadge, &badge))
	if badge != "3" {
		t.Errorf("badge %q, want the GET's 3", badge)
	}
	h.noErrors(t)
}

func TestNotificationsArriveLive(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	h.run(t, "open", h.openReady("/projects/p1"), h.openPanel())
	h.inbox.add(t, &domain.Notification{ProjectID: "p2", ProjectName: "tui", Source: domain.NotificationFromSystem, Kind: domain.NotificationReviewRequested, Level: domain.NotificationAction, Title: "Review requested: release plan", Link: "/projects/p2"})
	var rows, title, said string
	h.waitFollowers(t, 1)
	h.run(t, "arrival",
		railPoll(bellBadge+` === '4'`),
		railPoll(`document.title === '(4) harness · harness'`),
		chromedp.Evaluate(panelRows, &rows),
		chromedp.Title(&title),
		railPoll(`document.querySelector('.stream-bar [x-data="streamAnnouncer"]').textContent !== ''`),
		chromedp.Evaluate(`document.querySelector('.stream-bar [x-data="streamAnnouncer"]').textContent`, &said),
	)
	if first := rows[:len("Review requested: release plan|unread")]; first != "Review requested: release plan|unread" {
		t.Errorf("panel rows:\n%s", rows)
	}
	if said != "New in tui: Review requested: release plan" {
		t.Errorf("announced %q", said)
	}
	h.noErrors(t)
}

// The fake streams the echo before it answers the POST: the badge drops
// once, at the click, and never climbs back or drops twice.
func TestNotificationsMarkReadSurvivesItsEchoBeforeTheAnswer(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	h.inbox.hold = make(chan struct{})
	var seen string
	h.run(t, "open", h.openReady("/projects/p1"), h.openPanel(),
		chromedp.Evaluate(`window.__badges = []; new MutationObserver(() => window.__badges.push(`+bellBadge+`)).observe(document.querySelector('.topbar .bell'), { subtree: true, childList: true, characterData: true, attributes: true }); true`, nil),
	)
	h.run(t, "mark read", jsClick(`#notifications-panel button[data-id="n-review"]`))
	h.run(t, "the badge drops", railPoll(bellBadge+` === '2'`))
	h.run(t, "the row reads", railPoll(`document.querySelector('#notifications-panel .notification:has([data-id="n-review"])')?.dataset.state === 'read'`))
	time.Sleep(150 * time.Millisecond) // the echo is in; the answer is held
	close(h.inbox.hold)
	h.inbox.hold = nil
	time.Sleep(150 * time.Millisecond)
	h.run(t, "after", chromedp.Evaluate(`[...new Set(window.__badges)].join(',')`, &seen))
	if seen != "2" {
		t.Errorf("the badge read %q while the read was in flight, want only 2", seen)
	}
	if posts := h.inbox.postsSeen(); len(posts) != 1 || fmt.Sprint(posts[0]["ids"]) != "[n-review]" {
		t.Errorf("posts %v", posts)
	}

	// Opening one marks it read, then goes to its link.
	h.run(t, "open an item", jsClick(`#notifications-panel a[data-id="n-wait"]`))
	h.waitURL(t, "/projects/p1/tasks/t-flaky")
	if posts := h.inbox.postsSeen(); len(posts) != 2 || fmt.Sprint(posts[1]["ids"]) != "[n-wait]" {
		t.Errorf("opening posts %v", posts)
	}
	h.noErrors(t)
}

func TestNotificationsMarkAllRead(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	h.run(t, "panel",
		h.openReady("/projects/p1"), h.openPanel(),
		clickButton("#notifications-panel", "Mark all read"),
		railPoll(bellBadge+` === ''`),
		railPoll(`document.title === 'harness · harness'`),
	)
	if posts := h.inbox.postsSeen(); len(posts) != 1 || posts[0]["all"] != true || posts[0]["project_id"] != nil {
		t.Errorf("posts %v", posts)
	}

	h2 := newInboxHarness(t, inboxWorld())
	var label string
	h2.run(t, "page, one project",
		h2.openReady("/notifications?project=p2"),
		chromedp.Evaluate(`document.querySelector('main .toolbar button').textContent.trim()`, &label),
		chromedp.Click(`main .toolbar button`, chromedp.ByQuery),
		railPoll(bellBadge+` === '2'`),
	)
	if label != "Mark all read in tui" {
		t.Errorf("label %q", label)
	}
	if posts := h2.inbox.postsSeen(); len(posts) != 1 || posts[0]["all"] != true || posts[0]["project_id"] != "p2" {
		t.Errorf("posts %v", posts)
	}
	h.noErrors(t)
	h2.noErrors(t)
}

func TestNotificationsPageFiltersByProjectAndUnread(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	var ssr, all, p2, unread string
	h.run(t, "no script",
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/notifications?project=p1&unread=1"),
		chromedp.Evaluate(pageRows, &ssr),
		emulation.SetScriptExecutionDisabled(false),
	)
	if ssr != "Review requested: the dashboard plan\nA session waits for approval" {
		t.Errorf("server rows:\n%s", ssr)
	}
	h.run(t, "all", h.openReady("/notifications"), chromedp.Evaluate(pageRows, &all))
	if want := "Review requested: the dashboard plan\nRelease notes drafted\nA session waits for approval\nA session failed\nRedesign sidebar is in review"; all != want {
		t.Errorf("all:\n%s", all)
	}
	h.run(t, "pick tui",
		chromedp.SetValue(`#inbox-project`, "p2", chromedp.ByID),
		chromedp.Evaluate(`setTimeout(() => document.querySelector('#inbox-project').dispatchEvent(new Event('change', { bubbles: true }))); true`, nil),
	)
	h.waitURL(t, "/notifications?project=p2&unread=")
	h.run(t, "tui", chromedp.Evaluate(pageRows, &p2))
	if p2 != "Release notes drafted" {
		t.Errorf("tui:\n%s", p2)
	}
	// A live arrival joins only the list it matches. The page that just
	// opened may not be following yet: the change is sent again (the same
	// ids, so applying it twice changes nothing) until it shows.
	other := &domain.Notification{ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromAgent, Kind: domain.NotificationAgent, Level: domain.NotificationInfo, Title: "Not for this list", Link: "/projects/p1"}
	green := &domain.Notification{ProjectID: "p2", ProjectName: "tui", Source: domain.NotificationFromAgent, Kind: domain.NotificationAgent, Level: domain.NotificationInfo, Title: "Build green", Link: "/projects/p2"}
	h.inbox.add(t, other)
	h.inbox.add(t, green)
	var live string
	for range 10 {
		h.run(t, "live", chromedp.Evaluate(pageRows, &live))
		if live == "Build green\nRelease notes drafted" {
			break
		}
		h.inbox.stream.pushRaw(t, ports.NotificationChange{Changed: []*domain.Notification{other, green}, UnreadCount: 4})
		time.Sleep(200 * time.Millisecond)
	}
	if live != "Build green\nRelease notes drafted" {
		t.Errorf("live rows:\n%s", live)
	}
	h.run(t, "unread only", chromedp.Evaluate(`setTimeout(() => document.querySelector('input[name=unread][value="1"]').click()); true`, nil))
	h.waitURL(t, "/notifications?project=p2&unread=1")
	h.run(t, "unread", chromedp.Evaluate(pageRows, &unread))
	if unread != "Build green\nRelease notes drafted" {
		t.Errorf("unread:\n%s", unread)
	}
	h.noErrors(t)
}

func TestNotificationsPageLoadsMore(t *testing.T) {
	var list []*domain.Notification
	now := time.Now()
	for i := range 60 {
		list = append(list, &domain.Notification{ID: fmt.Sprintf("n%02d", i), ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromAgent, Kind: domain.NotificationAgent, Level: domain.NotificationInfo, Title: fmt.Sprintf("Note %02d", i), Link: "/projects/p1", CreatedAt: now.Add(-time.Duration(i) * time.Minute)})
	}
	h := newInboxHarness(t, list)
	var count int
	h.run(t, "first page", h.openReady("/notifications"), chromedp.Evaluate(`document.querySelectorAll('main .notification').length`, &count))
	if count != 50 {
		t.Errorf("first page has %d", count)
	}
	h.run(t, "more",
		clickButton("main", "Load more"),
		railPoll(`document.querySelectorAll('main .notification').length === 60`),
		// Load more goes once the last page is in.
		railPoll(`![...document.querySelectorAll('main button')].some(b => b.textContent.trim() === 'Load more' && b.checkVisibility())`),
	)
	h.noErrors(t)
}

func TestNotificationsPanelIsASheetOnPhones(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	var sheet, fits, toggleAndBell bool
	h.run(t, "phone",
		chromedp.EmulateViewport(375, 740),
		h.openReady("/projects/p1"),
		chromedp.Evaluate(`(() => { const t = document.querySelector('.topbar .tasks-toggle').getBoundingClientRect(), b = document.querySelector('.topbar .bell').getBoundingClientRect(); return t.width > 0 && b.width > 0 && Math.abs(t.top - b.top) < 8 && b.right <= innerWidth; })()`, &toggleAndBell),
		h.openPanel(),
		chromedp.Evaluate(`document.querySelector('#notifications-panel').hasAttribute('data-sheet')`, &sheet),
		chromedp.Navigate(h.srv.URL+"/notifications"),
		railPoll(`!document.querySelector('main [data-ssr]')`),
		chromedp.Evaluate(`document.documentElement.scrollWidth <= innerWidth`, &fits),
	)
	if !toggleAndBell || !sheet || !fits {
		t.Errorf("phone: tasks toggle and bell on one line %v, sheet %v, no sideways scroll %v", toggleAndBell, sheet, fits)
	}
	h.noErrors(t)
}

func TestNotificationsPanelFromTheKeyboard(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	var first, second, back, expanded string
	focused := `document.activeElement?.dataset?.id || document.activeElement?.className || ''`
	h.run(t, "keys",
		chromedp.EmulateViewport(1280, 900),
		h.openReady("/projects/p1"),
		chromedp.Focus(`.topbar .bell`, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Enter),
		railPoll(`document.activeElement?.dataset?.id === 'n-review'`),
		chromedp.Evaluate(focused, &first),
		chromedp.KeyEvent(kb.ArrowDown),
		chromedp.Evaluate(focused, &second),
		chromedp.KeyEvent(kb.Escape),
		railPoll(`!document.querySelector('#notifications-panel').matches(':popover-open')`),
		chromedp.Evaluate(focused, &back),
		chromedp.AttributeValue(`.topbar .bell`, "aria-expanded", &expanded, nil, chromedp.ByQuery),
	)
	if first != "n-review" || second != "n-tui" || back != "[ button ] [ bell ]" || expanded != "false" {
		t.Errorf("keys: first %q, then %q, Esc back to %q, expanded %q", first, second, back, expanded)
	}
	h.noErrors(t)
}
