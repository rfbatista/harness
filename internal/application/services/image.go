package services

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Image services run in a container through the container runner: the
// service's validated fields go to it as a ports.ServiceContainer, never as a
// command line. A start answers while the container is still starting (an
// image pull can take long); the run settles when the runtime reports the
// container running, or fails when the runtime ends first.

// imagePlan is what a start works out for an image service.
type imagePlan struct {
	container ports.ServiceContainer
}

// prepareImage refuses a start while the runtime's command is missing, so
// nothing starts, and gathers the container's fields. The repository's .env
// goes into the container's env as for a command service, but the server's
// own environment does not: the runtime command gets only the base, and the
// container's env goes in a file in the service's folder.
func (s *Service) prepareImage(ctx context.Context, svc *domain.Service, overrides map[string]string) (*prepared, error) {
	if svc.Image == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "image.ref is required for an image service"}
	}
	if err := s.checkRuntime(svc); err != nil {
		return nil, err
	}
	runtime := s.opts.Runtime.ContainerRuntime()
	if _, err := s.repository(ctx, svc); err != nil {
		return nil, err
	}
	env, notes, vars, err := s.layeredEnv(ctx, svc, nil, overrides)
	if err != nil {
		return nil, err
	}
	return &prepared{
		where: domain.ServiceWhere{Kind: "container"},
		env:   s.opts.BaseEnv(),
		notes: notes,
		vars:  vars,
		image: &imagePlan{container: ports.ServiceContainer{
			Runtime: runtime, ServiceID: svc.ID, Instance: s.opts.Instance,
			Image: svc.Image.Ref, Args: svc.Image.Args, Ports: svc.Image.Ports, Env: env,
			EnvDir: filepath.Join(s.folderRoot(), svc.ID),
		}},
	}, nil
}

// checkRuntime refuses an image service while the runtime's command is
// missing; a command service has none to miss.
func (s *Service) checkRuntime(svc *domain.Service) error {
	if svc.Kind != domain.ServiceKindImage {
		return nil
	}
	runtime := s.opts.Runtime.ContainerRuntime()
	if s.opts.LookPath == nil {
		return runtimeUnavailable(runtime)
	}
	if _, err := s.opts.LookPath(runtime); err != nil {
		return runtimeUnavailable(runtime)
	}
	return nil
}

func runtimeUnavailable(runtime string) error {
	return &domain.StructuredError{Code: "RUNTIME_UNAVAILABLE", Message: "the container runtime " + runtime + " was not found on this server's PATH"}
}

// containerName names a run's container by its service and run.
func containerName(serviceID, runID string) string {
	return "harness-svc-" + shortID(serviceID) + "-" + shortID(strings.TrimPrefix(runID, "run-"))
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// launchImage writes the run's [harness] lines and spawns the runtime's
// attached command. It returns once that is spawned; supervision settles
// the run.
func (s *Service) launchImage(ctx context.Context, r *run, svc *domain.Service, runner ports.ServiceProcessRunner, p *prepared, lg *logFile) {
	spec := ports.ServiceProcessSpec{Name: containerName(svc.ID, r.id), Env: p.env, Container: &p.image.container}
	if d, ok := runner.(ports.ServiceRunDescriber); ok {
		lg.append([]byte("[harness] command: " + oneLine(d.Describe(spec)) + "\n"))
	}
	lg.append([]byte("[harness] container: " + spec.Name + " (sandboxed by the container)\n"))
	for _, n := range p.notes {
		lg.append([]byte("[harness] " + n + "\n"))
	}
	h, err := runner.Start(ctx, spec)
	if err != nil {
		s.fail(r, lg, "could not start: "+oneLine(err.Error()))
		return
	}
	s.mu.Lock()
	r.handle = h
	stopNow := r.stopping
	s.mu.Unlock()
	go s.superviseImage(r, h, lg)
	if stopNow {
		go h.Stop(s.opts.StopGrace)
	}
}

// superviseImage settles the run when the container is going and records
// how the run ended. A run the runtime ended before the container started
// failed, with the last line of the runtime's output as its error.
func (s *Service) superviseImage(r *run, h ports.ServiceProcessHandle, lg *logFile) {
	var ready <-chan struct{}
	if rh, ok := h.(ports.ServiceReadyHandle); ok {
		ready = rh.Ready()
	} else {
		c := make(chan struct{})
		close(c)
		ready = c
	}
	tail := &lastLine{r: h.Output()}
	pumped := make(chan struct{})
	go func() {
		lg.pump(tail)
		close(pumped)
	}()
	ended := make(chan struct{})
	over := false // guarded by mu: the run's end is being recorded
	go func() {
		select {
		case <-ready:
		case <-ended:
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if over {
			return
		}
		s.moveLocked(r.serviceID, domain.ServiceRunning, nil)
		close(r.settled)
	}()
	code := h.Wait()
	close(ended)
	select {
	case <-pumped:
	case <-time.After(outputDrain):
	}
	_ = h.Output().Close()
	<-pumped

	s.mu.Lock()
	over = true
	stopped := r.stopping
	started := false
	select {
	case <-r.settled:
		started = true
	default:
	}
	s.mu.Unlock()

	msg := tail.last()
	if msg == "" {
		msg = "the container runtime ended with code " + strconv.Itoa(code) + " before the container started"
	}
	line := "exited with code " + strconv.Itoa(code)
	switch {
	case stopped:
		line = "stopped"
	case !started:
		line = "failed: " + msg
	}
	lg.append([]byte("[harness] " + line + "\n"))

	s.mu.Lock()
	defer s.mu.Unlock()
	next := domain.ServiceExited
	switch {
	case stopped:
		next = domain.ServiceStopped
	case !started:
		next = domain.ServiceFailed
	}
	s.moveLocked(r.serviceID, next, func(svc *domain.Service) {
		if next == domain.ServiceExited {
			svc.Run.ExitCode = &code
		}
		if next == domain.ServiceFailed {
			svc.Run.Error = msg
		}
		svc.Run.EndedAt = s.opts.Now().UTC().Format(time.RFC3339)
	})
	delete(s.runs, r.serviceID)
	if !started {
		close(r.settled)
	}
	close(r.done)
}

// lastLine passes a reader through and keeps its last non-empty line, one
// line and cut short: what the runtime said last is why it failed.
type lastLine struct {
	r    io.Reader
	mu   sync.Mutex
	part string
	line string
}

const lastLineMax = 300

func (l *lastLine) Read(b []byte) (int, error) {
	n, err := l.r.Read(b)
	if n > 0 {
		l.mu.Lock()
		l.part += string(b[:n])
		if i := strings.LastIndexByte(l.part, '\n'); i >= 0 {
			for _, ln := range strings.Split(l.part[:i], "\n") {
				if t := oneLine(ln); t != "" {
					l.line = t
				}
			}
			l.part = l.part[i+1:]
		}
		if len(l.part) > 4*lastLineMax {
			l.part = l.part[len(l.part)-lastLineMax:]
		}
		l.mu.Unlock()
	}
	return n, err
}

func (l *lastLine) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := l.line
	if t := oneLine(l.part); t != "" {
		line = t
	}
	if len(line) > lastLineMax {
		line = line[:lastLineMax]
	}
	return line
}
