// Package sandboxsettings keeps the person's own sandbox configuration
// (ADR 0017) in the settings: the switch (orchestration.SettingSandbox) and
// the extra allowlist paths. The orchestration reads both at each session
// start, through orchestration.SandboxSwitch and Store.Extra.
package sandboxsettings

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SettingPaths is the setting holding the extra paths, as a JSON array of
// {"path", "write"} in the order they were added.
const SettingPaths = "agent_sandbox_paths"

var _ ports.SandboxSettings = (*Store)(nil)

// Store is the sandbox configuration over the settings context.
type Store struct {
	Settings ports.Settings
	// Home is the person's home directory: "~" expands to it, and the path
	// that is the whole of it is refused. Empty: no home.
	Home string

	mu sync.Mutex // one add, change or remove at a time
}

// Enabled says whether the sandbox is on.
func (s *Store) Enabled() bool { return orchestration.SandboxSwitch(s.Settings)() }

// SetEnabled switches the sandbox on or off for sessions started afterwards.
func (s *Store) SetEnabled(on bool) error {
	v := "on"
	if !on {
		v = "off"
	}
	_, err := s.Settings.UpdateSettings(map[string]string{orchestration.SettingSandbox: v})
	return err
}

// Paths are the extra paths as the person wrote them.
func (s *Store) Paths() []ports.SandboxPath {
	var out []ports.SandboxPath
	if raw := s.Settings.Setting(SettingPaths); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out) // an unreadable value is no paths
	}
	return out
}

// Extra are the extra paths absolute, for the policy builder; it is the
// orchestration's ports.SandboxExtraPaths.
func (s *Store) Extra() []ports.SandboxPath {
	paths := s.Paths()
	for i := range paths {
		paths[i].Path = s.expand(paths[i].Path)
	}
	return paths
}

// AddPath adds p after validating it.
func (s *Store) AddPath(p ports.SandboxPath) ([]ports.SandboxPath, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.validate(p.Path)
	if err != nil {
		return nil, err
	}
	list := s.Paths()
	if s.index(list, path) >= 0 {
		return nil, invalid("Already in the list.")
	}
	return s.save(append(list, ports.SandboxPath{Path: path, Write: p.Write}))
}

// SetPathAccess makes the listed path read-only or read-write.
func (s *Store) SetPathAccess(path string, write bool) ([]ports.SandboxPath, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.Paths()
	i := s.index(list, path)
	if i < 0 {
		return nil, notListed(path)
	}
	list[i].Write = write
	return s.save(list)
}

// RemovePath drops the listed path.
func (s *Store) RemovePath(path string) ([]ports.SandboxPath, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.Paths()
	i := s.index(list, path)
	if i < 0 {
		return nil, notListed(path)
	}
	return s.save(append(list[:i], list[i+1:]...))
}

func (s *Store) save(list []ports.SandboxPath) ([]ports.SandboxPath, error) {
	b, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	if _, err := s.Settings.UpdateSettings(map[string]string{SettingPaths: string(b)}); err != nil {
		return nil, err
	}
	return list, nil
}

// index finds path in list; two spellings of one folder are the same entry.
func (s *Store) index(list []ports.SandboxPath, path string) int {
	want := s.expand(path)
	for i, p := range list {
		if s.expand(p.Path) == want {
			return i
		}
	}
	return -1
}

// validate answers the path as it is stored: cleaned, with a leading ~ kept.
func (s *Store) validate(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", invalid("Enter a path.")
	}
	if path != "~" && !strings.HasPrefix(path, "~/") && !filepath.IsAbs(path) {
		return "", invalid("Use an absolute path or one starting with ~.")
	}
	abs := s.expand(path)
	if abs == "/" || (s.Home != "" && abs == filepath.Clean(s.Home)) {
		return "", invalid("That would give agents your whole home directory. Add the specific folder instead.")
	}
	if strings.HasPrefix(path, "~") {
		return "~" + strings.TrimPrefix(abs, filepath.Clean(s.Home)), nil
	}
	return abs, nil
}

// expand makes a leading ~ the home directory and cleans the path.
func (s *Store) expand(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		path = s.Home + path[1:]
	}
	return filepath.Clean(path)
}

func invalid(message string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: message}
}

func notListed(path string) error {
	return &domain.StructuredError{Code: "SANDBOX_PATH_NOT_FOUND", Message: path + " is not in the list of extra paths"}
}
