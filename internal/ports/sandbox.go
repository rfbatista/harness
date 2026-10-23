package ports

import (
	"context"
	"time"
)

// The agent sandbox (ADR 0017): the OS-level confinement every agent claude
// process runs in. A sandbox driver applies it; the policy says what the
// process may touch.

// SandboxPath is one path of the sandbox allowlist, and whether the process
// may write it as well as read it.
type SandboxPath struct {
	Path  string `json:"path"`
	Write bool   `json:"write,omitempty"`
}

// SandboxPolicy is what an agent sandbox lets a process do with files. A
// writable path is readable too; Readable holds only the read-only ones.
type SandboxPolicy struct {
	// WorkingDir is the directory the process starts in: the session's
	// worktree.
	WorkingDir string   `json:"working_dir"`
	Readable   []string `json:"readable,omitempty"`
	Writable   []string `json:"writable,omitempty"`
}

// SandboxCommand is a command line as a sandbox driver wraps it.
type SandboxCommand struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

// SandboxDriver is the platform mechanism that applies the agent sandbox:
// Seatbelt on macOS, bubblewrap on Linux (driven).
type SandboxDriver interface {
	// Name says which mechanism this is, for the session's sandbox facts.
	Name() string
	// Wrap answers the command that runs cmd confined by policy.
	Wrap(policy SandboxPolicy, cmd SandboxCommand) (SandboxCommand, error)
}

// SandboxExtraPaths are the paths the person adds to the sandbox allowlist
// in settings, read at each session start.
type SandboxExtraPaths func() []SandboxPath

// SandboxViolation is one thing the sandbox denied a process: when, which
// operation, on what path (or other target), and the process's name.
type SandboxViolation struct {
	When      time.Time `json:"when"`
	Operation string    `json:"operation"`
	Path      string    `json:"path,omitempty"`
	Process   string    `json:"process"`
}

// SandboxViolationSource follows the violations a platform records for a
// process tree. Optional next to a SandboxDriver: a platform without one
// collects none.
type SandboxViolationSource interface {
	// Follow streams the violations of the processes confined with a policy
	// whose WorkingDir is workingDir (the session's worktree) and everything
	// they spawn, from when Follow returns until ctx ends; the channel then
	// closes.
	Follow(ctx context.Context, workingDir string) (<-chan SandboxViolation, error)
}

// MaxSandboxViolations is how many violations the harness keeps per session:
// the most recent ones.
const MaxSandboxViolations = 1000

// SandboxViolationStore keeps a session's sandbox violations: the latest
// MaxSandboxViolations, dropped with the session.
type SandboxViolationStore interface {
	// AddSandboxViolation stores v for the session, dropping its oldest
	// violations beyond the cap.
	AddSandboxViolation(sessionID string, v SandboxViolation) error
	// SandboxViolations are the session's stored violations, newest first.
	SandboxViolations(sessionID string) ([]SandboxViolation, error)
}
