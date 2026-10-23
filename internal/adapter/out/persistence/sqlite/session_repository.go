package sqlite

import (
	"context"
	"errors"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

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
		ID:              id,
		ProjectID:       s.ProjectID,
		RepositoryID:    s.RepositoryID,
		AgentID:         s.AgentID,
		ZoneID:          s.ZoneID,
		TicketID:        s.TicketID,
		Task:            s.Task,
		WorkingDir:      s.WorkingDir,
		Model:           s.Model,
		Status:          string(s.Status),
		AutoRun:         s.AutoRun,
		WorkspaceID:     s.WorkspaceID,
		Branch:          s.Branch,
		Interactive:     s.Interactive,
		ClaudeSessionID: s.ClaudeSessionID,
		RunsOn:          string(s.RunsOn),
		RunnerHost:      s.RunnerHost,
		ParentSessionID: s.ParentSessionID,
		Mode:            string(s.Mode),

		TokenSavers:             joinSavers(s.TokenSavers),
		TokenSaversNotInstalled: joinSavers(s.TokenSaversNotInstalled),
		CavemanIntensity:        string(s.CavemanIntensity),
	}
	m.setSandbox(s.Sandbox)
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Get is Find for callers that treat a failed read as not found.
func (r *SessionRepository) Get(id string) *domain.Session {
	s, _ := r.Find(context.Background(), id)
	return s
}

// Find reads a session: nil, nil when there is none, an error when the read
// failed.
func (r *SessionRepository) Find(ctx context.Context, id string) (*domain.Session, error) {
	var m SessionModel
	err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// List is Query for callers that treat a failed read as no sessions.
func (r *SessionRepository) List(ctx context.Context, filter ports.SessionFilter) []*domain.Session {
	list, _ := r.Query(ctx, filter)
	return list
}

// Query lists the sessions filter matches, newest first, or the error the
// read failed with.
func (r *SessionRepository) Query(ctx context.Context, filter ports.SessionFilter) ([]*domain.Session, error) {
	q := r.db.WithContext(ctx).Model(&SessionModel{})
	if filter.ProjectID != "" {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if filter.AgentID != "" {
		q = q.Where("agent_id = ?", filter.AgentID)
	}
	if filter.TicketID != "" {
		q = q.Where("ticket_id = ?", filter.TicketID)
	}
	if len(filter.TicketIDs) > 0 {
		q = q.Where("ticket_id IN ?", filter.TicketIDs)
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
		return nil, err
	}
	out := make([]*domain.Session, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out, nil
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
		if err := tx.Delete(&SandboxViolationModel{}, "session_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&SessionEventModel{}, "session_id = ?", id).Error
	})
}

var _ ports.SandboxViolationStore = (*SessionRepository)(nil)

// AddSandboxViolation stores v and drops the session's oldest violations
// beyond ports.MaxSandboxViolations.
func (r *SessionRepository) AddSandboxViolation(sessionID string, v ports.SandboxViolation) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&SandboxViolationModel{SessionID: sessionID, WhenMs: v.When.UnixMilli(),
			Operation: v.Operation, Path: v.Path, Process: v.Process}).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM sandbox_violations WHERE session_id = ? AND id NOT IN
			(SELECT id FROM sandbox_violations WHERE session_id = ? ORDER BY id DESC LIMIT ?)`,
			sessionID, sessionID, ports.MaxSandboxViolations).Error
	})
}

// SandboxViolations are the session's stored violations, newest stored first.
func (r *SessionRepository) SandboxViolations(sessionID string) ([]ports.SandboxViolation, error) {
	var models []SandboxViolationModel
	if err := r.db.Where("session_id = ?", sessionID).Order("id DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]ports.SandboxViolation, 0, len(models))
	for _, m := range models {
		out = append(out, ports.SandboxViolation{When: time.UnixMilli(m.WhenMs), Operation: m.Operation, Path: m.Path, Process: m.Process})
	}
	return out, nil
}

func (r *SessionRepository) UpdateStatus(id string, status domain.SessionStatus) error {
	return updateSession(r.db, id, map[string]any{"status": string(status)})
}

func (r *SessionRepository) UpdateMetrics(id string, costUSD float64, tokens ports.SessionTokens, lastAction string, pendingApprovals int) error {
	return updateSession(r.db, id, metricsColumns(ports.SessionMetrics{
		CostUSD: costUSD, Tokens: tokens, LastAction: lastAction, PendingApprovals: pendingApprovals,
	}))
}

// RecordEvent stores the event row, the metrics and the status in one write
// transaction.
func (r *SessionRepository) RecordEvent(sessionID string, ev ports.SessionEventRecord) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&SessionEventModel{
			SessionID: sessionID, Seq: ev.Seq, Type: ev.Type, Payload: ev.Payload,
		}).Error; err != nil {
			return err
		}
		cols := map[string]any{}
		if ev.Metrics != nil {
			cols = metricsColumns(*ev.Metrics)
		}
		if ev.Status != "" {
			cols["status"] = string(ev.Status)
		}
		if len(cols) == 0 {
			return nil
		}
		return updateSession(tx, sessionID, cols)
	})
}

func metricsColumns(m ports.SessionMetrics) map[string]any {
	return map[string]any{
		"cost_usd":                    m.CostUSD,
		"input_tokens":                m.Tokens.Input,
		"output_tokens":               m.Tokens.Output,
		"cache_creation_input_tokens": m.Tokens.CacheCreation,
		"cache_read_input_tokens":     m.Tokens.CacheRead,
		"last_action":                 m.LastAction,
		"pending_approvals":           m.PendingApprovals,
	}
}

// updateSession sets cols and updated_at on session id.
func updateSession(db *gorm.DB, id string, cols map[string]any) error {
	cols["updated_at"] = time.Now().UnixMilli()
	res := db.Model(&SessionModel{}).Where("id = ?", id).Updates(cols)
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

func (r *SessionRepository) UpdateClaudeSessionID(id, claudeSessionID string) error {
	res := r.db.Model(&SessionModel{}).Where("id = ?", id).Updates(map[string]any{
		"claude_session_id": claudeSessionID,
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

func (r *SessionRepository) UpdateRunner(id string, runsOn domain.Runner, host string) error {
	res := r.db.Model(&SessionModel{}).Where("id = ?", id).Updates(map[string]any{
		"runs_on":     string(runsOn),
		"runner_host": host,
		"updated_at":  time.Now().UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return nil
}

func (r *SessionRepository) UpdateSandbox(id string, facts domain.SandboxFacts) error {
	res := r.db.Model(&SessionModel{}).Where("id = ?", id).Updates(map[string]any{
		"sandboxed":          facts.Sandboxed,
		"sandbox_visibility": facts.Visibility,
		"sandbox_driver":     facts.Driver,
		"updated_at":         time.Now().UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return nil
}

func (r *SessionRepository) UpdateTokenSavers(id string, active, notInstalled []domain.TokenSaver) error {
	res := r.db.Model(&SessionModel{}).Where("id = ?", id).Updates(map[string]any{
		"token_savers":               joinSavers(active),
		"token_savers_not_installed": joinSavers(notInstalled),
		"updated_at":                 time.Now().UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return nil
}

func (r *SessionRepository) ListEvents(sessionID string, fromSeq int64) ([]ports.StoredEvent, error) {
	var models []SessionEventModel
	q := r.db.Where("session_id = ? AND seq > ?", sessionID, fromSeq).Order("seq ASC")
	if err := q.Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]ports.StoredEvent, 0, len(models))
	for _, m := range models {
		out = append(out, ports.StoredEvent{
			Seq: m.Seq, Type: m.Type, Payload: m.Payload, At: time.UnixMilli(m.CreatedAt),
		})
	}
	return out, nil
}

// ActivityByProject answers each project's live session count and newest
// session activity, in one query.
func (r *SessionRepository) ActivityByProject() (map[string]ports.SessionActivity, error) {
	var rows []struct {
		ProjectID string
		Live      int
		Last      int64
	}
	err := r.db.Model(&SessionModel{}).
		Select("project_id, SUM(CASE WHEN status NOT IN ? THEN 1 ELSE 0 END) AS live, MAX(updated_at) AS last",
			[]string{string(domain.SessionDone), string(domain.SessionFailed), string(domain.SessionStopped)}).
		Group("project_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]ports.SessionActivity, len(rows))
	for _, row := range rows {
		out[row.ProjectID] = ports.SessionActivity{LiveCount: row.Live, LastActivityAt: time.UnixMilli(row.Last)}
	}
	return out, nil
}
