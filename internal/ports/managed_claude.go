package ports

import (
	"context"
	"time"
)

// ManagedClaude is the claude CLI the harness installs itself (ADR 0019): the
// official native build, in the harness data dir, at a version the person
// picks. *managedclaude.Service satisfies it.
type ManagedClaude interface {
	// State lists the installed versions, which one agents use, and the
	// install that runs or the last one that failed.
	State() ManagedClaudeState
	// Releases asks the release server for the newest versions. Fails when
	// the server cannot be reached.
	Releases(ctx context.Context) (ManagedClaudeReleases, error)
	// Install downloads a version in the background ("latest", "stable" or
	// X.Y.Z), verifies its checksum and keeps it next to the others without
	// switching to it, except when nothing is active yet. Answers the state as
	// the install starts. Refuses INVALID_INPUT, MANAGED_CLAUDE_BUSY and
	// MANAGED_CLAUDE_VERSION_INSTALLED.
	Install(ctx context.Context, version string) (ManagedClaudeState, error)
	// Switch makes an installed version the one agents use and removes the
	// others. Refuses MANAGED_CLAUDE_VERSION_NOT_FOUND.
	Switch(version string) (ManagedClaudeState, error)
	// Cancel stops the install that runs and removes what it downloaded,
	// reporting no failure; with none running it changes nothing.
	Cancel() (ManagedClaudeState, error)
	// Remove deletes an installed version that is not the current one.
	// Refuses MANAGED_CLAUDE_VERSION_CURRENT and MANAGED_CLAUDE_VERSION_NOT_FOUND.
	Remove(version string) (ManagedClaudeState, error)
}

// ManagedClaudeState is what the person sees of the managed claude.
type ManagedClaudeState struct {
	// Path is the stable path agents run: it follows a switch.
	Path string `json:"path"`
	// Current is the active version; "" when none is installed.
	Current   string                 `json:"current"`
	Installed []ManagedClaudeVersion `json:"installed"`
	// Installing is the install in progress, nil when none is.
	Installing *ManagedClaudeInstall `json:"installing"`
	// Failed is the last install that failed, nil when it succeeded or a new
	// one started.
	Failed *ManagedClaudeFailure `json:"failed"`
	// KeepingSessions is, in the answer to a switch, how many running
	// sessions keep the version they started with; 0 in every other answer.
	KeepingSessions int `json:"keepingSessions"`
}

// ManagedClaudeVersion is one installed version.
type ManagedClaudeVersion struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
	Active      bool      `json:"active"`
}

// ManagedClaudeInstall is an install under way. Total is -1 while unknown.
type ManagedClaudeInstall struct {
	Version string `json:"version"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
}

// ManagedClaudeFailure is an install that did not complete.
type ManagedClaudeFailure struct {
	Version string `json:"version"`
	Message string `json:"message"`
}

// ManagedClaudeReleases are the versions the release server names.
type ManagedClaudeReleases struct {
	Latest string `json:"latest"`
	Stable string `json:"stable"`
}

// ManagedClaudeBin says which claude binary agents run (driving, read at each
// start).
type ManagedClaudeBin interface {
	// Bin is the path agents run: the -claude.bin override when set,
	// otherwise the managed current path (which may not exist yet).
	Bin() string
	// Installed says whether Bin can run: the override is trusted, the
	// managed path needs an installed version.
	Installed() bool
	// Overridden says whether -claude.bin / $CLAUDE_BIN decides.
	Overridden() bool
}
