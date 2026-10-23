package ports

// SandboxSettings is the person's own sandbox configuration (ADR 0017): the
// switch and the extra allowlist paths, kept in settings and read at each
// session start. Paths are keyed by the path as the person wrote it.
type SandboxSettings interface {
	// Enabled says whether the sandbox is on; it is on until switched off.
	Enabled() bool
	SetEnabled(on bool) error
	// Paths are the extra paths, in the order they were added.
	Paths() []SandboxPath
	// AddPath adds a path and answers the list. A path that is empty,
	// relative, too wide or already there is refused with INVALID_INPUT.
	AddPath(p SandboxPath) ([]SandboxPath, error)
	// SetPathAccess makes a listed path read-only or read-write.
	SetPathAccess(path string, write bool) ([]SandboxPath, error)
	// RemovePath drops a listed path.
	RemovePath(path string) ([]SandboxPath, error)
	// PathWarnings says what is worth a second look about a path, as codes:
	// "missing" (nothing exists there yet) and "credentials" (it is in a
	// folder that usually holds credentials). Warnings never block saving.
	PathWarnings(path string) []string
}

// SandboxBuiltinEntry is one thing the sandbox always allows.
type SandboxBuiltinEntry struct {
	// Label says what it is; Path is where, empty when it is per session.
	Label string `json:"label"`
	Path  string `json:"path"`
	Write bool   `json:"write"`
}

// SandboxBuiltinGroup is a titled run of the built-in allowlist.
type SandboxBuiltinGroup struct {
	Title   string                `json:"title"`
	Entries []SandboxBuiltinEntry `json:"entries"`
}
