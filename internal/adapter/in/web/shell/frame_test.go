package shell

import "testing"

// The row's meta and hidden words, as the live copy's toRow writes them
// (web/src/modules/tasks/presentation/railView.test.js pins the same cases).
func TestRailRowMetaIsOneItemByPriority(t *testing.T) {
	for _, c := range []struct {
		name                string
		link                Link
		reviews, lead, tail string
		waiting, running    bool
	}{
		{"worst case", Link{Live: 3, Attention: true, Reviews: 2}, "2 reviews", ", ", " waiting on you", false, false},
		{"review", Link{Attention: true, Reviews: 1}, "1 review", ", ", " waiting on you", false, false},
		{"waiting", Link{Live: 1, Attention: true}, "", ", ", " on you", true, false},
		{"running", Link{Live: 2}, "", ", running, ", " sessions", false, true},
		{"one session", Link{Live: 1}, "", ", running, ", " session", false, true},
		{"quiet", Link{}, "", "", "", false, false},
	} {
		l := c.link
		if l.ReviewsText() != c.reviews || l.MetaLead() != c.lead || l.MetaTail() != c.tail || l.Waiting() != c.waiting || l.Running() != c.running {
			t.Errorf("%s: reviews %q lead %q tail %q waiting %v running %v", c.name, l.ReviewsText(), l.MetaLead(), l.MetaTail(), l.Waiting(), l.Running())
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
