package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// UsageFilter selects stored usage records. An empty ProjectID selects the
// records attributed to no project: the harness's own spend.
type UsageFilter struct {
	ProjectID string
	Limit     int
}

// UsageRepository stores usage records; one Create is one short transaction.
type UsageRepository interface {
	Create(r *domain.UsageRecord) error
	// ListRecent answers the filter's records, newest first.
	ListRecent(f UsageFilter) ([]*domain.UsageRecord, error)
}

// UsageQuery asks for a project's recent usage records. Limit 0 is the
// default.
type UsageQuery struct {
	ProjectID string
	Limit     int
}

// UsageRecords is a page of recent usage records.
type UsageRecords struct {
	Records []*domain.UsageRecord `json:"records"`
}

// UsageReader is what the API reads of token usage.
type UsageReader interface {
	ListRecentUsage(ctx context.Context, q UsageQuery) (*UsageRecords, error)
}
