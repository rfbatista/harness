//go:build !windows

package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The daily digest (ADR 0113): a one-off product-owner run writes its text.

func TestAOneOffProductOwnerRunAnswersWithNoThreadAndStaysOutOfEverySessionSurface(t *testing.T) {
	e := newPOE2E(t)

	text, err := e.po.RunOnce(e.ctx, e.project, "write the digest")
	if err != nil {
		t.Fatal(err)
	}
	if text != "answer to: write the digest" {
		t.Fatalf("the run answered %q, want the fake's answer", text)
	}

	launches := e.launches()
	if len(launches) != 1 {
		t.Fatalf("claude was launched %d times, want once: %v", len(launches), launches)
	}
	if b, _ := os.ReadFile(filepath.Join(e.fakeDir, "received")); !strings.Contains(string(b), "write the digest") {
		t.Errorf("claude received %q, want the run's prompt", b)
	}

	var threads struct {
		Threads []any `json:"threads"`
	}
	e.get("product_owner_threads?project_id="+e.project, &threads)
	if len(threads.Threads) != 0 {
		t.Errorf("the run created threads: %v", threads.Threads)
	}

	sessions := e.sess.List(e.ctx, ports.SessionFilter{ProjectID: e.project, IncludeProductOwner: true})
	if len(sessions) != 1 || sessions[0].Mode != domain.SessionModeProductOwner {
		t.Fatalf("sessions = %+v, want the run's one product-owner session", sessions)
	}
	eventuallyPO(t, "the run's process ends", func() string {
		s := e.sess.Get(sessions[0].ID)
		if s == nil || !s.Status.IsTerminal() || e.orch.ProductOwnerRunning(s.ID) {
			return "the run's process is still alive"
		}
		return ""
	})
	sid := sessions[0].ID

	var list struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	e.get("sessions?project_id="+e.project, &list)
	for _, s := range list.Sessions {
		if s.ID == sid {
			t.Errorf("the project's session list shows the run's session")
		}
	}
	res, err := http.Get(e.srv.URL + "/api/agent_board")
	if err != nil {
		t.Fatal(err)
	}
	board, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(board), sid) {
		t.Errorf("the agent board shows the run's session")
	}
	notes, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes.Notifications) != 0 {
		t.Errorf("the inbox has %d items after a run, want none", len(notes.Notifications))
	}

	headers, _ := os.ReadFile(filepath.Join(e.fakeDir, "headers"))
	if want := "harness:product-owner:" + e.project; !strings.Contains(string(headers), want) {
		t.Errorf("claude was given the headers %q, want the attribution %s", headers, want)
	}
}

func TestAOneOffRunOfAnUnknownProjectIsRefused(t *testing.T) {
	e := newPOE2E(t)
	if _, err := e.po.RunOnce(e.ctx, "no-such-project", "x"); err == nil || !strings.Contains(err.Error(), "PROJECT_NOT_FOUND") {
		t.Fatalf("err = %v, want PROJECT_NOT_FOUND", err)
	}
	if n := len(e.launches()); n != 0 {
		t.Errorf("claude was launched %d times for an unknown project", n)
	}
}

// digestClock is the clock the digest scheduler reads in these tests. It
// starts well after the real time, so what the server stamps with the real
// time while a test sets up (a task's creation) lies before every period.
type digestClock struct {
	mu sync.Mutex
	t  time.Time
}

func newDigestClock() *digestClock {
	return &digestClock{t: time.Date(2030, 1, 1, 20, 0, 0, 0, time.Local)}
}

func (c *digestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *digestClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// at is the clock's day plus days, at hh:mm local.
func (c *digestClock) at(days, hh, mm int) time.Time {
	now := c.Now()
	return time.Date(now.Year(), now.Month(), now.Day()+days, hh, mm, 0, 0, time.Local)
}

// newDigestE2E is the product-owner server with the digest scheduler reading
// a controllable clock and ticking every few milliseconds.
func newDigestE2E(t *testing.T) (*poE2E, *digestClock) {
	t.Helper()
	clk := newDigestClock()
	e := newPOE2EWith(t, func(c *Config) {
		c.DigestClock = clk.Now
		c.DigestEvery = 20 * time.Millisecond
	})
	return e, clk
}

type poDigestItem struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	URL   string `json:"url"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type poDigest struct {
	DigestID    string         `json:"digest_id"`
	Date        string         `json:"date"`
	PeriodStart time.Time      `json:"period_start"`
	PeriodEnd   time.Time      `json:"period_end"`
	Summary     string         `json:"summary"`
	Items       []poDigestItem `json:"items"`
}

type poDigestPage struct {
	Digests    []poDigest `json:"digests"`
	HasEarlier bool       `json:"has_earlier"`
}

func (e *poE2E) digests(before string) poDigestPage {
	e.t.Helper()
	var page poDigestPage
	path := "product_owner_digests?project_id=" + e.project
	if before != "" {
		path += "&before=" + before
	}
	if status, code := e.get(path, &page); status != 200 {
		e.t.Fatalf("digests = %d %s", status, code)
	}
	return page
}

func (e *poE2E) setDigestTime(hhmm string) {
	e.t.Helper()
	if status, code := e.post("update_product_owner", map[string]any{"project_id": e.project, "digest_time": hhmm}, nil); status != 200 {
		e.t.Fatalf("set digest_time %q = %d %s", hhmm, status, code)
	}
}

// moveTask records a task's status move at the given time.
func (e *poE2E) moveTask(title string, to domain.TicketStatus, at time.Time) *domain.Ticket {
	e.t.Helper()
	tk, err := e.plan.CreateTicket(e.ctx, e.project, title, "", domain.TicketStatusTodo, "")
	if err != nil {
		e.t.Fatal(err)
	}
	e.moveExisting(tk, to, at)
	return tk
}

func (e *poE2E) moveExisting(tk *domain.Ticket, to domain.TicketStatus, at time.Time) {
	e.t.Helper()
	if err := e.plan.StatusHistory.Append(&domain.TaskStatusChange{TaskID: tk.ID, ProjectID: e.project, Status: to, By: domain.ChangedByArchitect, At: at}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *poE2E) waitDigests(n int) poDigestPage {
	e.t.Helper()
	var page poDigestPage
	eventuallyPO(e.t, "digests recorded", func() string {
		page = e.digests("")
		if len(page.Digests) != n {
			return fmt.Sprintf("%d digests, want %d", len(page.Digests), n)
		}
		return ""
	})
	return page
}

func TestADigestOfTheTasksThatMovedIsRecordedAtTheSetTimeAndAnnounced(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	tk := e.moveTask("Product owner digest", domain.TicketStatusReview, clk.at(1, 6, 0))

	// Before the digest time, nothing is recorded.
	clk.set(clk.at(1, 8, 0))
	time.Sleep(300 * time.Millisecond)
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests before the digest time", n)
	}
	if n := len(e.launches()); n != 0 {
		t.Fatalf("claude ran %d times before the digest time", n)
	}

	clk.set(clk.at(0, 8, 31))
	page := e.waitDigests(1)
	d := page.Digests[0]
	if d.Date != "2030-01-02" {
		t.Errorf("date = %q, want 2030-01-02", d.Date)
	}
	if !d.PeriodStart.Equal(time.Date(2030, 1, 1, 20, 0, 0, 0, time.Local)) || !d.PeriodEnd.Equal(clk.Now()) {
		t.Errorf("period = %s to %s", d.PeriodStart, d.PeriodEnd)
	}
	if !strings.HasPrefix(d.Summary, "answer to: ") {
		t.Errorf("summary = %q, want what the one-off run answered", d.Summary)
	}
	if len(d.Items) != 1 || d.Items[0].Kind != "task_moved" || d.Items[0].Title != "Product owner digest" || d.Items[0].To != "review" || d.Items[0].From != "" {
		t.Fatalf("items = %+v, want the task's move to review", d.Items)
	}
	if want := "/projects/" + e.project + "/tasks/" + tk.ID; d.Items[0].URL != want {
		t.Errorf("url = %q, want %q", d.Items[0].URL, want)
	}

	eventuallyPO(t, "the digest event", func() string {
		if ev := e.feed.of("product_owner_digest"); len(ev) != 1 || ev[0]["digest_id"] != d.DigestID || ev[0]["date"] != d.Date || ev[0]["project_id"] != e.project {
			return fmt.Sprintf("events = %v", ev)
		}
		return ""
	})

	// Later ticks of the same day record nothing more and run no model.
	clk.set(clk.at(0, 9, 30))
	time.Sleep(300 * time.Millisecond)
	if n := len(e.digests("").Digests); n != 1 {
		t.Errorf("%d digests after a second tick of the day", n)
	}
	if n := len(e.launches()); n != 1 {
		t.Errorf("claude ran %d times, want the one digest run", n)
	}
	var threads struct {
		Threads []any `json:"threads"`
	}
	e.get("product_owner_threads?project_id="+e.project, &threads)
	if len(threads.Threads) != 0 {
		t.Errorf("a digest appended a thread: %v", threads.Threads)
	}
	notes, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes.Notifications) != 0 {
		t.Errorf("a digest posted %d inbox items", len(notes.Notifications))
	}

	// The next digest starts where this one ended; a task that moved again
	// is reported from where it was.
	e.moveExisting(tk, domain.TicketStatusDone, clk.at(1, 7, 0))
	clk.set(clk.at(1, 9, 0))
	next := e.waitDigests(2).Digests[0]
	if !next.PeriodStart.Equal(d.PeriodEnd) {
		t.Errorf("period_start = %s, want the previous digest's end %s", next.PeriodStart, d.PeriodEnd)
	}
	if len(next.Items) != 1 || next.Items[0].From != "review" || next.Items[0].To != "done" {
		t.Errorf("items = %+v, want review to done", next.Items)
	}
}

func TestDigestsPageNewestFirstThirtyAtATime(t *testing.T) {
	e, _ := newDigestE2E(t)
	// Thirty-two days of digests, seeded straight into the store: a digest
	// run per day would take minutes.
	db, err := sqlite.Open(e.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewProductOwnerDigestRepository(db)
	for day := 1; day <= 32; day++ {
		date := time.Date(2029, 12, day, 0, 0, 0, 0, time.Local)
		d := domain.ProductOwnerDigest{ID: fmt.Sprintf("d%02d", day), ProjectID: e.project, Date: date.Format("2006-01-02"),
			PeriodStart: date, PeriodEnd: date.Add(time.Hour), Summary: "s", Items: []domain.ProductOwnerDigestItem{{Kind: domain.DigestItemTaskMoved, Title: "t", URL: "/u"}}}
		if err := repo.SaveDigest(e.ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	first := e.digests("")
	if len(first.Digests) != 30 || !first.HasEarlier {
		t.Fatalf("first page = %d digests, has_earlier %v; want 30 and true", len(first.Digests), first.HasEarlier)
	}
	if first.Digests[0].Date != "2030-01-01" || first.Digests[0].Date <= first.Digests[29].Date {
		t.Errorf("digests are not newest first: %s .. %s", first.Digests[0].Date, first.Digests[29].Date)
	}
	rest := e.digests(first.Digests[29].Date)
	if len(rest.Digests) != 2 || rest.HasEarlier {
		t.Errorf("second page = %d digests, has_earlier %v; want 2 and false", len(rest.Digests), rest.HasEarlier)
	}
	if status, code := e.get("product_owner_digests?project_id=nope", nil); status != 404 || code != "PROJECT_NOT_FOUND" {
		t.Errorf("unknown project = %d %s, want 404 PROJECT_NOT_FOUND", status, code)
	}
}

// endSession records a session of the project that is over, as of at.
func (e *poE2E) endSession(id, task string, status domain.SessionStatus, mode domain.SessionMode, taskID string, at time.Time) {
	e.t.Helper()
	if _, err := e.sess.Create(&domain.Session{ID: id, ProjectID: e.project, TicketID: taskID, Task: task, Status: status, Mode: mode, StatusSince: at}); err != nil {
		e.t.Fatal(err)
	}
}

func TestTheDigestListsTheSessionsThatEndedOrFailedAndWhatWaitsOnThePerson(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	tk := e.moveTask("A task with sessions", domain.TicketStatusReview, clk.at(1, 5, 0))
	e.endSession("ended-1", "Build the export", domain.SessionDone, domain.SessionModeImplement, tk.ID, clk.at(1, 5, 30))
	e.endSession("failed-1", "Migrate the schema", domain.SessionFailed, domain.SessionModeImplement, tk.ID, clk.at(1, 6, 0))
	// A stopped session, a product-owner session and a session that ended
	// before the period are not reported.
	e.endSession("stopped-1", "Stopped by the person", domain.SessionStopped, domain.SessionModeImplement, tk.ID, clk.at(1, 6, 30))
	e.endSession("po-1", "Product owner: harness", domain.SessionDone, domain.SessionModeProductOwner, "", clk.at(1, 7, 0))
	e.endSession("old-1", "Ended long ago", domain.SessionDone, domain.SessionModeImplement, tk.ID, clk.at(0, 19, 0))
	if _, err := e.notes.Notify(e.ctx, ports.NotifyRequest{ProjectID: e.project, TaskID: tk.ID, Title: "Review the export", Level: domain.NotificationAction}); err != nil {
		t.Fatal(err)
	}

	clk.set(clk.at(1, 8, 31))
	d := e.waitDigests(1).Digests[0]
	byKind := map[string][]poDigestItem{}
	for _, it := range d.Items {
		byKind[it.Kind] = append(byKind[it.Kind], it)
	}
	wantSession := func(kind, title, id string) {
		t.Helper()
		got := byKind[kind]
		if len(got) != 1 || got[0].Title != title || got[0].URL != "/projects/"+e.project+"/tasks/"+tk.ID+"?session="+id {
			t.Errorf("%s items = %+v, want %q at the session's page", kind, got, title)
		}
	}
	wantSession("session_ended", "Build the export", "ended-1")
	wantSession("session_failed", "Migrate the schema", "failed-1")
	if got := byKind["task_moved"]; len(got) != 1 {
		t.Errorf("task_moved items = %+v", got)
	}
	if got := byKind["waiting_on_person"]; len(got) != 1 || got[0].Title != "Review the export" || got[0].URL != "/projects/"+e.project+"/tasks/"+tk.ID {
		t.Errorf("waiting_on_person items = %+v, want the open review", got)
	}
	if len(d.Items) != 4 {
		t.Errorf("items = %+v, want four", d.Items)
	}

	// What the one-off run is given is the collected items, each with its title and url.
	prompt, _ := os.ReadFile(filepath.Join(e.fakeDir, "received"))
	for _, it := range d.Items {
		if !strings.Contains(string(prompt), it.Title) || !strings.Contains(string(prompt), it.URL) {
			t.Errorf("the digest prompt lacks %q or %q: %s", it.Title, it.URL, prompt)
		}
	}

	// The next day only the thing still waiting is left: it is not a change,
	// so the day is quiet and nothing is recorded.
	clk.set(clk.at(1, 9, 0))
	time.Sleep(300 * time.Millisecond)
	if n := len(e.digests("").Digests); n != 1 {
		t.Errorf("%d digests after a day with nothing but a waiting item, want 1", n)
	}
}

func (e *poE2E) setEnabled(on bool) {
	e.t.Helper()
	if status, code := e.post("update_product_owner", map[string]any{"project_id": e.project, "enabled": on}, nil); status != 200 {
		e.t.Fatalf("set enabled %v = %d %s", on, status, code)
	}
}

// settle gives the scheduler time to tick, for a test that expects nothing.
func settle() { time.Sleep(300 * time.Millisecond) }

func TestAQuietDayRecordsNothingAndRunsNoModel(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	// A product-owner thread is busy all day: it is not a change.
	id, _ := e.startThread("hello")
	e.waitTurn(id, "idle")
	launches := len(e.launches())

	clk.set(clk.at(1, 9, 0))
	settle()
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests on a quiet day", n)
	}
	if n := len(e.launches()); n != launches {
		t.Errorf("claude ran %d more times on a quiet day", n-launches)
	}
	if ev := e.feed.of("product_owner_digest"); len(ev) != 0 {
		t.Errorf("digest events on a quiet day: %v", ev)
	}

	// A change that comes later in the day waits for the next day.
	e.moveTask("Late change", domain.TicketStatusReview, clk.at(0, 15, 0))
	clk.set(clk.at(0, 16, 0))
	settle()
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("the quiet day was digested again the same day")
	}
	clk.set(clk.at(1, 9, 0))
	if d := e.waitDigests(1).Digests[0]; len(d.Items) != 1 || d.Items[0].Title != "Late change" {
		t.Errorf("items = %+v, want the late change", d.Items)
	}
}

func TestAProductOwnerThatIsOffRecordsNothingAndCatchesUpWhenSwitchedOn(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.setEnabled(false)
	e.moveTask("Moved while off", domain.TicketStatusReview, clk.at(1, 6, 0))

	clk.set(clk.at(1, 9, 0))
	settle()
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests while the product owner is off", n)
	}
	if n := len(e.launches()); n != 0 {
		t.Fatalf("claude ran %d times while the product owner is off", n)
	}

	e.setEnabled(true)
	d := e.waitDigests(1).Digests[0]
	if len(d.Items) != 1 || d.Items[0].Title != "Moved while off" {
		t.Errorf("items = %+v, want what moved while it was off", d.Items)
	}
}

func TestAProjectWithNoDigestTimeRecordsNothing(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.moveTask("A change", domain.TicketStatusReview, clk.at(1, 6, 0))
	clk.set(clk.at(1, 9, 0))
	settle()
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests with no digest time", n)
	}

	e.setDigestTime("08:30")
	e.setDigestTime("")
	clk.set(clk.at(2, 9, 0))
	settle()
	if n := len(e.digests("").Digests); n != 0 || len(e.launches()) != 0 {
		t.Errorf("%d digests and %d runs after the digest time was cleared", n, len(e.launches()))
	}
}

func TestChangingTheDigestTimeTakesEffectAtOnce(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("10:00")
	e.moveTask("A change", domain.TicketStatusReview, clk.at(1, 6, 0))

	clk.set(clk.at(1, 9, 0))
	settle()
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests before 10:00", n)
	}
	// An earlier time that has passed records now, once.
	e.setDigestTime("08:30")
	e.waitDigests(1)
	// A later time, set after the day was digested, records nothing more today.
	e.setDigestTime("09:30")
	settle()
	if n := len(e.digests("").Digests); n != 1 {
		t.Errorf("%d digests after the time moved on the same day, want 1", n)
	}
	if n := len(e.launches()); n != 1 {
		t.Errorf("claude ran %d times, want one", n)
	}
}

func TestADigestTimeMissedWhileTheServerWasDownIsRecordedOnceAfterBootCoveringTheGap(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	tk := e.moveTask("Day one", domain.TicketStatusReview, clk.at(1, 6, 0))
	clk.set(clk.at(1, 9, 0))
	first := e.waitDigests(1).Digests[0]

	// Changes on three later days, then the server goes down over all of them.
	e.moveExisting(tk, domain.TicketStatusInProgress, clk.at(2, 6, 0))
	e.moveTask("Day three", domain.TicketStatusReview, clk.at(3, 7, 0))
	e.moveTask("Day four", domain.TicketStatusReview, clk.at(4, 5, 0))
	end := clk.at(4, 6, 0)
	e.feed.stop()
	e.srv.Close()
	if err := e.app.Stop(e.ctx); err != nil {
		t.Fatal(err)
	}

	// Booting before that day's digest time records nothing yet.
	clk.set(clk.at(4, 7, 0))
	e.boot()
	e.serve()
	settle()
	if n := len(e.digests("").Digests); n != 1 {
		t.Fatalf("%d digests after a boot before the digest time, want 1", n)
	}

	clk.set(end.Add(3 * time.Hour))
	page := e.waitDigests(2)
	gap := page.Digests[0]
	if !gap.PeriodStart.Equal(first.PeriodEnd) || !gap.PeriodEnd.Equal(clk.Now()) {
		t.Errorf("period = %s to %s, want %s to now", gap.PeriodStart, gap.PeriodEnd, first.PeriodEnd)
	}
	titles := map[string]bool{}
	for _, it := range gap.Items {
		titles[it.Title] = true
	}
	if len(gap.Items) != 3 || !titles["Day one"] || !titles["Day three"] || !titles["Day four"] {
		t.Errorf("items = %+v, want the three tasks that moved in the gap", gap.Items)
	}

	// A restart the same day records nothing more.
	e.restart()
	settle()
	if n := len(e.digests("").Digests); n != 2 {
		t.Errorf("%d digests after a restart the same day, want 2", n)
	}

	// A gap with no change records nothing.
	clk.set(clk.at(3, 9, 0))
	e.restart()
	settle()
	if n := len(e.digests("").Digests); n != 2 {
		t.Errorf("%d digests after a gap with no change, want 2", n)
	}
}

// seedDigest stores a digest of a project straight into the database, for a
// test that reads digests and has no need to run the scheduler.
func (e *poE2E) seedDigest(project, date, summary string, items ...domain.ProductOwnerDigestItem) {
	e.t.Helper()
	db, err := sqlite.Open(e.cfg.DBPath)
	if err != nil {
		e.t.Fatal(err)
	}
	day, _ := time.ParseInLocation("2006-01-02", date, time.Local)
	d := domain.ProductOwnerDigest{ID: "seed-" + project + "-" + date, ProjectID: project, Date: date, PeriodStart: day.Add(-24 * time.Hour), PeriodEnd: day.Add(8 * time.Hour), Summary: summary, Items: items}
	if d.Items == nil {
		d.Items = []domain.ProductOwnerDigestItem{}
	}
	if err := sqlite.NewProductOwnerDigestRepository(db).SaveDigest(e.ctx, d); err != nil {
		e.t.Fatal(err)
	}
}

func TestTheProductOwnerReadsPastDigestsWithAToolOfItsOwnProjectOnly(t *testing.T) {
	e := newPOE2E(t)
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", e.cfg.Root)
	if err != nil {
		t.Fatal(err)
	}
	move := domain.ProductOwnerDigestItem{Kind: domain.DigestItemTaskMoved, Title: "Ship it", URL: "/projects/p/tasks/t", From: "review", To: "done"}
	e.seedDigest(e.project, "2030-01-07", "Tuesday: shipped it.", move)
	e.seedDigest(e.project, "2030-01-08", "Wednesday: quiet-ish.")
	e.seedDigest(e.project, "2030-01-10", "Friday.")
	e.seedDigest(other.ID, "2030-01-07", "SECRET of the other project")

	id, _ := e.startThread("hello")
	e.waitTurn(id, "idle")
	owner := e.dialSession(e.thread(id).Thread.SessionID)
	if names := e.toolNames(owner); !slices.Contains(names, "list_product_owner_digests") || slices.Contains(names, "start_task_session") {
		t.Fatalf("tools = %v, want the digest tool and no start tool", names)
	}

	type answer struct {
		Count   int `json:"count"`
		Digests []struct {
			Date    string         `json:"date"`
			Summary string         `json:"summary"`
			Items   []poDigestItem `json:"items"`
		} `json:"digests"`
	}
	read := func(args map[string]any) answer {
		t.Helper()
		text, refused := e.call(owner, "list_product_owner_digests", args)
		if refused {
			t.Fatalf("the tool refused %v: %s", args, text)
		}
		if strings.Contains(text, "SECRET") {
			t.Fatalf("the tool answered another project's digest: %s", text)
		}
		var a answer
		if err := json.Unmarshal([]byte(text), &a); err != nil {
			t.Fatalf("answer %s: %v", text, err)
		}
		return a
	}
	if a := read(map[string]any{"date": "2030-01-07"}); a.Count != 1 || a.Digests[0].Summary != "Tuesday: shipped it." || len(a.Digests[0].Items) != 1 || a.Digests[0].Items[0].To != "done" {
		t.Errorf("one day = %+v, want that day's digest", a)
	}
	if a := read(map[string]any{"date": "2030-01-09"}); a.Count != 0 || len(a.Digests) != 0 {
		t.Errorf("a day with no digest = %+v, want an empty list", a)
	}
	if a := read(map[string]any{"from": "2030-01-07", "to": "2030-01-09"}); a.Count != 2 || a.Digests[0].Date != "2030-01-08" || a.Digests[1].Date != "2030-01-07" {
		t.Errorf("a range = %+v, want the two digests of it, newest first", a)
	}
	if a := read(map[string]any{}); a.Count != 3 || a.Digests[0].Date != "2030-01-10" {
		t.Errorf("no arguments = %+v, want the newest digests", a)
	}
	if text, refused := e.call(owner, "list_product_owner_digests", map[string]any{"date": "last tuesday"}); !refused || !strings.Contains(text, "YYYY-MM-DD") {
		t.Errorf("a bad date = %q refused %v, want a refusal naming the format", text, refused)
	}

	// The opening brief tells the product owner to answer from digests and say so.
	l := e.launches()
	if len(l) == 0 || !strings.Contains(l[0], "list_product_owner_digests") || !strings.Contains(l[0], "digest") {
		t.Errorf("the brief does not name the digest tool")
	}
}

func TestSomethingThatOnlyWaitsOnThePersonDoesNotMakeAQuietDayCount(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	if _, err := e.notes.Notify(e.ctx, ports.NotifyRequest{ProjectID: e.project, Title: "Review the export", Level: domain.NotificationAction}); err != nil {
		t.Fatal(err)
	}
	clk.set(clk.at(1, 9, 0))
	settle()
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests for a day with nothing but a waiting item", n)
	}
	if n := len(e.launches()); n != 0 {
		t.Errorf("claude ran %d times for a quiet day", n)
	}

	// The next day something moves: the digest lists the waiting item too.
	e.moveTask("Moved", domain.TicketStatusReview, clk.at(1, 6, 0))
	clk.set(clk.at(1, 9, 0))
	d := e.waitDigests(1).Digests[0]
	kinds := map[string]int{}
	for _, it := range d.Items {
		kinds[it.Kind]++
	}
	if kinds["task_moved"] != 1 || kinds["waiting_on_person"] != 1 {
		t.Errorf("items = %+v, want the moved task and the waiting item", d.Items)
	}
}
