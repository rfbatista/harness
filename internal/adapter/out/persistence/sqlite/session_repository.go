package sqlite

import (
	"time"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"

	"gorm.io/gorm"
)

var _ ports.SessionRepository = (*SessionRepository)(nil)

type SessionRepository struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) Create(s *domain.Session) (*domain.Session, error) {
	id := s.ID
	if id == "" {
		var err error
		if id, err = genID(); err != nil {
			return nil, err
		}
	}
	m := &SessionModel{
		ID:           id,
		ProjectID:    s.ProjectID,
		RepositoryID: s.RepositoryID,
		AgentID:      s.AgentID,
		ZoneID:       s.ZoneID,
		TicketID:     s.TicketID,
		Task:         s.Task,
		WorkingDir:   s.WorkingDir,
		Model:        s.Model,
		Status:       string(s.Status),
		AutoRun:      s.AutoRun,
		WorkspaceID:  s.WorkspaceID,
		Branch:       s.Branch,
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *SessionRepository) Get(id string) *domain.Session {
	var m SessionModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *SessionRepository) List(filter ports.SessionFilter) []*domain.Session {
	q := r.db.Model(&SessionModel{})
	if filter.ProjectID != "" {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if filter.AgentID != "" {
		q = q.Where("agent_id = ?", filter.AgentID)
	}
	if filter.TicketID != "" {
		q = q.Where("ticket_id = ?", filter.TicketID)
	}
	if len(filter.Statuses) > 0 {
		values := make([]string, 0, len(filter.Statuses))
		for _, s := range filter.Statuses {
			values = append(values, string(s))
		}
		q = q.Where("status IN ?", values)
	}
	q = q.Order("created_at DESC")
	var models []SessionModel
	if err := q.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Session, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *SessionRepository) Delete(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&SessionModel{}, "id = ?", id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
		}
		return tx.Delete(&SessionEventModel{}, "session_id = ?", id).Error
	})
}

func (r *SessionRepository) UpdateStatus(id string, status domain.SessionStatus) error {
	res := r.db.Model(&SessionModel{}).Where("id = ?", id).Updates(map[string]any{
		"status":     string(status),
		"updated_at": time.Now().UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return nil
}

func (r *SessionRepository) UpdateMetrics(id string, costUSD float64, inputTokens, outputTokens int, lastAction string, pendingApprovals int) error {
	res := r.db.Model(&SessionModel{}).Where("id = ?", id).Updates(map[string]any{
		"cost_usd":          costUSD,
		"input_tokens":      inputTokens,
		"output_tokens":     outputTokens,
		"last_action":       lastAction,
		"pending_approvals": pendingApprovals,
		"updated_at":        time.Now().UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return nil
}

func (r *SessionRepository) UpdateAutoRun(id string, autoRun bool) error {
	res := r.db.Model(&SessionModel{}).Where("id = ?", id).Updates(map[string]any{
		"auto_run":   autoRun,
		"updated_at": time.Now().UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return nil
}

func (r *SessionRepository) AppendEvent(sessionID string, seq int64, typ string, payload []byte) error {
	return r.db.Create(&SessionEventModel{
		SessionID: sessionID, Seq: seq, Type: typ, Payload: payload,
	}).Error
}

func (r *SessionRepository) ListEvents(sessionID string, fromSeq int64) []ports.StoredEvent {
	var models []SessionEventModel
	q := r.db.Where("session_id = ? AND seq > ?", sessionID, fromSeq).Order("seq ASC")
	if err := q.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]ports.StoredEvent, 0, len(models))
	for _, m := range models {
		out = append(out, ports.StoredEvent{
			Seq: m.Seq, Type: m.Type, Payload: m.Payload, At: time.UnixMilli(m.CreatedAt),
		})
	}
	return out
}
