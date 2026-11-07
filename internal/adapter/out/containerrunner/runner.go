// Package containerrunner runs an image service through the container
// runtime (docker or podman): an attached `run` whose output is the
// service's log, stopped with `stop`. It builds the runtime's command from
// the service's validated fields only (see RunArgs).
package containerrunner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"operators-mcp/internal/ports"
)

var (
	_ ports.ServiceProcessRunner    = Runner{}
	_ ports.ServiceRunDescriber     = Runner{}
	_ ports.ServiceContainerSweeper = Runner{}
)

// readyPoll is how often a starting container is asked whether it runs.
const readyPoll = 200 * time.Millisecond

// Runner implements ports.ServiceProcessRunner for image services.
type Runner struct{}

func runtimeOK(runtime string) error {
	if runtime != "docker" && runtime != "podman" {
		return errors.New("the container runtime must be docker or podman")
	}
	return nil
}

// Describe implements ports.ServiceRunDescriber.
func (Runner) Describe(spec ports.ServiceProcessSpec) string {
	if spec.Container == nil {
		return ""
	}
	return Describe(spec.Name, *spec.Container)
}

// Start spawns `<runtime> run` attached and returns at once: the container
// is still pulling its image or starting. The handle's Ready closes when the
// runtime reports the container running. An error here is a spawn that
// failed; a runtime error (a pull that fails, a port in use) is the run
// ending, with the runtime's message in its output.
func (Runner) Start(_ context.Context, spec ports.ServiceProcessSpec) (ports.ServiceProcessHandle, error) {
	c := spec.Container
	if c == nil || spec.Name == "" {
		return nil, errors.New("not a container run")
	}
	if err := runtimeOK(c.Runtime); err != nil {
		return nil, err
	}
	args, err := RunArgs(spec.Name, *c)
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(c.Runtime, args...)
	cmd.Env = spec.Env
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = cmd.Start()
	_ = pw.Close()
	if err != nil {
		_ = pr.Close()
		return nil, err
	}
	h := &handle{cmd: cmd, out: pr, runtime: c.Runtime, name: spec.Name, env: spec.Env, done: make(chan struct{}), ready: make(chan struct{})}
	go h.reap()
	go h.watchReady()
	return h, nil
}

type handle struct {
	cmd     *exec.Cmd
	out     *os.File
	runtime string
	name    string
	env     []string
	done    chan struct{}
	ready   chan struct{}
	code    int
}

func (h *handle) Output() io.ReadCloser  { return h.out }
func (h *handle) Ready() <-chan struct{} { return h.ready }

func (h *handle) reap() {
	err := h.cmd.Wait()
	h.code = exitCode(h.cmd, err)
	close(h.done)
}

func exitCode(cmd *exec.Cmd, err error) int {
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	if err != nil && cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

func (h *handle) Wait() int {
	<-h.done
	return h.code
}

// watchReady asks the runtime whether the container runs until it does or
// the run ended.
func (h *handle) watchReady() {
	for {
		select {
		case <-h.done:
			return
		case <-time.After(readyPoll):
		}
		out, err := h.runtimeCmd(5*time.Second, "inspect", "-f", "{{.State.Running}}", h.name)
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			close(h.ready)
			return
		}
	}
}

func (h *handle) runtimeCmd(timeout time.Duration, args ...string) ([]byte, error) {
	return runtimeCmd(h.runtime, h.env, timeout, args...)
}

func runtimeCmd(runtime string, env []string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, runtime, args...)
	cmd.Env = env
	return cmd.CombinedOutput()
}

// Stop runs `<runtime> stop -t <grace>` with the runtime the run started
// with. A container that does not exist yet (its image is still pulling) is
// ended by signalling the attached command instead. What outlives the
// timeout is killed and removed.
func (h *handle) Stop(grace time.Duration) {
	select {
	case <-h.done:
		return
	default:
	}
	secs := max(1, int((grace+time.Second-1)/time.Second))
	if _, err := h.runtimeCmd(grace+10*time.Second, "stop", "-t", strconv.Itoa(secs), h.name); err != nil {
		_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGTERM)
	}
	select {
	case <-h.done:
	case <-time.After(grace + 5*time.Second):
		_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGKILL)
		_, _ = h.runtimeCmd(10*time.Second, "rm", "-f", h.name)
		<-h.done
	}
}

// ListServiceContainers implements ports.ServiceContainerSweeper.
func (Runner) ListServiceContainers(_ context.Context, runtime string) ([]ports.ServiceContainerRef, error) {
	if err := runtimeOK(runtime); err != nil {
		return nil, err
	}
	out, err := runtimeCmd(runtime, os.Environ(), 30*time.Second, "ps", "-a", "--filter", "label="+LabelService,
		"--format", `{{.Names}}	{{.Label "`+LabelService+`"}}	{{.Label "`+LabelInstance+`"}}`)
	if err != nil {
		return nil, errors.New(strings.TrimSpace(string(out)))
	}
	var refs []ports.ServiceContainerRef
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) == 3 && f[0] != "" {
			refs = append(refs, ports.ServiceContainerRef{Name: f[0], ServiceID: f[1], Instance: f[2]})
		}
	}
	return refs, nil
}

// RemoveServiceContainer implements ports.ServiceContainerSweeper.
func (Runner) RemoveServiceContainer(_ context.Context, runtime, name string) error {
	if err := runtimeOK(runtime); err != nil {
		return err
	}
	if name == "" || strings.HasPrefix(name, "-") {
		return errors.New("not a container name")
	}
	if out, err := runtimeCmd(runtime, os.Environ(), 30*time.Second, "rm", "-f", name); err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}
