// Package notificationstest builds a notifications service over an
// in-memory database, for the inbox contract suite on either side of the
// wire and for the service's own tests.
package notificationstest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// Clock is a test clock: every reading is one millisecond after the last, so
// notifications recorded one after another are strictly ordered.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock starts at a fixed instant.
func NewClock() *Clock { return &Clock{now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)} }

// Now steps and reads.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Env is everything a fixture built.
type Env struct {
	DB       *gorm.DB
	Repo     *sqlite.NotificationRepository
	Projects *sqlite.ProjectRepository
	Tickets  *sqlite.TicketRepository
	Sessions *sqlite.SessionRepository
	Clock    *Clock
	// ProjectA ("Alpha") and ProjectB ("Beta") exist.
	ProjectA, ProjectB string
}

// NewEnv opens the database and records the two projects.
func NewEnv(t *testing.T) *Env {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	e := &Env{
		DB: db, Repo: sqlite.NewNotificationRepository(db), Projects: sqlite.NewProjectRepository(db),
		Tickets: sqlite.NewTicketRepository(db), Sessions: sqlite.NewSessionRepository(db), Clock: NewClock(),
	}
	a, err := e.Projects.Create("Alpha", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Projects.Create("Beta", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.ProjectA, e.ProjectB = a.ID, b.ID
	return e
}

// Readers are the env's stores as the service reads them.
func (e *Env) Readers() notifications.Readers {
	return notifications.Readers{Projects: e.Projects, Tickets: e.Tickets, Sessions: e.Sessions}
}

// Service builds a service over the env with opts; the env's clock wins
// over opts.Clock.
func (e *Env) Service(t *testing.T, opts notifications.Options) *notifications.Service {
	t.Helper()
	opts.Clock = e.Clock.Now
	svc, err := notifications.New(e.Repo, e.Readers(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// New returns a service with no providers and the suite's fixture, which
// posts through svc as agents of distinct sessions (so the rate limit never
// applies).
func New(t *testing.T) (*notifications.Service, portstest.NotificationsFixture) {
	t.Helper()
	e := NewEnv(t)
	svc := e.Service(t, notifications.Options{})
	return svc, Fixture(e, svc)
}

// Fixture is the suite's fixture over an env and its service.
func Fixture(e *Env, svc ports.Notifier) portstest.NotificationsFixture {
	var mu sync.Mutex
	n := 0
	return portstest.NotificationsFixture{
		ProjectA: e.ProjectA, ProjectB: e.ProjectB,
		Post: func(t *testing.T, projectID, title string) string {
			t.Helper()
			mu.Lock()
			n++
			session := fmt.Sprintf("fixture-%d", n)
			mu.Unlock()
			got, err := svc.Notify(context.Background(), ports.NotifyRequest{SessionID: session, ProjectID: projectID, Title: title, Level: domain.NotificationAction})
			if err != nil {
				t.Fatal(err)
			}
			return got.ID
		},
	}
}
