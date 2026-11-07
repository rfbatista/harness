package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// Services (ADR 0076): project-level programs the person defines, run one at
// a time each and many at once. See "Contract: Harness server ↔ Web UI —
// services".

// ServiceStore keeps services and the last run of each (driven).
type ServiceStore interface {
	// ListServices are a project's services by name.
	ListServices(projectID string) ([]*domain.Service, error)
	// GetService is the service, or SERVICE_NOT_FOUND.
	GetService(id string) (*domain.Service, error)
	// SaveService creates or replaces the service with s.ID. A name another
	// service of the project has is SERVICE_NAME_TAKEN.
	SaveService(s *domain.Service) error
	// DeleteService removes the service, or SERVICE_NOT_FOUND.
	DeleteService(id string) error
	// RunningServices counts the project's services that are starting or
	// running: a single indexed read.
	RunningServices(projectID string) (int, error)
}

// ServiceRuntimeInfo says which container runtime image services use and
// whether its command is on the server's PATH.
type ServiceRuntimeInfo struct {
	Name      string `json:"name"` // docker | podman
	Available bool   `json:"available"`
}

// Sandbox states a service list reports.
const (
	ServiceSandboxOn          = "on"
	ServiceSandboxOff         = "off"
	ServiceSandboxUnavailable = "unavailable"
)

// ServiceList is a project's services as read at feed sequence number Seq.
type ServiceList struct {
	Services []*domain.Service  `json:"services"`
	Seq      int64              `json:"seq"`
	Runtime  ServiceRuntimeInfo `json:"runtime"`
	// Sandbox is on, off, or unavailable (the setting is on and the platform
	// has no driver).
	Sandbox string `json:"sandbox"`
}

// ServiceEvent is one change on a project's services feed. Seq grows by one
// per event within a project; an event with a Seq at most the list's is
// already in the list. RunningCount is the project's starting or running
// services.
type ServiceEvent struct {
	Seq          int64           `json:"seq"`
	Kind         string          `json:"kind"` // service_changed | service_deleted
	Service      *domain.Service `json:"service,omitempty"`
	ServiceID    string          `json:"service_id,omitempty"`
	RunningCount int             `json:"running_count"`
}

// Service feed event kinds.
const (
	ServiceChanged = "service_changed"
	ServiceDeleted = "service_deleted"
)

// ServiceRunningCounter is the cheap read of how many of a project's services
// are starting or running, for the server-rendered project navigation. It
// never touches a runtime.
type ServiceRunningCounter interface {
	RunningServiceCount(ctx context.Context, projectID string) (int, error)
}

// ServiceManager defines, reads and watches a project's services.
// Refusals: INVALID_INPUT naming the field, SERVICE_NAME_TAKEN,
// REPOSITORY_NOT_FOUND, PROJECT_NOT_FOUND, SERVICE_NOT_FOUND and, for a
// delete, SERVICE_RUNNING.
type ServiceManager interface {
	ServiceRunningCounter
	ListServices(ctx context.Context, projectID string) (*ServiceList, error)
	GetService(ctx context.Context, serviceID string) (*domain.Service, error)
	CreateService(ctx context.Context, projectID string, in domain.ServiceInput) (*domain.Service, error)
	// UpdateService replaces the service's definition; restartNeeded says it
	// runs and picks the change up at its next start.
	UpdateService(ctx context.Context, serviceID string, in domain.ServiceInput) (s *domain.Service, restartNeeded bool, err error)
	// DeleteService removes a service that is not starting or running, with
	// its log.
	DeleteService(ctx context.Context, serviceID string) error
	// FollowServices streams the project's changes until ctx ends. The
	// channel closes when the follower fell behind.
	FollowServices(ctx context.Context, projectID string) (<-chan ServiceEvent, error)
}
