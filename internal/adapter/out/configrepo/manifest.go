// Package configrepo lets harness read Agent/Skill/MCPServer data live from a
// dotfiles-style agents.json + agents/skills/ tree, alongside (not instead
// of) the SQLite-backed catalog. It never persists anything: the config
// files on disk are the source of truth, edited outside this process, and
// every List/Get re-reads them so results always reflect the current files.
//
// IDs for everything this package produces are prefixed "cfg:" (e.g.
// "cfg:go-developer", "cfg:tdd") so a caller — in particular the Merged*
// decorators in merge.go — can tell a config-sourced id from a database one
// without a lookup, and so an agent's resolved SkillIDs/MCPServerIDs stay
// stable across repeated reads without ever being written to the database.
package configrepo

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// IDPrefix marks every id this package hands out.
const IDPrefix = "cfg:"

// Manifest is the subset of agents.json this package understands. Unknown
// top-level keys (settings, etc.) are ignored — this package only cares about
// bundle/agent/skill resolution, not the local CLI launcher's own concerns.
type Manifest struct {
	Bundles map[string][]string    `json:"bundles"`
	Agents  map[string]AgentConfig `json:"agents"`
}

// AgentConfig is one agents.json "agents.<key>" entry.
type AgentConfig struct {
	Skills      []string `json:"skills"`
	MCP         string   `json:"mcp"`
	Dir         string   `json:"dir"`
	Description string   `json:"description"`
}

// LoadManifest reads and parses agents.json at path.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("configrepo: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("configrepo: parse manifest: %w", err)
	}
	return &m, nil
}

// ResolveSkills expands entries against bundles: a plain name passes through,
// an "@bundle" entry is replaced by that bundle's (recursively resolved)
// skill list. The result is deduped in first-seen order across the whole
// expansion, mirroring agents/agent_launch.py's resolve_skills exactly, so
// the two stay interchangeable descriptions of the same manifest.
func ResolveSkills(entries []string, bundles map[string][]string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	var walk func(entries []string, activeBundles map[string]bool) error
	walk = func(entries []string, activeBundles map[string]bool) error {
		for _, e := range entries {
			if !strings.HasPrefix(e, "@") {
				if !seen[e] {
					seen[e] = true
					out = append(out, e)
				}
				continue
			}
			b := strings.TrimPrefix(e, "@")
			if activeBundles[b] {
				return fmt.Errorf("configrepo: bundle cycle through @%s", b)
			}
			list, ok := bundles[b]
			if !ok {
				return fmt.Errorf("configrepo: unknown bundle @%s", b)
			}
			nested := make(map[string]bool, len(activeBundles)+1)
			for k := range activeBundles {
				nested[k] = true
			}
			nested[b] = true
			if err := walk(list, nested); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(entries, map[string]bool{}); err != nil {
		return nil, err
	}
	return out, nil
}

func stripIDPrefix(id string) (string, bool) {
	if !strings.HasPrefix(id, IDPrefix) {
		return "", false
	}
	return strings.TrimPrefix(id, IDPrefix), true
}

func errReadOnly(kind string) error {
	return fmt.Errorf("configrepo: %s is defined in agents.json/skills config and is read-only here — edit the files directly", kind)
}

func errNotFound(kind string) error {
	return fmt.Errorf("configrepo: %s not found", kind)
}
