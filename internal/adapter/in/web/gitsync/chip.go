// Package gitsync paints a worktree's git state on the server's first paint:
// the sync-state chip, with the words and attributes the browser's sync module
// (web/src/modules/sync/presentation/view.js) writes into the same markup, so
// the two paints match and nothing moves when the browser takes over.
package gitsync

import (
	"fmt"
	"time"

	"operators-mcp/internal/domain"
)

// oldAfter is how stale a fetch must be for its counts to read as old.
const oldAfter = time.Hour

// Chip is a sync-state chip: the block's data-state, the remote's name when
// named, the arrows' counts or a word, and the sentence that describes it.
type Chip struct {
	State       string
	Upstream    string
	HasCounts   bool
	Ahead       int
	Behind      int
	Word        string
	Title       string
	Describe    string
	Old         bool
	FetchedWord string
}

var chipStates = map[string]string{
	domain.UpstreamInSync: "in-sync", domain.UpstreamAhead: "ahead", domain.UpstreamBehind: "behind",
	domain.UpstreamDiverged: "diverged", domain.UpstreamNone: "none", domain.UpstreamGone: "gone", domain.UpstreamUnknown: "unknown",
}

var chipWords = map[string]string{
	domain.UpstreamInSync: "in sync", domain.UpstreamNone: "not pushed",
	domain.UpstreamGone: "upstream deleted", domain.UpstreamUnknown: "not fetched",
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// age is how long ago origin was last fetched (or the branch pushed when the
// repository was never fetched), in the browser's words.
type age struct {
	ago         string
	never, push bool
	old         bool
}

func ageOf(s domain.GitState, now time.Time) age {
	since := s.FetchedAt
	if since == nil {
		since = s.PushedAt
	}
	if since == nil {
		return age{never: true, old: true}
	}
	minutes := int(now.Sub(*since).Minutes())
	if minutes < 0 {
		minutes = 0
	}
	var ago string
	switch {
	case minutes < 1:
		ago = "just now"
	case minutes < 60:
		ago = fmt.Sprintf("%d min ago", minutes)
	case minutes < 24*60:
		ago = fmt.Sprintf("%d h ago", minutes/60)
	default:
		ago = plural(minutes/(24*60), "day", "days") + " ago"
	}
	return age{ago: ago, push: s.FetchedAt == nil, old: now.Sub(*since) > oldAfter}
}

// ChipOf is the chip of a state as the browser's chipOf writes it.
func ChipOf(s domain.GitState, now time.Time) Chip {
	up := s.Upstream
	counted := up.State == domain.UpstreamAhead || up.State == domain.UpstreamBehind || up.State == domain.UpstreamDiverged
	c := Chip{State: chipStates[up.State], Word: chipWords[up.State], HasCounts: counted}
	if counted {
		if up.Ahead != nil {
			c.Ahead = *up.Ahead
		}
		if up.Behind != nil {
			c.Behind = *up.Behind
		}
	}
	ref := ""
	if up.Ref != nil {
		ref = *up.Ref
		c.Upstream = ref
		if up.Remote != nil && ref == *up.Remote+"/"+s.Branch {
			c.Upstream = *up.Remote
		}
	}
	c.Title = ref
	if ref == "" {
		c.Title = "no upstream"
	}
	a := ageOf(s, now)
	c.Old = counted && a.old
	what := "No upstream"
	if ref != "" {
		what = "Upstream " + ref
	}
	facts := chipWords[up.State]
	if counted {
		facts = plural(c.Ahead, "commit", "commits") + " ahead, " + plural(c.Behind, "commit", "commits") + " behind"
	}
	tail := ""
	if !a.never {
		verb := "fetched"
		if a.push {
			verb = "pushed"
		}
		tail = ", " + verb + " " + a.ago
	}
	c.Describe = what + ": " + facts + tail + "."
	switch {
	case a.push && !a.never:
		c.FetchedWord = "pushed " + a.ago + " · never fetched"
	case a.never:
		c.FetchedWord = "not fetched yet"
	default:
		c.FetchedWord = "fetched " + a.ago
	}
	return c
}

// BaseOf is the chip of the comparison with the branch the worktree was cut
// from, as the panel's base row reads it.
func BaseOf(s domain.GitState) Chip {
	b := s.Base
	if b.Gone {
		return Chip{State: "gone", Word: "base gone"}
	}
	kind := "in-sync"
	switch {
	case b.Ahead > 0 && b.Behind > 0:
		kind = "diverged"
	case b.Ahead > 0:
		kind = "ahead"
	case b.Behind > 0:
		kind = "behind"
	}
	if kind == "in-sync" {
		return Chip{State: kind, Word: "in sync"}
	}
	return Chip{State: kind, HasCounts: true, Ahead: b.Ahead, Behind: b.Behind}
}
