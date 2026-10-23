package ports

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
