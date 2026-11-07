package services

import (
	"os"
	"path/filepath"
	"strings"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The sandbox of command services (ADR 0074): the agent sandbox, applied to
// a service's process by the driver agents use and from the allowlist they
// get.

// confinement is the sandbox a run is confined by.
type confinement struct {
	driver ports.SandboxDriver
	policy ports.SandboxPolicy
	// tmp is the run's temp dir, inside the service folder: the system temp
	// dir is closed to a sandboxed process.
	tmp string
}

// sandboxSwitchOn says whether the person's sandbox switch is on. Without a
// switch wired there is no sandbox in the way.
func (s *Service) sandboxSwitchOn() bool {
	return s.opts.Sandbox != nil && s.opts.Sandbox.Enabled()
}

// driver is the platform's sandbox driver, nil when it has none.
func (s *Service) driver() ports.SandboxDriver {
	if s.opts.Driver == nil {
		return nil
	}
	return s.opts.Driver()
}

// checkSandbox refuses a command service while the sandbox is on and the
// platform has no driver. Nothing has been started or created yet.
func (s *Service) checkSandbox(svc *domain.Service) error {
	if svc.Kind == domain.ServiceKindCommand && s.sandboxSwitchOn() && s.driver() == nil {
		return &domain.StructuredError{Code: "SANDBOX_UNAVAILABLE", Message: "the agent sandbox is on and this platform has no sandbox driver: command services only run confined. Install a driver or switch the sandbox off in Agent isolation settings"}
	}
	return nil
}

// confine works out the sandbox of a run of svc in where: nil when the
// person's switch is off, the refusal when it is on without a driver. The
// policy comes from the agent policy builder: writable are the working
// directory, the service's folder and the allowlist's writable paths;
// readable are the allowlist and the project's repositories. The network is
// not restricted.
func (s *Service) confine(svc *domain.Service, where domain.ServiceWhere) (*confinement, error) {
	if svc.Kind != domain.ServiceKindCommand || !s.sandboxSwitchOn() {
		return nil, nil
	}
	if err := s.checkSandbox(svc); err != nil {
		return nil, err
	}
	folder := filepath.Join(s.folderRoot(), svc.ID)
	sess := orchestration.SandboxSession{
		Mode: domain.SessionModeImplement, Worktree: where.Dir, ReadAllRepos: true,
		Allow: []ports.SandboxPath{{Path: folder, Write: true}},
	}
	if s.opts.Allow != nil {
		sess.Allow = append(sess.Allow, s.opts.Allow()...)
	}
	if s.opts.Checkouts != nil {
		sess.Repositories = s.opts.Checkouts(svc.ProjectID)
	}
	return &confinement{driver: s.driver(), policy: orchestration.BuildSandboxPolicy(sess), tmp: filepath.Join(folder, "tmp")}, nil
}

// wrap answers spec as the driver runs it under the policy. The folder and
// its temp dir are made first, and the run's TMPDIR is that temp dir.
func (c *confinement) wrap(spec ports.ServiceProcessSpec) (ports.ServiceProcessSpec, error) {
	if err := os.MkdirAll(c.tmp, 0o700); err != nil {
		return spec, err
	}
	env := make([]string, 0, len(spec.Env)+2)
	for _, kv := range spec.Env {
		if !strings.HasPrefix(kv, "TMPDIR=") {
			env = append(env, kv)
		}
	}
	env = append(env, "TMPDIR="+c.tmp, ports.SandboxMarkerEnv+"="+c.driver.Name())
	out, err := c.driver.Wrap(c.policy, ports.SandboxCommand{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: env})
	if err != nil {
		return spec, err
	}
	spec.Path, spec.Args, spec.Dir, spec.Env = out.Path, out.Args, out.Dir, out.Env
	return spec, nil
}

// logLine is the [harness] line that says how the run is confined.
func (c *confinement) logLine() string {
	if c == nil {
		return "not sandboxed"
	}
	return "sandboxed (" + c.driver.Name() + ")"
}
