package bench

import (
	"context"
	"strings"
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
	// An omitted list runs every case; a list that is present but empty is refused, so it never turns into a run of everything by accident.
	partial := req.CaseIDs != nil
	if partial && len(req.CaseIDs) == 0 {
		return domain.BenchRun{}, &domain.StructuredError{Code: "BENCH_CASE_IDS_INVALID", Message: "case_ids is empty; omit case_ids to run every case"}
	}
	if partial {
		if cases, err = pickCases(cases, req.CaseIDs); err != nil {
			return domain.BenchRun{}, err
		}
	}
	if req.Model == "" && s.d.SessionModel != nil {
		req.Model = s.d.SessionModel()
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

	go s.execute(runCtx, live, cases, setupReq, setup, partial)
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
func (s *Service) execute(ctx context.Context, live *liveRun, cases []domain.BenchCase, setupReq ports.BenchSetupRequest, setup Setup, partial bool) {
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
	}, scores, scorecardStatus(status, partial))
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

// scorecardStatus: a cancelled run holds only some cases, and so does one that
// was asked for only some; neither is a baseline.
func scorecardStatus(runStatus string, partial bool) string {
	switch {
	case runStatus == domain.BenchRunCancelled:
		return domain.ScorecardCancelled
	case partial:
		return domain.ScorecardPartial
	}
	return domain.ScorecardFinished
}

// pickCases are the project's cases with the ids, in the order the project
// lists them; an id that is none of them (unknown, or another project's) is
// refused, named.
func pickCases(all []domain.BenchCase, ids []string) ([]domain.BenchCase, error) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var picked []domain.BenchCase
	for _, c := range all {
		if want[c.ID] {
			picked = append(picked, c)
			delete(want, c.ID)
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for _, id := range ids {
			if want[id] {
				missing = append(missing, id)
				delete(want, id)
			}
		}
		return nil, &domain.StructuredError{Code: "BENCH_CASE_IDS_INVALID", Message: "not cases of this project: " + strings.Join(missing, ", ")}
	}
	return picked, nil
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

// CompareScorecard sets the scorecard against the baseline the person chose,
// or else the project's scorecard just before it.
func (s *Service) CompareScorecard(ctx context.Context, projectID, scorecardID, baselineID string) (domain.ScorecardComparison, error) {
	cur, err := s.GetScorecard(ctx, projectID, scorecardID)
	if err != nil {
		return domain.ScorecardComparison{}, err
	}
	if baselineID != "" {
		base, err := s.d.Scorecards.Find(ctx, baselineID)
		if err != nil {
			return domain.ScorecardComparison{}, err
		}
		if base == nil {
			return domain.ScorecardComparison{}, &domain.StructuredError{Code: "BENCH_SCORECARD_NOT_FOUND", Message: "scorecard not found: " + baselineID}
		}
		if base.ProjectID != projectID {
			return domain.ScorecardComparison{}, &domain.StructuredError{Code: "BENCH_BASELINE_OTHER_PROJECT", Message: "the baseline " + baselineID + " is a scorecard of another project"}
		}
		return Compare(*base, cur), nil
	}
	all, err := s.d.Scorecards.List(ctx, projectID)
	if err != nil {
		return domain.ScorecardComparison{}, err
	}
	// Newest first: the first finished one older than cur is the previous. A
	// cancelled or partial run holds only some of the cases, so it is no baseline.
	for _, c := range all {
		if c.ID != cur.ID && c.Status == domain.ScorecardFinished && c.StartedAt.Before(cur.StartedAt) {
			return Compare(c, cur), nil
		}
	}
	return domain.ScorecardComparison{}, &domain.StructuredError{Code: "BENCH_NO_PREVIOUS_SCORECARD", Message: "there is no earlier scorecard to compare with"}
}
