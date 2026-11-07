// Package containerrunnertest is a stand-in for the container runtime, for
// tests of image services: it takes the runs the real runner would take,
// records the command line the real builder makes for each and plays the
// runtime's part (pulling, running, failing, stopping) without a container.
package containerrunnertest

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/adapter/out/containerrunner"
	"operators-mcp/internal/ports"
)

var (
	_ ports.ServiceProcessRunner = (*Fake)(nil)
	_ ports.ServiceRunDescriber  = (*Fake)(nil)
)

// Fake implements ports.ServiceProcessRunner. Hold keeps each start pulling
// until its Run is Released; Fail makes each start end as the runtime does
// on an error, with Fail on its stderr.
type Fake struct {
	Hold bool
	Fail string

	mu   sync.Mutex
	runs []*Run
}

// Run is one start the fake took.
type Run struct {
	// Runtime is the runtime the run started with; Argv the arguments of its
	// `run` (without the runtime itself).
	Runtime string
	Argv    []string
	Spec    ports.ServiceProcessSpec

	mu       sync.Mutex
	stops    []string
	ready    chan struct{}
	readyOne sync.Once
	pr       *io.PipeReader
	pw       *io.PipeWriter
	done     chan struct{}
	endOne   sync.Once
	code     int
}

// Describe implements ports.ServiceRunDescriber.
func (*Fake) Describe(spec ports.ServiceProcessSpec) string {
	return containerrunner.Runner{}.Describe(spec)
}

// Start implements ports.ServiceProcessRunner.
func (f *Fake) Start(_ context.Context, spec ports.ServiceProcessSpec) (ports.ServiceProcessHandle, error) {
	if spec.Container == nil {
		return nil, errors.New("not a container run")
	}
	argv, err := containerrunner.RunArgs(spec.Name, *spec.Container)
	if err != nil {
		return nil, err
	}
	r := &Run{Runtime: spec.Container.Runtime, Argv: argv, Spec: spec, ready: make(chan struct{}), done: make(chan struct{})}
	r.pr, r.pw = io.Pipe()
	f.mu.Lock()
	hold, fail := f.Hold, f.Fail
	f.runs = append(f.runs, r)
	f.mu.Unlock()
	switch {
	case fail != "":
		go func() {
			_, _ = io.WriteString(r.pw, fail+"\n")
			r.End(125)
		}()
	case hold:
		go func() { _, _ = io.WriteString(r.pw, "Unable to find image locally\npulling...\n") }()
	default:
		r.Release()
	}
	return &handle{r: r}, nil
}

// Runs are the starts taken so far.
func (f *Fake) Runs() []*Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*Run(nil), f.runs...)
}

// Release says the container has started.
func (r *Run) Release() { r.readyOne.Do(func() { close(r.ready) }) }

// Say writes to the run's output, as the container's stdout would.
func (r *Run) Say(text string) { _, _ = io.WriteString(r.pw, text) }

// End ends the run on its own with code.
func (r *Run) End(code int) {
	r.endOne.Do(func() {
		r.code = code
		_ = r.pw.Close()
		close(r.done)
	})
}

// Stops are the runtimes `stop` was called with, in order, as
// "<runtime> stop -t <seconds> <name>".
func (r *Run) Stops() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.stops...)
}

// Has says whether argv holds the flag with exactly value as its argument.
func (r *Run) Has(flag, value string) bool {
	for i := 0; i+1 < len(r.Argv); i++ {
		if r.Argv[i] == flag && r.Argv[i+1] == value {
			return true
		}
	}
	return false
}

type handle struct{ r *Run }

func (h *handle) Output() io.ReadCloser  { return h.r.pr }
func (h *handle) Ready() <-chan struct{} { return h.r.ready }
func (h *handle) Wait() int              { <-h.r.done; return h.r.code }

func (h *handle) Stop(grace time.Duration) {
	r := h.r
	secs := max(1, int((grace+time.Second-1)/time.Second))
	r.mu.Lock()
	r.stops = append(r.stops, strings.Join([]string{r.Runtime, "stop", "-t", itoa(secs), r.Spec.Name}, " "))
	r.mu.Unlock()
	r.End(143)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
