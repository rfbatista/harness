package sqlite

import (
	"time"

	"operators-mcp/internal/domain"
)

type SessionModel struct {
	ID               string  `gorm:"primaryKey"`
	ProjectID        string  `gorm:"column:project_id;index"`
	RepositoryID     string  `gorm:"column:repository_id;index"`
	AgentID          string  `gorm:"column:agent_id;index"`
	ZoneID           string  `gorm:"column:zone_id"`
	TicketID         string  `gorm:"column:ticket_id;index"`
	Task             string  `gorm:"column:task"`
	WorkingDir       string  `gorm:"column:working_dir"`
	Model            string  `gorm:"column:model"`
	Status           string  `gorm:"column:status;index"`
	CostUSD          float64 `gorm:"column:cost_usd"`
	InputTokens      int     `gorm:"column:input_tokens"`
	OutputTokens     int     `gorm:"column:output_tokens"`
	LastAction       string  `gorm:"column:last_action"`
	PendingApprovals int     `gorm:"column:pending_approvals"`
	AutoRun          bool    `gorm:"column:auto_run"`
	WorkspaceID      string  `gorm:"column:workspace_id;index"`
	Branch           string  `gorm:"column:branch"`
	Interactive      bool    `gorm:"column:interactive"`
	ClaudeSessionID  string  `gorm:"column:claude_session_id"`
	CreatedAt        int64   `gorm:"autoCreateTime:milli"`
	UpdatedAt        int64   `gorm:"autoUpdateTime:milli"`
}

func (SessionModel) TableName() string { return "sessions" }

func (m *SessionModel) ToDomain() *domain.Session {
	if m == nil {
		return nil
	}
	return &domain.Session{
		ID:               m.ID,
		ProjectID:        m.ProjectID,
		RepositoryID:     m.RepositoryID,
		AgentID:          m.AgentID,
		ZoneID:           m.ZoneID,
		TicketID:         m.TicketID,
		Task:             m.Task,
		WorkingDir:       m.WorkingDir,
		Model:            m.Model,
		Status:           domain.SessionStatus(m.Status),
		CostUSD:          m.CostUSD,
		InputTokens:      m.InputTokens,
		OutputTokens:     m.OutputTokens,
		LastAction:       m.LastAction,
		PendingApprovals: m.PendingApprovals,
		AutoRun:          m.AutoRun,
		WorkspaceID:      m.WorkspaceID,
		Branch:           m.Branch,
		Interactive:      m.Interactive,
		ClaudeSessionID:  m.ClaudeSessionID,
		CreatedAt:        time.UnixMilli(m.CreatedAt),
		UpdatedAt:        time.UnixMilli(m.UpdatedAt),
	}
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
