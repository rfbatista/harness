package domain

import (
	"strings"
	"time"
)

// TokenSaverInstallation is one token saver downloaded onto the machine, at
// the version the harness pinned when it was installed (an Installation in
// the Glossary). It is machine-wide; being installed does not make a saver
// active anywhere.
type TokenSaverInstallation struct {
	Saver   TokenSaver `json:"saver"`
	Version string     `json:"version"`
	// Dir is <data dir>/tools/<saver>/<version>.
	Dir string `json:"dir"`
	// Path is the file a session uses: the executable for rtk and
	// codegraph, the ruleset (SKILL.md) for caveman.
	Path        string    `json:"path"`
	InstalledAt time.Time `json:"installed_at"`
}

// TokenSaverState is where a token saver stands on this machine.
type TokenSaverState string

const (
	TokenSaverNotInstalled TokenSaverState = "not_installed"
	TokenSaverInstalled    TokenSaverState = "installed"
	// TokenSaverOutdated is installed at a version other than the pinned one.
	TokenSaverOutdated   TokenSaverState = "outdated"
	TokenSaverInstalling TokenSaverState = "installing"
	// TokenSaverFailed is the last install failing; whatever was installed
	// before it is kept.
	TokenSaverFailed TokenSaverState = "failed"
)

// TokenSaverStatus is one token saver as the Token savers page shows it.
type TokenSaverStatus struct {
	Saver   TokenSaver `json:"saver"`
	Summary string     `json:"summary"`
	// PinnedVersion is the version this harness installs.
	PinnedVersion string          `json:"pinned_version"`
	State         TokenSaverState `json:"state"`
	// Installed is nil when nothing is installed.
	Installed *TokenSaverInstallation `json:"installed,omitempty"`
	// Progress says what an install is doing, while it runs.
	Progress string `json:"progress,omitempty"`
	// Error is why the last install failed.
	Error *TokenSaverError `json:"error,omitempty"`
	// OnSomewhere is a project, repository or task having it on. Not
	// installed, the sessions it is on for run without it.
	OnSomewhere bool `json:"on_somewhere,omitempty"`
}

// TokenSaverError is a failed install as the page shows it.
type TokenSaverError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TokenSaverList is every token saver and the platform they install for.
type TokenSaverList struct {
	Savers []TokenSaverStatus `json:"token_savers"`
	// Platform is the server's, e.g. darwin/arm64.
	Platform string `json:"platform"`
	// Supported is false on a platform the harness pins no release for;
	// Unsupported then says which ones it does.
	Supported   bool   `json:"supported"`
	Unsupported string `json:"unsupported,omitempty"`
	// CavemanIntensity is the machine-wide caveman intensity.
	CavemanIntensity CavemanIntensity `json:"caveman_intensity"`
}

// Platform is an operating system and architecture, as Go names them.
type Platform struct{ OS, Arch string }

func (p Platform) String() string { return p.OS + "/" + p.Arch }

// TokenSaverPlatforms are the platforms token savers install on.
var TokenSaverPlatforms = []Platform{{"darwin", "amd64"}, {"darwin", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}}

// Supported reports whether token savers install on p.
func (p Platform) Supported() bool {
	for _, s := range TokenSaverPlatforms {
		if p == s {
			return true
		}
	}
	return false
}

// UnsupportedPlatformMessage is why nothing installs on p.
func UnsupportedPlatformMessage(p Platform) string {
	names := make([]string, len(TokenSaverPlatforms))
	for i, s := range TokenSaverPlatforms {
		names[i] = s.String()
	}
	list := strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	return "Token savers install only on " + list + "; this server runs on " + p.String() + "."
}

// TokenSaverErr is a coded token saver error.
func TokenSaverErr(code, message string) *StructuredError {
	return &StructuredError{Code: code, Message: message}
}

// InstallationsBySaver indexes installations by their saver.
func InstallationsBySaver(list []*TokenSaverInstallation) map[TokenSaver]*TokenSaverInstallation {
	out := make(map[TokenSaver]*TokenSaverInstallation, len(list))
	for _, in := range list {
		out[in.Saver] = in
	}
	return out
}
