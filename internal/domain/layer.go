package domain

import (
	"strings"
)

// AccessLevel is what an agent may do to the paths of a layer. Change implies
// read.
type AccessLevel string

const (
	AccessNone   AccessLevel = "none"
	AccessRead   AccessLevel = "read"
	AccessChange AccessLevel = "change"
)

// Valid reports whether l is one of the three levels.
func (l AccessLevel) Valid() bool {
	return l == AccessNone || l == AccessRead || l == AccessChange
}

// PlainAgent is the reserved agent name of a session that runs with no agent.
const PlainAgent = "plain"

// LayerAccess says what each agent may do to a layer: the level of every
// agent not listed, and an entry per listed agent. Agents are named by their
// catalog name, or PlainAgent.
type LayerAccess struct {
	Default AccessLevel            `json:"default" yaml:"default"`
	Agents  map[string]AccessLevel `json:"agents,omitempty" yaml:"agents,omitempty"`
}

// For is agent's level: its entry, else the default (read when that is unset).
func (a LayerAccess) For(agent string) AccessLevel {
	if l, ok := a.Agents[agent]; ok && l.Valid() {
		return l
	}
	if a.Default.Valid() {
		return a.Default
	}
	return AccessRead
}

// CanChange reports whether agent may change the layer's paths.
func (a LayerAccess) CanChange(agent string) bool { return a.For(agent) == AccessChange }

// CanRead reports whether agent may read the layer's paths.
func (a LayerAccess) CanRead(agent string) bool { return a.For(agent) != AccessNone }

// LayerIndex says which of zones a path belongs to under the layer rule: the
// narrowest zone that contains it wins. A zone with a pattern (a regex) beats
// one with explicit paths (a folder); between regexes the one listed first
// wins; between folders the deeper one wins, and the one listed first when
// they are as deep. It answers -1 when no zone contains path.
//
// It is the one place the harness decides which layer a path is in: the
// layer preview, the hooks and the commit check all call it.
func LayerIndex(m *ZoneMatcher, path string) int {
	best, bestRank := -1, [2]int{}
	for i, z := range m.zones {
		if !m.Contains(i, path) {
			continue
		}
		rank := zoneRank(z, path)
		if best < 0 || rank[0] > bestRank[0] || rank[0] == bestRank[0] && rank[1] > bestRank[1] {
			best, bestRank = i, rank
		}
	}
	return best
}

// zoneRank orders zones that contain path: higher is narrower. A regex ranks
// above every folder; the earlier regex above the later (the first match
// wins because later ones do not beat it on a tie).
func zoneRank(z *Zone, path string) [2]int {
	if z.Pattern != "" {
		return [2]int{2, 0}
	}
	depth := 0
	for _, ep := range z.ExplicitPaths {
		if path == ep || strings.HasPrefix(path, ep+"/") {
			if d := strings.Count(ep, "/") + 1; d > depth {
				depth = d
			}
		}
	}
	return [2]int{1, depth}
}

// Matching returns the indexes of the zones that match path, in the zones' order.
func (m *ZoneMatcher) Matching(path string) []int {
	var out []int
	for i := range m.zones {
		if m.Contains(i, path) {
			out = append(out, i)
		}
	}
	return out
}

// Zones is the zones the matcher was built on.
func (m *ZoneMatcher) Zones() []*Zone { return m.zones }
