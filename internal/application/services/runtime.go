package services

import (
	"context"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SettingContainerRuntime is the setting holding the container runtime.
const SettingContainerRuntime = "services_container_runtime"

// RuntimeSetting keeps the container runtime in the settings.
type RuntimeSetting struct{ Settings ports.Settings }

var _ ports.ServiceRuntimeSetting = RuntimeSetting{}

// ContainerRuntime is the stored runtime; docker when none or an unknown one
// is stored.
func (r RuntimeSetting) ContainerRuntime() string {
	if v := r.Settings.Setting(SettingContainerRuntime); validRuntime(v) {
		return v
	}
	return DefaultRuntime
}

// SetContainerRuntime implements ports.ServiceRuntimeSetting.
func (r RuntimeSetting) SetContainerRuntime(name string) error {
	if !validRuntime(name) {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "container_runtime must be docker or podman"}
	}
	_, err := r.Settings.UpdateSettings(map[string]string{SettingContainerRuntime: name})
	return err
}

func validRuntime(name string) bool { return name == "docker" || name == "podman" }

// memoryRuntime is the setting of a module built without settings.
type memoryRuntime struct {
	mu   sync.Mutex
	name string
}

func (m *memoryRuntime) ContainerRuntime() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.name == "" {
		return DefaultRuntime
	}
	return m.name
}

func (m *memoryRuntime) SetContainerRuntime(name string) error {
	if !validRuntime(name) {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "container_runtime must be docker or podman"}
	}
	m.mu.Lock()
	m.name = name
	m.mu.Unlock()
	return nil
}

// ContainerRuntime implements ports.ServiceManager.
func (s *Service) ContainerRuntime(context.Context) ports.ServiceRuntimeInfo { return s.runtimeInfo() }

// SetContainerRuntime implements ports.ServiceManager. A run that is going
// keeps the runtime it started with.
func (s *Service) SetContainerRuntime(_ context.Context, name string) (ports.ServiceRuntimeInfo, error) {
	if err := s.opts.Runtime.SetContainerRuntime(name); err != nil {
		return ports.ServiceRuntimeInfo{}, err
	}
	return s.runtimeInfo(), nil
}
