package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ServiceRequestStore = (*ServiceRequestRepository)(nil)

// ServiceRequestModel is a service request (domain.ServiceRequest): the
// request as JSON, variable values included, kept apart from the service so no
// read of a service can carry one. The columns are what a list filters by.
type ServiceRequestModel struct {
	ID          string    `gorm:"primaryKey"`
	ProjectID   string    `gorm:"column:project_id;index"`
	ServiceID   string    `gorm:"column:service_id;index"`
	SessionID   string    `gorm:"column:session_id;index"`
	State       string    `gorm:"index"`
	RequestedAt time.Time `gorm:"column:requested_at"`
	RequestJSON string    `gorm:"column:request_json"`
}

// TableName overrides the table name.
func (ServiceRequestModel) TableName() string { return "service_requests" }

// ServiceRequestRepository stores service requests.
type ServiceRequestRepository struct{ db *gorm.DB }

// NewServiceRequestRepository returns the store.
func NewServiceRequestRepository(db *gorm.DB) *ServiceRequestRepository {
	return &ServiceRequestRepository{db: db}
}

func serviceRequestModel(r *domain.ServiceRequest) (ServiceRequestModel, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return ServiceRequestModel{}, err
	}
	at, _ := time.Parse(time.RFC3339, r.RequestedAt)
	return ServiceRequestModel{
		ID: r.ID, ProjectID: r.ProjectID, ServiceID: r.ServiceID, SessionID: r.SessionID, State: string(r.State),
		RequestedAt: at, RequestJSON: string(b),
	}, nil
}

func (m ServiceRequestModel) domain() (domain.ServiceRequest, error) {
	var r domain.ServiceRequest
	err := json.Unmarshal([]byte(m.RequestJSON), &r)
	r.ID = m.ID
	return r, err
}

// Add implements ports.ServiceRequestStore; it sets the request's id.
func (r *ServiceRequestRepository) Add(ctx context.Context, req *domain.ServiceRequest) error {
	id, err := genID()
	if err != nil {
		return err
	}
	req.ID = id
	m, err := serviceRequestModel(req)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&m).Error
}

// Get implements ports.ServiceRequestStore; nil when there is none.
func (r *ServiceRequestRepository) Get(ctx context.Context, id string) (*domain.ServiceRequest, error) {
	var m ServiceRequestModel
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	req, err := m.domain()
	return &req, err
}

// Save implements ports.ServiceRequestStore.
func (r *ServiceRequestRepository) Save(ctx context.Context, req *domain.ServiceRequest) error {
	m, err := serviceRequestModel(req)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Save(&m).Error
}

// List implements ports.ServiceRequestStore.
func (r *ServiceRequestRepository) List(ctx context.Context, f domain.ServiceRequestFilter) ([]domain.ServiceRequest, error) {
	q := r.db.WithContext(ctx)
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.ServiceID != "" {
		q = q.Where("service_id = ?", f.ServiceID)
	}
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if f.State != "" {
		q = q.Where("state = ?", string(f.State))
	}
	var rows []ServiceRequestModel
	if err := q.Order("requested_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ServiceRequest, 0, len(rows))
	for _, m := range rows {
		req, err := m.domain()
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}
