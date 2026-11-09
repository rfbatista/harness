package shell

import (
	"context"
	"strings"
	"testing"
)

// The row's meta and hidden words, as the live copy's toRow writes them
// (web/src/modules/tasks/presentation/railView.test.js pins the same cases).
func TestRailRowMetaIsOneItemByPriority(t *testing.T) {
	for _, c := range []struct {
		name                      string
		link                      Link
		reviews, lead, tail       string
		waiting, running, merging bool
	}{
		{"worst case", Link{Live: 3, Attention: true, Reviews: 2, Merge: "merged"}, "2 reviews", ", ", " waiting on you", false, false, false},
		{"review", Link{Attention: true, Reviews: 1}, "1 review", ", ", " waiting on you", false, false, false},
		{"waiting", Link{Live: 1, Attention: true, Merge: "merged"}, "", ", ", " on you", true, false, false},
		{"running", Link{Live: 2, Merge: "merged"}, "", ", running, ", " sessions", false, true, false},
		{"one session", Link{Live: 1}, "", ", running, ", " session", false, true, false},
		{"merge", Link{Merge: "partial", MergeText: "1/2 merged"}, "", ", ", "", false, false, true},
		{"quiet", Link{}, "", "", "", false, false, false},
	} {
		l := c.link
		if l.ReviewsText() != c.reviews || l.MetaLead() != c.lead || l.MetaTail() != c.tail || l.Waiting() != c.waiting || l.Running() != c.running || l.Merging() != c.merging {
			t.Errorf("%s: reviews %q lead %q tail %q waiting %v running %v merging %v", c.name, l.ReviewsText(), l.MetaLead(), l.MetaTail(), l.Waiting(), l.Running(), l.Merging())
		}
	}
}

// A quiet Review or Done row shows its merge glyph and lower-case word, with
// the state hook; a row without one carries no hook.
func TestRailRowShowsTheMergeWordWithItsStateHook(t *testing.T) {
	render := func(l Link) string {
		var b strings.Builder
		if err := railLink(l).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	got := render(Link{TaskID: "t1", Label: "Ship it", Href: "/x", Merge: "not_merged", MergeText: "not merged"})
	for _, want := range []string{`class="[ meta ]" data-merge-state="not_merged"`, `data-glyph="merged"`, `data-glyph="not_merged"`, `>not merged</span>`} {
		if !strings.Contains(got, want) {
			t.Errorf("row lacks %q:\n%s", want, got)
		}
	}
	for _, l := range []Link{{TaskID: "t1", Label: "Quiet"}, {TaskID: "t1", Label: "Busy", Live: 1, Merge: "merged", MergeText: "merged"}} {
		if got := render(l); strings.Contains(got, "data-merge-state") || strings.Contains(got, "data-glyph") {
			t.Errorf("%s shows a merge meta:\n%s", l.Label, got)
		}
	}
}

func TestRailGroupHeadingWords(t *testing.T) {
	g := RailGroup{Key: "backlog", Total: 9, Links: []Link{{Attention: true}, {}}}
	if g.CountWords() != " tasks, 1 needs you" || g.HeadingID() != "rail-backlog-heading" || g.ListID() != "rail-backlog" {
		t.Errorf("words %q ids %q %q", g.CountWords(), g.HeadingID(), g.ListID())
	}
	if w := (RailGroup{Total: 1}).CountWords(); w != " task" {
		t.Errorf("one task: %q", w)
	}
	if m := (RailGroup{More: make([]Link, 7)}).MoreText(); m != "Show 7 more" {
		t.Errorf("more: %q", m)
	}
}

func renderProjectNav(t *testing.T, r Rail) string {
	t.Helper()
	var b strings.Builder
	if err := projectNav(r).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The project navigation's Services item sits after Code, carries the running
// count for the browser to keep current (contract: Harness server ↔ Web UI —
// services, section 8), and shows its badge only above zero.
func TestProjectNavServicesItemFollowsCodeAndCarriesTheRunningCount(t *testing.T) {
	rail := Rail{BoardHref: "/b", CodeHref: "/c", ReviewsHref: "/r", Services: ServicesNav{Href: "/projects/p1/services", ProjectID: "p1", Running: 3}}
	got := renderProjectNav(t, rail)
	for _, want := range []string{`data-page="services"`, `data-running-services="3"`, `data-project-id="p1"`, `href="/projects/p1/services"`, `>3</span>`, `> running</span>`} {
		if !strings.Contains(got, want) {
			t.Errorf("nav lacks %q:\n%s", want, got)
		}
	}
	code, services, reviews := strings.Index(got, `data-page="code"`), strings.Index(got, `data-page="services"`), strings.Index(got, `data-page="reviews"`)
	if !(code < services && services < reviews) {
		t.Errorf("Services must sit after Code and before Review requests: code %d services %d reviews %d", code, services, reviews)
	}
}

func TestProjectNavServicesBadgeIsHiddenAtZeroAndCapsAt99(t *testing.T) {
	zero := renderProjectNav(t, Rail{Services: ServicesNav{Href: "/s", ProjectID: "p1"}})
	if !strings.Contains(zero, `data-running-services="0"`) || !strings.Contains(zero, `style="display: none"`) {
		t.Errorf("at zero the badge is drawn hidden:\n%s", zero)
	}
	if got := renderProjectNav(t, Rail{Services: ServicesNav{Href: "/s", ProjectID: "p1", Running: 140}}); !strings.Contains(got, `>99+</span>`) {
		t.Errorf("140 reads 99+:\n%s", got)
	}
	if got := renderProjectNav(t, Rail{}); strings.Contains(got, `data-page="services"`) {
		t.Errorf("no services href, no item:\n%s", got)
	}
}
