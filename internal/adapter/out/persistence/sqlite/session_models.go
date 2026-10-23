package sqlite

import (
	"strings"
	"time"

	"operators-mcp/internal/domain"
)

type SessionModel struct {
	ID                       string  `gorm:"primaryKey"`
	ProjectID                string  `gorm:"column:project_id;index"`
	RepositoryID             string  `gorm:"column:repository_id;index"`
	AgentID                  string  `gorm:"column:agent_id;index"`
	ZoneID                   string  `gorm:"column:zone_id"`
	TicketID                 string  `gorm:"column:ticket_id;index"`
	Task                     string  `gorm:"column:task"`
	WorkingDir               string  `gorm:"column:working_dir"`
	Model                    string  `gorm:"column:model"`
	Status                   string  `gorm:"column:status;index"`
	CostUSD                  float64 `gorm:"column:cost_usd"`
	InputTokens              int     `gorm:"column:input_tokens"`
	OutputTokens             int     `gorm:"column:output_tokens"`
	CacheCreationInputTokens int     `gorm:"column:cache_creation_input_tokens"`
	CacheReadInputTokens     int     `gorm:"column:cache_read_input_tokens"`
	LastAction               string  `gorm:"column:last_action"`
	PendingApprovals         int     `gorm:"column:pending_approvals"`
	AutoRun                  bool    `gorm:"column:auto_run"`
	WorkspaceID              string  `gorm:"column:workspace_id;index"`
	Branch                   string  `gorm:"column:branch"`
	Interactive              bool    `gorm:"column:interactive"`
	ClaudeSessionID          string  `gorm:"column:claude_session_id"`
	RunsOn                   string  `gorm:"column:runs_on;index"`
	RunnerHost               string  `gorm:"column:runner_host"`
	ParentSessionID          string  `gorm:"column:parent_session_id"`
	Mode                     string  `gorm:"column:mode"`
	CreatedAt                int64   `gorm:"autoCreateTime:milli"`
	UpdatedAt                int64   `gorm:"autoUpdateTime:milli"`

	// TokenSavers and TokenSaversNotInstalled are comma-separated saver names.
	TokenSavers             string `gorm:"column:token_savers"`
	TokenSaversNotInstalled string `gorm:"column:token_savers_not_installed"`
	CavemanIntensity        string `gorm:"column:caveman_intensity"`

	// The sandbox facts. A visibility of "" is a session from before the
	// sandbox: it has none.
	Sandboxed         bool   `gorm:"column:sandboxed"`
	SandboxVisibility string `gorm:"column:sandbox_visibility"`
	SandboxDriver     string `gorm:"column:sandbox_driver"`
}

func (m *SessionModel) setSandbox(f *domain.SandboxFacts) {
	if f == nil {
		m.Sandboxed, m.SandboxVisibility, m.SandboxDriver = false, "", ""
		return
	}
	m.Sandboxed, m.SandboxVisibility, m.SandboxDriver = f.Sandboxed, f.Visibility, f.Driver
}

func (m *SessionModel) sandbox() *domain.SandboxFacts {
	if m.SandboxVisibility == "" {
		return nil
	}
	return &domain.SandboxFacts{Sandboxed: m.Sandboxed, Visibility: m.SandboxVisibility, Driver: m.SandboxDriver}
}

func (SessionModel) TableName() string { return "sessions" }

func (m *SessionModel) ToDomain() *domain.Session {
	if m == nil {
		return nil
	}
	return &domain.Session{
		ID:                       m.ID,
		ProjectID:                m.ProjectID,
		RepositoryID:             m.RepositoryID,
		AgentID:                  m.AgentID,
		ZoneID:                   m.ZoneID,
		TicketID:                 m.TicketID,
		Task:                     m.Task,
		WorkingDir:               m.WorkingDir,
		Model:                    m.Model,
		Status:                   domain.SessionStatus(m.Status),
		CostUSD:                  m.CostUSD,
		InputTokens:              m.InputTokens,
		OutputTokens:             m.OutputTokens,
		CacheCreationInputTokens: m.CacheCreationInputTokens,
		CacheReadInputTokens:     m.CacheReadInputTokens,
		LastAction:               m.LastAction,
		PendingApprovals:         m.PendingApprovals,
		AutoRun:                  m.AutoRun,
		WorkspaceID:              m.WorkspaceID,
		Branch:                   m.Branch,
		Interactive:              m.Interactive,
		ClaudeSessionID:          m.ClaudeSessionID,
		RunsOn:                   domain.Runner(m.RunsOn),
		RunnerHost:               m.RunnerHost,
		ParentSessionID:          m.ParentSessionID,
		Mode:                     domain.SessionMode(m.Mode),
		TokenSavers:              splitSavers(m.TokenSavers),
		TokenSaversNotInstalled:  splitSavers(m.TokenSaversNotInstalled),
		CavemanIntensity:         domain.CavemanIntensity(m.CavemanIntensity),
		Sandbox:                  m.sandbox(),
		CreatedAt:                time.UnixMilli(m.CreatedAt),
		UpdatedAt:                time.UnixMilli(m.UpdatedAt),
	}
}

// joinSavers is savers as a session row stores them.
func joinSavers(savers []domain.TokenSaver) string {
	names := make([]string, len(savers))
	for i, s := range savers {
		names[i] = string(s)
	}
	return strings.Join(names, ",")
}

func splitSavers(s string) []domain.TokenSaver {
	if s == "" {
		return nil
	}
	var out []domain.TokenSaver
	for _, name := range strings.Split(s, ",") {
		out = append(out, domain.TokenSaver(name))
	}
	return out
}

type SessionEventModel struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	SessionID string `gorm:"column:session_id;index"`
	Seq       int64  `gorm:"column:seq;index"`
	Type      string `gorm:"column:type"`
	Payload   []byte `gorm:"column:payload"`
	CreatedAt int64  `gorm:"autoCreateTime:milli"`
}

func (SessionEventModel) TableName() string { return "session_events" }
