package sqlite

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ServiceStore = (*ServiceRepository)(nil)

// ServiceModel is one service (domain.Service): its definition and the last
// run, kept as JSON beside the status so a count of the running ones is one
// indexed read.
type ServiceModel struct {
	ID                  string `gorm:"primaryKey"`
	ProjectID           string `gorm:"column:project_id;index:idx_services_project_status,priority:1;uniqueIndex:idx_services_project_name,priority:1"`
	Name                string `gorm:"uniqueIndex:idx_services_project_name,priority:2"`
	Kind                string
	RepositoryID        string `gorm:"column:repository_id"`
	ExportRepositoryEnv bool   `gorm:"column:export_repository_env"`
	Env                 string
	CommandJSON         string `gorm:"column:command_json"`
	ImageJSON           string `gorm:"column:image_json"`
	Status              string `gorm:"index:idx_services_project_status,priority:2"`
	RunJSON             string `gorm:"column:run_json"`
	CreatedByJSON       string `gorm:"column:created_by_json"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// TableName overrides the table name.
func (ServiceModel) TableName() string { return "services" }

// ServiceRunRecordModel is the run record of a service (ports.ServiceRunRecord):
// the variables of its current or last run, values included, one row per
// service. It is its own table so that no read of a service, and so no feed
// event, can carry a value.
type ServiceRunRecordModel struct {
	ServiceID  string `gorm:"primaryKey;column:service_id"`
	RunID      string `gorm:"column:run_id;index"`
	RecordJSON string `gorm:"column:record_json"`
}

// TableName overrides the table name.
func (ServiceRunRecordModel) TableName() string { return "service_run_records" }

// ServiceRepository stores services.
type ServiceRepository struct{ db *gorm.DB }

// NewServiceRepository returns the store.
func NewServiceRepository(db *gorm.DB) *ServiceRepository { return &ServiceRepository{db: db} }

func marshalOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func serviceToModel(s *domain.Service) ServiceModel {
	m := ServiceModel{
		ID: s.ID, ProjectID: s.ProjectID, Name: s.Name, Kind: string(s.Kind), RepositoryID: s.RepositoryID,
		ExportRepositoryEnv: s.ExportRepositoryEnv, Env: s.Env, Status: string(s.Status),
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	if s.Command != nil {
		m.CommandJSON = marshalOrEmpty(s.Command)
	}
	if s.Image != nil {
		m.ImageJSON = marshalOrEmpty(s.Image)
	}
	if s.Run != nil {
		m.RunJSON = marshalOrEmpty(s.Run)
	}
	m.CreatedByJSON = marshalOrEmpty(s.CreatedBy)
	return m
}

func (m *ServiceModel) toDomain() *domain.Service {
	s := &domain.Service{
		ID: m.ID, ProjectID: m.ProjectID, Name: m.Name, Kind: domain.ServiceKind(m.Kind), RepositoryID: m.RepositoryID,
		ExportRepositoryEnv: m.ExportRepositoryEnv, Env: m.Env, Status: domain.ServiceStatus(m.Status),
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if m.CommandJSON != "" {
		s.Command = &domain.ServiceCommand{}
		_ = json.Unmarshal([]byte(m.CommandJSON), s.Command)
	}
	if m.ImageJSON != "" {
		s.Image = &domain.ServiceImage{}
		_ = json.Unmarshal([]byte(m.ImageJSON), s.Image)
	}
	if m.RunJSON != "" {
		s.Run = &domain.ServiceRun{}
		_ = json.Unmarshal([]byte(m.RunJSON), s.Run)
	}
	if m.CreatedByJSON != "" {
		_ = json.Unmarshal([]byte(m.CreatedByJSON), &s.CreatedBy)
	}
	if s.CreatedBy.Kind == "" {
		s.CreatedBy.Kind = "person" // a service made before agents could create them
	}
	return s
}

func serviceNotFound() error {
	return &domain.StructuredError{Code: "SERVICE_NOT_FOUND", Message: "service not found"}
}

// ListServices returns a project's services by name.
func (r *ServiceRepository) ListServices(projectID string) ([]*domain.Service, error) {
	var models []ServiceModel
	if err := r.db.Where("project_id = ?", projectID).Order("name").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.Service, 0, len(models))
	for i := range models {
		out = append(out, models[i].toDomain())
	}
	return out, nil
}

// GetService returns one service, or SERVICE_NOT_FOUND.
func (r *ServiceRepository) GetService(id string) (*domain.Service, error) {
	var m ServiceModel
	if err := r.db.Where("id = ?", id).Take(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, serviceNotFound()
		}
		return nil, err
	}
	return m.toDomain(), nil
}

// SaveService creates or replaces a service.
func (r *ServiceRepository) SaveService(s *domain.Service) error {
	m := serviceToModel(s)
	err := r.db.Save(&m).Error
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return &domain.StructuredError{Code: "SERVICE_NAME_TAKEN", Message: "a service named " + s.Name + " already exists in this project"}
	}
	return err
}

// DeleteService removes a service, or SERVICE_NOT_FOUND.
func (r *ServiceRepository) DeleteService(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&ServiceModel{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return serviceNotFound()
		}
		return tx.Where("service_id = ?", id).Delete(&ServiceRunRecordModel{}).Error
	})
}

// SaveRunRecord replaces the service's run record.
func (r *ServiceRepository) SaveRunRecord(rec *ports.ServiceRunRecord) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return r.db.Save(&ServiceRunRecordModel{ServiceID: rec.ServiceID, RunID: rec.RunID, RecordJSON: string(b)}).Error
}

// GetRunRecord is the service's run record, nil for none.
func (r *ServiceRepository) GetRunRecord(serviceID string) (*ports.ServiceRunRecord, error) {
	var m ServiceRunRecordModel
	if err := r.db.Where("service_id = ?", serviceID).Take(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	rec := &ports.ServiceRunRecord{}
	if err := json.Unmarshal([]byte(m.RecordJSON), rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// RunningServices counts a project's starting and running services.
func (r *ServiceRepository) RunningServices(projectID string) (int, error) {
	var n int64
	err := r.db.Model(&ServiceModel{}).
		Where("project_id = ? AND status IN ?", projectID, []string{string(domain.ServiceStarting), string(domain.ServiceRunning)}).
		Count(&n).Error
	return int(n), err
}

// ActiveServices lists the starting and running services of every project.
func (r *ServiceRepository) ActiveServices() ([]*domain.Service, error) {
	var models []ServiceModel
	err := r.db.Where("status IN ?", []string{string(domain.ServiceStarting), string(domain.ServiceRunning)}).Order("project_id, name").Find(&models).Error
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Service, 0, len(models))
	for i := range models {
		out = append(out, models[i].toDomain())
	}
	return out, nil
}
