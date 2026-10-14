package domain

import "time"

// UsageRecord is one finished LLM request as the harness stores it (see the
// Glossary's "Usage monitoring"): when, its attribution resolved to session,
// task and project, the model, the four token kinds, duration, status and
// whether it streamed. It never holds a request or response body.
type UsageRecord struct {
	ID          string `json:"id"`
	Attribution string `json:"attribution"`
	// ProjectID, TaskID and SessionID are what the attribution resolved to;
	// all empty for the harness's own spend and for an attribution that names
	// no session. TaskTitle is read with the record, never stored.
	ProjectID                string    `json:"project_id"`
	TaskID                   string    `json:"task_id"`
	TaskTitle                string    `json:"task_title"`
	SessionID                string    `json:"session_id"`
	Method                   string    `json:"method"`
	Path                     string    `json:"path"`
	Model                    string    `json:"model"`
	Status                   int       `json:"status"`
	Streamed                 bool      `json:"streamed"`
	StartedAt                time.Time `json:"started_at"`
	DurationMS               int64     `json:"duration_ms"`
	InputTokens              int64     `json:"input_tokens"`
	OutputTokens             int64     `json:"output_tokens"`
	CacheCreationInputTokens int64     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64     `json:"cache_read_input_tokens"`
	Error                    string    `json:"error"`
	// EstimatedCostUSD comes from the harness's price table when the record
	// is read; nil when the model has no price.
	EstimatedCostUSD *float64 `json:"estimated_cost_usd"`
}

// TotalTokens is every token the request used, of any kind.
func (r *UsageRecord) TotalTokens() int64 {
	return r.InputTokens + r.OutputTokens + r.CacheCreationInputTokens + r.CacheReadInputTokens
}
