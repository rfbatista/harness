package app

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Gap-hunting tests for the notifications ledger (Spec: harness server —
// notifications that say who, what and when; Contract revision 6). They drive
// the same wired server as notifications_ledger_e2e_test.go and use its
// helpers.

// outcomesOf is the contract's table (§1) of the outcomes each kind may end
// with. A kind with none never resolves.
var outcomesOf = map[string][]string{
	"review_requested":     {"approved", "changes_requested", "withdrawn", "ended"},
	"session_waiting":      {"approved", "denied", "ended", "answered", "dismissed"},
	"task_review":          {"done", "reopened", "ended"},
	"session_failed":       {},
	"commit_check_off":     {"repaired", "ended"},
	"branch_request":       {"approved", "denied", "expired"},
	"branch_finding":       {"acknowledged"},
	"service_request":      {"approved", "denied", "expired"},
	"sessions_interrupted": {"resumed", "ended"},
	"agent":                {},
}

// rows is everything the inbox holds, newest first.
func (l *ledger) rows() []jsonObj {
	l.t.Helper()
	list, _ := l.list(url.Values{"limit": {"200"}})
	return list
}

// fire publishes ev and answers the change it put on the stream.
func (l *ledger) fire(ev domain.Event) ports.NotificationChange {
	l.t.Helper()
	l.publish(ev)
	return l.next()
}

// postRaw sends a POST and answers its status and raw body.
func (l *ledger) postRaw(path, body string) (int, string) {
	l.t.Helper()
	resp, err := http.Post(l.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		l.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// wantTable fails for a resolved row whose outcome the table does not allow
// for its kind, a resolved row with none, and a kind that never resolves
// being resolved.
func (l *ledger) wantTable(why string) {
	l.t.Helper()
	for _, n := range l.rows() {
		kind, outcome := str(n, "kind"), str(n, "outcome")
		allowed, known := outcomesOf[kind]
		switch {
		case !known:
			l.t.Errorf("%s: kind %q is not in the contract table", why, kind)
		case n["resolved_at"] == nil && outcome != "":
			l.t.Errorf("%s: open %s %q has an outcome %q", why, kind, str(n, "title"), outcome)
		case n["resolved_at"] != nil && len(allowed) == 0:
			l.t.Errorf("%s: %s %q resolves, but the table says it never does (outcome %q)", why, kind, str(n, "title"), outcome)
		case n["resolved_at"] != nil && !slices.Contains(allowed, outcome):
			l.t.Errorf("%s: %s %q resolved with %q, the table allows %v", why, kind, str(n, "title"), outcome, allowed)
		}
	}
}

// Deleting a task ends every unresolved notification of it, of any kind,
// with the outcome ended, because its cause is gone (contract revision 6, "A
// deleted task ends what it held": on top of the table, and including the
// kinds the table says never resolve).
func TestADeletedTaskEndsEveryNotificationOfIt(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	l.session("arch", "architect-agent", domain.SessionModeArchitect)
	l.session("w", "worker", domain.SessionModeImplement)
	l.session("f", "failing", domain.SessionModeImplement)

	l.fire(domain.ReviewRequestChanged{ReviewID: "rv", TaskID: tid, ProjectID: pid, Subject: "plan", ArchitectSessionID: "arch", State: domain.ReviewPending})
	l.fire(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusReview, BySessionID: "arch"}})
	l.fire(domain.SessionStatusChanged{SessionID: "w", Status: domain.SessionWaitingApproval})
	l.fire(domain.BranchRequestChanged{ProjectID: pid, Request: domain.BranchRequest{ID: "br", SessionID: "w", TaskID: &tid, Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestPending}})
	l.fire(domain.ServiceRequestChanged{ProjectID: pid, Request: domain.ServiceRequest{ID: "sr", ProjectID: pid, SessionID: "w", TaskID: tid, Action: domain.ServiceRequestStop, ServiceName: "api", State: domain.ServiceRequestPending}})
	l.fire(domain.BranchFindingFound{ProjectID: pid, TaskID: tid, Kind: domain.FindingPush, Branch: "x", FoundAt: time.Now()})
	l.fire(domain.SessionEnded{SessionID: "f", Status: domain.SessionFailed})
	if _, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: "w", ProjectID: pid, TaskID: tid, Title: "FYI"}); err != nil {
		t.Fatal(err)
	}
	l.next()
	if got := len(l.rows()); got != 8 {
		t.Fatalf("setup: the inbox holds %d notifications, want 8 (one per kind and cause)", got)
	}

	l.publish(domain.TicketDeleted{TicketID: tid, ProjectID: pid})
	l.next()
	rows := l.rows()
	if len(rows) != 8 {
		t.Fatalf("after the task was deleted the inbox holds %d notifications, want 8", len(rows))
	}
	for _, n := range rows {
		if n["resolved_at"] == nil || str(n, "outcome") != "ended" {
			t.Errorf("after the task was deleted: %s %q has outcome %q (resolved %v), want ended", str(n, "kind"), str(n, "title"), str(n, "outcome"), n["resolved_at"] != nil)
		}
	}
}

// A resolve with no outcome is impossible: the repository refuses an empty
// outcome and leaves the row open (Spec story 7: the resolve operation
// requires it).
func TestAResolveWithNoOutcomeIsRefused(t *testing.T) {
	l := newLedger(t)
	repo := sqlite.NewNotificationRepository(l.db)
	for name, outcome := range map[string]domain.NotificationOutcome{"empty": "", "unknown": "shrugged"} {
		t.Run(name, func(t *testing.T) {
			key := "cause:" + name
			if _, err := repo.Create(&domain.Notification{
				ProjectID: l.project.ID, Source: domain.NotificationFromSystem, Kind: domain.NotificationSessionWaiting,
				Level: domain.NotificationAction, Title: name, Link: "/", CauseKey: key,
			}); err != nil {
				t.Fatal(err)
			}
			_, err := repo.Resolve(key, time.Now(), outcome)
			still := repo.OpenByCause(key)
			if err == nil || still == nil {
				t.Fatalf("Resolve(%q) err = %v, still open = %v: a resolve with outcome %q must fail and leave the row open", outcome, err, still != nil, outcome)
			}
		})
	}
}

// Every agent-actor kind carries the session's mode word, and a session with
// no mode, or one the server does not know, reads "session". A session whose
// agent has no name reads "agent", never an id.
func TestEveryAgentKindCarriesTheModeWord(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	cases := []struct {
		agent string
		mode  domain.SessionMode
		word  string
	}{
		{"a-none", domain.SessionMode(""), "session"},
		{"a-qa", domain.SessionModeQA, "QA"},
		{"a-ds", domain.SessionModeDesignSystem, "design system"},
		{"a-odd", domain.SessionMode("bogus"), "session"},
		{"", domain.SessionModeTest, "test"},
	}
	for i, c := range cases {
		sid := fmt.Sprintf("sess-%d", i)
		wantName := c.agent
		if wantName == "" {
			wantName = "agent"
		}
		l.session(sid, c.agent, c.mode)
		var changes []*domain.Notification
		add := func(ch ports.NotificationChange) { changes = append(changes, ch.Changed...) }
		add(l.fire(domain.SessionStatusChanged{SessionID: sid, Status: domain.SessionWaitingApproval}))
		l.fire(domain.SessionStatusChanged{SessionID: sid, Status: domain.SessionThinking})
		add(l.fire(domain.BranchRequestChanged{ProjectID: pid, Request: domain.BranchRequest{ID: "br" + sid, SessionID: sid, TaskID: &tid, Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestPending}}))
		add(l.fire(domain.ServiceRequestChanged{ProjectID: pid, Request: domain.ServiceRequest{ID: "sr" + sid, ProjectID: pid, SessionID: sid, TaskID: tid, Action: domain.ServiceRequestStart, ServiceName: "api", Worktree: "main", State: domain.ServiceRequestPending}}))
		add(l.fire(domain.ReviewRequestChanged{ReviewID: "rv" + sid, TaskID: tid, ProjectID: pid, Subject: "plan", ArchitectSessionID: sid, State: domain.ReviewPending}))
		add(l.fire(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusReview, BySessionID: sid}}))
		l.fire(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusInProgress}})
		add(l.fire(domain.InterviewOpened{InterviewID: "iv" + sid, SessionID: sid, Title: "scope", Questions: 2}))
		posted, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: sid, ProjectID: pid, TaskID: tid, Title: "Done " + sid})
		if err != nil {
			t.Fatal(err)
		}
		l.next()
		changes = append(changes, posted)
		add(l.fire(domain.SessionEnded{SessionID: sid, Status: domain.SessionFailed}))

		kinds := map[domain.NotificationKind]bool{}
		for _, n := range changes {
			kinds[n.Kind] = true
			a := n.Actor
			if a.Kind != domain.ActorAgent || a.Name != wantName || a.Mode != c.word || a.SessionID != sid {
				t.Errorf("session %q (mode %q): %s actor = %+v, want agent %q in mode %q", sid, c.mode, n.Kind, a, wantName, c.word)
			}
		}
		if len(kinds) != 7 {
			t.Errorf("session %q: reached %d agent kinds, want 7 (%v)", sid, len(kinds), kinds)
		}
	}
	// The API says the same for every one of those rows.
	for _, n := range l.rows() {
		actor := obj(n, "actor")
		if str(actor, "kind") != "agent" {
			continue
		}
		var want string
		for i, c := range cases {
			if str(actor, "session_id") == fmt.Sprintf("sess-%d", i) {
				want = c.word
			}
		}
		if str(actor, "mode") != want || want == "" {
			t.Errorf("wire: %s actor = %v, want mode %q", str(n, "kind"), actor, want)
		}
	}
}

// openCounts is the open count of every surface the bell can read: the list
// answer (with a filter that hides everything), the first-paint port, and the
// last stream message.
func (l *ledger) openCounts(step string, want int, stream *ports.NotificationChange) {
	l.t.Helper()
	_, page := l.list(url.Values{"kind": {"agent"}, "limit": {"1"}})
	if got := count(page, "open_count"); got != want {
		l.t.Errorf("%s: GET open_count = %d, want %d", step, got, want)
	}
	_, page = l.list(nil)
	if got := count(page, "open_count"); got != want {
		l.t.Errorf("%s: GET open_count = %d, want %d", step, got, want)
	}
	first, err := l.notes.ListNotifications(l.ctx, ports.NotificationQuery{})
	if err != nil {
		l.t.Fatal(err)
	}
	if first.OpenCount != want {
		l.t.Errorf("%s: first paint OpenCount = %d, want %d", step, first.OpenCount, want)
	}
	if stream != nil && stream.OpenCount != want {
		l.t.Errorf("%s: stream open_count = %d, want %d", step, stream.OpenCount, want)
	}
	open, _ := l.list(url.Values{"open": {"true"}, "limit": {"200"}})
	if len(open) != want {
		l.t.Errorf("%s: open=true lists %d rows, want %d", step, len(open), want)
	}
}

// open_count is right on every surface, through acknowledge, resolve, read,
// an info notification and a restart, and it is kept by a restart.
func TestOpenCountIsRightOnEverySurface(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	l.session("s1", "devops", domain.SessionModeOperate)

	c := l.fire(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionWaitingApproval})
	approval := c.Changed[0].ID
	l.openCounts("an approval", 1, &c)
	c = l.fire(domain.BranchFindingFound{ProjectID: pid, TaskID: tid, Kind: domain.FindingPush, Branch: "x", FoundAt: time.Now()})
	finding := c.Changed[0].ID
	l.openCounts("a finding", 2, &c)
	c = l.fire(domain.ReviewRequestChanged{ReviewID: "rv", TaskID: tid, ProjectID: pid, Subject: "plan", State: domain.ReviewPending})
	l.openCounts("a review", 3, &c)
	if _, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: "s1", ProjectID: pid, TaskID: tid, Title: "FYI"}); err != nil {
		t.Fatal(err)
	}
	c = l.next()
	l.openCounts("an info notification", 3, &c)

	// POST read, by id and for all, answers the global count and leaves it.
	status, body := l.postRaw("/api/notifications/read", `{"ids":["`+approval+`"]}`)
	if status != http.StatusOK || !strings.Contains(body, `"open_count":3`) {
		t.Fatalf("read by id = %d %s, want open_count 3", status, body)
	}
	c = l.next()
	l.openCounts("after reading one", 3, &c)
	status, body = l.postRaw("/api/notifications/read", `{"all":true}`)
	if status != http.StatusOK || !strings.Contains(body, `"open_count":3`) {
		t.Fatalf("read all = %d %s, want open_count 3", status, body)
	}
	c = l.next()
	l.openCounts("after reading all", 3, &c)

	// Acknowledge lowers it, and the answer for a repeat does not.
	if status, body := l.postRaw("/api/notifications/"+finding+"/acknowledge", "{}"); status != http.StatusOK {
		t.Fatalf("acknowledge = %d %s", status, body)
	}
	c = l.next()
	l.openCounts("after acknowledge", 2, &c)
	l.postRaw("/api/notifications/"+finding+"/acknowledge", "{}")
	l.openCounts("after a second acknowledge", 2, nil)

	// A resolve lowers it, whether the row was read or not.
	c = l.fire(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionThinking})
	l.openCounts("after the approval is granted", 1, &c)
	c = l.fire(domain.ReviewRequestChanged{ReviewID: "rv", TaskID: tid, ProjectID: pid, Subject: "plan", State: domain.ReviewChangesRequested})
	l.openCounts("after the review is answered", 0, &c)

	// A new cause after everything settled counts again, and a restart keeps the count.
	c = l.fire(domain.BranchFindingFound{ProjectID: pid, TaskID: tid, Kind: domain.FindingPush, Branch: "y", FoundAt: time.Now()})
	l.openCounts("a new finding", 1, &c)
	l.reboot()
	l.openCounts("after a restart", 1, nil)
}

// Every filter combines with every other and with the cursor: paging through
// any combination, at any page size, answers exactly the rows the unfiltered
// list holds that match, in the same order, each once, even when many rows
// share a millisecond.
func TestFiltersCombineWithTheCursorAcrossPages(t *testing.T) {
	l := newLedger(t)
	repo := sqlite.NewNotificationRepository(l.db)
	other, err := l.cat.Projects.CreateProject(l.ctx, "tui", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	actors := []domain.NotificationActor{
		{Kind: domain.ActorAgent, Name: "devops", Mode: "operate", SessionID: "x1"},
		{Kind: domain.ActorAgent, Name: "frontend", Mode: "implementing", SessionID: "x2"},
		{Kind: domain.ActorSystem, Name: "branch watch"},
	}
	kinds := []domain.NotificationKind{domain.NotificationBranchRequest, domain.NotificationBranchFinding, domain.NotificationAgent}
	for i := 0; i < 36; i++ {
		n := &domain.Notification{
			ProjectID: []string{l.project.ID, other.ID}[i%2], Source: domain.NotificationFromSystem,
			Kind: kinds[i%3], Level: domain.NotificationAction, Actor: actors[(i/3)%3],
			Title: fmt.Sprintf("n%02d", i), Link: "/",
			// Three groups of twelve share one millisecond each.
			CreatedAt: base.Add(time.Duration(i/12) * time.Millisecond),
		}
		if i%5 == 0 {
			n.Level = domain.NotificationInfo
		}
		if i%4 == 0 {
			at := base.Add(time.Minute)
			n.ResolvedAt, n.Outcome = &at, domain.OutcomeApproved
		}
		if i%3 == 0 {
			at := base.Add(time.Minute)
			n.ReadAt = &at
		}
		if _, err := repo.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := l.list(url.Values{"limit": {"200"}})
	if len(all) != 36 {
		t.Fatalf("setup: %d rows, want 36", len(all))
	}

	type combo struct {
		name  string
		query url.Values
		keep  func(jsonObj) bool
	}
	// Unread is not read and not settled (revision 5).
	unread := func(n jsonObj) bool { return n["read_at"] == nil && n["resolved_at"] == nil }
	isOpen := func(n jsonObj) bool { return str(n, "level") == "action" && n["resolved_at"] == nil }
	combos := []combo{
		{"open", url.Values{"open": {"true"}}, isOpen},
		{"not open", url.Values{"open": {"false"}}, func(n jsonObj) bool { return !isOpen(n) }},
		{"actor", url.Values{"actor": {"devops"}}, func(n jsonObj) bool { return str(obj(n, "actor"), "name") == "devops" }},
		{"kind", url.Values{"kind": {"branch_finding"}}, func(n jsonObj) bool { return str(n, "kind") == "branch_finding" }},
		{"project", url.Values{"project_id": {other.ID}}, func(n jsonObj) bool { return str(n, "project_id") == other.ID }},
		{"unread", url.Values{"unread": {"true"}}, func(n jsonObj) bool { return unread(n) }},
		{"open actor", url.Values{"open": {"true"}, "actor": {"frontend"}}, func(n jsonObj) bool {
			return isOpen(n) && str(obj(n, "actor"), "name") == "frontend"
		}},
		{"open kind project", url.Values{"open": {"true"}, "kind": {"branch_request"}, "project_id": {l.project.ID}}, func(n jsonObj) bool {
			return isOpen(n) && str(n, "kind") == "branch_request" && str(n, "project_id") == l.project.ID
		}},
		{"everything", url.Values{"open": {"true"}, "actor": {"branch watch"}, "kind": {"branch_finding"}, "project_id": {other.ID}, "unread": {"true"}}, func(n jsonObj) bool {
			return isOpen(n) && str(obj(n, "actor"), "name") == "branch watch" && str(n, "kind") == "branch_finding" && str(n, "project_id") == other.ID && unread(n)
		}},
		{"actor kind unread", url.Values{"actor": {"devops"}, "kind": {"agent"}, "unread": {"true"}}, func(n jsonObj) bool {
			return str(obj(n, "actor"), "name") == "devops" && str(n, "kind") == "agent" && unread(n)
		}},
	}
	for _, c := range combos {
		var want []string
		for _, n := range all {
			if c.keep(n) {
				want = append(want, str(n, "id"))
			}
		}
		if len(want) == 0 {
			t.Fatalf("%s: setup holds no matching row", c.name)
		}
		for _, size := range []int{1, 2, 3, 7, 50} {
			var got []string
			before := ""
			for pages := 0; pages < 100; pages++ {
				q := url.Values{}
				for k, v := range c.query {
					q[k] = v
				}
				q.Set("limit", fmt.Sprint(size))
				if before != "" {
					q.Set("before", before)
				}
				list, page := l.list(q)
				got = append(got, ids(list)...)
				if before = str(page, "next_before"); before == "" {
					break
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("%s at page size %d: got %d rows %v, want %d rows %v", c.name, size, len(got), got, len(want), want)
			}
		}
	}
}

// Acknowledge answers INVALID_INPUT (400) for every kind but a branch finding
// and leaves that notification as it was, and NOTIFICATION_NOT_FOUND (404)
// for an id nobody holds.
func TestAcknowledgeRefusesEveryOtherKindAndUnknownIds(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	l.session("s1", "devops", domain.SessionModeOperate)
	l.session("f", "failing", domain.SessionModeImplement)
	l.fire(domain.ReviewRequestChanged{ReviewID: "rv", TaskID: tid, ProjectID: pid, Subject: "plan", State: domain.ReviewPending})
	l.fire(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionWaitingApproval})
	l.fire(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusReview, BySessionID: "s1"}})
	l.fire(domain.BranchRequestChanged{ProjectID: pid, Request: domain.BranchRequest{ID: "br", SessionID: "s1", TaskID: &tid, Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestPending}})
	l.fire(domain.ServiceRequestChanged{ProjectID: pid, Request: domain.ServiceRequest{ID: "sr", ProjectID: pid, SessionID: "s1", TaskID: tid, Action: domain.ServiceRequestStop, ServiceName: "api", State: domain.ServiceRequestPending}})
	l.fire(domain.SessionEnded{SessionID: "f", Status: domain.SessionFailed})
	if _, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: "s1", ProjectID: pid, TaskID: tid, Title: "FYI"}); err != nil {
		t.Fatal(err)
	}
	l.next()
	l.off("s1")
	l.next()
	l.session("s2", "backend", domain.SessionModeImplement)
	at := time.Now()
	if err := l.sessions.RecordEvent("s2", ports.SessionEventRecord{Seq: 1, Type: "interrupted_by_restart", Interrupted: &at, Status: domain.SessionStopped}); err != nil {
		t.Fatal(err)
	}
	l.fire(domain.SessionsInterrupted{SessionIDs: []string{"s2"}})

	before := l.rows()
	seen := map[string]bool{}
	for _, n := range before {
		seen[str(n, "kind")] = true
		status, body := l.postRaw("/api/notifications/"+str(n, "id")+"/acknowledge", "{}")
		if status != http.StatusBadRequest || !strings.Contains(body, "INVALID_INPUT") {
			t.Errorf("acknowledge on %s = %d %s, want 400 INVALID_INPUT", str(n, "kind"), status, body)
		}
	}
	if len(seen) < 9 {
		t.Errorf("setup: only %d kinds in the inbox: %v", len(seen), seen)
	}
	after := l.rows()
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("a refused acknowledge changed a notification:\nbefore %v\nafter  %v", before, after)
	}
	_, page := l.list(nil)
	if count(page, "open_count") != 7 {
		t.Errorf("open_count after refusals = %v, want 7", page["open_count"])
	}

	for _, id := range []string{"nope", "00000000-0000-0000-0000-000000000000"} {
		status, body := l.postRaw("/api/notifications/"+id+"/acknowledge", "{}")
		if status != http.StatusNotFound || !strings.Contains(body, "NOTIFICATION_NOT_FOUND") {
			t.Errorf("acknowledge on %q = %d %s, want 404 NOTIFICATION_NOT_FOUND", id, status, body)
		}
	}

	// Nothing reached the stream from the refusals: the next event is the next change.
	c := l.fire(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionThinking})
	if len(c.Changed) != 1 || c.Changed[0].Kind != domain.NotificationSessionWaiting {
		t.Errorf("after the refusals the stream says %+v, want only the approval settling", c.Changed)
	}
}

// A branch finding the person acknowledged stays acknowledged: no later
// event reopens it or changes its outcome, and it keeps its repository name.
func TestAnAcknowledgedFindingIsFinal(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	c := l.fire(domain.BranchFindingFound{ProjectID: pid, TaskID: tid, RepositoryID: l.repo.ID, Kind: domain.FindingPush, Branch: "x", FoundAt: time.Now()})
	id := c.Changed[0].ID
	if status, body := l.postRaw("/api/notifications/"+id+"/acknowledge", "{}"); status != http.StatusOK {
		t.Fatalf("acknowledge = %d %s", status, body)
	}
	l.next()
	l.publish(domain.TicketDeleted{TicketID: tid, ProjectID: pid})
	n := l.only("branch_finding")
	if str(n, "outcome") != "acknowledged" || str(n, "repository_name") != "subqdocs-frontend" {
		t.Fatalf("the acknowledged finding after its task was deleted = %v", n)
	}
	status, body := l.postRaw("/api/notifications/"+id+"/acknowledge", "{}")
	if status != http.StatusOK || !strings.Contains(body, `"outcome":"acknowledged"`) {
		t.Fatalf("a repeat acknowledge = %d %s", status, body)
	}
}

var uuidPattern = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// No title, body, verb or object holds a UUID, for any kind, when every id
// involved is one: sessions, reviews, requests, interviews, the task and the
// project.
func TestNoTitleOrBodyHoldsAnId(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	const (
		worker  = "5f3a9c1e-1111-4222-8333-444455556666"
		arch    = "5f3a9c1e-2222-4222-8333-444455556666"
		bare    = "5f3a9c1e-3333-4222-8333-444455556666"
		failing = "5f3a9c1e-4444-4222-8333-444455556666"
		gone    = "5f3a9c1e-5555-4222-8333-444455556666"
	)
	l.session(worker, "devops", domain.SessionModeOperate)
	l.session(arch, "software-architect", domain.SessionModeArchitect)
	l.session(bare, "", domain.SessionModeDefault) // an agent with no name
	l.session(failing, "go-developer", domain.SessionModeImplement)
	if err := l.sessions.RecordEvent(worker, ports.SessionEventRecord{Seq: 1, Type: "x", Status: domain.SessionWaitingApproval, Metrics: &ports.SessionMetrics{LastAction: "Bash: go test"}}); err != nil {
		t.Fatal(err)
	}
	// A session with no recorded action, and one that is gone.
	l.fire(domain.SessionStatusChanged{SessionID: bare, Status: domain.SessionWaitingApproval})
	l.fire(domain.SessionStatusChanged{SessionID: worker, Status: domain.SessionWaitingApproval})
	l.fire(domain.InterviewOpened{InterviewID: "5f3a9c1e-6666-4222-8333-444455556666", SessionID: bare, Title: "scope", Questions: 3})
	l.fire(domain.ReviewRequestChanged{ReviewID: "5f3a9c1e-7777-4222-8333-444455556666", TaskID: tid, ProjectID: pid, Subject: "plan", ArchitectSessionID: arch, State: domain.ReviewPending})
	l.fire(domain.ReviewRequestChanged{ReviewID: "5f3a9c1e-7778-4222-8333-444455556666", TaskID: tid, ProjectID: pid, Subject: "plan", ArchitectSessionID: gone, State: domain.ReviewPending})
	l.fire(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusReview, BySessionID: bare}})
	l.fire(domain.SessionEnded{SessionID: failing, Status: domain.SessionFailed})
	rem, to := "feat/y", "agent/z"
	for i, req := range []domain.BranchRequest{
		{Action: domain.BranchPush, Branch: "a", Remote: &domain.BranchRemote{Name: "origin"}, RemoteBranch: &rem},
		{Action: domain.BranchRename, Branch: "a", NewName: &to},
		{Action: domain.BranchDelete, Branch: "a"},
		{Action: domain.BranchRemoteAdd, Remote: &domain.BranchRemote{Name: "up", URL: "https://example.com/x.git"}},
		{Action: domain.BranchPush, Branch: "a"},
	} {
		req.ID, req.SessionID, req.TaskID, req.RepositoryID, req.State = fmt.Sprintf("5f3a9c1e-80%02d-4222-8333-444455556666", i), worker, &tid, l.repo.ID, domain.BranchRequestPending
		l.fire(domain.BranchRequestChanged{ProjectID: pid, Request: req})
	}
	for i, a := range []domain.ServiceRequestAction{domain.ServiceRequestCreate, domain.ServiceRequestUpdate, domain.ServiceRequestStart, domain.ServiceRequestRestart, domain.ServiceRequestStop} {
		l.fire(domain.ServiceRequestChanged{ProjectID: pid, Request: domain.ServiceRequest{
			ID: fmt.Sprintf("5f3a9c1e-90%02d-4222-8333-444455556666", i), ProjectID: pid, SessionID: worker, TaskID: tid,
			Action: a, ServiceID: "5f3a9c1e-a000-4222-8333-444455556666", ServiceName: "api", Worktree: "own", State: domain.ServiceRequestPending,
		}})
	}
	for _, k := range []domain.BranchFindingKind{domain.FindingPush, domain.FindingRemote, domain.FindingDelete, domain.FindingRename} {
		l.fire(domain.BranchFindingFound{ProjectID: pid, TaskID: tid, RepositoryID: l.repo.ID, Kind: k, Branch: "feat/x", NewName: "feat/y", SessionID: worker, Candidates: []string{worker, arch, gone}, FoundAt: time.Now()})
	}
	l.fire(domain.BranchFindingFound{ProjectID: pid, Kind: domain.FindingPush, Branch: "feat/x", SessionID: gone, Candidates: []string{gone}, FoundAt: time.Now().Add(time.Second)})
	for _, id := range []string{worker, arch, bare} {
		l.off(id)
		l.next()
	}
	l.repaired(bare)
	l.next()
	at := time.Now()
	for _, id := range []string{worker, arch} {
		if err := l.sessions.RecordEvent(id, ports.SessionEventRecord{Seq: 9, Type: "interrupted_by_restart", Interrupted: &at, Status: domain.SessionStopped}); err != nil {
			t.Fatal(err)
		}
	}
	l.fire(domain.SessionsInterrupted{SessionIDs: []string{worker, arch}})
	if _, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: worker, ProjectID: pid, TaskID: tid, Title: "Done"}); err != nil {
		t.Fatal(err)
	}
	l.next()

	rows := l.rows()
	if len(rows) < 20 {
		t.Fatalf("setup: only %d notifications", len(rows))
	}
	for _, n := range rows {
		if str(n, "kind") == "agent" {
			continue // the agent's own words
		}
		text := []string{str(n, "title"), str(n, "body"), str(n, "verb"), str(obj(n, "object"), "text")}
		for _, s := range text {
			if uuidPattern.MatchString(s) {
				t.Errorf("%s: a UUID in %q (title %q)", str(n, "kind"), s, str(n, "title"))
			}
		}
		if str(n, "title") == "" || str(n, "link") == "" {
			t.Errorf("%s: title or link empty: %v", str(n, "kind"), n)
		}
		// A system title reads as the actor, then the verb: never empty of either.
		if v := str(n, "verb"); v != "" && !strings.HasPrefix(str(n, "title"), str(obj(n, "actor"), "name")+" "+v) {
			t.Errorf("%s: title %q does not read as actor %q + verb %q", str(n, "kind"), str(n, "title"), str(obj(n, "actor"), "name"), v)
		}
		if str(obj(n, "actor"), "name") == "" {
			t.Errorf("%s: empty actor name", str(n, "kind"))
		}
	}
}

// A repository renamed after the notification still shows its current name,
// and one that is gone shows none rather than its id.
func TestRepositoryNameIsTheCurrentOne(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	l.session("s1", "devops", domain.SessionModeOperate)
	l.fire(domain.BranchRequestChanged{ProjectID: pid, Request: domain.BranchRequest{ID: "br", SessionID: "s1", TaskID: &tid, RepositoryID: l.repo.ID, Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestPending}})
	if got := str(l.only("branch_request"), "repository_name"); got != "subqdocs-frontend" {
		t.Fatalf("repository_name = %q", got)
	}
	l.fire(domain.BranchRequestChanged{ProjectID: pid, Request: domain.BranchRequest{ID: "br2", SessionID: "s1", TaskID: &tid, Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestPending}})
	list, _ := l.list(url.Values{"kind": {"branch_request"}, "limit": {"1"}})
	if _, has := list[0]["repository_name"]; has && str(list[0], "repository_name") != "" {
		t.Fatalf("a request with no repository carries repository_name %q", str(list[0], "repository_name"))
	}
}

// Rows from before the change: no outcome, no actor, a commit check off row
// of the old per-session kind. They read as the contract says and do not
// break the new rules.
func TestRowsFromBeforeTheChange(t *testing.T) {
	l := newLedger(t)
	repo := sqlite.NewNotificationRepository(l.db)
	pid, tid := l.project.ID, l.task.ID
	l.session("ag1", "devops", domain.SessionModeOperate)
	l.session("s1", "backend", domain.SessionModeImplement)
	resolved := time.Now().Add(-time.Hour)
	mk := func(n *domain.Notification) string {
		n.ProjectID, n.Source, n.Link = pid, domain.NotificationFromSystem, "/"
		if n.CreatedAt.IsZero() {
			n.CreatedAt = time.Now().Add(-2 * time.Hour)
		}
		created, err := repo.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	oldSettled := mk(&domain.Notification{Kind: domain.NotificationBranchRequest, Level: domain.NotificationAction, Title: "old approved", ResolvedAt: &resolved, CauseKey: "branch_request:old"})
	oldAgent := mk(&domain.Notification{Kind: domain.NotificationAgent, Level: domain.NotificationInfo, Title: "old agent note", SessionID: "ag1", TaskID: tid})
	oldFinding := mk(&domain.Notification{Kind: domain.NotificationBranchFinding, Level: domain.NotificationAction, Title: "old finding", CauseKey: "branch_finding:r:push:x:t"})
	oldCheck := mk(&domain.Notification{Kind: domain.NotificationCommitCheckOff, Level: domain.NotificationAction, Title: "Commit check off", SessionID: "s1", CauseKey: "commit_check_off:s1"})

	byID := map[string]jsonObj{}
	for _, n := range l.rows() {
		byID[str(n, "id")] = n
	}
	// No outcome is made up for a row resolved before outcomes, and it is not open.
	if n := byID[oldSettled]; n["resolved_at"] == nil || str(n, "outcome") != "" {
		t.Errorf("old settled row = %v, want resolved with no outcome", n)
	}
	// An old row still has a who, from what it holds.
	for id, want := range map[string]string{oldAgent: "devops", oldFinding: "branch watch", oldCheck: "harness"} {
		if got := str(obj(byID[id], "actor"), "name"); got != want {
			t.Errorf("old row %s actor = %q, want %q", str(byID[id], "kind"), got, want)
		}
	}
	_, page := l.list(nil)
	if count(page, "open_count") != 2 { // the old finding and the old commit check
		t.Errorf("open_count = %d, want 2 (old finding and old commit check)", count(page, "open_count"))
	}
	notOpen, _ := l.list(url.Values{"open": {"false"}})
	if !slices.Contains(ids(notOpen), oldSettled) || !slices.Contains(ids(notOpen), oldAgent) {
		t.Errorf("open=false lists %v, want the old settled row and the old agent note", ids(notOpen))
	}

	// The actor filter matches the actor stored with a row, and a row from
	// before actors were stored has none: it shows the actor it is read with,
	// but the filter does not find it (agreed: no backfill). A new row does.
	byActor, _ := l.list(url.Values{"actor": {"devops"}})
	if slices.Contains(ids(byActor), oldAgent) {
		t.Errorf("actor=devops lists the old agent note %v: old rows are not backfilled, the filter must not match them", ids(byActor))
	}

	// An old finding is acknowledged like a new one.
	if status, body := l.postRaw("/api/notifications/"+oldFinding+"/acknowledge", "{}"); status != http.StatusOK || !strings.Contains(body, `"acknowledged"`) {
		t.Errorf("acknowledging an old finding = %d %s", status, body)
	}
	// Acknowledging it did not touch the other rows with no cause of their own.
	if n := l.onlyByID(oldAgent); n["resolved_at"] != nil {
		t.Errorf("acknowledging a finding resolved the old agent note: %v", n)
	}

	// A new commit check off for the project leaves at most one open: the old
	// per-session row is ended by it (Spec story 13).
	l.off("s1")
	l.next()
	open, _ := l.list(url.Values{"kind": {"commit_check_off"}, "open": {"true"}})
	if len(open) > 1 {
		t.Errorf("%d commit check off notifications are open on one project, want at most one: %v", len(open), ids(open))
	}
}

// onlyByID is the row with id.
func (l *ledger) onlyByID(id string) jsonObj {
	l.t.Helper()
	for _, n := range l.rows() {
		if str(n, "id") == id {
			return n
		}
	}
	l.t.Fatalf("no notification %s", id)
	return nil
}

// commit_check_off over several boots: each boot's first session off posts a
// new notification for the project and ends the earlier boot's one, so one is
// open per project, and an ended row keeps what it listed.
func TestCommitCheckOffAcrossThreeBoots(t *testing.T) {
	l := newLedger(t)
	for _, id := range []string{"s1", "s2", "s3"} {
		l.session(id, "agent-"+id, domain.SessionModeImplement)
	}
	pid := l.project.ID
	openChecks := func() []jsonObj {
		list, _ := l.list(url.Values{"kind": {"commit_check_off"}, "open": {"true"}, "project_id": {pid}})
		return list
	}

	l.off("s1")
	l.next()
	l.off("s2")
	l.next()
	first := l.only("commit_check_off")
	if len(listed(first)) != 2 {
		t.Fatalf("boot 1 lists %v", listed(first))
	}

	// Boot 2: a session of the earlier boot is repaired before anything new is found.
	l.reboot()
	l.repaired("s1")
	c := l.next()
	if len(c.Changed) != 1 || c.Changed[0].ID != str(first, "id") || c.Changed[0].ResolvedAt != nil {
		t.Fatalf("boot 2, s1 repaired: %+v, want the first notification again, still open", c.Changed)
	}
	if got := listed(l.only("commit_check_off")); got["s1"] != "repaired" || got["s2"] != "off" {
		t.Fatalf("boot 2 lists %v", got)
	}
	// Now a new one is found: it ends the first and starts afresh.
	l.off("s3")
	c = l.next()
	if len(c.Changed) != 2 {
		t.Fatalf("boot 2, s3 off: %+v, want the first ended and a new one", c.Changed)
	}
	second := openChecks()
	if len(second) != 1 || str(second[0], "id") == str(first, "id") {
		t.Fatalf("after boot 2 open = %v, want exactly one, new", ids(second))
	}
	if got := listed(second[0]); len(got) != 1 || got["s3"] != "off" {
		t.Fatalf("boot 2's notification lists %v, want s3 off", got)
	}
	ended := l.onlyByID(str(first, "id"))
	if str(ended, "outcome") != "ended" || ended["resolved_at"] == nil {
		t.Fatalf("boot 1's notification = %v, want resolved ended", ended)
	}
	if got := listed(ended); len(got) != 2 || got["s1"] != "repaired" {
		t.Fatalf("the ended notification lists %v, want what it listed", got)
	}

	// Boot 3 does it again, and nothing older reopens.
	l.reboot()
	l.off("s1")
	l.next()
	third := openChecks()
	if len(third) != 1 || str(third[0], "id") == str(second[0], "id") {
		t.Fatalf("after boot 3 open = %v, want exactly one, new", ids(third))
	}
	if n := l.onlyByID(str(second[0], "id")); str(n, "outcome") != "ended" {
		t.Fatalf("boot 2's notification = %v, want ended", n)
	}
	// A repair now touches only the open one; the ended ones stay ended.
	l.repaired("s3")
	l.publish(domain.SessionStatusChanged{SessionID: "s3", Status: domain.SessionThinking})
	if n := l.onlyByID(str(second[0], "id")); str(n, "outcome") != "ended" || listed(n)["s3"] != "off" {
		t.Fatalf("a repair in boot 3 changed boot 2's ended notification: %v", n)
	}
	_, page := l.list(nil)
	if count(page, "open_count") != 1 {
		t.Fatalf("open_count = %v, want 1", page["open_count"])
	}
}

// Listed sessions that are deleted end; a notification whose every session is
// deleted ends rather than repaired; a state that changes back resends; a
// repeat or a session it does not list sends nothing.
func TestCommitCheckOffDeletedSessionsAndResends(t *testing.T) {
	l := newLedger(t)
	for _, id := range []string{"s1", "s2", "s3"} {
		l.session(id, "agent-"+id, domain.SessionModeImplement)
	}
	// sentinel is an event that always puts one change on the stream: it shows
	// the events before it sent nothing.
	l.session("probe", "prober", domain.SessionModeImplement)
	sentinel := func(step string) {
		t.Helper()
		c := l.fire(domain.SessionStatusChanged{SessionID: "probe", Status: domain.SessionWaitingApproval})
		if len(c.Changed) != 1 || c.Changed[0].Kind != domain.NotificationSessionWaiting {
			t.Fatalf("%s: the stream says %+v before the probe, want nothing", step, c.Changed)
		}
		l.fire(domain.SessionStatusChanged{SessionID: "probe", Status: domain.SessionThinking})
	}

	l.off("s1")
	l.next()
	l.off("s2")
	l.next()
	id := str(l.only("commit_check_off"), "id")

	// A session the notification does not list, repaired, sends nothing.
	l.repaired("s3")
	sentinel("repairing a session that is not listed")
	// Deleting one that is not listed sends nothing either.
	l.publish(domain.SessionDeleted{SessionID: "s3"})
	sentinel("deleting a session that is not listed")

	// Repaired then off again: back to off, and it resends.
	l.repaired("s1")
	c := l.next()
	if len(c.Changed) != 1 || listedOf(c.Changed[0])["s1"] != domain.CommitCheckRepaired {
		t.Fatalf("s1 repaired: %+v", c.Changed)
	}
	l.off("s1")
	c = l.next()
	if len(c.Changed) != 1 || c.Changed[0].ID != id || listedOf(c.Changed[0])["s1"] != domain.CommitCheckOff || c.Changed[0].ResolvedAt != nil {
		t.Fatalf("s1 off again: %+v, want the same notification with s1 off", c.Changed)
	}
	if got := listed(l.only("commit_check_off")); got["s1"] != "off" || got["s2"] != "off" {
		t.Fatalf("listed = %v", got)
	}

	// Both deleted, none repaired: it ends.
	l.publish(domain.SessionDeleted{SessionID: "s1"})
	c = l.next()
	if len(c.Changed) != 1 || c.Changed[0].ResolvedAt != nil || listedOf(c.Changed[0])["s1"] != domain.CommitCheckEnded {
		t.Fatalf("s1 deleted: %+v, want s1 ended and the notification open", c.Changed)
	}
	l.publish(domain.SessionDeleted{SessionID: "s2"})
	l.settled("commit_check_off", "ended")
	if got := listed(l.only("commit_check_off")); got["s1"] != "ended" || got["s2"] != "ended" {
		t.Fatalf("listed after both are deleted = %v", got)
	}

	// A session that was deleted without an event reads as ended when read.
	l.session("s4", "agent-s4", domain.SessionModeImplement)
	l.session("s5", "agent-s5", domain.SessionModeImplement)
	l.off("s4")
	l.next()
	l.off("s5")
	l.next()
	if err := l.sessions.Delete("s4"); err != nil {
		t.Fatal(err)
	}
	var open jsonObj
	rows, _ := l.list(url.Values{"kind": {"commit_check_off"}, "open": {"true"}})
	open = rows[0]
	if got := listed(open); got["s4"] != "ended" || got["s5"] != "off" {
		t.Fatalf("listed after s4's row is gone = %v, want s4 ended and s5 off", got)
	}
}

// listedOf is the state of each session a commit check notification lists.
func listedOf(n *domain.Notification) map[string]domain.CommitCheckState {
	out := map[string]domain.CommitCheckState{}
	for _, e := range n.ListedSessions {
		out[e.SessionID] = e.State
	}
	return out
}

// The restart's notification ends with "resumed" once none of its sessions is
// marked, and with "ended" when a newer boot supersedes it; commit checks in
// the same boot do not touch it.
func TestSessionsInterruptedEndsWhenSuperseded(t *testing.T) {
	l := newLedger(t)
	l.session("s1", "backend", domain.SessionModeImplement)
	at := time.Now()
	mark := func(seq int64) {
		if err := l.sessions.RecordEvent("s1", ports.SessionEventRecord{Seq: seq, Type: "interrupted_by_restart", Interrupted: &at, Status: domain.SessionStopped}); err != nil {
			t.Fatal(err)
		}
	}
	mark(1)
	c := l.fire(domain.SessionsInterrupted{SessionIDs: []string{"s1"}})
	first := c.Changed[0].ID
	mark(2)
	c = l.fire(domain.SessionsInterrupted{SessionIDs: []string{"s1"}})
	if len(c.Changed) != 2 {
		t.Fatalf("a second boot: %+v, want the first ended and a new one", c.Changed)
	}
	var ended *domain.Notification
	for _, n := range c.Changed {
		if n.ID == first {
			ended = n
		}
	}
	if ended == nil || ended.Outcome != domain.OutcomeEnded || ended.ResolvedAt == nil {
		t.Fatalf("the superseded notification = %+v, want resolved ended", ended)
	}
	// A boot that found none posts nothing and ends nothing.
	l.publish(domain.SessionsInterrupted{})
	l.session("probe", "p", domain.SessionModeImplement)
	if got := l.fire(domain.SessionStatusChanged{SessionID: "probe", Status: domain.SessionWaitingApproval}); got.Changed[0].Kind != domain.NotificationSessionWaiting {
		t.Fatalf("a boot with none interrupted sent %+v", got.Changed)
	}
	open, _ := l.list(url.Values{"kind": {"sessions_interrupted"}, "open": {"true"}})
	if len(open) != 1 {
		t.Fatalf("%d open restart notifications, want 1", len(open))
	}
	l.wantTable("after two boots")
}

// The table holds for the whole inbox after every kind went through every
// ending the server can give it.
func TestEveryEndingIsInTheContractTable(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	l.session("s1", "devops", domain.SessionModeOperate)
	l.session("arch", "software-architect", domain.SessionModeArchitect)

	for _, st := range []domain.ReviewState{domain.ReviewApproved, domain.ReviewChangesRequested, domain.ReviewWithdrawn} {
		ev := domain.ReviewRequestChanged{ReviewID: "rv-" + string(st), TaskID: tid, ProjectID: pid, Subject: "plan", ArchitectSessionID: "arch", State: domain.ReviewPending}
		l.fire(ev)
		ev.State = st
		l.fire(ev)
	}
	// Branch and service requests leave pending for every state the server has.
	for i, st := range []domain.BranchRequestState{domain.BranchRequestApproved, domain.BranchRequestDenied, domain.BranchRequestExpired} {
		req := domain.BranchRequest{ID: fmt.Sprintf("br%d", i), SessionID: "s1", TaskID: &tid, Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestPending}
		l.fire(domain.BranchRequestChanged{ProjectID: pid, Request: req})
		req.State = st
		l.fire(domain.BranchRequestChanged{ProjectID: pid, Request: req})
	}
	for i, st := range []domain.ServiceRequestState{domain.ServiceRequestApproved, domain.ServiceRequestDenied, domain.ServiceRequestExpired} {
		req := domain.ServiceRequest{ID: fmt.Sprintf("sr%d", i), ProjectID: pid, SessionID: "s1", TaskID: tid, Action: domain.ServiceRequestStop, ServiceName: "api", State: domain.ServiceRequestPending}
		l.fire(domain.ServiceRequestChanged{ProjectID: pid, Request: req})
		req.State = st
		l.fire(domain.ServiceRequestChanged{ProjectID: pid, Request: req})
	}
	// Approvals: granted, denied, the session stopping, the session deleted.
	for _, end := range []domain.Event{
		domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionThinking},
		domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionThinking, ApprovalDenied: true},
		domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionStopped},
		domain.SessionEnded{SessionID: "s1", Status: domain.SessionStopped},
		domain.SessionDeleted{SessionID: "s1"},
	} {
		l.fire(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionWaitingApproval})
		l.fire(end)
	}
	// Interviews.
	for i, st := range []domain.InterviewState{domain.InterviewSent, domain.InterviewDismissed, domain.InterviewExpired} {
		id := fmt.Sprintf("iv%d", i)
		l.fire(domain.InterviewOpened{InterviewID: id, SessionID: "arch", Title: "t", Questions: 1})
		l.fire(domain.InterviewClosed{InterviewID: id, SessionID: "arch", State: st})
	}
	// Task review: done, reopened, and the task deleted while in review.
	review := domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusReview, BySessionID: "arch"}
	for _, to := range []domain.TicketStatus{domain.TicketStatusDone, domain.TicketStatusInProgress, domain.TicketStatusTodo} {
		l.fire(domain.TicketStatusChanged{Change: review})
		l.fire(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: to}})
	}
	l.wantTable("after every ending")

	// Every kind that can resolve did, and every one with an outcome the table allows.
	reached := map[string]map[string]bool{}
	for _, n := range l.rows() {
		if n["resolved_at"] == nil {
			continue
		}
		if reached[str(n, "kind")] == nil {
			reached[str(n, "kind")] = map[string]bool{}
		}
		reached[str(n, "kind")][str(n, "outcome")] = true
	}
	for kind, want := range map[string][]string{
		"review_requested": {"approved", "changes_requested", "withdrawn"},
		"session_waiting":  {"approved", "denied", "ended", "answered", "dismissed"},
		"branch_request":   {"approved", "denied", "expired"},
		"service_request":  {"approved", "denied", "expired"},
		"task_review":      {"done", "reopened"},
	} {
		for _, o := range want {
			if !reached[kind][o] {
				t.Errorf("%s never ended with %q: reached %v", kind, o, reached[kind])
			}
		}
	}
}
