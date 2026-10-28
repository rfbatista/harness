package web

import (
	"regexp"
	"testing"

	"operators-mcp/internal/adapter/in/web/sessions"
)

// The task_band cookie holds the open panel's key. An unknown or malformed
// value counts as unset (as rail_collapsed's malformed values do).
func TestTaskBandCookieParsesToAPanelKeyOrUnset(t *testing.T) {
	for value, want := range map[string]string{
		"reviews":      "reviews",
		"feature":      "feature",
		"token-savers": "token-savers",
		"none":         "none",
		"":             "",
		"Feature":      "",
		"savers":       "",
		"reviews,none": "",
		"nope":         "",
		" feature":     "",
	} {
		if got := sessions.ParseBand(value); got != want {
			t.Errorf("ParseBand(%q) = %q, want %q", value, got, want)
		}
	}
}

var openBandAttr = regexp.MustCompile(`aria-label="Task context"[^>]*data-open="([a-z-]*)"`)

// openBandOf is the panel the server paints open, and whether its button
// says so.
func openBandOf(t *testing.T, body string) (open string, expanded map[string]bool) {
	t.Helper()
	m := openBandAttr.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page has no context strip carrying data-open")
	}
	expanded = map[string]bool{}
	for _, band := range []string{"reviews", "feature", "token-savers"} {
		tag := tagAt(body, `data-band="`+band+`"`)
		if tag != "" {
			expanded[band] = regexp.MustCompile(`aria-expanded="true"`).MatchString(tag)
		}
	}
	return m[1], expanded
}

func bandWorld(pending int) world {
	w := featureBoard()
	w.tickets[0].PendingReviews = pending
	return w
}

func TestTheServerPaintsTheOpenPanelFromTheCookieAndTheDefault(t *testing.T) {
	for _, c := range []struct {
		name    string
		pending int
		cookie  string
		want    string
	}{
		{"nothing stored, a review waits: Reviews opens", 2, "", "reviews"},
		{"nothing stored, nothing waits: all closed", 0, "", ""},
		{"Feature stored", 0, "feature", "feature"},
		{"Feature stored beats a waiting review, which stays on the strip", 2, "feature", "feature"},
		{"none stored beats a waiting review", 2, "none", ""},
		{"Reviews stored and waiting", 1, "reviews", "reviews"},
		{"Reviews stored, nothing waits and nothing is painted settled", 0, "reviews", ""},
		{"an unknown value counts as unset", 2, "bogus", "reviews"},
		{"a malformed value counts as unset", 0, "feature,reviews", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHandler(t, bandWorld(c.pending))
			var body string
			if c.cookie == "" {
				body = get(t, h, "/projects/p1/tasks/t-feed").Body.String()
			} else {
				body = getWithCookie(t, h, "/projects/p1/tasks/t-feed", sessions.BandCookie+"="+c.cookie)
			}
			open, expanded := openBandOf(t, body)
			if open != c.want {
				t.Errorf("open panel = %q, want %q", open, c.want)
			}
			for band, isOpen := range expanded {
				if isOpen != (band == c.want) {
					t.Errorf("the %s button's aria-expanded is %v, with %q open", band, isOpen, c.want)
				}
			}
		})
	}
}

func TestAStoredBandThatTheTaskLacksIsIgnored(t *testing.T) {
	w := board() // t-feed has no children, no token savers
	w.tickets[0].PendingReviews = 1
	body := getWithCookie(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed", sessions.BandCookie+"=feature")
	if open, _ := openBandOf(t, body); open != "reviews" {
		t.Errorf("open panel = %q, want the default (reviews) when the stored Feature does not exist here", open)
	}
}
