// Package agentcatalog is the one module that owns the agent catalog: the
// folder in the harness data directory that holds agents.json, skills/ and
// mcp-servers.json (ADR 0030). It reads and validates the folder, resolves
// bundles, computes who gets what and what is wrong, and writes it.
package agentcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"operators-mcp/internal/domain"
)

// File names inside the catalog folder.
const (
	agentsFile     = "agents.json"
	mcpServersFile = "mcp-servers.json"
	skillsDir      = "skills"
	skillFileName  = "SKILL.md"
)

var (
	slugRe       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	serverNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	envRefRe     = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// Catalog is the catalog folder at Root.
type Catalog struct {
	root      string
	lookupEnv func(string) (string, bool)

	// wmu serializes writes: one change is built and applied at a time.
	wmu sync.Mutex

	mu sync.Mutex
	// The last version of each file that parsed, shown while the file on disk
	// is broken.
	lastAgents  *node
	lastServers *node

	// last is the latest change, the only one that can be undone. It lives
	// in memory: a restart clears it.
	last *storedChange
	subs *subscribers
}

// Option configures New.
type Option func(*Catalog)

// WithEnv sets how the catalog tells whether a variable is set. The default
// is the process environment.
func WithEnv(lookup func(string) (string, bool)) Option {
	return func(c *Catalog) { c.lookupEnv = lookup }
}

// New is the catalog in the folder root. The folder need not exist yet: a
// missing catalog is an empty one.
func New(root string, opts ...Option) *Catalog {
	c := &Catalog{root: root, lookupEnv: os.LookupEnv, subs: &subscribers{}}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Root is the catalog folder.
func (c *Catalog) Root() string { return c.root }

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// loaded is the two JSON files as read, parsed, with their versions.
type loaded struct {
	agents  *node // never nil: an empty object when there is no usable file
	servers *node // the "mcpServers" object, never nil
	// serversRoot is the whole mcp-servers.json object, never nil.
	serversRoot *node
	versions    map[string]string
	broken      *domain.CatalogBroken
	// text is each file's bytes as read, nil when missing.
	text map[string][]byte
}

// load reads agents.json and mcp-servers.json. A file that does not parse
// leaves the last version that did, and is reported in broken.
func (c *Catalog) load() (*loaded, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	l := &loaded{versions: map[string]string{}, text: map[string][]byte{}}
	for _, name := range []string{agentsFile, mcpServersFile} {
		data, err := os.ReadFile(filepath.Join(c.root, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		var n *node
		if err == nil {
			l.text[name] = data
			l.versions[name] = hashBytes(data)
			var perr *parseError
			n, perr = parseNode(data)
			if perr == nil && (n == nil || n.kind != 'o') {
				perr = &parseError{Line: 1, Column: 1, Message: "the file must hold a JSON object"}
			}
			if perr != nil {
				if l.broken == nil {
					l.broken = &domain.CatalogBroken{Path: name, Line: perr.Line, Column: perr.Column, Message: perr.Message}
				}
				n = nil
			}
		} else {
			n = newObject()
		}
		if name == agentsFile {
			if n != nil {
				c.lastAgents = n
			}
			l.agents = c.lastAgents
		} else {
			if n != nil {
				c.lastServers = n
			}
			if c.lastServers != nil {
				l.serversRoot = c.lastServers
				l.servers = c.lastServers.get("mcpServers")
			}
		}
	}
	if l.agents == nil {
		l.agents = newObject()
	}
	if l.serversRoot == nil {
		l.serversRoot = newObject()
	}
	if l.servers == nil {
		l.servers = newObject()
	}
	return l, nil
}

// folderHash is the version of a skill folder: a hash over every path and
// file's bytes, in path order.
type folderHash struct{ h hash.Hash }

func newFolderHash() *folderHash { return &folderHash{h: sha256.New()} }

func (f *folderHash) add(path string, data []byte) {
	fmt.Fprintf(f.h, "%s\x00%d\x00", path, len(data))
	f.h.Write(data)
}

func (f *folderHash) sum() string { return "sha256:" + hex.EncodeToString(f.h.Sum(nil)) }
