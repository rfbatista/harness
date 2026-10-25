package domain

import (
	"regexp"
	"strings"
)

// Zone holds zone state (pattern, metadata, explicit paths).
// It is the core entity of the architecture context.
// A zone belongs to a project and paths are relative to that project's root.
type Zone struct {
	ID               string   `json:"id"`
	ProjectID        string   `json:"project_id"`
	BoundedContextID string   `json:"bounded_context_id,omitempty"`
	Name             string   `json:"name"`
	Pattern          string   `json:"pattern"`
	Purpose          string   `json:"purpose"`
	Rules            []Prompt `json:"rules"`
	AssignedAgents   []Agent  `json:"assigned_agents"`
	ExplicitPaths    []string `json:"explicit_paths"`
	// RepositoryID is set on a layer: a zone that belongs to one repository's
	// architecture, with the Access its agents have. Zones with no
	// repository are not layers and carry no access.
	RepositoryID string `json:"repository_id,omitempty"`
	// Position is the layer's place in its record, so equal rivals resolve
	// the same way after a round trip through the database.
	Position int          `json:"position,omitempty"`
	Access   *LayerAccess `json:"access,omitempty"`
}

// Contains reports whether a repository-relative path belongs to the zone:
// its pattern (a regular expression, unanchored) matches the path, or the
// path is one of its explicit paths or under one. A zone with neither
// matches nothing; an invalid pattern matches nothing.
func (z *Zone) Contains(path string) bool {
	return NewZoneMatcher([]*Zone{z}).Contains(0, path)
}

// ZoneMatcher matches paths against a set of zones, each pattern compiled
// once. It is the one rule for which zone a path belongs to.
type ZoneMatcher struct {
	zones    []*Zone
	patterns []*regexp.Regexp // nil: no pattern, or an invalid one
}

// NewZoneMatcher compiles the zones' patterns.
func NewZoneMatcher(zones []*Zone) *ZoneMatcher {
	m := &ZoneMatcher{zones: zones, patterns: make([]*regexp.Regexp, len(zones))}
	for i, z := range zones {
		if z.Pattern != "" {
			m.patterns[i], _ = regexp.Compile(z.Pattern)
		}
	}
	return m
}

// Contains reports whether path belongs to the i-th zone.
func (m *ZoneMatcher) Contains(i int, path string) bool {
	if re := m.patterns[i]; re != nil && re.MatchString(path) {
		return true
	}
	for _, ep := range m.zones[i].ExplicitPaths {
		if path == ep || strings.HasPrefix(path, ep+"/") {
			return true
		}
	}
	return false
}

// Match returns the ids of the zones path belongs to, in the zones' order;
// an empty (non-nil) list when none.
func (m *ZoneMatcher) Match(path string) []string {
	ids := []string{}
	for i, z := range m.zones {
		if m.Contains(i, path) {
			ids = append(ids, z.ID)
		}
	}
	return ids
}
