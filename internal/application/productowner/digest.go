package productowner

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The daily digest (ADR 0113). The server collects what changed, with no
// model; only when something did, a one-off product-owner run writes the
// summary, and the digest store keeps the record.

// DefaultDigestEvery is how often the scheduler looks at the projects.
const DefaultDigestEvery = time.Minute

// digestRetry is how long a project waits after a digest run that failed.
const digestRetry = 10 * time.Minute

// DigestProjects is what the digests read of the project catalog.
type DigestProjects interface {
	ListProjects(ctx context.Context) ([]*domain.Project, error)
	GetProject(ctx context.Context, projectID string) (*domain.Project, error)
}

// OneOff is the product owner's one-off run.
type OneOff interface {
	RunOnce(ctx context.Context, projectID, prompt string) (string, error)
}

// DigestSources are what the collection reads of the project.
type DigestSources struct {
	Tasks    ports.TicketReader
	Changes  ports.TaskStatusChangeRepository
	Sessions ports.SessionRepository
	// Notes is the person's inbox, the source of what waits on them; nil
	// leaves that out.
	Notes ports.NotificationReader
}

// Digests collects, writes and keeps the daily digests of every project whose
// product owner is on and has a digest time.
//
// What waits on the person is listed in a digest only when something else
// changed in its period (a task moved, a session ended or failed). It never
// makes a quiet day count: a review that keeps waiting would else be a digest
// every day.
type Digests struct {
	store    ports.ProductOwnerDigestStore
	settings ports.ProductOwnerStore
	projects DigestProjects
	run      OneOff
	feed     ports.ProjectChangeSink
	src      DigestSources
	clock    func() time.Time
	every    time.Duration

	mu      sync.Mutex
	running map[string]bool
	retryAt map[string]time.Time
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewDigests returns the digests of every project. A nil clock reads the real
// time; a zero every ticks once a minute.
func NewDigests(store ports.ProductOwnerDigestStore, settings ports.ProductOwnerStore, projects DigestProjects, run OneOff, feed ports.ProjectChangeSink, src DigestSources, clock func() time.Time, every time.Duration) *Digests {
	if clock == nil {
		clock = time.Now
	}
	if every <= 0 {
		every = DefaultDigestEvery
	}
	return &Digests{store: store, settings: settings, projects: projects, run: run, feed: feed, src: src, clock: clock, every: every,
		running: map[string]bool{}, retryAt: map[string]time.Time{}}
}

var _ ports.ProductOwnerDigests = (*Digests)(nil)

// Start runs the scheduler until Stop.
func (d *Digests) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel, d.done = cancel, make(chan struct{})
	go func() {
		defer close(d.done)
		t := time.NewTicker(d.every)
		defer t.Stop()
		for {
			d.Tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Stop ends the scheduler and waits for the run it is in.
func (d *Digests) Stop() {
	if d.cancel != nil {
		d.cancel()
		<-d.done
	}
}

// Digests implements ports.ProductOwnerDigests.
func (d *Digests) Digests(ctx context.Context, projectID, before string) ([]domain.ProductOwnerDigest, bool, error) {
	if _, err := d.projects.GetProject(ctx, projectID); err != nil {
		return nil, false, err
	}
	return d.store.Digests(ctx, projectID, before, domain.ProductOwnerDigestPage)
}

// DigestTimeChanged is told when a project's digest time changes: the first
// time one is set, the digests start from that moment.
func (d *Digests) DigestTimeChanged(ctx context.Context, projectID, before, after string) {
	if after == "" || before != "" {
		return
	}
	st, err := d.store.DigestState(ctx, projectID)
	if err != nil || st != nil {
		return
	}
	_ = d.store.SaveDigestState(ctx, domain.ProductOwnerDigestState{ProjectID: projectID, SinceAt: d.clock()})
}

// Tick looks at every project once: one whose digest time has passed today and
// that has not been looked at today is collected, and digested when something
// changed.
func (d *Digests) Tick(ctx context.Context) {
	projects, err := d.projects.ListProjects(ctx)
	if err != nil {
		return
	}
	for _, p := range projects {
		if ctx.Err() != nil {
			return
		}
		d.tickProject(ctx, p)
	}
}

func (d *Digests) tickProject(ctx context.Context, p *domain.Project) {
	st, err := d.settings.Settings(ctx, p.ID)
	if err != nil || !st.Enabled || st.DigestTime == "" {
		return
	}
	now := d.clock().Local()
	today := now.Format("2006-01-02")
	if !d.dueToday(st.DigestTime, now) {
		return
	}
	d.mu.Lock()
	if d.running[p.ID] || now.Before(d.retryAt[p.ID]) {
		d.mu.Unlock()
		return
	}
	d.running[p.ID] = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.running, p.ID)
		d.mu.Unlock()
	}()

	state, err := d.store.DigestState(ctx, p.ID)
	if err != nil {
		return
	}
	if state == nil {
		// A digest time set before the digests existed: they start now.
		state = &domain.ProductOwnerDigestState{ProjectID: p.ID, SinceAt: now}
		if err := d.store.SaveDigestState(ctx, *state); err != nil {
			return
		}
	}
	if state.CheckedDate == today {
		return
	}
	items := d.collect(ctx, p, state.SinceAt, now)
	if len(items) == 0 {
		state.CheckedDate = today
		_ = d.store.SaveDigestState(ctx, *state)
		return
	}
	summary, err := d.run.RunOnce(ctx, p.ID, digestPrompt(p, state.SinceAt, now, items))
	if err != nil {
		slog.Warn("product owner digest run failed; retrying later", "project", p.ID, "error", err)
		d.mu.Lock()
		d.retryAt[p.ID] = now.Add(digestRetry)
		d.mu.Unlock()
		return
	}
	id, err := newID()
	if err != nil {
		return
	}
	dig := domain.ProductOwnerDigest{ID: id, ProjectID: p.ID, Date: today, PeriodStart: state.SinceAt, PeriodEnd: now, Summary: summary, Items: items}
	if err := d.store.SaveDigest(ctx, dig); err != nil {
		slog.Warn("product owner digest not saved", "project", p.ID, "error", err)
		return
	}
	state.SinceAt, state.CheckedDate = now, today
	if err := d.store.SaveDigestState(ctx, *state); err != nil {
		slog.Warn("product owner digest state not saved", "project", p.ID, "error", err)
	}
	if d.feed != nil {
		d.feed.AnnounceChange(ports.ProjectChange{ProductOwnerDigest: &ports.ProductOwnerDigestChange{ProjectID: p.ID, DigestID: id, Date: today}})
	}
}

// dueToday says now is at or after the digest time of its own day.
func (d *Digests) dueToday(digestTime string, now time.Time) bool {
	var h, m int
	if _, err := fmt.Sscanf(digestTime, "%d:%d", &h, &m); err != nil {
		return false
	}
	return !now.Before(time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location()))
}

// collect is what changed in the project in (since, until], with no model.
// What waits on the person is listed when something changed, and is never a
// change by itself: a thing that keeps waiting would else be a digest a day.
func (d *Digests) collect(ctx context.Context, p *domain.Project, since, until time.Time) []domain.ProductOwnerDigestItem {
	items := append(d.collectTaskMoves(ctx, p, since, until), d.collectSessions(ctx, p, since, until)...)
	if len(items) == 0 {
		return nil
	}
	return append(items, d.collectWaiting(ctx, p, items)...)
}

// collectSessions has the project's sessions that finished or failed in the
// period. Product-owner sessions are not listed: the repository leaves them out
// of its lists. A session the person stopped is neither.
func (d *Digests) collectSessions(ctx context.Context, p *domain.Project, since, until time.Time) []domain.ProductOwnerDigestItem {
	if d.src.Sessions == nil {
		return nil
	}
	var out []domain.ProductOwnerDigestItem
	for _, s := range d.src.Sessions.List(ctx, ports.SessionFilter{ProjectID: p.ID}) {
		if s.StatusSince.After(until) || !s.StatusSince.After(since) {
			continue
		}
		kind := ""
		switch s.Status {
		case domain.SessionDone:
			kind = domain.DigestItemSessionEnded
		case domain.SessionFailed:
			kind = domain.DigestItemSessionFailed
		default:
			continue
		}
		out = append(out, domain.ProductOwnerDigestItem{Kind: kind, Title: s.Task, URL: sessionLink(p.ID, s.TicketID, s.ID)})
	}
	return out
}

// sessionLink is the page of a session: its task's, or its project's.
func sessionLink(projectID, taskID, sessionID string) string {
	return domain.TaskLink(projectID, taskID) + "?session=" + url.QueryEscape(sessionID)
}

// collectWaiting is what waits on the person now: the open items of the inbox
// (reviews, approvals, tasks in review, failed sessions), apart from a failed
// session the digest already lists.
func (d *Digests) collectWaiting(ctx context.Context, p *domain.Project, listed []domain.ProductOwnerDigestItem) []domain.ProductOwnerDigestItem {
	if d.src.Notes == nil {
		return nil
	}
	open := true
	page, err := d.src.Notes.ListNotifications(ctx, ports.NotificationQuery{ProjectID: p.ID, Open: &open, Limit: 50})
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, it := range listed {
		seen[it.URL] = true
	}
	var out []domain.ProductOwnerDigestItem
	for _, n := range page.Notifications {
		if n.Kind == domain.NotificationSessionFailed && seen[sessionLink(p.ID, n.TaskID, n.SessionID)] {
			continue
		}
		out = append(out, domain.ProductOwnerDigestItem{Kind: domain.DigestItemWaitingOnPerson, Title: n.Title, URL: n.Link})
	}
	return out
}

// collectTaskMoves has one item per task that moved: from where it was when
// the period began to where it is at its end.
func (d *Digests) collectTaskMoves(ctx context.Context, p *domain.Project, since, until time.Time) []domain.ProductOwnerDigestItem {
	tasks, err := d.src.Tasks.ListTickets(ctx, p.ID, ports.TicketFilter{})
	if err != nil {
		return nil
	}
	type moved struct {
		at   time.Time
		item domain.ProductOwnerDigestItem
	}
	var found []moved
	for _, tk := range tasks {
		from, to, at := "", "", time.Time{}
		for _, c := range d.src.Changes.ListByTask(tk.ID) {
			switch {
			case !c.At.After(since):
				from = string(c.Status)
			case !c.At.After(until):
				to, at = string(c.Status), c.At
			}
		}
		if to == "" || to == from {
			continue
		}
		found = append(found, moved{at, domain.ProductOwnerDigestItem{Kind: domain.DigestItemTaskMoved, Title: tk.Title, URL: domain.TaskLink(p.ID, tk.ID), From: from, To: to}})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].at.Before(found[j].at) })
	out := make([]domain.ProductOwnerDigestItem, len(found))
	for i, f := range found {
		out[i] = f.item
	}
	return out
}

// digestPrompt is all the one-off run is given: the collected items. It reads
// nothing else, and no other session's transcript.
func digestPrompt(p *domain.Project, since, until time.Time, items []domain.ProductOwnerDigestItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Write the daily digest of project %s for the period %s to %s.\n\n", p.Name, since.Format(time.RFC3339), until.Format(time.RFC3339))
	b.WriteString("The server collected the items below. Write a short summary in Markdown that mentions every item with a Markdown link to its url. " +
		"Use only these items: look nothing else up, and never read another session's transcript. Answer with the summary alone.\n\nItems:\n")
	for _, it := range items {
		line := "- [" + strings.ReplaceAll(it.Kind, "_", " ") + "] " + it.Title
		if it.To != "" {
			from := it.From
			if from == "" {
				from = "new"
			}
			line += " (" + from + " to " + it.To + ")"
		}
		b.WriteString(line + " " + it.URL + "\n")
	}
	return b.String()
}

// AttachDigests makes the service answer the digests of d, so the web UI's API
// reaches them through the product owner.
func (s *Service) AttachDigests(d *Digests) {
	s.digests = d
	s.OnDigestTimeChanged(d.DigestTimeChanged)
}

var _ ports.ProductOwnerDigests = (*Service)(nil)

// Digests implements ports.ProductOwnerDigests.
func (s *Service) Digests(ctx context.Context, projectID, before string) ([]domain.ProductOwnerDigest, bool, error) {
	if s.digests == nil {
		return nil, false, &domain.StructuredError{Code: "UNAVAILABLE", Message: "digests are not available on this server"}
	}
	return s.digests.Digests(ctx, projectID, before)
}
