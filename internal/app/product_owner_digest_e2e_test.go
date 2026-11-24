//go:build !windows

package app

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
