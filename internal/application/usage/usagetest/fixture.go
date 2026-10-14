// Package usagetest builds a usage service over an in-memory database with a
// project, a task and a session in each of two projects, for the usage API
// tests on the HTTP side and the service's own.
package usagetest

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/usage"
	"operators-mcp/internal/domain"
)

// Env is everything the fixture built. Project A ("Alpha") has task TaskA
// ("Build it") with session SessionA; project B ("Beta") has TaskB and
// SessionB.
type Env struct {
	DB                 *gorm.DB
	Repo               *sqlite.UsageRepository
	Service            *usage.Service
	ProjectA, ProjectB string
	TaskA, TaskB       string
	SessionA, SessionB string
	Log                *SyncBuffer
}

// SyncBuffer is a log sink tests can read while the service writes.
type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *SyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *SyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Option adjusts the service the fixture builds.
type Option func(*usage.Options)

// WithClock sets the service's clock.
func WithClock(now func() time.Time) Option { return func(o *usage.Options) { o.Clock = now } }

// NewEnv opens the database, records the projects, tasks and sessions, and
// builds the service.
func NewEnv(t *testing.T, opts ...Option) *Env {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects, tickets, sessions := sqlite.NewProjectRepository(db), sqlite.NewTicketRepository(db), sqlite.NewSessionRepository(db)
	e := &Env{DB: db, Repo: sqlite.NewUsageRepository(db), Log: &SyncBuffer{}}
	for _, p := range []struct {
		name, task      string
		project, ticket *string
		session         *string
	}{
		{"Alpha", "Build it", &e.ProjectA, &e.TaskA, &e.SessionA},
		{"Beta", "Fix it", &e.ProjectB, &e.TaskB, &e.SessionB},
	} {
		proj, err := projects.Create(p.name, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		tk, err := tickets.Create(proj.ID, p.task, "", domain.TicketStatusTodo)
		if err != nil {
			t.Fatal(err)
		}
		s, err := sessions.Create(&domain.Session{ProjectID: proj.ID, TicketID: tk.ID, Status: domain.SessionRunning})
		if err != nil {
			t.Fatal(err)
		}
		*p.project, *p.ticket, *p.session = proj.ID, tk.ID, s.ID
	}
	o := usage.Options{Logger: slog.New(slog.NewTextHandler(e.Log, nil))}
	for _, opt := range opts {
		opt(&o)
	}
	e.Service = usage.New(e.Repo, usage.Readers{Sessions: sessions, Tickets: tickets}, o)
	return e
}
