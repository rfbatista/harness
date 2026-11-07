package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// ServiceContainer is an image service's run as the container runner takes
// it: validated service fields only, never a command line.
type ServiceContainer struct {
	// Runtime is the runtime's command: docker or podman.
	Runtime string
	// ServiceID and Instance label the container: the service it runs and the
	// harness that started it.
	ServiceID string
	Instance  string
	Image     string
	// Args replace the image's command; none keeps it.
	Args  []string
	Ports []domain.ServicePort
	// Env are the container's KEY=VALUE variables. They reach the runtime in
	// a file made just before it starts and removed once the container runs.
	Env []string
	// EnvDir is the harness-owned folder that file is made in.
	EnvDir string
}

// ServiceRunDescriber is a process runner that can say, without starting
// anything, the command line it would run. The line never holds an env value.
type ServiceRunDescriber interface {
	Describe(spec ServiceProcessSpec) string
}

// ServiceReadyHandle is a handle whose process is not going yet when Start
// returns, as a container is not while its image is pulled. Ready closes
// when it is going; it never closes for a process that ended first.
type ServiceReadyHandle interface {
	ServiceProcessHandle
	Ready() <-chan struct{}
}

// ServiceContainerRef is a container of a service run found on the machine.
type ServiceContainerRef struct {
	Name      string
	ServiceID string
	Instance  string
}

// ServiceContainerSweeper finds and removes the containers the harness
// started (driven), for clean-up of those a stopped harness left behind.
type ServiceContainerSweeper interface {
	// ListServiceContainers are the runtime's containers, running or not,
	// that carry the harness's service label, each with its instance label
	// (the caller keeps its own).
	ListServiceContainers(ctx context.Context, runtime string) ([]ServiceContainerRef, error)
	// RemoveServiceContainer force-removes the container.
	RemoveServiceContainer(ctx context.Context, runtime, name string) error
}

// ServiceRuntimeSetting is the server-wide container runtime image services
// start with (driven): docker by default.
type ServiceRuntimeSetting interface {
	ContainerRuntime() string
	// SetContainerRuntime stores docker or podman; anything else is
	// INVALID_INPUT.
	SetContainerRuntime(name string) error
}
