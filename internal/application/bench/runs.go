package bench

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BenchRuns = (*Service)(nil)

// liveRun is a run in this process: how far it is, and what ends it.
type liveRun struct {
	run    domain.BenchRun
	cancel context.CancelFunc
}

// StartRun starts a run over all the project's cases and answers at once.
func (s *Service) StartRun(ctx context.Context, projectID string, req domain.BenchRunRequest) (domain.BenchRun, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.BenchRun{}, err
	}
	if s.d.Sessions == nil || s.d.ServeMock == nil {
		return domain.BenchRun{}, &domain.StructuredError{Code: "BENCH_UNAVAILABLE", Message: "this server cannot run an eval bench"}
	}
	cases, err := s.d.Cases.List(ctx, projectID)
	if err != nil {
		return domain.BenchRun{}, err
	}
	if len(cases) == 0 {
		return domain.BenchRun{}, &domain.StructuredError{Code: "BENCH_NO_CASES", Message: "the project has no bench cases to run"}
	}
	setupReq := ports.BenchSetupRequest{ProjectID: projectID, RepositoryID: req.RepositoryID, AgentID: req.AgentID, Model: req.Model}
	info, err := s.d.Sessions.Setup(ctx, setupReq)
	if err != nil {
		return domain.BenchRun{}, err
	}
	judgeModel := s.judgeModel(ctx, projectID)
	setup := SetupOf(info, judgeModel)

	run := domain.BenchRun{ID: newID(), ProjectID: projectID, Status: domain.BenchRunRunning, Total: len(cases), StartedAt: time.Now().UTC()}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	if other, busy := s.active[projectID]; busy {
		s.mu.Unlock()
		cancel()
		return domain.BenchRun{}, &domain.StructuredError{Code: "BENCH_RUN_IN_PROGRESS", Message: "the project already has a bench run going: " + other}
	}
	live := &liveRun{run: run, cancel: cancel}
	s.runs[run.ID] = live
	s.active[projectID] = run.ID
	s.mu.Unlock()

	go s.execute(runCtx, live, cases, setupReq, setup)
	return run, nil
}

// judgeModel is the project's effective critic model, the model that judges.
func (s *Service) judgeModel(ctx context.Context, projectID string) string {
	if s.d.Settings == nil {
		return ""
	}
	eff, err := s.d.Settings.Effective(ctx, projectID, "")
	if err != nil {
		return ""
	}
	return eff.Review.Critic.Model.Value
}

// execute runs each case in turn and ends the run with its scorecard.
func (s *Service) execute(ctx context.Context, live *liveRun, cases []domain.BenchCase, setupReq ports.BenchSetupRequest, setup Setup) {
	run := live.run
	judgeModel := setup.JudgeModel
	var scores []domain.BenchCaseScore
	status := domain.BenchRunFinished
	for _, c := range cases {
		if ctx.Err() != nil {
			status = domain.BenchRunCancelled
			break
		}
		cr, err := s.RunCase(ctx, setupReq, c)
		if ctx.Err() != nil {
			status = domain.BenchRunCancelled
			break
		}
		if err != nil {
			scores = append(scores, domain.BenchCaseScore{CaseID: c.ID, CaseName: c.Name, Calls: []domain.BenchCall{}, Error: err.Error()})
		} else {
			scores = append(scores, s.ScoreCase(ctx, c, cr, judgeModel))
		}
		s.progress(live, func(r *domain.BenchRun) { r.Done = len(scores) })
	}

	finished := time.Now().UTC()
	card := NewScorecard(run.ProjectID, domain.BenchSetupRecord{
		Fingerprint: Fingerprint(setup), AgentName: setup.AgentName, Model: setup.Model, JudgeModel: setup.JudgeModel, CLIVersion: setup.CLIVersion,
	}, scores, scorecardStatus(status))
	card.ID, card.StartedAt, card.FinishedAt = run.ID, run.StartedAt, finished

	var saveErr error
	if s.d.Scorecards != nil {
		saveErr = s.d.Scorecards.Save(context.WithoutCancel(ctx), card)
	}
	s.mu.Lock()
	delete(s.active, run.ProjectID)
	s.mu.Unlock()
	s.progress(live, func(r *domain.BenchRun) {
		r.Status, r.FinishedAt = status, finished
		if saveErr != nil {
			r.Status, r.Error = domain.BenchRunFailed, "the scorecard was not stored: "+saveErr.Error()
		} else {
			r.ScorecardID = card.ID
		}
	})
}

func scorecardStatus(runStatus string) string {
	if runStatus == domain.BenchRunCancelled {
		return domain.ScorecardCancelled
	}
	return domain.ScorecardFinished
}

// progress changes the run under the lock and announces it.
func (s *Service) progress(live *liveRun, change func(*domain.BenchRun)) {
	s.mu.Lock()
	change(&live.run)
	r := live.run
	s.mu.Unlock()
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{BenchRun: &ports.BenchRunChange{
			RunID: r.ID, Status: r.Status, Done: r.Done, Total: r.Total, ScorecardID: r.ScorecardID, ProjectID: r.ProjectID,
		}})
	}
}

func (s *Service) liveRun(projectID, runID string) (*liveRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live, ok := s.runs[runID]
	if !ok || live.run.ProjectID != projectID {
		return nil, &domain.StructuredError{Code: "BENCH_RUN_NOT_FOUND", Message: "bench run not found: " + runID}
	}
	return live, nil
}

// GetRun answers a run of this server's.
func (s *Service) GetRun(ctx context.Context, projectID, runID string) (domain.BenchRun, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.BenchRun{}, err
	}
	live, err := s.liveRun(projectID, runID)
	if err != nil {
		return domain.BenchRun{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return live.run, nil
}

// CancelRun stops a running run: its context ends, which stops the CLI, and the
// scorecard of the cases done is kept, marked cancelled.
func (s *Service) CancelRun(ctx context.Context, projectID, runID string) (domain.BenchRun, error) {
	run, err := s.GetRun(ctx, projectID, runID)
	if err != nil {
		return run, err
	}
	if run.Status != domain.BenchRunRunning {
		return run, &domain.StructuredError{Code: "BENCH_RUN_NOT_RUNNING", Message: "the bench run is " + run.Status}
	}
	s.mu.Lock()
	live := s.runs[runID]
	s.mu.Unlock()
	live.cancel()
	return run, nil
}

// ListScorecards answers the project's scorecards, newest first.
func (s *Service) ListScorecards(ctx context.Context, projectID string) ([]domain.Scorecard, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return nil, err
	}
	if s.d.Scorecards == nil {
		return nil, nil
	}
	return s.d.Scorecards.List(ctx, projectID)
}

// GetScorecard answers one scorecard of the project.
func (s *Service) GetScorecard(ctx context.Context, projectID, scorecardID string) (domain.Scorecard, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.Scorecard{}, err
	}
	if s.d.Scorecards != nil {
		card, err := s.d.Scorecards.Get(ctx, projectID, scorecardID)
		if err != nil {
			return domain.Scorecard{}, err
		}
		if card != nil {
			return *card, nil
		}
	}
	return domain.Scorecard{}, &domain.StructuredError{Code: "BENCH_SCORECARD_NOT_FOUND", Message: "scorecard not found: " + scorecardID}
}

// CompareScorecard sets the scorecard against the project's scorecard just
// before it.
func (s *Service) CompareScorecard(ctx context.Context, projectID, scorecardID string) (domain.ScorecardComparison, error) {
	cur, err := s.GetScorecard(ctx, projectID, scorecardID)
	if err != nil {
		return domain.ScorecardComparison{}, err
	}
	all, err := s.d.Scorecards.List(ctx, projectID)
	if err != nil {
		return domain.ScorecardComparison{}, err
	}
	for _, c := range all { // newest first: the first one older than cur is the previous
		if c.ID != cur.ID && (c.StartedAt.Before(cur.StartedAt)) {
			return Compare(c, cur), nil
		}
	}
	return domain.ScorecardComparison{}, &domain.StructuredError{Code: "BENCH_NO_PREVIOUS_SCORECARD", Message: "there is no earlier scorecard to compare with"}
}
