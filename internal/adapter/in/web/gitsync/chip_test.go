package gitsync

import (
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

func ptr[T any](v T) *T { return &v }

var now = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// The chip says what the browser's chipOf says (view.js): the remote alone for
// origin/<branch>, counts only when counted, one word otherwise, and a sentence
// that carries the fetch age.
func TestChipSaysWhatTheBrowsersChipSays(t *testing.T) {
	fetched := now.Add(-12 * time.Minute)
	long := now.Add(-3 * time.Hour)
	cases := []struct {
		name  string
		state domain.GitState
		want  Chip
	}{
		{"ahead", domain.GitState{Branch: "agent/x", FetchedAt: &fetched, Upstream: domain.UpstreamState{
			State: domain.UpstreamAhead, Ref: ptr("origin/agent/x"), Remote: ptr("origin"), Ahead: ptr(3), Behind: ptr(0)}},
			Chip{State: "ahead", Upstream: "origin", HasCounts: true, Ahead: 3, Title: "origin/agent/x",
				Describe: "Upstream origin/agent/x: 3 commits ahead, 0 commits behind, fetched 12 min ago.", FetchedWord: "fetched 12 min ago"}},
		{"another ref keeps its name and old counts are old", domain.GitState{Branch: "agent/x", FetchedAt: &long, Upstream: domain.UpstreamState{
			State: domain.UpstreamBehind, Ref: ptr("origin/main"), Remote: ptr("origin"), Ahead: ptr(0), Behind: ptr(1)}},
			Chip{State: "behind", Upstream: "origin/main", HasCounts: true, Behind: 1, Title: "origin/main", Old: true,
				Describe: "Upstream origin/main: 0 commits ahead, 1 commit behind, fetched 3 h ago.", FetchedWord: "fetched 3 h ago"}},
		{"never pushed", domain.GitState{Branch: "agent/x", Upstream: domain.UpstreamState{State: domain.UpstreamNone}},
			Chip{State: "none", Word: "not pushed", Title: "no upstream", Describe: "No upstream: not pushed.", FetchedWord: "not fetched yet"}},
		{"pushed, never fetched", domain.GitState{Branch: "agent/x", PushedAt: &fetched, Upstream: domain.UpstreamState{
			State: domain.UpstreamInSync, Ref: ptr("origin/agent/x"), Remote: ptr("origin")}},
			Chip{State: "in-sync", Upstream: "origin", Word: "in sync", Title: "origin/agent/x",
				Describe: "Upstream origin/agent/x: in sync, pushed 12 min ago.", FetchedWord: "pushed 12 min ago · never fetched"}},
	}
	for _, c := range cases {
		if got := ChipOf(c.state, now); got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestBaseChipCountsOrSaysInSyncOrGone(t *testing.T) {
	for _, c := range []struct {
		base domain.BaseState
		want Chip
	}{
		{domain.BaseState{Ahead: 3, Behind: 8}, Chip{State: "diverged", HasCounts: true, Ahead: 3, Behind: 8}},
		{domain.BaseState{Ahead: 2}, Chip{State: "ahead", HasCounts: true, Ahead: 2}},
		{domain.BaseState{}, Chip{State: "in-sync", Word: "in sync"}},
		{domain.BaseState{Gone: true}, Chip{State: "gone", Word: "base gone"}},
	} {
		if got := BaseOf(domain.GitState{Base: c.base}); got != c.want {
			t.Errorf("%+v: got %+v want %+v", c.base, got, c.want)
		}
	}
}
