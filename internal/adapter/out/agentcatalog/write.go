package agentcatalog

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
)

// fileEdit is one file a change writes. before and after are nil when the
// file is missing before or after.
type fileEdit struct {
	path          string // slash-separated, relative to the catalog folder
	before, after []byte
}

// storedChange is the latest change, kept for an undo.
type storedChange struct {
	id      string
	summary string
	edits   []*fileEdit
}

// txn is one write being built. Its nodes are copies: nothing on disk or in
// the last-good state changes until commit.
type txn struct {
	c           *Catalog
	l           *loaded
	agents      *node
	serversRoot *node
	edits       []*fileEdit
}

// begin starts a write. The caller holds c.wmu.
func (c *Catalog) begin() (*txn, error) {
	l, err := c.load()
	if err != nil {
		return nil, err
	}
	return &txn{c: c, l: l, agents: l.agents.clone(), serversRoot: l.serversRoot.clone()}, nil
}

// bundlesView and serversView read an object without adding it to the file.
func (t *txn) bundlesView() *node { return t.peek(t.agents, "bundles") }
func (t *txn) serversView() *node { return t.peek(t.serversRoot, "mcpServers") }
func (t *txn) peek(parent *node, key string) *node {
	if n := parent.get(key); n != nil && n.kind == 'o' {
		return n
	}
	return newObject()
}

func (t *txn) agentsMap() *node  { return t.object(t.agents, "agents") }
func (t *txn) bundlesMap() *node { return t.object(t.agents, "bundles") }
func (t *txn) serversMap() *node { return t.object(t.serversRoot, "mcpServers") }
func (t *txn) object(parent *node, key string) *node {
	if n := parent.get(key); n != nil && n.kind == 'o' {
		return n
	}
	n := newObject()
	parent.set(key, n)
	return n
}

// addEdit records a file this change writes. before is read from disk.
func (t *txn) addEdit(path string, after []byte) error {
	before, err := os.ReadFile(filepath.Join(t.c.root, filepath.FromSlash(path)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		before = nil
	}
	t.edits = append(t.edits, &fileEdit{path: path, before: before, after: after})
	return nil
}

// versionKey is the version a file belongs to: the file itself, or its skill
// folder.
func versionKey(path string) string {
	if rest, ok := strings.CutPrefix(path, skillsDir+"/"); ok {
		name, _, _ := strings.Cut(rest, "/")
		return skillsDir + "/" + name
	}
	return path
}

// currentVersion is the version of key on disk now, "" when missing.
func (c *Catalog) currentVersion(key string) string {
	if strings.HasPrefix(key, skillsDir+"/") {
		dir := filepath.Join(c.root, filepath.FromSlash(key))
		if _, err := os.Stat(dir); err != nil {
			return ""
		}
		_, v, err := readSkillFolder(dir)
		if err != nil {
			return ""
		}
		return v
	}
	data, err := os.ReadFile(filepath.Join(c.root, key))
	if err != nil {
		return ""
	}
	return hashBytes(data)
}

// finishJSON turns the changed JSON documents into edits.
func (t *txn) finishJSON() error {
	for _, f := range []struct {
		path string
		root *node
		orig *node
	}{{agentsFile, t.agents, t.l.agents}, {mcpServersFile, t.serversRoot, t.l.serversRoot}} {
		if bytes.Equal(f.root.encode(), f.orig.encode()) {
			continue
		}
		after := append(f.root.encode(), '\n')
		if err := t.addEdit(f.path, after); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// conflict is the CATALOG_CHANGED refusal for the keys whose version moved.
func (c *Catalog) conflict(edits []*fileEdit, moved map[string]bool) error {
	current := map[string]string{}
	var changed []map[string]any
	for key := range moved {
		current[key] = c.currentVersion(key)
	}
	for _, e := range edits {
		if !moved[versionKey(e.path)] {
			continue
		}
		var theirs any
		if data, err := os.ReadFile(filepath.Join(c.root, filepath.FromSlash(e.path))); err == nil {
			theirs = string(data)
		}
		changed = append(changed, map[string]any{"path": e.path, "theirs": theirs})
	}
	return domain.NewCatalogError("CATALOG_CHANGED", "the catalog changed since you opened it: "+strings.Join(sortedKeys(moved), ", "),
		map[string]any{"current_versions": current, "changed": changed})
}

// commit checks, previews or applies the edits the transaction holds.
func (t *txn) commit(opts domain.CatalogWriteOptions, summary string) (*domain.CatalogWriteResult, error) {
	c := t.c
	if err := t.finishJSON(); err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, e := range t.edits {
		keys[versionKey(e.path)] = true
	}
	if b := t.l.broken; b != nil && keys[b.Path] {
		return nil, domain.NewCatalogError("CATALOG_BROKEN", b.Path+" does not parse (line "+strconv.Itoa(b.Line)+"): fix it by hand first",
			map[string]any{"path": b.Path, "line": b.Line, "column": b.Column})
	}
	if !opts.Force {
		moved := map[string]bool{}
		for k := range keys {
			if base, ok := opts.BaseVersions[k]; ok && base != c.currentVersion(k) {
				moved[k] = true
			}
		}
		if len(moved) > 0 {
			return nil, c.conflict(t.edits, moved)
		}
	}
	change := domain.CatalogChange{Summary: summary, Writes: []domain.CatalogFileWrite{}}
	for _, e := range t.edits {
		change.Writes = append(change.Writes, domain.CatalogFileWrite{Path: e.path, Before: text(e.before), After: text(e.after)})
	}
	if opts.DryRun || len(t.edits) == 0 {
		return &domain.CatalogWriteResult{Change: change, Versions: c.versionsAfter(t.l, keys, false)}, nil
	}
	if err := c.apply(t.edits); err != nil {
		return nil, err
	}
	change.ID = newChangeID()
	c.last = &storedChange{id: change.ID, summary: summary, edits: t.edits}
	c.publish(sortedKeys(keys))
	return &domain.CatalogWriteResult{Change: change, Versions: c.versionsAfter(t.l, keys, true)}, nil
}

func text(b []byte) *string {
	if b == nil {
		return nil
	}
	s := string(b)
	return &s
}

func newChangeID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// versionsAfter is the catalog files' versions, with the touched keys as
// they are on disk now (written), or as the unchanged load had them.
func (c *Catalog) versionsAfter(l *loaded, touched map[string]bool, written bool) map[string]string {
	out := map[string]string{}
	for k, v := range l.versions {
		out[k] = v
	}
	if !written {
		return out
	}
	for k := range touched {
		if v := c.currentVersion(k); v != "" {
			out[k] = v
		} else {
			delete(out, k)
		}
	}
	return out
}

// apply writes every edit, each file atomically. A failure part way puts the
// files already written back.
func (c *Catalog) apply(edits []*fileEdit) error {
	for i, e := range edits {
		if err := c.writeFile(e.path, e.after); err != nil {
			for j := i; j >= 0; j-- {
				_ = c.writeFile(edits[j].path, edits[j].before)
			}
			return err
		}
	}
	return nil
}

// writeFile writes data to path with a temp file and a rename, or removes
// the file when data is nil. Folders left empty by a removal go too.
func (c *Catalog) writeFile(path string, data []byte) error {
	full := filepath.Join(c.root, filepath.FromSlash(path))
	if data == nil {
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		c.pruneEmpty(filepath.Dir(full))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".catalog-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), full); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// pruneEmpty removes dir and its parents while they are empty, never the
// catalog folder itself.
func (c *Catalog) pruneEmpty(dir string) {
	for dir != c.root && strings.HasPrefix(dir, c.root) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// UndoCatalogChange restores the bytes every file had before the latest
// change.
func (c *Catalog) UndoCatalogChange(in domain.UndoCatalogChange) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.last == nil || c.last.id == "" || c.last.id != in.ChangeID {
		return nil, domain.NewCatalogError("CHANGE_NOT_UNDOABLE", "only the latest change of this server can be undone", nil)
	}
	l, err := c.load()
	if err != nil {
		return nil, err
	}
	// Undo runs the change backwards; a file that is not as the change left it
	// has been changed since.
	var edits []*fileEdit
	moved := map[string]bool{}
	for i := len(c.last.edits) - 1; i >= 0; i-- {
		e := c.last.edits[i]
		cur, err := os.ReadFile(filepath.Join(c.root, filepath.FromSlash(e.path)))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err != nil {
			cur = nil
		}
		if !bytes.Equal(cur, e.after) {
			moved[versionKey(e.path)] = true
		}
		edits = append(edits, &fileEdit{path: e.path, before: e.after, after: e.before})
	}
	if len(moved) > 0 && !in.Force {
		return nil, c.conflict(edits, moved)
	}
	keys := map[string]bool{}
	change := domain.CatalogChange{Summary: "Undid: " + c.last.summary, Writes: []domain.CatalogFileWrite{}}
	for _, e := range edits {
		keys[versionKey(e.path)] = true
		change.Writes = append(change.Writes, domain.CatalogFileWrite{Path: e.path, Before: text(e.before), After: text(e.after)})
	}
	if in.DryRun {
		return &domain.CatalogWriteResult{Change: change, Versions: c.versionsAfter(l, keys, false)}, nil
	}
	if err := c.apply(edits); err != nil {
		return nil, err
	}
	change.ID = newChangeID()
	c.last = &storedChange{id: change.ID, summary: change.Summary, edits: edits}
	c.publish(sortedKeys(keys))
	return &domain.CatalogWriteResult{Change: change, Versions: c.versionsAfter(l, keys, true)}, nil
}

// subscribers fans each write's paths out to the followers.
type subscribers struct {
	mu   sync.Mutex
	subs map[chan domain.CatalogChanged]struct{}
}

// followBuffer is how many writes a follower may have waiting.
const followBuffer = 64

// FollowCatalogChanges delivers the changed paths of each write until ctx
// ends. A follower that falls behind is closed.
func (c *Catalog) FollowCatalogChanges(ctx context.Context) (<-chan domain.CatalogChanged, error) {
	ch := make(chan domain.CatalogChanged, followBuffer)
	s := c.subs
	s.mu.Lock()
	if s.subs == nil {
		s.subs = map[chan domain.CatalogChanged]struct{}{}
	}
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}()
	return ch, nil
}

func (c *Catalog) publish(paths []string) {
	s := c.subs
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- domain.CatalogChanged{Paths: paths}:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
}
