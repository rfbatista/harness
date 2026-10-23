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
	// Warnings are what is worth a second look about each path, by path.
	Warnings map[string][]string
	// Builtin is the allowlist every session gets, read-only on the page.
	Builtin []ports.SandboxBuiltinGroup
	// AllowPath is a path an "Allow this path" link brought: it opens in the
	// add-path field and is not saved.
	AllowPath string
}

// SandboxSeed is what the section's component takes over from: the body of
// GET /api/get_sandbox_settings.
type SandboxSeed struct {
	Enabled   bool       `json:"enabled"`
	Driver    string     `json:"driver"`
	Paths     []PathSeed `json:"paths"`
	AllowPath string     `json:"allowPath,omitempty"`
}

// PathSeed is an extra path in the seed, with its warning codes.
type PathSeed struct {
	ports.SandboxPath
	Warnings []string `json:"warnings"`
}

// NewSandboxView reads the sandbox settings; without a store the sandbox
// shows as on with no paths.
func NewSandboxView(settings ports.SandboxSettings, driver func() ports.SandboxDriver) SandboxView {
	v := SandboxView{Enabled: true, Paths: []ports.SandboxPath{}, Warnings: map[string][]string{}}
	if settings != nil {
		v.Enabled = settings.Enabled()
		if paths := settings.Paths(); paths != nil {
			v.Paths = paths
		}
		for _, p := range v.Paths {
			v.Warnings[p.Path] = settings.PathWarnings(p.Path)
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
	paths := make([]PathSeed, 0, len(v.Paths))
	for _, p := range v.Paths {
		w := v.Warnings[p.Path]
		if w == nil {
			w = []string{}
		}
		paths = append(paths, PathSeed{SandboxPath: p, Warnings: w})
	}
	return SandboxSeed{Enabled: v.Enabled, Driver: v.Driver, Paths: paths, AllowPath: v.AllowPath}
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
	// Missing and Credentials are the warnings under the path, "" for none.
	Missing     string
	Credentials string
}

// The words under a path that is worth a second look.
const (
	MissingWarning     = "This folder does not exist yet. It will apply once it does."
	CredentialsWarning = "This folder usually holds credentials."
)

// Rows are the extra paths for the first paint.
func (v SandboxView) Rows() []PathRow {
	rows := make([]PathRow, 0, len(v.Paths))
	for _, p := range v.Paths {
		access := "read"
		if p.Write {
			access = "write"
		}
		row := PathRow{Path: p.Path, Access: access, AccessLabel: "Access for " + p.Path, RemoveLabel: "Remove " + p.Path}
		for _, w := range v.Warnings[p.Path] {
			switch w {
			case "missing":
				row.Missing = MissingWarning
			case "credentials":
				row.Credentials = CredentialsWarning
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// BuiltinCount is how many paths the built-in allowlist names.
func (v SandboxView) BuiltinCount() int {
	n := 0
	for _, g := range v.Builtin {
		n += len(g.Entries)
	}
	return n
}

// BuiltinSummary is the closed details' summary line.
func (v SandboxView) BuiltinSummary() string {
	return "Built-in allowlist (" + strconv.Itoa(v.BuiltinCount()) + " paths, not editable)"
}

// AccessWord says what an entry of the built-in allowlist may do.
func AccessWord(write bool) string {
	if write {
		return "Read and write"
	}
	return "Read-only"
}
