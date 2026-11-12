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
	// SaveRunRecord replaces the run record of rec.ServiceID: one per service,
	// the current or last run's. It holds values, so it is kept apart from
	// the service and never reaches a feed.
	SaveRunRecord(rec *ServiceRunRecord) error
	// GetRunRecord is the service's run record; nil, nil for none.
	GetRunRecord(serviceID string) (*ServiceRunRecord, error)
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
	// Container is what the container runner starts instead of Path and
	// Args; Env is then the environment of the runtime's own command.
	Container *ServiceContainer
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

// ServiceRunWorktrees lists the run worktrees of a repository, for the
// services that run in one: its main checkout first, then each session's
// worktree that exists on disk, newest first. It is the lookup the App tab
// uses (ADR 0039).
type ServiceRunWorktrees interface {
	ListRepositoryRunWorktrees(ctx context.Context, projectID, repositoryID string) ([]*domain.RunWorktree, error)
}

// ServiceBranch is a branch a command service can run, as the picker lists it
// (ADR 0080). A remote branch is one of origin's, named without "origin/".
type ServiceBranch struct {
	Name        string `json:"name"`
	Remote      bool   `json:"remote"`
	SHA         string `json:"sha"`
	Subject     string `json:"subject"`
	CommittedAt string `json:"committed_at"`
}

// ServiceBranchList is a service's branches, at most ServiceBranchLimit;
// Truncated says there were more.
type ServiceBranchList struct {
	Branches  []ServiceBranch `json:"branches"`
	Truncated bool            `json:"truncated"`
}

// ServiceBranchLimit is how many branches one list answers.
const ServiceBranchLimit = 200

// ServiceBranchGit is the git a service's branch runs need (driven; gitcli
// implements it). Every path is one the module made or the repository's own
// root.
type ServiceBranchGit interface {
	// ListServiceBranches lists the local branches then origin's remote-tracking
	// branches of the checkout at repoRoot, each group newest commit first,
	// without HEAD aliases. It never fetches.
	ListServiceBranches(ctx context.Context, repoRoot string) ([]ServiceBranch, error)
	// ResolveCommit is the full sha of the commit ref names in the checkout.
	ResolveCommit(ctx context.Context, repoRoot, ref string) (string, error)
	// PrepareServiceWorktree makes the service worktree at dir for the
	// repository at repoRoot when it is not there, detached at HEAD.
	PrepareServiceWorktree(ctx context.Context, repoRoot, dir string) error
	// CheckoutDetached moves the worktree at dir to sha, detached, resetting
	// its tracked files and keeping untracked and ignored ones. It creates and
	// moves no branch.
	CheckoutDetached(ctx context.Context, dir, sha string) error
	// RemoveServiceWorktree deregisters and deletes the worktree at dir; one
	// already gone is no error.
	RemoveServiceWorktree(ctx context.Context, dir string) error
}

// ServiceBranchFetcher fetches one branch of a repository's origin into its
// tracking ref (driven; the workspaces context implements it with the stored
// credentials). An error is worded for a run's `error`: git's reason, never a
// credential.
type ServiceBranchFetcher interface {
	FetchServiceBranch(ctx context.Context, repositoryID, repoRoot, branch string) error
}

// ServiceStartOptions say who starts a run and where.
type ServiceStartOptions struct {
	StartedBy domain.ServiceStartedBy
	// WorktreeKey is the run worktree the person picked: main or session:<id>,
	// one ListServiceRunWorktrees offers now. Empty is the main checkout. Any
	// other key is WORKTREE_NOT_AVAILABLE, and a key on a service with no
	// repository, or an image service, is INVALID_INPUT. It is ignored when
	// Worktree is set.
	WorktreeKey string
	// Repeat makes a restart run where the last run did, ignoring WorktreeKey
	// and Worktree. It has no effect on a start.
	Repeat bool
	// Env are the run overrides (ADR 0077): variables for this run only, on
	// top of the repository .env and the service's own. Nil is none given;
	// on a restart that means "repeat the last run's", and an empty map clears
	// them. A bad entry is INVALID_INPUT naming the key.
	Env map[string]string
	// Worktree, when set, is where a command service of a repository runs
	// (its subdir applies inside it); nil is the main checkout.
	Worktree *ServiceWorktree
}

// Where a run variable comes from, the one that won.
const (
	VariableFromRepositoryEnv = "repository_env"
	VariableFromService       = "service"
	VariableFromOverride      = "override"
)

// ServiceRunVariable is one variable a run was given, with its value, the
// source that won and, for an override, who set it as the person reads it.
// The base system environment is never one.
type ServiceRunVariable struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Source string `json:"source"`
	By     string `json:"by"`
}

// ServiceRunRecord is what a run was given, stored beside the service so a
// restart of the harness still repeats its overrides: every variable with its
// source, and the override values with who set them.
type ServiceRunRecord struct {
	ServiceID  string                   `json:"service_id"`
	RunID      string                   `json:"run_id"`
	Variables  []ServiceRunVariable     `json:"variables"`
	Overrides  map[string]string        `json:"overrides"`
	OverrideBy *domain.ServiceStartedBy `json:"override_by"`
}

// ServiceRunVariables are the variables of a service's current or last run,
// sorted by name; RunID is empty for a service that never ran. Values travel
// only in this answer, never on a feed.
type ServiceRunVariables struct {
	RunID     string               `json:"run_id"`
	Variables []ServiceRunVariable `json:"variables"`
	// Overrides are the override values by name, for the agent that set them.
	// It is not part of the person's answer.
	Overrides map[string]string `json:"-"`
	// OverrideBy is who set them; nil for none.
	OverrideBy *domain.ServiceStartedBy `json:"-"`
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
	// ListServiceRunWorktrees is the run worktrees a start can pick for the
	// service now: empty for an image service or a service with no repository.
	ListServiceRunWorktrees(ctx context.Context, serviceID string) ([]*domain.RunWorktree, error)
	// ListServiceBranches is the local and origin branches a command service
	// of a repository can run, filtered by a case-insensitive substring of the
	// name; empty for an image service, a service with no repository, or a
	// repository with no checkout on this machine.
	ListServiceBranches(ctx context.Context, serviceID, query string) (*ServiceBranchList, error)
	// ContainerRuntime is the runtime image services start with, and whether
	// its command is on the server's PATH. SetContainerRuntime changes it for
	// the next start (INVALID_INPUT for a runtime other than docker or podman).
	ContainerRuntime(ctx context.Context) ServiceRuntimeInfo
	SetContainerRuntime(ctx context.Context, name string) (ServiceRuntimeInfo, error)
	CreateService(ctx context.Context, projectID string, in domain.ServiceInput) (*domain.Service, error)
	// UpdateService replaces the service's definition; restartNeeded says it
	// runs and picks the change up at its next start.
	UpdateService(ctx context.Context, serviceID string, in domain.ServiceInput) (s *domain.Service, restartNeeded bool, err error)
	// DeleteService removes a service that is not starting or running, with
	// its log.
	DeleteService(ctx context.Context, serviceID string) error
	// ValidateStart refuses a start that could not happen, as StartService
	// would before it creates a run: the sandbox and the container runtime, the
	// run overrides and the run worktree. It starts nothing.
	ValidateStart(ctx context.Context, serviceID string, opts ServiceStartOptions) error
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
	// GetRunVariables is the variables the service's current or last run was
	// given. Values held in memory only: after a restart of the harness the
	// run is known but its variables are not.
	GetRunVariables(ctx context.Context, serviceID string) (*ServiceRunVariables, error)
	// ReadServiceLog reads the current or last run's log from offset; nil
	// reads the last ServiceLogTailBytes.
	ReadServiceLog(ctx context.Context, serviceID string, offset *int64) (*ServiceLogRead, error)
	// ReadServiceLogTail reads the last lines of the log (at most
	// ServiceLogMaxRead bytes of it, from the start of a line); truncated says
	// that window cut the answer short of the lines asked for, with earlier
	// lines beyond it. Older lines left out of a full answer are not a cut.
	ReadServiceLogTail(ctx context.Context, serviceID string, lines int) (read *ServiceLogRead, truncated bool, err error)
	// FollowServiceLog streams the log from byte offset from (nil: its end)
	// and then each new chunk, until ctx ends. The channel closes when the
	// follower fell behind.
	FollowServiceLog(ctx context.Context, serviceID string, from *int64) (<-chan ServiceLogEvent, error)
	// FollowServices streams the project's changes until ctx ends. The
	// channel closes when the follower fell behind.
	FollowServices(ctx context.Context, projectID string) (<-chan ServiceEvent, error)
}
