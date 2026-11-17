package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/httpclient"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ledger is the wired server of the notifications ledger (Spec: harness
// server — notifications that say who, what and when): the inbox over its
// HTTP API and its stream, driven by the events each cause publishes.
type ledger struct {
	t        *testing.T
	ctx      context.Context
	cat      catalog.Catalog
	plan     *planning.Service
	channel  *taskchannel.Service
	sessions ports.SessionRepository
	notes    *notifications.Service
	db       *gorm.DB
	repos    ports.RepositoryRepository
	srv      *httptest.Server
	changes  <-chan ports.NotificationChange
	project  *domain.Project
	task     *domain.Ticket
	repo     *domain.Repository
	// dbPath and root are the database and data root every boot shares;
	// stop ends the boot that is running.
	dbPath, root string
	stop         func()
}

// jsonObj is one JSON object as the wire carries it.
type jsonObj = map[string]any

func newLedger(t *testing.T) *ledger {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	l := &ledger{t: t, ctx: ctx, dbPath: filepath.Join(t.TempDir(), "harness.db"), root: t.TempDir()}
	l.boot()
	var err error
	if l.project, err = l.cat.Projects.CreateProject(ctx, "harness", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if l.task, err = l.plan.CreateTicket(ctx, l.project.ID, "Notifications", "", domain.TicketStatusInProgress, ""); err != nil {
		t.Fatal(err)
	}
	if l.repo, err = l.repos.Create(l.project.ID, "subqdocs-frontend", "", "https://github.com/acme/frontend.git", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return l
}

// reboot restarts the server over the same database file, as the harness
// restarts: what was stored stays, and the inbox starts a new boot.
func (l *ledger) reboot() {
	l.t.Helper()
	l.stop()
	l.boot()
}

// boot starts the wired server and follows its stream.
func (l *ledger) boot() {
	t, ctx := l.t, l.ctx
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: l.dbPath, Root: l.root, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&l.cat, &l.plan, &l.channel, &l.sessions, &l.notes, &l.db, &l.repos),
		fx.NopLogger,
	)
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Notifications: l.notes, Settings: l.cat.Settings, Planning: l.plan, TaskChannel: l.channel,
	})))
	l.srv = httptest.NewServer(mux)
	// The stream ends before the server closes, which waits for it.
	follow, stopFollowing := context.WithCancel(ctx)
	var err error
	if l.changes, err = httpclient.NewNotifications(httpclient.New(l.srv.URL)).FollowNotifications(follow); err != nil {
		t.Fatal(err)
	}
	srv := l.srv
	var once sync.Once
	l.stop = func() {
		once.Do(func() {
			stopFollowing()
			srv.Close()
			_ = app.Stop(context.Background())
		})
	}
	t.Cleanup(l.stop)
}

// session stores a session of the named agent in the given mode, on the task.
func (l *ledger) session(id, agentName string, mode domain.SessionMode) {
	l.t.Helper()
	agentID := ""
	if agentName != "" {
		a, err := l.cat.Agents.CreateAgent(l.ctx, agentName, "", "", nil, nil)
		if err != nil {
			l.t.Fatal(err)
		}
		agentID = a.ID
	}
	if _, err := l.sessions.Create(&domain.Session{ID: id, ProjectID: l.project.ID, TicketID: l.task.ID, AgentID: agentID, Mode: mode, Status: domain.SessionRunning}); err != nil {
		l.t.Fatal(err)
	}
}

// publish announces ev. The inbox orders by the millisecond, so each event
// gets a millisecond of its own.
func (l *ledger) publish(ev domain.Event) {
	l.t.Helper()
	time.Sleep(2 * time.Millisecond)
	if err := l.cat.Bus.Publish(l.ctx, ev); err != nil {
		l.t.Fatal(err)
	}
}

// next is the next change on the stream.
func (l *ledger) next() ports.NotificationChange {
	l.t.Helper()
	select {
	case c, ok := <-l.changes:
		if !ok {
			l.t.Fatal("the stream closed")
		}
		return c
	case <-l.ctx.Done():
		l.t.Fatal("no change on the stream")
	}
	return ports.NotificationChange{}
}

// get answers the JSON of GET path.
func (l *ledger) get(path string) jsonObj {
	l.t.Helper()
	resp, err := http.Get(l.srv.URL + path)
	if err != nil {
		l.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		l.t.Fatalf("GET %s = %d %s", path, resp.StatusCode, body)
	}
	var out jsonObj
	if err := json.Unmarshal(body, &out); err != nil {
		l.t.Fatalf("GET %s: %v: %s", path, err, body)
	}
	return out
}

// list is the notifications GET /api/notifications answers for query, newest
// first, with the page around them.
func (l *ledger) list(query url.Values) ([]jsonObj, jsonObj) {
	l.t.Helper()
	page := l.get("/api/notifications?" + query.Encode())
	var out []jsonObj
	for _, n := range page["notifications"].([]any) {
		out = append(out, n.(jsonObj))
	}
	return out, page
}

// only is the one notification of kind the inbox holds.
func (l *ledger) only(kind string) jsonObj {
	l.t.Helper()
	list, _ := l.list(url.Values{"kind": {kind}})
	if kind == "" {
		list, _ = l.list(nil)
	}
	var found []jsonObj
	for _, n := range list {
		if n["kind"] == kind {
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		l.t.Fatalf("want one %s notification, the inbox holds %d: %v", kind, len(found), list)
	}
	return found[0]
}

func str(o jsonObj, key string) string {
	s, _ := o[key].(string)
	return s
}

func obj(o jsonObj, key string) jsonObj {
	m, _ := o[key].(jsonObj)
	return m
}

// Every notification names its actor: the agent of the session that caused
// it with that session's mode word, or the harness's own source (ticket 02).
func TestNotificationsNameTheirActor(t *testing.T) {
	l := newLedger(t)
	modes := []struct {
		mode domain.SessionMode
		word string
	}{
		{domain.SessionModeArchitect, "architect"}, {domain.SessionModeImplement, "implementing"},
		{domain.SessionModeDesign, "design"}, {domain.SessionModeProduct, "product"},
		{domain.SessionModeReview, "review"}, {domain.SessionModeTest, "test"},
		{domain.SessionModeQA, "QA"}, {domain.SessionModeOperate, "operate"},
		{domain.SessionModeDesignSystem, "design system"}, {domain.SessionModeDefault, "session"},
	}
	for i, m := range modes {
		slug := strings.ToLower(strings.ReplaceAll(m.word, " ", "-"))
		id := "s-" + slug
		l.session(id, "agent-"+slug, m.mode)
		l.publish(domain.SessionStatusChanged{SessionID: id, Status: domain.SessionWaitingApproval})
		got := l.next().Changed[0]
		if got.Actor.Kind != domain.ActorAgent || got.Actor.Name != "agent-"+slug || got.Actor.Mode != m.word || got.Actor.SessionID != id {
			t.Fatalf("mode %q: actor = %+v", m.mode, got.Actor)
		}
		// The API says the same.
		list, _ := l.list(url.Values{"limit": {"1"}})
		actor := obj(list[0], "actor")
		if str(actor, "kind") != "agent" || str(actor, "name") != "agent-"+slug || str(actor, "mode") != m.word || str(actor, "session_id") != id {
			t.Fatalf("mode %q (%d): wire actor = %v", m.mode, i, actor)
		}
	}

	// The harness's own sources.
	l.publish(domain.BranchFindingFound{ProjectID: l.project.ID, TaskID: l.task.ID, Kind: domain.FindingPush, Branch: "feat/x", RepositoryID: "r1", FoundAt: time.Now()})
	if a := l.next().Changed[0].Actor; a.Kind != domain.ActorSystem || a.Name != "branch watch" || a.SessionID != "" {
		t.Fatalf("branch finding actor = %+v", a)
	}
	l.publish(domain.SessionsInterrupted{SessionIDs: nil})
	l.session("cc", "backend", domain.SessionModeImplement)
	l.publish(domain.SessionCommitCheckChanged{SessionID: "cc", ProjectID: l.project.ID, TicketID: l.task.ID, Off: true, Reason: "x"})
	if a := l.next().Changed[0].Actor; a.Kind != domain.ActorSystem || a.Name != "harness" {
		t.Fatalf("commit check off actor = %+v", a)
	}

	// A task a session moved to review names that session's agent; one the
	// person moved names the person.
	l.publish(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: l.task.ID, ProjectID: l.project.ID, Status: domain.TicketStatusReview, BySessionID: "s-architect", By: domain.ChangedByArchitect}})
	if a := l.next().Changed[0].Actor; a.Kind != domain.ActorAgent || a.Name != "agent-architect" || a.Mode != "architect" {
		t.Fatalf("task review by a session: actor = %+v", a)
	}
	l.publish(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: l.task.ID, ProjectID: l.project.ID, Status: domain.TicketStatusInProgress}})
	l.next()
	l.publish(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: l.task.ID, ProjectID: l.project.ID, Status: domain.TicketStatusReview, By: domain.ChangedByPerson}})
	if a := l.next().Changed[0].Actor; a.Kind != domain.ActorPerson || a.Name == "" || a.SessionID != "" {
		t.Fatalf("task review by the person: actor = %+v", a)
	}

	// An agent's own notification takes the actor from the posting session.
	posted, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: "s-operate", ProjectID: l.project.ID, TaskID: l.task.ID, Title: "Done"})
	if err != nil {
		t.Fatal(err)
	}
	if a := posted.Actor; a.Kind != domain.ActorAgent || a.Name != "agent-operate" || a.Mode != "operate" || a.SessionID != "s-operate" {
		t.Fatalf("agent notification actor = %+v", a)
	}

	// No actor name is an id, and title, body and link keep their meaning.
	list, _ := l.list(url.Values{"limit": {"200"}})
	for _, n := range list {
		actor := obj(n, "actor")
		if str(actor, "kind") == "" || str(actor, "name") == "" {
			t.Fatalf("a notification has no actor: %v", n)
		}
		if str(n, "title") == "" || str(n, "link") == "" {
			t.Fatalf("title or link lost: %v", n)
		}
	}
}

// outcomeOf is the outcome of the notification the stream just announced as
// settled, after checking the API says the same.
func (l *ledger) settled(kind, want string) {
	l.t.Helper()
	c := l.next()
	var got *domain.Notification
	for _, n := range c.Changed {
		if string(n.Kind) == kind {
			got = n
		}
	}
	if got == nil || got.ResolvedAt == nil || string(got.Outcome) != want {
		l.t.Fatalf("%s: stream change = %+v, want resolved with outcome %q", kind, c.Changed, want)
	}
	wire, _ := l.list(url.Values{"kind": {kind}, "limit": {"200"}})
	for _, n := range wire {
		if str(n, "id") == got.ID {
			if str(n, "outcome") != want || n["resolved_at"] == nil {
				l.t.Fatalf("%s: wire outcome = %v, want %q", kind, n["outcome"], want)
			}
			return
		}
	}
	l.t.Fatalf("%s: the API does not list %s", kind, got.ID)
}

// A resolved notification records how it ended, for every way each kind can
// end (ticket 04, contract revision 6 §1).
func TestNotificationsRecordTheirOutcome(t *testing.T) {
	l := newLedger(t)
	l.session("s1", "frontend", domain.SessionModeImplement)
	l.session("arch", "architect-agent", domain.SessionModeArchitect)
	pid, tid := l.project.ID, l.task.ID

	// Reviews: approved, changes requested, withdrawn.
	for _, state := range []domain.ReviewState{domain.ReviewApproved, domain.ReviewChangesRequested, domain.ReviewWithdrawn} {
		ev := domain.ReviewRequestChanged{ReviewID: "rv-" + string(state), TaskID: tid, ProjectID: pid, Subject: "plan", ArchitectSessionID: "arch", State: domain.ReviewPending}
		l.publish(ev)
		l.next()
		ev.State = state
		l.publish(ev)
		l.settled("review_requested", string(state))
	}
	// A task deleted ends a review that is still open.
	l.publish(domain.ReviewRequestChanged{ReviewID: "rv-open", TaskID: tid, ProjectID: pid, Subject: "plan", ArchitectSessionID: "arch", State: domain.ReviewPending})
	l.next()

	// Approvals: approved, denied, ended by the session ending.
	wait := func() {
		l.publish(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionWaitingApproval})
		l.next()
	}
	wait()
	l.publish(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionThinking})
	l.settled("session_waiting", "approved")
	wait()
	l.publish(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionThinking, ApprovalDenied: true})
	l.settled("session_waiting", "denied")
	wait()
	l.publish(domain.SessionEnded{SessionID: "s1", Status: domain.SessionStopped})
	l.settled("session_waiting", "ended")

	// Interviews: answered, dismissed, and ended when it expires.
	for i, c := range []struct {
		state domain.InterviewState
		want  string
	}{{domain.InterviewSent, "answered"}, {domain.InterviewDismissed, "dismissed"}, {domain.InterviewExpired, "ended"}} {
		id := "iv-" + string(rune('a'+i))
		l.publish(domain.InterviewOpened{InterviewID: id, SessionID: "s1", Title: "scope", Questions: 2})
		l.next()
		l.publish(domain.InterviewClosed{InterviewID: id, SessionID: "s1", State: c.state})
		l.settled("session_waiting", c.want)
	}

	// Branch and service requests: approved, denied, expired.
	for _, c := range []struct {
		state domain.BranchRequestState
		want  string
	}{{domain.BranchRequestApproved, "approved"}, {domain.BranchRequestDenied, "denied"}, {domain.BranchRequestExpired, "expired"}} {
		req := domain.BranchRequest{ID: "br-" + string(c.state), SessionID: "s1", Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestPending}
		l.publish(domain.BranchRequestChanged{Request: req, ProjectID: pid})
		l.next()
		req.State = c.state
		l.publish(domain.BranchRequestChanged{Request: req, ProjectID: pid})
		l.settled("branch_request", c.want)
	}
	for _, c := range []struct {
		state domain.ServiceRequestState
		want  string
	}{{domain.ServiceRequestApproved, "approved"}, {domain.ServiceRequestDenied, "denied"}, {domain.ServiceRequestExpired, "expired"}} {
		req := domain.ServiceRequest{ID: "sr-" + string(c.state), ProjectID: pid, SessionID: "s1", Action: domain.ServiceRequestRestart, ServiceName: "api", State: domain.ServiceRequestPending}
		l.publish(domain.ServiceRequestChanged{Request: req, ProjectID: pid})
		l.next()
		req.State = c.state
		l.publish(domain.ServiceRequestChanged{Request: req, ProjectID: pid})
		l.settled("service_request", c.want)
	}

	// A task in review: done, reopened.
	review := domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusReview, BySessionID: "arch"}
	l.publish(domain.TicketStatusChanged{Change: review})
	l.next()
	l.publish(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusDone}})
	l.settled("task_review", "done")
	l.publish(domain.TicketStatusChanged{Change: review})
	l.next()
	l.publish(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusInProgress}})
	l.settled("task_review", "reopened")

	// A commit check repaired.
	l.publish(domain.SessionCommitCheckChanged{SessionID: "s1", ProjectID: pid, TicketID: tid, Off: true, Reason: "x"})
	l.next()
	l.publish(domain.SessionCommitCheckChanged{SessionID: "s1", ProjectID: pid, TicketID: tid, Off: false})
	l.settled("commit_check_off", "repaired")

	// A restart's interrupted sessions: resumed once none is marked.
	l.session("s2", "backend", domain.SessionModeImplement)
	at := time.Now()
	if err := l.sessions.RecordEvent("s2", ports.SessionEventRecord{Seq: 1, Type: "interrupted_by_restart", Interrupted: &at, Status: domain.SessionStopped}); err != nil {
		t.Fatal(err)
	}
	l.publish(domain.SessionsInterrupted{SessionIDs: []string{"s2"}})
	l.next()
	if err := l.sessions.RecordEvent("s2", ports.SessionEventRecord{Seq: 2, Type: "resumed", ClearInterrupted: true, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	l.publish(domain.SessionStatusChanged{SessionID: "s2", Status: domain.SessionRunning})
	l.settled("sessions_interrupted", "resumed")

	// Deleting the task ends what is still open on it: the open review.
	l.publish(domain.TicketDeleted{TicketID: tid, ProjectID: pid})
	c := l.next()
	if len(c.Changed) == 0 || c.Changed[0].Outcome != domain.OutcomeEnded {
		t.Fatalf("a deleted task: stream change = %+v, want outcome ended", c.Changed)
	}

	// A row resolved before outcomes were recorded has none: it reads as
	// settled. Every notification resolved above has one.
	resolved := time.Now()
	old, err := sqlite.NewNotificationRepository(l.db).Create(&domain.Notification{
		ProjectID: pid, Source: domain.NotificationFromAgent, Kind: domain.NotificationAgent, Level: domain.NotificationAction,
		Title: "from before", Link: "/", ResolvedAt: &resolved,
	})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := l.list(url.Values{"limit": {"200"}})
	for _, n := range list {
		if n["resolved_at"] == nil {
			continue
		}
		if str(n, "id") == old.ID {
			if _, has := n["outcome"]; has {
				t.Fatalf("a row resolved before outcomes has outcome %v", n["outcome"])
			}
		} else if str(n, "outcome") == "" {
			t.Fatalf("a resolved notification has no outcome: %v", n)
		}
	}
}

func count(o jsonObj, key string) int {
	f, _ := o[key].(float64)
	return int(f)
}

// The bell counts open causes (ADR 0096): level action, not resolved, read or
// unread, whatever the filter, on the API, the stream and the read answer
// (ticket 05).
func TestNotificationsCountOpenCauses(t *testing.T) {
	l := newLedger(t)
	l.session("s1", "frontend", domain.SessionModeImplement)
	pid, tid := l.project.ID, l.task.ID

	// An info notification never counts.
	if _, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: "s1", ProjectID: pid, TaskID: tid, Title: "FYI"}); err != nil {
		t.Fatal(err)
	}
	if c := l.next(); c.OpenCount != 0 || c.UnreadCount != 1 {
		t.Fatalf("after an info notification: open %d unread %d, want 0 and 1", c.OpenCount, c.UnreadCount)
	}

	// An action does; so does a second, on another task.
	l.publish(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionWaitingApproval})
	if c := l.next(); c.OpenCount != 1 || c.UnreadCount != 2 {
		t.Fatalf("after an approval: open %d unread %d, want 1 and 2", c.OpenCount, c.UnreadCount)
	}
	l.publish(domain.ReviewRequestChanged{ReviewID: "rv", TaskID: tid, ProjectID: pid, Subject: "plan", State: domain.ReviewPending})
	if c := l.next(); c.OpenCount != 2 {
		t.Fatalf("after a review: open %d, want 2", c.OpenCount)
	}
	_, page := l.list(url.Values{"unread": {"true"}, "project_id": {pid}, "limit": {"1"}})
	if count(page, "open_count") != 2 || count(page, "unread_count") != 3 {
		t.Fatalf("list page = %v, want the global open 2 and unread 3 whatever the filter", page)
	}

	// Reading everything clears unread and leaves open as it was.
	resp, err := http.Post(l.srv.URL+"/api/notifications/read", "application/json", strings.NewReader(`{"all":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var read jsonObj
	if err := json.NewDecoder(resp.Body).Decode(&read); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if count(read, "unread_count") != 0 || count(read, "open_count") != 2 {
		t.Fatalf("read answer = %v, want unread 0 and open 2", read)
	}
	if c := l.next(); c.UnreadCount != 0 || c.OpenCount != 2 {
		t.Fatalf("stream after read: open %d unread %d, want 2 and 0", c.OpenCount, c.UnreadCount)
	}
	_, page = l.list(nil)
	if count(page, "open_count") != 2 {
		t.Fatalf("list after read = %v, want open 2", page)
	}

	// Resolving lowers open.
	l.publish(domain.ReviewRequestChanged{ReviewID: "rv", TaskID: tid, ProjectID: pid, Subject: "plan", State: domain.ReviewApproved})
	if c := l.next(); c.OpenCount != 1 {
		t.Fatalf("after the review is answered: open %d, want 1", c.OpenCount)
	}
}

// wantWhat checks the verb and object of a notification on the wire and that
// its title and body hold no id.
func (l *ledger) wantWhat(n jsonObj, verb, object string, code bool, title string) {
	l.t.Helper()
	got := obj(n, "object")
	if str(n, "verb") != verb || str(got, "text") != object || got["code"] != code || str(n, "title") != title {
		l.t.Fatalf("%s: verb %q object %v title %q\nwant verb %q object %q (code %v) title %q",
			str(n, "kind"), str(n, "verb"), got, str(n, "title"), verb, object, code, title)
	}
	ids := []string{l.project.ID, l.task.ID, "5f3a9c1e-1111-4222-8333-444455556666", l.repo.ID}
	for _, id := range ids {
		if strings.Contains(str(n, "title"), id) || strings.Contains(str(n, "body"), id) {
			l.t.Fatalf("%s: an id (%s) is in the title or body: %v", str(n, "kind"), id, n)
		}
	}
}

// Every system kind carries a verb in the agent's voice and an object, and
// its title reads as actor, verb and object (ticket 03, UX study §3 and §8).
func TestSystemNotificationsReadAsActorVerbAndObject(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	const sid = "5f3a9c1e-1111-4222-8333-444455556666"
	l.session(sid, "frontend-developer", domain.SessionModeImplement)
	l.session("arch", "software-architect", domain.SessionModeArchitect)
	l.session("ops", "devops", domain.SessionModeOperate)

	// review_requested
	l.publish(domain.ReviewRequestChanged{ReviewID: "rv", TaskID: tid, ProjectID: pid, Subject: "the tickets", ArchitectSessionID: "arch", State: domain.ReviewPending})
	l.wantWhat(l.only("review_requested"), "asks you to review", "the tickets", false, "software-architect asks you to review the tickets")

	// session_waiting: approval, then interview
	l.session("s2", "go-developer", domain.SessionModeImplement)
	if err := l.sessions.RecordEvent("s2", ports.SessionEventRecord{Seq: 1, Type: "x", Status: domain.SessionWaitingApproval, Metrics: &ports.SessionMetrics{LastAction: "Bash: go test ./internal/..."}}); err != nil {
		t.Fatal(err)
	}
	l.publish(domain.SessionStatusChanged{SessionID: "s2", Status: domain.SessionWaitingApproval})
	l.wantWhat(l.only("session_waiting"), "waits for your approval to run", "go test ./internal/... (Bash)", true, "go-developer waits for your approval to run go test ./internal/... (Bash)")
	l.publish(domain.InterviewOpened{InterviewID: "iv", SessionID: "arch", Title: "the scope of the first release", Questions: 5})
	list, _ := l.list(url.Values{"kind": {"session_waiting"}, "limit": {"1"}})
	l.wantWhat(list[0], "has 5 questions for you about", "the scope of the first release", false, "software-architect has 5 questions for you about the scope of the first release")
	l.publish(domain.InterviewOpened{InterviewID: "iv1", SessionID: "arch", Title: "naming", Questions: 1})
	list, _ = l.list(url.Values{"kind": {"session_waiting"}, "limit": {"1"}})
	l.wantWhat(list[0], "has 1 question for you about", "naming", false, "software-architect has 1 question for you about naming")

	// task_review
	l.publish(domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tid, ProjectID: pid, Status: domain.TicketStatusReview, BySessionID: "arch", Reason: "PRD written"}})
	n := l.only("task_review")
	l.wantWhat(n, "moved the task to review", "Notifications", false, "software-architect moved the task to review: Notifications")
	if str(n, "body") != "PRD written" {
		t.Fatalf("task_review body = %q", str(n, "body"))
	}

	// session_failed
	if err := l.sessions.RecordEvent(sid, ports.SessionEventRecord{Seq: 1, Type: "x", Status: domain.SessionFailed, Metrics: &ports.SessionMetrics{LastAction: "Bash: npm test"}}); err != nil {
		t.Fatal(err)
	}
	l.publish(domain.SessionEnded{SessionID: sid, Status: domain.SessionFailed})
	l.wantWhat(l.only("session_failed"), "failed while running", "npm test (Bash)", true, "frontend-developer failed while running npm test (Bash)")

	// branch_request: push, with the repository's name
	req := domain.BranchRequest{ID: "br", SessionID: sid, TaskID: &tid, RepositoryID: l.repo.ID, Action: domain.BranchPush, Branch: "agent/x", Remote: &domain.BranchRemote{Name: "origin"}, State: domain.BranchRequestPending, Reason: "PR branch"}
	rb := "feat/y"
	req.RemoteBranch = &rb
	l.publish(domain.BranchRequestChanged{Request: req, ProjectID: pid})
	n = l.only("branch_request")
	l.wantWhat(n, "wants to push", "agent/x → origin/feat/y", true, "frontend-developer wants to push agent/x → origin/feat/y")
	if str(n, "repository_name") != "subqdocs-frontend" || str(n, "body") != "PR branch" {
		t.Fatalf("branch_request repository %q body %q", str(n, "repository_name"), str(n, "body"))
	}
	del := req
	del.ID, del.Action, del.Remote, del.RemoteBranch = "br2", domain.BranchDelete, nil, nil
	l.publish(domain.BranchRequestChanged{Request: del, ProjectID: pid})
	list, _ = l.list(url.Values{"kind": {"branch_request"}, "limit": {"1"}})
	l.wantWhat(list[0], "wants to delete", "agent/x", true, "frontend-developer wants to delete agent/x")
	ren := req
	ren.ID, ren.Action, ren.Remote, ren.RemoteBranch = "br3", domain.BranchRename, nil, nil
	to := "agent/z"
	ren.NewName = &to
	l.publish(domain.BranchRequestChanged{Request: ren, ProjectID: pid})
	list, _ = l.list(url.Values{"kind": {"branch_request"}, "limit": {"1"}})
	l.wantWhat(list[0], "wants to rename", "agent/x → agent/z", true, "frontend-developer wants to rename agent/x → agent/z")

	// branch_finding
	l.publish(domain.BranchFindingFound{ProjectID: pid, TaskID: tid, RepositoryID: l.repo.ID, Kind: domain.FindingPush, Branch: "feat/refunds", Candidates: []string{sid, "arch"}, FoundAt: time.Now()})
	n = l.only("branch_finding")
	l.wantWhat(n, "found a push with no approved request on", "feat/refunds", true, "branch watch found a push with no approved request on feat/refunds")
	if str(n, "repository_name") != "subqdocs-frontend" || str(n, "body") != "Turns of frontend-developer and software-architect overlapped it." {
		t.Fatalf("branch_finding repository %q body %q", str(n, "repository_name"), str(n, "body"))
	}

	// service_request, on its ref
	sreq := domain.ServiceRequest{ID: "sr", ProjectID: pid, SessionID: "ops", TaskID: tid, Action: domain.ServiceRequestRestart, ServiceName: "api", Worktree: "main", State: domain.ServiceRequestPending, Reason: "pick up the fix"}
	l.publish(domain.ServiceRequestChanged{Request: sreq, ProjectID: pid})
	n = l.only("service_request")
	l.wantWhat(n, "wants to restart", "api on main", true, "devops wants to restart api on main")
	if str(n, "body") != "pick up the fix" {
		t.Fatalf("service_request body = %q", str(n, "body"))
	}
	sreq.ID, sreq.Action, sreq.Worktree, sreq.Branch = "sr2", domain.ServiceRequestStart, "branch:origin/feat/x", &domain.ServiceRequestBranch{Name: "origin/feat/x", Remote: true}
	l.publish(domain.ServiceRequestChanged{Request: sreq, ProjectID: pid})
	list, _ = l.list(url.Values{"kind": {"service_request"}, "limit": {"1"}})
	l.wantWhat(list[0], "wants to start", "api on origin/feat/x", true, "devops wants to start api on origin/feat/x")

	// commit_check_off
	l.publish(domain.SessionCommitCheckChanged{SessionID: sid, ProjectID: pid, TicketID: tid, Off: true, Reason: "the repair failed"})
	l.wantWhat(l.only("commit_check_off"), "could not guard 1 session", "commit checks off", false, "harness could not guard 1 session: commit checks off")

	// sessions_interrupted
	at := time.Now()
	for _, id := range []string{sid, "arch", "ops"} {
		if err := l.sessions.RecordEvent(id, ports.SessionEventRecord{Seq: 9, Type: "interrupted_by_restart", Interrupted: &at, Status: domain.SessionStopped}); err != nil {
			t.Fatal(err)
		}
	}
	l.publish(domain.SessionsInterrupted{SessionIDs: []string{sid, "arch", "ops"}})
	l.wantWhat(l.only("sessions_interrupted"), "interrupted 3 sessions", "sessions", false, "restart interrupted 3 sessions")

	// An agent's own notification keeps its title and has no verb or object.
	posted, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: "arch", ProjectID: pid, TaskID: tid, Title: "One merge left"})
	if err != nil {
		t.Fatal(err)
	}
	if posted.Verb != "" || posted.Object != nil || posted.Title != "One merge left" {
		t.Fatalf("agent notification = %+v", posted)
	}
}

// ids lists the ids of a list answer.
func ids(list []jsonObj) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		out = append(out, str(n, "id"))
	}
	return out
}

// The list filters by open causes, actor name and kind, alone or together,
// with the project and unread filters and the cursor (ticket 06).
func TestNotificationsFilterByOpenActorAndKind(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	l.session("a1", "devops", domain.SessionModeOperate)
	l.session("a2", "frontend", domain.SessionModeImplement)
	// The inbox orders by the millisecond: space the notifications out.
	post := func(ev domain.Event) string {
		time.Sleep(3 * time.Millisecond)
		l.publish(ev)
		return l.next().Changed[0].ID
	}
	push := func(id, session string) string {
		return post(domain.BranchRequestChanged{ProjectID: pid, Request: domain.BranchRequest{ID: id, SessionID: session, TaskID: &tid, Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestPending}})
	}
	p1 := push("p1", "a1")
	p2 := push("p2", "a2")
	p3 := push("p3", "a1")
	finding := post(domain.BranchFindingFound{ProjectID: pid, TaskID: tid, Kind: domain.FindingPush, Branch: "x", FoundAt: time.Now()})
	// p1 is settled, and an info notification from a1 is not an open cause.
	l.publish(domain.BranchRequestChanged{ProjectID: pid, Request: domain.BranchRequest{ID: "p1", SessionID: "a1", TaskID: &tid, Action: domain.BranchPush, Branch: "b", State: domain.BranchRequestApproved}})
	l.next()
	time.Sleep(3 * time.Millisecond)
	posted, err := l.notes.Notify(l.ctx, ports.NotifyRequest{SessionID: "a1", ProjectID: pid, TaskID: tid, Title: "FYI"})
	if err != nil {
		t.Fatal(err)
	}
	l.next()

	want := func(name string, q url.Values, expect ...string) {
		t.Helper()
		got, _ := l.list(q)
		if g := ids(got); !slices.Equal(g, expect) {
			t.Fatalf("%s: got %v, want %v", name, g, expect)
		}
	}
	want("open", url.Values{"open": {"true"}}, finding, p3, p2)
	want("not open", url.Values{"open": {"false"}}, posted.ID, p1)
	want("actor", url.Values{"actor": {"devops"}}, posted.ID, p3, p1)
	want("a system actor", url.Values{"actor": {"branch watch"}}, finding)
	want("an unknown actor", url.Values{"actor": {"nobody"}})
	want("kind", url.Values{"kind": {"branch_request"}}, p3, p2, p1)
	want("open and actor", url.Values{"open": {"true"}, "actor": {"devops"}}, p3)
	want("open and kind", url.Values{"open": {"true"}, "kind": {"branch_request"}}, p3, p2)
	want("actor and kind", url.Values{"actor": {"devops"}, "kind": {"branch_request"}}, p3, p1)
	want("with the project", url.Values{"open": {"true"}, "project_id": {pid}}, finding, p3, p2)
	want("with another project", url.Values{"open": {"true"}, "project_id": {"nope"}})

	// Unread: reading p3 leaves it out of the unread list, but not the open one.
	post2, _ := http.Post(l.srv.URL+"/api/notifications/read", "application/json", strings.NewReader(`{"ids":["`+p3+`"]}`))
	post2.Body.Close()
	l.next()
	want("open and unread", url.Values{"open": {"true"}, "unread": {"true"}}, finding, p2)

	// The cursor keeps working: pages of one through the open list.
	var paged []string
	before := ""
	for i := 0; i < 5; i++ {
		q := url.Values{"open": {"true"}, "limit": {"1"}}
		if before != "" {
			q.Set("before", before)
		}
		got, page := l.list(q)
		paged = append(paged, ids(got)...)
		before = str(page, "next_before")
		if before == "" {
			break
		}
	}
	if !slices.Equal(paged, []string{finding, p3, p2}) {
		t.Fatalf("paged = %v, want every open one once", paged)
	}

	// Bad values.
	for _, q := range []string{"kind=nonsense", "open=maybe"} {
		resp, err := http.Get(l.srv.URL + "/api/notifications?" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET ?%s = %d, want 400", q, resp.StatusCode)
		}
	}
}

// post sends an empty-bodied POST and answers its status and JSON.
func (l *ledger) post(path string) (int, jsonObj) {
	l.t.Helper()
	resp, err := http.Post(l.srv.URL+path, "application/json", strings.NewReader("{}"))
	if err != nil {
		l.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out jsonObj
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// The person acknowledges a branch finding: it resolves as acknowledged, is
// marked read, leaves the open count, and nothing else can be acknowledged
// (ticket 07).
func TestABranchFindingIsAcknowledged(t *testing.T) {
	l := newLedger(t)
	pid, tid := l.project.ID, l.task.ID
	l.publish(domain.BranchFindingFound{ProjectID: pid, TaskID: tid, Kind: domain.FindingPush, Branch: "feat/x", FoundAt: time.Now()})
	opened := l.next()
	finding := opened.Changed[0]
	if opened.OpenCount != 1 {
		t.Fatalf("open count with the finding = %d, want 1", opened.OpenCount)
	}

	status, body := l.post("/api/notifications/" + finding.ID + "/acknowledge")
	if status != http.StatusOK {
		t.Fatalf("acknowledge = %d %v", status, body)
	}
	got := obj(body, "notification")
	if str(got, "id") != finding.ID || str(got, "outcome") != "acknowledged" || got["resolved_at"] == nil || got["read_at"] == nil {
		t.Fatalf("acknowledged notification = %v", got)
	}
	if c := l.next(); len(c.Changed) != 1 || c.Changed[0].Outcome != domain.OutcomeAcknowledged || c.OpenCount != 0 || c.UnreadCount != 0 {
		t.Fatalf("stream after acknowledge = %+v", c)
	}

	// Again: unchanged, and nothing on the stream.
	status, again := l.post("/api/notifications/" + finding.ID + "/acknowledge")
	if status != http.StatusOK || str(obj(again, "notification"), "resolved_at") != str(got, "resolved_at") {
		t.Fatalf("a second acknowledge = %d %v, want the same notification", status, again)
	}
	l.session("s1", "devops", domain.SessionModeOperate)
	l.publish(domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionWaitingApproval})
	waiting := l.next()
	if len(waiting.Changed) != 1 || waiting.Changed[0].Kind != domain.NotificationSessionWaiting {
		t.Fatalf("the second acknowledge sent %+v on the stream", waiting)
	}

	// Another kind and an unknown id.
	if status, body := l.post("/api/notifications/" + waiting.Changed[0].ID + "/acknowledge"); status != http.StatusBadRequest {
		t.Fatalf("acknowledge on another kind = %d %v, want 400", status, body)
	}
	if status, body := l.post("/api/notifications/nope/acknowledge"); status != http.StatusNotFound {
		t.Fatalf("acknowledge on an unknown id = %d %v, want 404", status, body)
	}
}

// listed is the sessions a commit_check_off notification lists, by session id,
// with their state.
func listed(n jsonObj) map[string]string {
	out := map[string]string{}
	entries, _ := n["listed_sessions"].([]any)
	for _, e := range entries {
		entry := e.(jsonObj)
		out[str(entry, "session_id")] = str(entry, "state")
	}
	return out
}

// off announces the commit check of a session of the ledger's task as off.
func (l *ledger) off(session string) {
	l.publish(domain.SessionCommitCheckChanged{SessionID: session, ProjectID: l.project.ID, TicketID: l.task.ID, Off: true, Reason: "the repair failed"})
}

func (l *ledger) repaired(session string) {
	l.publish(domain.SessionCommitCheckChanged{SessionID: session, ProjectID: l.project.ID, TicketID: l.task.ID, Off: false})
}

// A boot that finds sessions with the commit check off posts one notification
// per project, listing them all; repairing them resolves it, and the next boot
// ends it (ticket 08, ADR 0098).
func TestCommitCheckOffIsOneNotificationPerProjectPerBoot(t *testing.T) {
	l := newLedger(t)
	for _, id := range []string{"s1", "s2", "s3"} {
		l.session(id, "agent-"+id, domain.SessionModeImplement)
	}
	other, err := l.cat.Projects.CreateProject(l.ctx, "tui", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Three sessions in one boot: one notification that grows.
	l.off("s1")
	first := l.next()
	if len(first.Changed) != 1 || first.OpenCount != 1 {
		t.Fatalf("the first session: %+v", first)
	}
	l.off("s2")
	l.off("s3")
	l.off("s1") // a check that finds it off again adds nothing
	l.next()
	l.next()
	n := l.only("commit_check_off")
	if got := listed(n); len(got) != 3 || got["s1"] != "off" || got["s2"] != "off" || got["s3"] != "off" {
		t.Fatalf("listed sessions = %v, want s1 s2 s3 off", got)
	}
	if str(n, "verb") != "could not guard 3 sessions" || str(obj(n, "object"), "text") != "commit checks off" || str(obj(n, "actor"), "name") != "harness" ||
		str(n, "title") != "harness could not guard 3 sessions: commit checks off" {
		t.Fatalf("copy = %v", n)
	}
	entry := n["listed_sessions"].([]any)[0].(jsonObj)
	if str(entry, "agent_name") != "agent-s1" || str(entry, "task_id") != l.task.ID || str(entry, "task_title") != "Notifications" ||
		!strings.Contains(str(entry, "link"), "session=s1") {
		t.Fatalf("listed session = %v", entry)
	}
	nid := str(n, "id")

	// The repeat of s1 sent nothing: the next change on the stream is the next event.
	l.repaired("s1")
	c := l.next()
	if len(c.Changed) != 1 || c.Changed[0].ID != nid || c.Changed[0].ResolvedAt != nil || c.OpenCount != 1 {
		t.Fatalf("after s1 is repaired: %+v", c)
	}
	if got := listed(l.only("commit_check_off")); got["s1"] != "repaired" || got["s2"] != "off" {
		t.Fatalf("listed after s1 = %v", got)
	}

	// A deleted session ends; the last one repaired resolves the notification as repaired.
	l.publish(domain.SessionDeleted{SessionID: "s3"})
	c = l.next()
	if got := listed(l.only("commit_check_off")); got["s3"] != "ended" || len(c.Changed) != 1 || c.Changed[0].ResolvedAt != nil {
		t.Fatalf("after s3 is deleted: listed %v, stream %+v", got, c)
	}
	l.repaired("s2")
	l.settled("commit_check_off", "repaired")

	// Another project keeps its own.
	l.publish(domain.SessionCommitCheckChanged{SessionID: "s1", ProjectID: other.ID, Off: true, Reason: "x"})
	l.next()
	l.off("s1") // a new episode on the first project: a new notification
	l.next()
	list, page := l.list(url.Values{"kind": {"commit_check_off"}, "open": {"true"}})
	if len(list) != 2 || count(page, "open_count") != 2 {
		t.Fatalf("two projects: %d open commit check notifications, open_count %v", len(list), page["open_count"])
	}

	// The next boot posts a new one for the project and ends the old one.
	l.reboot()
	l.off("s2")
	c = l.next()
	if len(c.Changed) != 2 {
		t.Fatalf("the next boot: %+v, want the ended one and the new one", c.Changed)
	}
	var ended, created *domain.Notification
	for _, ch := range c.Changed {
		if ch.ResolvedAt != nil {
			ended = ch
		} else {
			created = ch
		}
	}
	if ended == nil || created == nil || ended.Outcome != domain.OutcomeEnded || ended.ProjectID != l.project.ID || created.ProjectID != l.project.ID {
		t.Fatalf("the next boot: ended %+v created %+v", ended, created)
	}
	list, _ = l.list(url.Values{"kind": {"commit_check_off"}, "open": {"true"}})
	if len(list) != 2 {
		t.Fatalf("open commit check notifications after the next boot = %d, want one per project (2)", len(list))
	}
}
