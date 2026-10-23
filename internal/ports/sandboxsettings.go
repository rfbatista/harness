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
}
