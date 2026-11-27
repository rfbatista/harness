// Package bench is the eval bench (ADR 0015): a project's bench cases, and
// the runs and scorecards built on them.
package bench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BenchCases = (*Service)(nil)

// What bounds a case that its deps do not set: a run that does not end or an
// agent that loops would otherwise hold the project's one run for ever.
const (
	DefaultCaseTimeout = 10 * time.Minute
	DefaultMaxTurns    = 30
)

// Deps are what the bench is built on.
type Deps struct {
	Cases ports.BenchCaseStore
	// Sessions runs the CLI for a case; ServeMock offers a case's mock layer
	// as the one MCP server the run is given (URL and a stop). Both nil: the
	// service keeps cases only and refuses to run.
	Sessions  ports.BenchSessions
	ServeMock func(*MockLayer) (url string, stop func(), err error)
	// Judge is the model that judges outcome and trajectory; nil leaves those
	// patterns unscored.
	Judge ports.BenchModel
	// Scorecards keep the scorecard of each run; Settings gives the judge
	// model (the project's effective critic model); Feed announces a run's
	// progress and end. Each may be nil: no scorecard is kept, the judge model
	// is "", nothing is announced.
	Scorecards ports.BenchScorecardStore
	Settings   interface {
		Effective(ctx context.Context, projectID, repositoryID string) (domain.EffectiveAgentSystem, error)
	}
	Feed interface{ AnnounceChange(ports.ProjectChange) }
	// Trajectories keeps each task's recording choice and the trajectories
	// recorded; Tasks finds a task; SessionLog reads a session and its stored
	// events. Each may be nil: nothing is recorded.
	Trajectories ports.BenchTrajectoryStore
	Tasks        interface {
		Get(ctx context.Context, id string) *domain.Ticket
	}
	SessionLog interface {
		Find(ctx context.Context, id string) (*domain.Session, error)
		ListEvents(sessionID string, fromSeq int64) ([]ports.StoredEvent, error)
	}
	// SessionModel is the model a session of the project runs on; a run whose
	// request names none uses it. Nil: the CLI's own.
	SessionModel func() string
	// MockName is the mock server's name in the CLI's MCP configuration.
	MockName string
	// CaseTimeout is the longest one case may run before it is stopped and
	// scored as a failure; MaxTurns the most turns its CLI may take. Zero:
	// DefaultCaseTimeout and DefaultMaxTurns.
	CaseTimeout time.Duration
	MaxTurns    int
	Projects interface {
		Get(ctx context.Context, id string) *domain.Project
	}
}

// Service is the eval bench.
type Service struct {
	d Deps

	mu     sync.Mutex
	runs   map[string]*liveRun // by run id
	active map[string]string   // project id → the run going on in it
}

// New returns the bench.
func New(d Deps) *Service {
	return &Service{d: d, runs: map[string]*liveRun{}, active: map[string]string{}}
}

func (s *Service) checkProject(ctx context.Context, projectID string) error {
	if s.d.Projects.Get(ctx, projectID) == nil {
		return &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found: " + projectID}
	}
	return nil
}

// ListCases answers the project's cases.
func (s *Service) ListCases(ctx context.Context, projectID string) ([]domain.BenchCase, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.d.Cases.List(ctx, projectID)
}

// CreateCase validates and stores a case from a definition.
func (s *Service) CreateCase(ctx context.Context, projectID string, def domain.BenchCase) (domain.BenchCase, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.BenchCase{}, err
	}
	def.ProjectID = projectID
	if def.Source == "" {
		def.Source = domain.BenchSourceDefinition
	}
	if def.Tools == nil {
		def.Tools = []domain.BenchTool{}
	}
	if verr := def.Validate(); verr != nil {
		return domain.BenchCase{}, verr
	}
	def.ID = newID()
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	if err := s.d.Cases.Save(ctx, def); err != nil {
		return domain.BenchCase{}, err
	}
	return def, nil
}

// UpdateExpectation replaces a case's expectation, validating the case as a
// definition is.
func (s *Service) UpdateExpectation(ctx context.Context, projectID, caseID string, expect domain.BenchExpectation) (domain.BenchCase, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.BenchCase{}, err
	}
	cases, err := s.d.Cases.List(ctx, projectID)
	if err != nil {
		return domain.BenchCase{}, err
	}
	for _, c := range cases {
		if c.ID != caseID {
			continue
		}
		c.Expect = expect
		if verr := c.Validate(); verr != nil {
			return domain.BenchCase{}, verr
		}
		if err := s.d.Cases.Save(ctx, c); err != nil {
			return domain.BenchCase{}, err
		}
		return c, nil
	}
	return domain.BenchCase{}, &domain.StructuredError{Code: "BENCH_CASE_NOT_FOUND", Message: "bench case not found: " + caseID}
}

// DeleteCase removes a case of the project.
func (s *Service) DeleteCase(ctx context.Context, projectID, caseID string) error {
	if err := s.checkProject(ctx, projectID); err != nil {
		return err
	}
	removed, err := s.d.Cases.Delete(ctx, projectID, caseID)
	if err != nil {
		return err
	}
	if !removed {
		return &domain.StructuredError{Code: "BENCH_CASE_NOT_FOUND", Message: "bench case not found: " + caseID}
	}
	return nil
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Subscribe has the bench follow the event bus: a session that ends is
// recorded when its task is, and a deleted project takes its cases, scorecards
// and trajectories with it.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.SessionEnded) error {
		return s.RecordSession(ctx, ev.SessionID)
	})
	ports.On(sub, func(ctx context.Context, ev domain.ProjectDeleted) error {
		if s.d.Cases != nil {
			if err := s.d.Cases.DeleteProject(ctx, ev.ProjectID); err != nil {
				return err
			}
		}
		if s.d.Scorecards != nil {
			if err := s.d.Scorecards.DeleteProject(ctx, ev.ProjectID); err != nil {
				return err
			}
		}
		if s.d.Trajectories != nil {
			return s.d.Trajectories.DeleteProject(ctx, ev.ProjectID)
		}
		return nil
	})
}
