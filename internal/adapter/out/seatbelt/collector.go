package seatbelt

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"operators-mcp/internal/ports"
)

// Collector follows the macOS unified log for sandbox denials (sender
// Sandbox). Every profile the Driver renders tags its denials with the
// session's working directory, so a denial belongs to the session whose
// process tree it came from however briefly the process lived.
type Collector struct {
	// LogBin is the log command. Empty: /usr/bin/log.
	LogBin string
	// StartTimeout bounds the wait for the log stream to come up. Zero: 10s.
	StartTimeout time.Duration
}

var _ ports.SandboxViolationSource = (*Collector)(nil)

// NewCollector is a Collector reading the system's unified log.
func NewCollector() *Collector { return &Collector{} }

// Follow starts a log stream and yields the denials of the processes confined
// with a policy whose working dir is workingDir, and of their descendants. It
// returns once the stream is up. The channel closes when ctx
// ends or the stream does.
func (c *Collector) Follow(ctx context.Context, workingDir string) (<-chan ports.SandboxViolation, error) {
	bin := c.LogBin
	if bin == "" {
		bin = "/usr/bin/log"
	}
	cmd := exec.CommandContext(ctx, bin, "stream", "--style", "ndjson",
		"--predicate", `sender == "Sandbox" AND eventMessage CONTAINS "deny"`)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox violations: %w", err)
	}
	r := bufio.NewReaderSize(stdout, 1<<16)
	timeout := c.StartTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	// The stream prints a "Filtering" header line once it is attached.
	hdr := make(chan error, 1)
	var first []byte
	go func() {
		line, err := r.ReadBytes('\n')
		first = line
		hdr <- err
	}()
	select {
	case err := <-hdr:
		if err != nil {
			cmd.Wait()
			return nil, fmt.Errorf("sandbox violations: log stream ended: %w", err)
		}
	case <-time.After(timeout):
		cmd.Process.Kill()
		cmd.Wait()
		return nil, errors.New("sandbox violations: log stream did not start")
	}

	tag := tagFor(workingDir)
	out := make(chan ports.SandboxViolation, 64)
	go func() {
		defer close(out)
		defer cmd.Wait()
		handle := func(line []byte) {
			if v, vtag, ok := parseViolation(line); ok && vtag == tag {
				select {
				case out <- v:
				case <-ctx.Done():
				}
			}
		}
		handle(first)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				handle(line)
			}
			if err != nil {
				return
			}
		}
	}()
	return out, nil
}

var denyRe = regexp.MustCompile(`(?s)^Sandbox: (.+)\((\d+)\) deny\(\d+\) (\S+)(?: (.*))?$`)

// tagPrefix starts the message every profile attaches to its denials.
const tagPrefix = "harness-sandbox:"

// tagFor is the tag of the sandboxes of one working dir.
func tagFor(workingDir string) string {
	return tagPrefix + strings.ReplaceAll(filepath.Clean(workingDir), "\n", " ")
}

// parseViolation reads one ndjson line of the log stream: the denial and the
// tag its profile put after the message.
func parseViolation(line []byte) (ports.SandboxViolation, string, bool) {
	var rec struct {
		Message   string `json:"eventMessage"`
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return ports.SandboxViolation{}, "", false
	}
	msg, tag := rec.Message, ""
	if i := strings.LastIndex(msg, "\n"+tagPrefix); i >= 0 {
		msg, tag = msg[:i], msg[i+1:]
	}
	m := denyRe.FindStringSubmatch(msg)
	if m == nil {
		return ports.SandboxViolation{}, "", false
	}
	when, err := time.Parse("2006-01-02 15:04:05.999999-0700", rec.Timestamp)
	if err != nil {
		when = time.Now()
	}
	return ports.SandboxViolation{When: when, Operation: m[3], Path: m[4], Process: m[1]}, tag, true
}
