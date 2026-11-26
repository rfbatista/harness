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
