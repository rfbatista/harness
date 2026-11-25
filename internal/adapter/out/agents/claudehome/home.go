package claudehome

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
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

// EnsureSharing is Ensure for a project Claude home (ADR 0102): beside the
// seed it makes the home's projects/ folder a link to the agent home's, so the
// transcripts claude files while it runs with this home as its config dir land
// where History, the transcript reader and resume read them, and a session
// either home started resumes under the other. What a projects/ folder in this
// home already held moves to the agent home. The user settings and the prompt
// history (settings.json, history.jsonl) are shared the same way once the agent
// home has them, so the project home holds the login and the account state
// only; .claude.json (onboarding seed and folder trust) stays per home. It is
// repeatable, and cheap enough to run at each start: a file the agent home
// gained since is linked then.
func (h Home) EnsureSharing(agent Home) error {
	// Two starts of one project run this at once; each home is done by one.
	mu := sharingLock(h.dir)
	mu.Lock()
	defer mu.Unlock()
	if err := h.Ensure(); err != nil {
		return err
	}
	if err := h.shareDir(agent, "projects"); err != nil {
		return err
	}
	for _, name := range []string{"settings.json", "history.jsonl"} {
		if err := h.shareFile(agent, name); err != nil {
			return err
		}
	}
	return nil
}

var (
	sharingMu    sync.Mutex
	sharingLocks = map[string]*sync.Mutex{}
)

// sharingLock is the lock of the home at dir, made on first use.
func sharingLock(dir string) *sync.Mutex {
	sharingMu.Lock()
	defer sharingMu.Unlock()
	if m, ok := sharingLocks[dir]; ok {
		return m
	}
	m := &sync.Mutex{}
	sharingLocks[dir] = m
	return m
}

// shareFile links name in this home to the agent home's, when the agent home
// has it. A regular file this home holds may be claude's latest write of it
// (an atomic rewrite replaces the link with a file): when it is newer than the
// agent home's copy it moves onto it, otherwise it is a stale copy and is
// dropped; the link comes back either way, so sharing never silently stops.
func (h Home) shareFile(agent Home, name string) error {
	target := filepath.Join(agent.dir, name)
	link := filepath.Join(h.dir, name)
	fi, err := os.Lstat(link)
	if err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if to, err := os.Readlink(link); err == nil && to == target {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if err == nil {
		if err := adoptOrDrop(link, target); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(target); err != nil {
		return nil
	}
	return os.Symlink(target, link)
}

// adoptOrDrop settles a regular file where the link belongs: it replaces the
// agent home's copy only when it is newer than that copy, else it is dropped.
// It looks again right before it acts, so what became a link meanwhile is left.
func adoptOrDrop(file, target string) error {
	fi, err := os.Lstat(file)
	if err != nil || !fi.Mode().IsRegular() {
		return err
	}
	if ti, err := os.Stat(target); err == nil && !fi.ModTime().After(ti.ModTime()) {
		return os.Remove(file)
	}
	return os.Rename(file, target)
}

// shareDir makes name, a folder of this home, a link to the agent home's.
func (h Home) shareDir(agent Home, name string) error {
	shared := filepath.Join(agent.dir, name)
	if err := os.MkdirAll(shared, 0o700); err != nil {
		return err
	}
	link := filepath.Join(h.dir, name)
	fi, err := os.Lstat(link)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case fi.Mode()&os.ModeSymlink != 0:
		if to, err := os.Readlink(link); err == nil && to == shared {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	default:
		if err := moveInto(link, shared); err != nil {
			return err
		}
	}
	return os.Symlink(shared, link)
}

// moveInto moves what dir holds into dst, entry by entry, then removes dir; an
// entry dst already has stays where it is.
func moveInto(dir, dst string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		to := filepath.Join(dst, e.Name())
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if err := os.Rename(filepath.Join(dir, e.Name()), to); err != nil {
			return err
		}
	}
	return os.RemoveAll(dir)
}

// credentialsFile is the login claude keeps in the home, plaintext.
const credentialsFile = ".credentials.json"

// secure makes the home private (0700, tightening a looser existing mode)
// and repairs a looser login file to 0600. The file's content is never read.
func (h Home) secure() error {
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(h.dir, 0o700); err != nil {
		return err
	}
	cred := filepath.Join(h.dir, credentialsFile)
	fi, err := os.Stat(cred)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode().IsRegular() && fi.Mode().Perm()&0o077 != 0 {
		return os.Chmod(cred, 0o600)
	}
	return nil
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
	if err := h.secure(); err != nil {
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

// HasLogin says whether the home holds a Claude account login: a
// .credentials.json that parses as JSON with a claudeAiOauth object. It
// reads the file and starts no process; the content is never returned.
func (h Home) HasLogin() bool {
	if h.dir == "" {
		return false
	}
	b, err := os.ReadFile(filepath.Join(h.dir, credentialsFile))
	if err != nil {
		return false
	}
	var f struct {
		Login map[string]json.RawMessage `json:"claudeAiOauth"`
	}
	return json.Unmarshal(b, &f) == nil && f.Login != nil
}
