package agentisolation

import "operators-mcp/internal/ports"

// ClaudeView is the Managed claude section (#claude): the installed
// versions, which one agents use, and the install that runs.
type ClaudeView struct {
	State ports.ManagedClaudeState
	// Overridden says -claude.bin / $CLAUDE_BIN decides; Bin is then its path.
	Overridden bool
	Bin        string
}

// ClaudeSeed is what the section's component takes over from: the body of
// GET /api/get_managed_claude.
type ClaudeSeed struct {
	ports.ManagedClaudeState
	Overridden bool   `json:"overridden"`
	Bin        string `json:"bin"`
}

// NewClaudeView reads the managed claude; without one nothing is installed.
func NewClaudeView(managed ports.ManagedClaude, bin ports.ManagedClaudeBin) ClaudeView {
	v := ClaudeView{State: ports.ManagedClaudeState{Installed: []ports.ManagedClaudeVersion{}}}
	if managed != nil {
		v.State = managed.State()
		v.Bin = v.State.Path
	}
	if bin != nil {
		v.Overridden, v.Bin = bin.Overridden(), bin.Bin()
	}
	return v
}

// Seed is the section's seed.
func (v ClaudeView) Seed() ClaudeSeed {
	return ClaudeSeed{ManagedClaudeState: v.State, Overridden: v.Overridden, Bin: v.Bin}
}

// None: no version is installed.
func (v ClaudeView) None() bool { return len(v.State.Installed) == 0 }

// Missing: agents cannot start for want of a claude (the override counts as
// one) and no install is running.
func (v ClaudeView) Missing() bool { return v.None() && !v.Overridden && v.State.Installing == nil }

// Installing: an install runs.
func (v ClaudeView) Installing() bool { return v.State.Installing != nil }

// Failed: the last install failed.
func (v ClaudeView) Failed() bool { return v.State.Failed != nil }

// ChipText and ChipTone are the section's status chip.
func (v ClaudeView) ChipText() string {
	if v.None() {
		return "None"
	}
	return v.State.Current
}

func (v ClaudeView) ChipTone() string {
	if v.None() {
		return "danger"
	}
	return "signal"
}

// VersionRow is one installed version as the server paints it.
type VersionRow struct {
	Version string
	Date    string
	Active  bool
}

// Rows are the installed versions, for the first paint.
func (v ClaudeView) Rows() []VersionRow {
	rows := make([]VersionRow, 0, len(v.State.Installed))
	for _, i := range v.State.Installed {
		rows = append(rows, VersionRow{Version: i.Version, Date: i.InstalledAt.Format("2 Jan"), Active: i.Active})
	}
	return rows
}

// FailedTitle and FailedMessage are the failed install's banner.
func (v ClaudeView) FailedTitle() string {
	if v.State.Failed == nil {
		return ""
	}
	return "Could not install " + v.State.Failed.Version + "."
}

func (v ClaudeView) FailedMessage() string {
	if v.State.Failed == nil {
		return ""
	}
	return v.State.Failed.Message
}

// InstallingVersion is the version being installed.
func (v ClaudeView) InstallingVersion() string {
	if v.State.Installing == nil {
		return ""
	}
	return v.State.Installing.Version
}
