package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// A service is a program of a project the person defined to run beside the
// others: a command line in the agent sandbox or a container image. The
// server keeps at most one run of each (ADR 0076). A service run is that one
// run, the current or the last.

// ServiceKind says what a service runs.
type ServiceKind string

const (
	ServiceKindCommand ServiceKind = "command"
	ServiceKindImage   ServiceKind = "image"
)

// ServiceStatus is where a service stands. A service that never ran is
// stopped.
type ServiceStatus string

const (
	ServiceStopped  ServiceStatus = "stopped"
	ServiceStarting ServiceStatus = "starting"
	ServiceRunning  ServiceStatus = "running"
	ServiceExited   ServiceStatus = "exited"
	ServiceFailed   ServiceStatus = "failed"
)

// Active says whether a run is going: starting or running.
func (s ServiceStatus) Active() bool { return s == ServiceStarting || s == ServiceRunning }

// CanMoveTo says whether a service in status s may take status next:
// stopped, exited and failed start; starting runs, fails or is stopped;
// running exits or is stopped.
func (s ServiceStatus) CanMoveTo(next ServiceStatus) bool {
	switch s {
	case ServiceStopped, ServiceExited, ServiceFailed:
		return next == ServiceStarting
	case ServiceStarting:
		return next == ServiceRunning || next == ServiceFailed || next == ServiceStopped
	case ServiceRunning:
		return next == ServiceExited || next == ServiceStopped
	}
	return false
}

// ServiceCommand is what a command service runs.
type ServiceCommand struct {
	Line string `json:"line"`
	// Subdir is relative to the repository's main checkout; empty is its root.
	Subdir string `json:"subdir"`
	// Ports are informational: the page links to them.
	Ports []int `json:"ports"`
}

// ServicePort maps a host port to a container port.
type ServicePort struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol"` // tcp | udp
}

// ServiceImage is what an image service runs.
type ServiceImage struct {
	Ref string `json:"ref"`
	// Args replace the image's command; none keeps it.
	Args  []string      `json:"args"`
	Ports []ServicePort `json:"ports"`
}

// ServiceStartedBy says who started a run.
type ServiceStartedBy struct {
	Kind      string `json:"kind"` // person | session
	SessionID string `json:"session_id"`
	Agent     string `json:"agent"`
}

// ServiceWhere says where a run runs: main (a repository's main checkout),
// session (an agent's worktree), service_folder (the harness-owned folder of
// a command service with no repository) or container.
type ServiceWhere struct {
	Kind      string `json:"kind"`
	Dir       string `json:"dir"`
	Branch    string `json:"branch"`
	SessionID string `json:"session_id"`
}

// ServiceRun is the current or last run of a service.
type ServiceRun struct {
	ID        string `json:"id"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	// ExitCode is set for an exited run only.
	ExitCode *int `json:"exit_code"`
	// Error is why a failed run could not start, in one line.
	Error     string           `json:"error"`
	StartedBy ServiceStartedBy `json:"started_by"`
	Where     ServiceWhere     `json:"where"`
	// Sandboxed is false for a command service that ran unconfined because
	// the sandbox setting was off (ADR 0074).
	Sandboxed bool `json:"sandboxed"`
}

// Service is a project's definition of a service, with its last run.
type Service struct {
	ID                  string          `json:"id"`
	ProjectID           string          `json:"project_id"`
	Name                string          `json:"name"`
	Kind                ServiceKind     `json:"kind"`
	RepositoryID        string          `json:"repository_id"`
	ExportRepositoryEnv bool            `json:"export_repository_env"`
	Env                 string          `json:"env"`
	Command             *ServiceCommand `json:"command"`
	Image               *ServiceImage   `json:"image"`
	Status              ServiceStatus   `json:"status"`
	Run                 *ServiceRun     `json:"run"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// ServiceInput is what the person defines: a Service's own fields.
type ServiceInput struct {
	Name                string          `json:"name"`
	Kind                ServiceKind     `json:"kind"`
	RepositoryID        string          `json:"repository_id"`
	ExportRepositoryEnv bool            `json:"export_repository_env"`
	Env                 string          `json:"env"`
	Command             *ServiceCommand `json:"command"`
	Image               *ServiceImage   `json:"image"`
}

var serviceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

func invalidService(format string, args ...any) error {
	return &StructuredError{Code: "INVALID_INPUT", Message: fmt.Sprintf(format, args...)}
}

// Normalized checks the input and returns it in its stored form: names and
// lines trimmed, the block of the other kind dropped, lists never nil,
// protocols defaulted, and the repository's env export off without a
// repository. A refusal is INVALID_INPUT with a message naming the field;
// it never quotes an env line.
func (in ServiceInput) Normalized() (ServiceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !serviceNamePattern.MatchString(in.Name) {
		return in, invalidService("name must be 1 to 64 characters: lowercase letters, digits, '-', '_' or '.', starting with a letter or digit")
	}
	if in.RepositoryID == "" {
		in.ExportRepositoryEnv = false
	}
	if _, skipped := ParseDotenv(in.Env); len(skipped) > 0 {
		return in, invalidService("env line %d is not KEY=VALUE", skipped[0])
	}
	switch in.Kind {
	case ServiceKindCommand:
		in.Image = nil
		c, err := normalizedServiceCommand(in.Command)
		if err != nil {
			return in, err
		}
		in.Command = c
	case ServiceKindImage:
		in.Command = nil
		img, err := normalizedServiceImage(in.Image)
		if err != nil {
			return in, err
		}
		in.Image = img
	default:
		return in, invalidService("kind must be command or image")
	}
	return in, nil
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

func normalizedServiceCommand(c *ServiceCommand) (*ServiceCommand, error) {
	if c == nil {
		return nil, invalidService("command.line is required for a command service")
	}
	out := ServiceCommand{Line: strings.TrimSpace(c.Line), Subdir: strings.TrimSpace(c.Subdir), Ports: append([]int{}, c.Ports...)}
	if out.Line == "" {
		return nil, invalidService("command.line is required for a command service")
	}
	if strings.HasPrefix(out.Subdir, "/") || strings.HasPrefix(out.Subdir, "~") || hasDotDot(out.Subdir) {
		return nil, invalidService("command.subdir must be relative and stay inside the repository")
	}
	for i, p := range out.Ports {
		if !validPort(p) {
			return nil, invalidService("command.ports[%d] must be a port from 1 to 65535", i)
		}
	}
	return &out, nil
}

func hasDotDot(p string) bool {
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func normalizedServiceImage(img *ServiceImage) (*ServiceImage, error) {
	if img == nil || strings.TrimSpace(img.Ref) == "" {
		return nil, invalidService("image.ref is required for an image service")
	}
	out := ServiceImage{Ref: strings.TrimSpace(img.Ref), Args: append([]string{}, img.Args...), Ports: append([]ServicePort{}, img.Ports...)}
	// The ref is an argument of the container runtime: it may not look like
	// an option or hold anything a shell or the runtime would split.
	if strings.HasPrefix(out.Ref, "-") || strings.ContainsFunc(out.Ref, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return nil, invalidService("image.ref must be an image reference without spaces, not starting with '-'")
	}
	hostSeen := map[int]bool{}
	for i := range out.Ports {
		p := &out.Ports[i]
		if p.Protocol == "" {
			p.Protocol = "tcp"
		}
		switch {
		case !validPort(p.Host):
			return nil, invalidService("image.ports[%d].host must be a port from 1 to 65535", i)
		case !validPort(p.Container):
			return nil, invalidService("image.ports[%d].container must be a port from 1 to 65535", i)
		case p.Protocol != "tcp" && p.Protocol != "udp":
			return nil, invalidService("image.ports[%d].protocol must be tcp or udp", i)
		case hostSeen[p.Host]:
			return nil, invalidService("image.ports[%d].host: host port %d is mapped twice", i, p.Host)
		}
		hostSeen[p.Host] = true
	}
	return &out, nil
}

// Clone is a copy that shares nothing with s, for a feed event that outlives
// the change.
func (s *Service) Clone() *Service {
	c := *s
	if s.Command != nil {
		cmd := *s.Command
		cmd.Ports = append([]int{}, s.Command.Ports...)
		c.Command = &cmd
	}
	if s.Image != nil {
		img := *s.Image
		img.Args = append([]string{}, s.Image.Args...)
		img.Ports = append([]ServicePort{}, s.Image.Ports...)
		c.Image = &img
	}
	if s.Run != nil {
		run := *s.Run
		if s.Run.ExitCode != nil {
			code := *s.Run.ExitCode
			run.ExitCode = &code
		}
		c.Run = &run
	}
	return &c
}
