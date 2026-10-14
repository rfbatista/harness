// Package usage keeps the harness's token usage: it is the usage sink the
// LLM proxy hands each finished request to, resolves the request's
// attribution to session, task and project, stores one usage record per
// request, and answers what the API reads of it.
//
// Recording is best-effort (ADR 0006): a failed write is logged and never
// reaches the LLM call that was measured.
package usage

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.UsageReader = (*Service)(nil)

const (
	// SystemAttribution is the attribution of the harness's own one-shots
	// (summarize, generate, translate).
	SystemAttribution = "harness:system"
	// SystemProject is the project key the API reads the harness's own
	// spend under: records attributed to no project.
	SystemProject = "_harness"

	defaultLimit = 50
	maxLimit     = 200
	// writeTimeout bounds storing one record: the LLM proxy runs the sink
	// in a goroutine per request, so a stuck write would pile them up.
	writeTimeout = 5 * time.Second
	// followerBuffer is how far a follower may fall behind before it is
	// closed and has to reload.
	followerBuffer = 64
)

// Request is one finished LLM request as the LLM proxy reports it; it has
// the shape of spillway's llmproxy.UsageRecord.
type Request struct {
	ID          string
	Attribution string
	Method      string
	Path        string
	Model       string
	Status      int
	Streamed    bool
	StartedAt   time.Time
	EndedAt     time.Time

	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64

	Error string
}

// Sessions finds a session; the sqlite session repository satisfies it.
type Sessions interface {
	Get(id string) *domain.Session
}

// Tickets finds a task; the sqlite ticket repository satisfies it.
type Tickets interface {
	Get(id string) *domain.Ticket
}

// Readers are what the service reads from other contexts.
type Readers struct {
	Sessions Sessions
	Tickets  Tickets
}

// Options configure logging and time.
type Options struct {
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Clock defaults to time.Now.
	Clock func() time.Time
}

// Service implements the usage sink and ports.UsageReader.
type Service struct {
	repo    ports.UsageRepository
	readers Readers
	log     *slog.Logger
	now     func() time.Time

	// mu guards followers, by stored project id; nothing under it waits on
	// the database or the network.
	mu        sync.Mutex
	followers map[string]map[chan *domain.UsageRecord]struct{}
}

// New returns the service.
func New(repo ports.UsageRepository, readers Readers, opts Options) *Service {
	s := &Service{repo: repo, readers: readers, log: opts.Logger, now: opts.Clock,
		followers: map[string]map[chan *domain.UsageRecord]struct{}{}}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Record stores r as a usage record attributed to its session, task and
// project, and streams it to the project's followers. It never fails: a
// failed write is logged, and the record is not streamed.
func (s *Service) Record(ctx context.Context, r Request) {
	rec := &domain.UsageRecord{
		ID: r.ID, Attribution: r.Attribution, Method: r.Method, Path: r.Path, Model: r.Model,
		Status: r.Status, Streamed: r.Streamed, StartedAt: r.StartedAt, DurationMS: r.EndedAt.Sub(r.StartedAt).Milliseconds(),
		InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens, CacheReadInputTokens: r.CacheReadInputTokens,
		Error: r.Error,
	}
	if r.Attribution != "" && r.Attribution != SystemAttribution {
		if sess := s.readers.Sessions.Get(r.Attribution); sess != nil {
			rec.SessionID, rec.TaskID, rec.ProjectID = sess.ID, sess.TicketID, sess.ProjectID
		}
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := s.repo.Create(ctx, rec); err != nil {
		s.log.Error("usage record not stored", "id", r.ID, "attribution", r.Attribution, "model", r.Model, "err", err)
		return
	}
	s.decorate([]*domain.UsageRecord{rec})
	s.broadcast(rec)
}

// broadcast sends r to its project's followers without waiting: one that is
// full has fallen behind and is closed, so it reloads.
func (s *Service) broadcast(r *domain.UsageRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.followers[r.ProjectID] {
		select {
		case ch <- r:
		default:
			s.drop(r.ProjectID, ch)
		}
	}
}

// drop closes a follower; s.mu is held.
func (s *Service) drop(project string, ch chan *domain.UsageRecord) {
	if _, ok := s.followers[project][ch]; !ok {
		return
	}
	delete(s.followers[project], ch)
	if len(s.followers[project]) == 0 {
		delete(s.followers, project)
	}
	close(ch)
}

// FollowUsage streams each usage record stored for a project from now on,
// until ctx ends or the follower falls behind; then the channel closes.
func (s *Service) FollowUsage(ctx context.Context, projectID string) (<-chan *domain.UsageRecord, error) {
	project := projectKey(projectID)
	ch := make(chan *domain.UsageRecord, followerBuffer)
	s.mu.Lock()
	if s.followers[project] == nil {
		s.followers[project] = map[chan *domain.UsageRecord]struct{}{}
	}
	s.followers[project][ch] = struct{}{}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.drop(project, ch)
	}()
	return ch, nil
}

// ListRecentUsage answers a project's recent usage records, newest first.
func (s *Service) ListRecentUsage(_ context.Context, q ports.UsageQuery) (*ports.UsageRecords, error) {
	limit := q.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return nil, invalid(fmt.Sprintf("limit must be between 1 and %d", maxLimit))
	}
	list, err := s.repo.ListRecent(ports.UsageFilter{ProjectID: projectKey(q.ProjectID), Limit: limit})
	if err != nil {
		return nil, err
	}
	s.decorate(list)
	return &ports.UsageRecords{Records: list}, nil
}

// decorate fills what a record carries beyond what is stored.
func (s *Service) decorate(list []*domain.UsageRecord) {
	titles := map[string]string{}
	for _, r := range list {
		r.EstimatedCostUSD = estimatedCost(r)
		if r.TaskID == "" {
			continue
		}
		title, ok := titles[r.TaskID]
		if !ok {
			if t := s.readers.Tickets.Get(r.TaskID); t != nil {
				title = t.Title
			}
			titles[r.TaskID] = title
		}
		r.TaskTitle = title
	}
}

// projectKey maps the API's project key to the stored project id.
func projectKey(p string) string {
	if p == SystemProject {
		return ""
	}
	return p
}

func invalid(msg string) error { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }
