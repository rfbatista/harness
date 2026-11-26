package ports

import (
	"context"

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
