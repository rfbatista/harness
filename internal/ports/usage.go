package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// UsageFilter selects stored usage records. An empty ProjectID selects the
// records attributed to no project: the harness's own spend.
type UsageFilter struct {
	ProjectID string
	Limit     int
}

// UsageRepository stores usage records; one Create is one short transaction,
// given up when ctx ends.
type UsageRepository interface {
	Create(ctx context.Context, r *domain.UsageRecord) error
	// ListRecent answers the filter's records, newest first.
	ListRecent(f UsageFilter) ([]*domain.UsageRecord, error)
	UsageAggregates
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

// UsageDashboard is what the web pages read of token usage for their seeds:
// the Usage page's summary over a range and recent records, and a task's or
// session's totals for the task page.
type UsageDashboard interface {
	ListRecentUsage(ctx context.Context, q UsageQuery) (*UsageRecords, error)
	UsageSummary(ctx context.Context, projectID string, r UsageRange) (*UsageSummary, error)
	UsageTotals(ctx context.Context, q UsageTotalsQuery) (*UsageTotals, error)
}

// UsageReader is what the API reads of token usage.
type UsageReader interface {
	UsageDashboard
	// FollowUsage streams each usage record stored for a project, until ctx
	// ends or the follower falls behind; then the channel closes.
	FollowUsage(ctx context.Context, projectID string) (<-chan *domain.UsageRecord, error)
}

// UsageGroupFilter selects the records to aggregate: one project's since a
// time, or one task's or session's over all time. LongAbove splits each
// group by whether a record's prompt (input and cache tokens) exceeds it,
// for prices that change with prompt size.
type UsageGroupFilter struct {
	ProjectID string
	TaskID    string
	SessionID string
	Since     time.Time
	LongAbove int64
}

// UsageSums are token sums over a set of records.
type UsageSums struct {
	Requests                 int64
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

// UsageGroup is the sums of one task's, session's and model's records, on
// one side of the filter's LongAbove.
type UsageGroup struct {
	TaskID    string
	SessionID string
	Model     string
	Long      bool
	UsageSums
}

// UsageSeriesPoint is the sums of one model's records in one time bucket,
// which starts at Start.
type UsageSeriesPoint struct {
	Start time.Time
	Model string
	UsageSums
}

// UsageBucket is the width of a series bucket, in local time.
type UsageBucket string

const (
	UsageBucketHour UsageBucket = "hour"
	UsageBucketDay  UsageBucket = "day"
)

// UsageAggregates is what the repository sums in SQL.
type UsageAggregates interface {
	// Groups sums the filter's records per task, session, model and prompt
	// size.
	Groups(f UsageGroupFilter) ([]UsageGroup, error)
	// Series sums a project's records since a time per bucket and model,
	// oldest first.
	Series(projectID string, since time.Time, bucket UsageBucket) ([]UsageSeriesPoint, error)
}

// UsageRange is a span the usage summary covers, ending now: today since
// local midnight, or the last 7 or 30 days counting today.
type UsageRange string

const (
	UsageToday UsageRange = "today"
	Usage7d    UsageRange = "7d"
	Usage30d   UsageRange = "30d"
)

// UsageTotals are token totals with their estimated cost: the sum over the
// records whose model has a price, nil when no tokens were priced at all.
// UnpricedTokens is how many tokens it leaves out.
type UsageTotals struct {
	Requests                 int64    `json:"requests"`
	InputTokens              int64    `json:"input_tokens"`
	OutputTokens             int64    `json:"output_tokens"`
	CacheCreationInputTokens int64    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64    `json:"cache_read_input_tokens"`
	TotalTokens              int64    `json:"total_tokens"`
	EstimatedCostUSD         *float64 `json:"estimated_cost_usd"`
	UnpricedTokens           int64    `json:"unpriced_tokens"`
}

// UsageSeriesTokens is one model's tokens in one bucket of the summary.
type UsageSeriesTokens struct {
	Start                    time.Time `json:"start"`
	Model                    string    `json:"model"`
	InputTokens              int64     `json:"input_tokens"`
	OutputTokens             int64     `json:"output_tokens"`
	CacheCreationInputTokens int64     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64     `json:"cache_read_input_tokens"`
	TotalTokens              int64     `json:"total_tokens"`
}

// UsageByTask is one task's share of the summary.
type UsageByTask struct {
	TaskID    string      `json:"task_id"`
	TaskTitle string      `json:"task_title"`
	Totals    UsageTotals `json:"totals"`
}

// UsageBySession is one session's share of the summary.
type UsageBySession struct {
	SessionID string      `json:"session_id"`
	TaskID    string      `json:"task_id"`
	TaskTitle string      `json:"task_title"`
	Totals    UsageTotals `json:"totals"`
}

// UsageSummary is a project's usage over a range: totals, tokens over time
// split by model, and the top tasks and sessions by tokens.
type UsageSummary struct {
	Range       UsageRange          `json:"range"`
	Since       time.Time           `json:"since"`
	Bucket      UsageBucket         `json:"bucket"`
	Totals      UsageTotals         `json:"totals"`
	Series      []UsageSeriesTokens `json:"series"`
	TopTasks    []UsageByTask       `json:"top_tasks"`
	TopSessions []UsageBySession    `json:"top_sessions"`
}

// UsageTotalsQuery names exactly one task or one session.
type UsageTotalsQuery struct {
	TaskID    string
	SessionID string
}
