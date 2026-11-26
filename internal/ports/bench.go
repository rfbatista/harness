package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// BenchCases is a project's bench cases as the HTTP API uses them (ADR 0015).
type BenchCases interface {
	// ListCases answers the project's cases, oldest first.
	ListCases(ctx context.Context, projectID string) ([]domain.BenchCase, error)
	// CreateCase stores a case from a definition and answers it with its id; a
	// refused definition is a *domain.BenchCaseValidationError.
	CreateCase(ctx context.Context, projectID string, def domain.BenchCase) (domain.BenchCase, error)
	// DeleteCase removes a case of the project; an unknown one is BENCH_CASE_NOT_FOUND.
	DeleteCase(ctx context.Context, projectID, caseID string) error
}

// BenchCaseStore persists bench cases (driven; sqlite implements it).
type BenchCaseStore interface {
	List(ctx context.Context, projectID string) ([]domain.BenchCase, error)
	Save(ctx context.Context, c domain.BenchCase) error
	// Delete reports whether a case of that project was removed.
	Delete(ctx context.Context, projectID, caseID string) (bool, error)
	// DeleteProject removes all the project's cases.
	DeleteProject(ctx context.Context, projectID string) error
}

// BenchSetupRequest names the agent setup a bench run runs on: the project's,
// as a session in one of its repositories would receive it. An empty
// RepositoryID is the project's first repository, an empty AgentID plain
// claude, an empty Model the CLI's own.
type BenchSetupRequest struct {
	ProjectID    string
	RepositoryID string
	AgentID      string
	Model        string
}

// BenchSetupFile is a skill file or an instruction file a setup delivers.
type BenchSetupFile struct {
	Path    string
	Content string
}

// BenchSetupInfo is what a setup is made of, for the scorecard's fingerprint.
type BenchSetupInfo struct {
	AgentName    string
	AgentPrompt  string
	Skills       []BenchSetupFile
	Instructions []BenchSetupFile
	Model        string
	CLIVersion   string
}

// BenchSessionRequest is one prompt run on a setup with the one MCP server
// the run is given in place of every other.
type BenchSessionRequest struct {
	BenchSetupRequest
	Prompt  string
	MCPName string
	MCPURL  string
}

// BenchSessionResult is how a run ended: the final result the CLI reported,
// what it cost and how long it took.
type BenchSessionResult struct {
	FinalText string
	CostUSD   float64
	Duration  time.Duration
}

// BenchSessions runs the real claude CLI headless for a bench case (ADR 0015),
// in a throwaway directory with no task, worktree or repository of the
// project, on the setup the project's sessions get.
type BenchSessions interface {
	// Setup describes what a run on the request would be set up with.
	Setup(ctx context.Context, req BenchSetupRequest) (BenchSetupInfo, error)
	// Run sends the prompt and waits for the CLI's result.
	Run(ctx context.Context, req BenchSessionRequest) (BenchSessionResult, error)
}

// BenchModel is a model the bench asks to judge (driven): one stateless turn
// of text in, text out, on the named model. The scorecard names the model
// that judged it.
type BenchModel interface {
	Ask(ctx context.Context, model, system, input string) (string, error)
}

// BenchRuns starts, follows and ends a project's bench runs, and reads their
// scorecards, as the HTTP API and the CLI use them (ADR 0015).
type BenchRuns interface {
	// StartRun starts a run over all the project's cases and answers it at once,
	// running. A project with no cases is BENCH_NO_CASES, one that already has a
	// run going BENCH_RUN_IN_PROGRESS.
	StartRun(ctx context.Context, projectID string, req domain.BenchRunRequest) (domain.BenchRun, error)
	GetRun(ctx context.Context, projectID, runID string) (domain.BenchRun, error)
	// CancelRun ends a running run: the CLI is stopped and the scorecard of the
	// cases already done is kept, marked cancelled.
	CancelRun(ctx context.Context, projectID, runID string) (domain.BenchRun, error)
	ListScorecards(ctx context.Context, projectID string) ([]domain.Scorecard, error)
	GetScorecard(ctx context.Context, projectID, scorecardID string) (domain.Scorecard, error)
	// CompareScorecard sets the scorecard against the project's previous one;
	// with none before it, BENCH_NO_PREVIOUS_SCORECARD.
	CompareScorecard(ctx context.Context, projectID, scorecardID string) (domain.ScorecardComparison, error)
}

// BenchScorecardStore persists scorecards (driven; sqlite implements it).
type BenchScorecardStore interface {
	Save(ctx context.Context, card domain.Scorecard) error
	// Get answers nil when the project has no such scorecard.
	Get(ctx context.Context, projectID, id string) (*domain.Scorecard, error)
	// List answers the project's scorecards, newest first.
	List(ctx context.Context, projectID string) ([]domain.Scorecard, error)
	// DeleteProject removes all the project's scorecards.
	DeleteProject(ctx context.Context, projectID string) error
}

// BenchRunChange is a bench run's progress or end on the project feed, as
// "bench_run". ProjectID routes it and is not on the wire.
type BenchRunChange struct {
	RunID       string `json:"run_id"`
	Status      string `json:"status"`
	Done        int    `json:"done"`
	Total       int    `json:"total"`
	ScorecardID string `json:"scorecard_id,omitempty"`
	ProjectID   string `json:"-"`
}

// BenchTrajectories records the sessions of tasks opted in to trajectory
// recording and turns a recording into a case (ADR 0015).
type BenchTrajectories interface {
	// TaskRecording is whether the task's sessions are recorded. An unset task
	// follows the effective settings default of its project, or of the
	// repository when one is named.
	TaskRecording(ctx context.Context, projectID, taskID, repositoryID string) (domain.TaskTrajectoryRecording, error)
	// SetTaskRecording opts the task in or out; nil makes it follow the default.
	SetTaskRecording(ctx context.Context, projectID, taskID string, enabled *bool) (domain.TaskTrajectoryRecording, error)
	ListTrajectories(ctx context.Context, projectID string) ([]domain.BenchTrajectory, error)
	GetTrajectory(ctx context.Context, projectID, trajectoryID string) (domain.BenchTrajectory, error)
	// CreateCaseFromTrajectory stores a case that replays the recording.
	CreateCaseFromTrajectory(ctx context.Context, projectID, trajectoryID string) (domain.BenchCase, error)
}

// BenchTrajectoryStore persists trajectories and each task's own recording
// choice (driven; sqlite implements it).
type BenchTrajectoryStore interface {
	SaveTrajectory(ctx context.Context, t domain.BenchTrajectory) error
	// GetTrajectory answers nil when the project has none by that id.
	GetTrajectory(ctx context.Context, projectID, id string) (*domain.BenchTrajectory, error)
	// ListTrajectories answers the project's, newest first.
	ListTrajectories(ctx context.Context, projectID string) ([]domain.BenchTrajectory, error)
	// TaskFlag is the task's own choice, nil when it made none.
	TaskFlag(ctx context.Context, taskID string) (*bool, error)
	// SetTaskFlag stores the choice, or clears it with nil.
	SetTaskFlag(ctx context.Context, projectID, taskID string, enabled *bool) error
	// DeleteProject removes the project's trajectories and its tasks' choices.
	DeleteProject(ctx context.Context, projectID string) error
}
