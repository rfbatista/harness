// Package psprocs reads the machine's process table with ps(1), for the
// vigia de memória (ADR 0005), and signals processes. One ps run reads every
// process; the full command line is parsed in memory for its name, first
// argument and agent conversation id and is never kept.
package psprocs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"operators-mcp/internal/ports"
)

var (
	_ ports.ProcessTable    = Table{}
	_ ports.ProcessSignaler = Signaler{}
)

// Table is the process table as ps shows it.
type Table struct{}

// Read runs ps once. The session column is compared, never interpreted: on
// macOS it is the kernel's session handle, equal for processes of one session.
func (Table) Read(ctx context.Context) ([]ports.ProcessInfo, error) {
	out, err := psCommand(ctx).Output()
	if err != nil {
		return nil, fmt.Errorf("read the process table: %w", err)
	}
	return Parse(string(out)), nil
}

// psCommand is the ps run. It runs in the C locale: lstart is printed in the
// locale's own %c layout, and only C's is the five words Parse reads. LC_ALL is
// last so one inherited from the server cannot win.
func psCommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "ps", "-axww", "-o", "pid=,ppid=,sess=,rss=,lstart=,command=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd
}

// Parse reads ps's "pid ppid sess rss lstart command" lines; rss is in KB; lstart is five words ("Thu Nov 26 08:59:31 2026"). A line
// that does not parse is skipped.
func Parse(out string) []ports.ProcessInfo {
	var rows []ports.ProcessInfo
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		rss, e4 := strconv.ParseInt(f[3], 10, 64)
		if e1 != nil || e2 != nil || e4 != nil {
			continue
		}
		if !cLayoutStart(f[4:9]) {
			continue // another locale's layout: every later field would be shifted
		}
		started := strings.Join(f[4:9], " ")
		args := f[9:]
		p := ports.ProcessInfo{PID: pid, PPID: ppid, SID: sessionKey(f[2]), RSSBytes: rss << 10, Started: started, Name: filepath.Base(args[0]), Session: conversation(args)}
		if len(args) > 1 {
			p.Arg = args[1]
		}
		rows = append(rows, p)
	}
	return rows
}

// cLayoutStart reports whether w is C's lstart: "Thu Nov 26 08:59:31 2026".
func cLayoutStart(w []string) bool {
	if len(w) != 5 || len(w[0]) != 3 || len(w[1]) != 3 || len(w[4]) != 4 || strings.Count(w[3], ":") != 2 {
		return false
	}
	if _, err := strconv.Atoi(w[2]); err != nil {
		return false
	}
	_, err := strconv.Atoi(w[4])
	return err == nil
}

// sessionKey folds ps's session column (a number, or a hex handle on macOS)
// into an int; 0 when it has none.
func sessionKey(s string) int {
	if n, err := strconv.ParseInt(s, 0, 64); err == nil {
		return int(n)
	}
	if n, err := strconv.ParseInt(s, 16, 64); err == nil {
		return int(n)
	}
	return 0
}

// conversation is the claude conversation id a process was started for, from
// --session-id or --resume (also in their =value form).
func conversation(args []string) string {
	for i, a := range args {
		for _, flag := range []string{"--session-id", "--resume"} {
			if a == flag && i+1 < len(args) {
				return args[i+1]
			}
			if v, ok := strings.CutPrefix(a, flag+"="); ok {
				return v
			}
		}
	}
	return ""
}

// Signaler sends signals with kill(2). A process that is gone is not an error.
type Signaler struct{}

// Signal sends sig to pid.
func (Signaler) Signal(pid int, sig syscall.Signal) error {
	if err := syscall.Kill(pid, sig); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}
