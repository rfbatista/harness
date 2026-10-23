package agentisolation

import (
	"runtime"
	"strconv"

	"operators-mcp/internal/ports"
)

// SandboxView is the Sandbox section (#sandbox): the switch, the driver the
// platform has, and the extra paths.
type SandboxView struct {
	Enabled bool
	// Driver names the mechanism ("" when the platform has none).
	Driver string
	Paths  []ports.SandboxPath
}

// SandboxSeed is what the section's component takes over from: the body of
// GET /api/get_sandbox_settings.
type SandboxSeed struct {
	Enabled bool                `json:"enabled"`
	Driver  string              `json:"driver"`
	Paths   []ports.SandboxPath `json:"paths"`
}

// NewSandboxView reads the sandbox settings; without a store the sandbox
// shows as on with no paths.
func NewSandboxView(settings ports.SandboxSettings, driver func() ports.SandboxDriver) SandboxView {
	v := SandboxView{Enabled: true, Paths: []ports.SandboxPath{}}
	if settings != nil {
		v.Enabled = settings.Enabled()
		if paths := settings.Paths(); paths != nil {
			v.Paths = paths
		}
	}
	if driver != nil {
		if d := driver(); d != nil {
			v.Driver = d.Name()
		}
	}
	return v
}

// Seed is the section's seed.
func (v SandboxView) Seed() SandboxSeed {
	return SandboxSeed{Enabled: v.Enabled, Driver: v.Driver, Paths: v.Paths}
}

// NoDriver: the platform has no sandbox driver.
func (v SandboxView) NoDriver() bool { return v.Driver == "" }

// Blocks: the sandbox is on and cannot be applied, so agents refuse to start.
func (v SandboxView) Blocks() bool { return v.Enabled && v.NoDriver() }

// Off: the person turned the sandbox off.
func (v SandboxView) Off() bool { return !v.Enabled }

// EnabledAttr is the switch's aria-checked.
func (v SandboxView) EnabledAttr() string { return strconv.FormatBool(v.Enabled) }

// DriverLabel is the driver as the page names it.
func (v SandboxView) DriverLabel() string {
	switch v.Driver {
	case "":
		return "no driver"
	case "seatbelt":
		return "Seatbelt"
	case "bubblewrap":
		return "bubblewrap"
	}
	return v.Driver
}

// InstallHint is what to do about a missing driver, for this platform.
func (v SandboxView) InstallHint() string {
	if runtime.GOOS == "linux" {
		return "Install bubblewrap (apt install bubblewrap), or turn the sandbox off."
	}
	return "Turn the sandbox off to run agents without it."
}

// ChipText and ChipTone are the section's status chip.
func (v SandboxView) ChipText() string {
	switch {
	case v.Off():
		return "Off"
	case v.NoDriver():
		return "No driver"
	}
	return "On"
}

func (v SandboxView) ChipTone() string {
	switch {
	case v.Off():
		return "attention"
	case v.NoDriver():
		return "danger"
	}
	return "signal"
}

// PathRow is one extra path as the server paints it.
type PathRow struct {
	Path        string
	Access      string // "read" or "write"
	AccessLabel string
	RemoveLabel string
}

// Rows are the extra paths for the first paint.
func (v SandboxView) Rows() []PathRow {
	rows := make([]PathRow, 0, len(v.Paths))
	for _, p := range v.Paths {
		access := "read"
		if p.Write {
			access = "write"
		}
		rows = append(rows, PathRow{Path: p.Path, Access: access, AccessLabel: "Access for " + p.Path, RemoveLabel: "Remove " + p.Path})
	}
	return rows
}
