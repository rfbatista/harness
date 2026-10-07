package sqlite

import (
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.TaskMessageRepository      = (*TaskMessageRepository)(nil)
	_ ports.ReviewRequestRepository    = (*ReviewRequestRepository)(nil)
	_ ports.StatusCheckRepository      = (*StatusCheckRepository)(nil)
	_ ports.TaskStatusChangeRepository = (*TaskStatusChangeRepository)(nil)
)

// TaskMessageRepository persists task messages in SQLite via GORM.
type TaskMessageRepository struct{ db *gorm.DB }

func NewTaskMessageRepository(db *gorm.DB) *TaskMessageRepository {
	return &TaskMessageRepository{db: db}
}

func (r *TaskMessageRepository) Create(msg *domain.TaskMessage) (*domain.TaskMessage, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	created := msg.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	m := &TaskMessageModel{
		ID: id, TaskID: msg.TaskID, ProjectID: msg.ProjectID, FromSessionID: msg.FromSessionID, ToSessionID: msg.ToSessionID,
		Kind: string(msg.Kind), Subject: msg.Subject, Body: msg.Body, Status: string(msg.Status), Verdict: string(msg.Verdict),
		InReplyTo: msg.InReplyTo, DocumentIDs: idsToJSON(msg.DocumentIDs), ArtifactIDs: idsToJSON(msg.ArtifactIDs),
		Delivered: msg.Delivered, DeliveredAt: milliOf(msg.DeliveredAt), Queued: msg.Queued, CreatedAt: created.UnixMilli(),
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TaskMessageRepository) Get(id string) *domain.TaskMessage {
	var m TaskMessageModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *TaskMessageRepository) List(taskID string, f ports.TaskMessageFilter) []*domain.TaskMessage {
	q := r.db.Model(&TaskMessageModel{}).Where("task_id = ?", taskID)
	if f.SessionID != "" {
		q = q.Where("from_session_id = ? OR to_session_id = ?", f.SessionID, f.SessionID)
	}
	if !f.Since.IsZero() {
		q = q.Where("created_at > ?", f.Since.UnixMilli())
	}
	return r.find(q.Order("created_at ASC, id ASC"))
}

func (r *TaskMessageRepository) MarkDelivered(id string, at time.Time) (*domain.TaskMessage, error) {
	res := r.db.Model(&TaskMessageModel{}).Where("id = ?", id).
		Updates(map[string]any{"delivered": true, "delivered_at": at.UnixMilli(), "queued": false})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, &domain.StructuredError{Code: "MESSAGE_NOT_FOUND", Message: "message not found"}
	}
	return r.Get(id), nil
}

func (r *TaskMessageRepository) SetQueued(id string, queued bool) error {
	return r.db.Model(&TaskMessageModel{}).Where("id = ?", id).Update("queued", queued).Error
}

func (r *TaskMessageRepository) ListQueued() []*domain.TaskMessage {
	return r.find(r.db.Model(&TaskMessageModel{}).Where("queued = ?", true).Order("created_at ASC, id ASC"))
}

func (r *TaskMessageRepository) LastReport(fromSessionID string) *domain.TaskMessage {
	var m TaskMessageModel
	err := r.db.Where("from_session_id = ? AND kind = ?", fromSessionID, string(domain.MessageStatusReport)).
		Order("created_at DESC, id DESC").First(&m).Error
	if err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *TaskMessageRepository) find(q *gorm.DB) []*domain.TaskMessage {
	var models []TaskMessageModel
	if err := q.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.TaskMessage, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

// ReviewRequestRepository persists review requests in SQLite via GORM.
type ReviewRequestRepository struct{ db *gorm.DB }

func NewReviewRequestRepository(db *gorm.DB) *ReviewRequestRepository {
	return &ReviewRequestRepository{db: db}
}

func (r *ReviewRequestRepository) Create(rr *domain.ReviewRequest) (*domain.ReviewRequest, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	created := rr.CreatedAt
	if created.IsZero() {
		created = now
	}
	m := &ReviewRequestModel{
		ID: id, TaskID: rr.TaskID, ProjectID: rr.ProjectID, ArchitectSessionID: rr.ArchitectSessionID,
		AboutSessionID: rr.AboutSessionID, Subject: rr.Subject, Body: rr.Body,
		DocumentIDs: idsToJSON(rr.DocumentIDs), ArtifactIDs: idsToJSON(rr.ArtifactIDs), State: string(rr.State),
		CreatedAt: created.UnixMilli(), UpdatedAt: created.UnixMilli(),
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *ReviewRequestRepository) Get(id string) *domain.ReviewRequest {
	var m ReviewRequestModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *ReviewRequestRepository) Update(rr *domain.ReviewRequest) error {
	updated := rr.UpdatedAt
	if updated.IsZero() {
		updated = time.Now()
	}
	res := r.db.Model(&ReviewRequestModel{}).Where("id = ?", rr.ID).Updates(map[string]any{
		"state": string(rr.State), "response_note": rr.ResponseNote,
		"responded_at": milliOf(rr.RespondedAt), "updated_at": updated.UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "REVIEW_NOT_FOUND", Message: "review request not found"}
	}
	return nil
}

func (r *ReviewRequestRepository) List(f ports.ReviewFilter) []*domain.ReviewRequest {
	q := r.db.Model(&ReviewRequestModel{})
	if f.TaskID != "" {
		q = q.Where("task_id = ?", f.TaskID)
	}
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.State != "" {
		q = q.Where("state = ?", string(f.State))
	}
	var models []ReviewRequestModel
	if err := q.Order("created_at DESC, id DESC").Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.ReviewRequest, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *ReviewRequestRepository) CountPending(taskID string) int {
	var n int64
	r.db.Model(&ReviewRequestModel{}).Where("task_id = ? AND state = ?", taskID, string(domain.ReviewPending)).Count(&n)
	return int(n)
}

// StatusCheckRepository persists status-check loops in SQLite via GORM.
type StatusCheckRepository struct{ db *gorm.DB }

func NewStatusCheckRepository(db *gorm.DB) *StatusCheckRepository {
	return &StatusCheckRepository{db: db}
}

func (r *StatusCheckRepository) Save(c *domain.StatusCheck) error {
	return r.db.Save(statusCheckModel(c)).Error
}

func (r *StatusCheckRepository) Get(delegateSessionID string) *domain.StatusCheck {
	var m StatusCheckModel
	if err := r.db.First(&m, "delegate_session_id = ?", delegateSessionID).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *StatusCheckRepository) ListByTask(taskID string) []*domain.StatusCheck {
	return r.find(r.db.Where("task_id = ?", taskID).Order("delegate_session_id ASC"))
}

func (r *StatusCheckRepository) ListByArchitect(architectSessionID string) []*domain.StatusCheck {
	return r.find(r.db.Where("architect_session_id = ?", architectSessionID).Order("delegate_session_id ASC"))
}

func (r *StatusCheckRepository) ListActive() []*domain.StatusCheck {
	return r.find(r.db.Where("state = ?", string(domain.StatusCheckActive)).Order("next_at ASC, delegate_session_id ASC"))
}

func (r *StatusCheckRepository) find(q *gorm.DB) []*domain.StatusCheck {
	var models []StatusCheckModel
	if err := q.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.StatusCheck, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

// TaskStatusChangeRepository keeps the status history in SQLite via GORM.
type TaskStatusChangeRepository struct{ db *gorm.DB }

func NewTaskStatusChangeRepository(db *gorm.DB) *TaskStatusChangeRepository {
	return &TaskStatusChangeRepository{db: db}
}

func (r *TaskStatusChangeRepository) Append(c *domain.TaskStatusChange) error {
	at := c.At
	if at.IsZero() {
		at = time.Now()
	}
	return r.db.Create(&TicketStatusChangeModel{
		TicketID: c.TaskID, ProjectID: c.ProjectID, Status: string(c.Status), Reason: c.Reason,
		By: string(c.By), BySessionID: c.BySessionID, At: at.UnixMilli(),
	}).Error
}

func (r *TaskStatusChangeRepository) ListByTask(taskID string) []*domain.TaskStatusChange {
	var models []TicketStatusChangeModel
	if err := r.db.Where("ticket_id = ?", taskID).Order("at ASC, id ASC").Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.TaskStatusChange, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}
