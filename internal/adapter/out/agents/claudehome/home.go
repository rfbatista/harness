package claudehome

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Home is the agent Claude home (ADR 0018): the one config directory every
// agent claude process runs in, as CLAUDE_CONFIG_DIR, so nothing of the
// person's ~/.claude or ~/.claude.json loads into agents or is written by
// them. Transcripts live in its projects/ directory.
type Home struct{ dir string }

// NewHome returns the agent Claude home at dir. Nothing is created until
// Ensure or TrustFolder.
func NewHome(dir string) Home { return Home{dir: dir} }

// Path is the directory to pass as CLAUDE_CONFIG_DIR.
func (h Home) Path() string { return h.dir }

// Ensure creates the home and seeds the config file claude reads at
// $CLAUDE_CONFIG_DIR/.claude.json so an interactive claude starts without
// first-run prompts. It sets only keys that are missing, so claude's own
// later writes, other keys and the transcripts under projects/ are kept, and
// running it again changes nothing. Seeded (claude 2.1.x):
//
//	hasCompletedOnboarding: true  skips the welcome and onboarding flow
//	theme: "dark"                 skips the theme picker
//
// Folder trust is per directory: see TrustFolder.
func (h Home) Ensure() error {
	return h.update(func(m map[string]any) {
		seed(m)
	})
}

// TrustFolder marks dir (and, as claude treats trust, what is under it) as
// trusted, so the "do you trust this folder" prompt does not appear for
// session worktrees: projects[dir].hasTrustDialogAccepted = true. It also
// runs Ensure.
func (h Home) TrustFolder(dir string) error {
	return h.update(func(m map[string]any) {
		seed(m)
		projects, _ := m["projects"].(map[string]any)
		if projects == nil {
			projects = map[string]any{}
			m["projects"] = projects
		}
		e, _ := projects[dir].(map[string]any)
		if e == nil {
			e = map[string]any{}
			projects[dir] = e
		}
		e["hasTrustDialogAccepted"] = true
	})
}

// update applies f to the config file's object and writes it back when it
// changed, atomically.
func (h Home) update(f func(map[string]any)) error {
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(h.dir, ".claude.json")
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m := map[string]any{}
	if len(old) > 0 {
		if err := json.Unmarshal(old, &m); err != nil || m == nil {
			m = map[string]any{} // unreadable: claude would reset it too
		}
	}
	f(m)
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(old), b) {
		return nil
	}
	tmp, err := os.CreateTemp(h.dir, ".claude.json.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func seed(m map[string]any) {
	if _, ok := m["hasCompletedOnboarding"]; !ok {
		m["hasCompletedOnboarding"] = true
	}
	if _, ok := m["theme"]; !ok {
		m["theme"] = "dark"
	}
}
