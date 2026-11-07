package ports

import (
	"context"
	"io"
	"time"

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
	// ActiveServices are the starting and running services of every project.
	ActiveServices() ([]*domain.Service, error)
}

// ServiceProcessSpec is a program a process runner starts: the command line a
// sandbox driver may already have wrapped (ports.SandboxCommand), with the
// name the runner knows the process by.
type ServiceProcessSpec struct {
	// Name identifies the run to a runner that needs one (a container's name).
	Name string
	Path string
	Args []string
	Dir  string
	Env  []string
}

// ServiceProcessRunner starts the process of a service run (driven). The command
// runner starts a program in its own process group; the container runner
// builds a runtime command line. The process outlives ctx: only Stop or its
// own end finishes it.
type ServiceProcessRunner interface {
	Start(ctx context.Context, spec ServiceProcessSpec) (ServiceProcessHandle, error)
}

// ServiceProcessHandle is a started process.
type ServiceProcessHandle interface {
	// Output is the combined stdout and stderr; it reaches EOF once the
	// process and everything it started closed them. The caller closes it
	// after reading.
	Output() io.ReadCloser
	// Wait blocks until the process has ended and returns its exit code (a
	// death by signal is 128 plus the signal).
	Wait() int
	// Stop ends the whole process tree: SIGTERM, then SIGKILL once grace has
	// passed. It returns when the process has ended.
	Stop(grace time.Duration)
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

// ServiceWorktree is a directory a command service of a repository runs in
// instead of the repository's main checkout: an agent session's worktree.
type ServiceWorktree struct {
	Dir       string
	Branch    string
	SessionID string
}

// ServiceStartOptions say who starts a run and where.
type ServiceStartOptions struct {
	StartedBy domain.ServiceStartedBy
	// Worktree, when set, is where a command service of a repository runs
	// (its subdir applies inside it); nil is the main checkout.
	Worktree *ServiceWorktree
}

// ServiceLogRead is one read of a service's log.
type ServiceLogRead struct {
	Text string `json:"text"`
	// Offset is where the text starts: the requested offset, or First when
	// that fell before it.
	Offset      int64  `json:"offset"`
	NextOffset  int64  `json:"next_offset"`
	FirstOffset int64  `json:"first_offset"`
	RunID       string `json:"run_id"`
}

// Limits of one log read.
const (
	ServiceLogTailBytes = 64 << 10 // an omitted offset reads this much of the end
	ServiceLogMaxRead   = 256 << 10
)

// Service log event kinds.
const (
	ServiceLogChunk = "chunk"
	ServiceLogReset = "reset"
)

// ServiceLogEvent is one change on a service's log feed: a chunk of text at
// its byte offset, or a reset because a new run cleared the log.
type ServiceLogEvent struct {
	Kind   string `json:"kind"`
	RunID  string `json:"run_id"`
	Offset *int64 `json:"offset,omitempty"`
	Text   string `json:"text,omitempty"`
}

// ServiceRunningCounter is the cheap read of how many of a project's services
// are starting or running, for the server-rendered project navigation. It
// never touches a runtime.
type ServiceRunningCounter interface {
	RunningServiceCount(ctx context.Context, projectID string) (int, error)
}

// ServiceManager defines, runs, reads and watches a project's services.
// Refusals: INVALID_INPUT naming the field, SERVICE_NAME_TAKEN,
// REPOSITORY_NOT_FOUND, PROJECT_NOT_FOUND, SERVICE_NOT_FOUND and, for a
// delete, SERVICE_RUNNING; for a start, SANDBOX_UNAVAILABLE and
// RUNTIME_UNAVAILABLE with nothing started.
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
	// StartService starts the service's run, in the repository's main checkout
	// (or its service folder) unless opts name a worktree, and answers once the
	// run is going (running) or could not start (failed). A service that is
	// starting or running is returned unchanged with alreadyRunning, also when
	// two starts race.
	StartService(ctx context.Context, serviceID string, opts ServiceStartOptions) (s *domain.Service, alreadyRunning bool, err error)
	// StopService ends the run's whole process tree and answers once it has
	// ended; a service with no run going is returned unchanged.
	StopService(ctx context.Context, serviceID string) (*domain.Service, error)
	// RestartService stops the run, if any, then starts it as StartService.
	RestartService(ctx context.Context, serviceID string, opts ServiceStartOptions) (*domain.Service, error)
	// WaitServiceSettled blocks until the service is no longer starting (or
	// ctx ends) and returns it.
	WaitServiceSettled(ctx context.Context, serviceID string) (*domain.Service, error)
	// ReadServiceLog reads the current or last run's log from offset; nil
	// reads the last ServiceLogTailBytes.
	ReadServiceLog(ctx context.Context, serviceID string, offset *int64) (*ServiceLogRead, error)
	// ReadServiceLogTail reads the last lines of the log (at most
	// ServiceLogMaxRead bytes of it); truncated says earlier lines exist
	// beyond what was read.
	ReadServiceLogTail(ctx context.Context, serviceID string, lines int) (read *ServiceLogRead, truncated bool, err error)
	// FollowServiceLog streams the log from byte offset from (nil: its end)
	// and then each new chunk, until ctx ends. The channel closes when the
	// follower fell behind.
	FollowServiceLog(ctx context.Context, serviceID string, from *int64) (<-chan ServiceLogEvent, error)
	// FollowServices streams the project's changes until ctx ends. The
	// channel closes when the follower fell behind.
	FollowServices(ctx context.Context, projectID string) (<-chan ServiceEvent, error)
}
