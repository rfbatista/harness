package sqlite

import (
	"encoding/json"
	"time"

	"operators-mcp/internal/domain"
)

// The architect channel's tables. None has a foreign key to sessions:
// messages and review requests outlive the sessions they name.

// TaskMessageModel is the GORM model for domain.TaskMessage. Document and
// artifact ids are stored as JSON arrays.
type TaskMessageModel struct {
	ID            string `gorm:"primaryKey"`
	TaskID        string `gorm:"column:task_id;index:idx_task_messages_task_created"`
	ProjectID     string `gorm:"column:project_id"`
	FromSessionID string `gorm:"column:from_session_id;index"`
	ToSessionID   string `gorm:"column:to_session_id;index"`
	Kind          string `gorm:"column:kind"`
	Subject       string `gorm:"column:subject"`
	Body          string `gorm:"column:body"`
	Status        string `gorm:"column:status"`
	Verdict       string `gorm:"column:verdict"`
	InReplyTo     string `gorm:"column:in_reply_to"`
	DocumentIDs   string `gorm:"column:document_ids"`
	ArtifactIDs   string `gorm:"column:artifact_ids"`
	Delivered     bool   `gorm:"column:delivered"`
	DeliveredAt   int64  `gorm:"column:delivered_at"`
	Queued        bool   `gorm:"column:queued;index"`
	CreatedAt     int64  `gorm:"column:created_at;index:idx_task_messages_task_created"`
}

func (TaskMessageModel) TableName() string { return "task_messages" }

func (m *TaskMessageModel) ToDomain() *domain.TaskMessage {
	return &domain.TaskMessage{
		ID: m.ID, TaskID: m.TaskID, ProjectID: m.ProjectID, FromSessionID: m.FromSessionID, ToSessionID: m.ToSessionID,
		Kind: domain.TaskMessageKind(m.Kind), Subject: m.Subject, Body: m.Body,
		Status: domain.ReportStatus(m.Status), Verdict: domain.Verdict(m.Verdict), InReplyTo: m.InReplyTo,
		DocumentIDs: idsFromJSON(m.DocumentIDs), ArtifactIDs: idsFromJSON(m.ArtifactIDs),
		Delivered: m.Delivered, DeliveredAt: timeFromMilli(m.DeliveredAt), Queued: m.Queued,
		CreatedAt: time.UnixMilli(m.CreatedAt),
	}
}

// ReviewRequestModel is the GORM model for domain.ReviewRequest.
type ReviewRequestModel struct {
	ID                 string `gorm:"primaryKey"`
	TaskID             string `gorm:"column:task_id;index:idx_review_requests_task_state"`
	ProjectID          string `gorm:"column:project_id;index:idx_review_requests_project_state"`
	ArchitectSessionID string `gorm:"column:architect_session_id"`
	AboutSessionID     string `gorm:"column:about_session_id"`
	Subject            string `gorm:"column:subject"`
	Body               string `gorm:"column:body"`
	DocumentIDs        string `gorm:"column:document_ids"`
	ArtifactIDs        string `gorm:"column:artifact_ids"`
	State              string `gorm:"column:state;index:idx_review_requests_task_state;index:idx_review_requests_project_state"`
	ResponseNote       string `gorm:"column:response_note"`
	RespondedAt        int64  `gorm:"column:responded_at"`
	CreatedAt          int64  `gorm:"column:created_at"`
	UpdatedAt          int64  `gorm:"column:updated_at"`
}

func (ReviewRequestModel) TableName() string { return "review_requests" }

func (m *ReviewRequestModel) ToDomain() *domain.ReviewRequest {
	return &domain.ReviewRequest{
		ID: m.ID, TaskID: m.TaskID, ProjectID: m.ProjectID, ArchitectSessionID: m.ArchitectSessionID,
		AboutSessionID: m.AboutSessionID, Subject: m.Subject, Body: m.Body,
		DocumentIDs: idsFromJSON(m.DocumentIDs), ArtifactIDs: idsFromJSON(m.ArtifactIDs),
		State: domain.ReviewState(m.State), ResponseNote: m.ResponseNote, RespondedAt: timeFromMilli(m.RespondedAt),
		CreatedAt: time.UnixMilli(m.CreatedAt), UpdatedAt: time.UnixMilli(m.UpdatedAt),
	}
}

// StatusCheckModel is the GORM model for domain.StatusCheck: one loop per
// delegate.
type StatusCheckModel struct {
	DelegateSessionID  string `gorm:"primaryKey;column:delegate_session_id"`
	TaskID             string `gorm:"column:task_id;index"`
	ProjectID          string `gorm:"column:project_id"`
	ArchitectSessionID string `gorm:"column:architect_session_id;index"`
	EveryMinutes       int    `gorm:"column:every_minutes"`
	NextAt             int64  `gorm:"column:next_at;index:idx_status_checks_state_next"`
	LastFiredAt        int64  `gorm:"column:last_fired_at"`
	FiredCount         int    `gorm:"column:fired_count"`
	State              string `gorm:"column:state;index:idx_status_checks_state_next"`
}

func (StatusCheckModel) TableName() string { return "status_checks" }

func (m *StatusCheckModel) ToDomain() *domain.StatusCheck {
	return &domain.StatusCheck{
		TaskID: m.TaskID, ProjectID: m.ProjectID, ArchitectSessionID: m.ArchitectSessionID,
		DelegateSessionID: m.DelegateSessionID, EveryMinutes: m.EveryMinutes,
		NextAt: timeFromMilli(m.NextAt), LastFiredAt: timeFromMilli(m.LastFiredAt),
		FiredCount: m.FiredCount, State: domain.StatusCheckState(m.State),
	}
}

func statusCheckModel(c *domain.StatusCheck) *StatusCheckModel {
	return &StatusCheckModel{
		DelegateSessionID: c.DelegateSessionID, TaskID: c.TaskID, ProjectID: c.ProjectID,
		ArchitectSessionID: c.ArchitectSessionID, EveryMinutes: c.EveryMinutes,
		NextAt: milliOf(c.NextAt), LastFiredAt: milliOf(c.LastFiredAt), FiredCount: c.FiredCount, State: string(c.State),
	}
}

// TicketStatusChangeModel is one row of a task's status history.
type TicketStatusChangeModel struct {
	ID          uint   `gorm:"primaryKey;autoIncrement"`
	TicketID    string `gorm:"column:ticket_id;index:idx_ticket_status_changes_ticket_at"`
	ProjectID   string `gorm:"column:project_id"`
	Status      string `gorm:"column:status"`
	Reason      string `gorm:"column:reason"`
	By          string `gorm:"column:by"`
	BySessionID string `gorm:"column:by_session_id"`
	At          int64  `gorm:"column:at;index:idx_ticket_status_changes_ticket_at"`
}

func (TicketStatusChangeModel) TableName() string { return "ticket_status_changes" }

func (m *TicketStatusChangeModel) ToDomain() *domain.TaskStatusChange {
	return &domain.TaskStatusChange{
		TaskID: m.TicketID, ProjectID: m.ProjectID, Status: domain.TicketStatus(m.Status), Reason: m.Reason,
		By: domain.StatusChanger(m.By), BySessionID: m.BySessionID, At: time.UnixMilli(m.At),
	}
}

// idsToJSON stores an id list; nil and empty both read back as empty, so the
// wire always says [] rather than null.
func idsToJSON(ids []string) string {
	if len(ids) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func idsFromJSON(s string) []string {
	out := []string{}
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func timeFromMilli(ms int64) *time.Time {
	if ms == 0 {
		return nil
	}
	t := time.UnixMilli(ms)
	return &t
}

func milliOf(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.UnixMilli()
}
