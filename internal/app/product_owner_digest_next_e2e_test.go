package app

import (
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The settings answer when the next digest will be recorded and the zone it is
// read in (contract "Product owner settings", ticket 07).

// wantNext fails unless the settings name want as the next digest.
func (e *poE2E) wantNext(label string, want time.Time) {
	e.t.Helper()
	s := e.settings()
	if s.NextDigestAt == nil {
		e.t.Fatalf("%s: next_digest_at is null, want %s", label, want)
	}
	got, err := time.Parse(time.RFC3339, *s.NextDigestAt)
	if err != nil {
		e.t.Fatalf("%s: next_digest_at %q is not RFC 3339: %v", label, *s.NextDigestAt, err)
	}
	if !got.Equal(want) {
		e.t.Errorf("%s: next_digest_at = %s, want %s", label, got, want)
	}
	if _, off := got.Zone(); off != func() int { _, o := want.Zone(); return o }() {
		e.t.Errorf("%s: next_digest_at %q is not in the server's local zone", label, *s.NextDigestAt)
	}
}

func (e *poE2E) wantNoNext(label string) {
	e.t.Helper()
	if s := e.settings(); s.NextDigestAt != nil {
		e.t.Errorf("%s: next_digest_at = %s, want null", label, *s.NextDigestAt)
	}
}

func TestTheNextDigestIsTodayWhenTheTimeIsStillAheadAndTomorrowWhenItHasPassed(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.wantNoNext("no digest time")

	clk.set(clk.at(1, 7, 0))
	e.setDigestTime("08:30")
	e.wantNext("a time after now", clk.at(0, 8, 30))

	e.setDigestTime("06:00")
	e.wantNext("a time before now", clk.at(1, 6, 0))

	e.setDigestTime("")
	e.wantNoNext("a cleared time")
	if s := e.settings(); s.DigestTime != nil {
		t.Errorf("digest_time = %q, want null", *s.DigestTime)
	}
}

func TestTheNextDigestIsNullWhileTheProductOwnerIsOffAndBackWhenItIsOn(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.wantNext("on", clk.at(1, 8, 30))
	e.setEnabled(false)
	e.wantNoNext("off")
	e.setEnabled(true)
	e.wantNext("on again", clk.at(1, 8, 30))
}

func TestTheNextDigestIsTomorrowAfterADigestAndNowWhileOneIsOwed(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.moveTask("Moved", domain.TicketStatusReview, clk.at(1, 6, 0))
	e.setEnabled(false)
	clk.set(clk.at(1, 9, 0))
	settle()

	// Switched on after the time passed: a digest is owed and the run is held,
	// so the next is the instant the scheduler took it up.
	e.hold()
	e.setEnabled(true)
	eventuallyPO(t, "the owed digest run starts", func() string {
		if len(e.launches()) == 0 {
			return "claude was not launched"
		}
		return ""
	})
	e.wantNext("an owed digest", clk.Now())
	e.release()
	e.waitDigests(1)
	e.wantNext("after the digest", clk.at(1, 8, 30))
}

func TestADigestIsRecordedAtTheInstantTheNextDigestNamed(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.moveTask("Moved", domain.TicketStatusReview, clk.at(1, 6, 0))
	s := e.settings()
	if s.NextDigestAt == nil {
		t.Fatal("next_digest_at is null")
	}
	next, err := time.Parse(time.RFC3339, *s.NextDigestAt)
	if err != nil {
		t.Fatal(err)
	}
	clk.set(next.Add(-time.Minute))
	settle()
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests a minute before next_digest_at, want none", n)
	}
	clk.set(next)
	e.waitDigests(1)
}

func TestAChangeOfTheDigestTimeReachesTheFeedWithTheNextDigest(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.setDigestTime("10:00")
	e.wantNext("after the change", clk.at(1, 10, 0))
	eventuallyPO(t, "the settings event with the new next digest", func() string {
		for _, c := range e.feed.of("product_owner_settings") {
			s, _ := c["settings"].(map[string]any)
			at, _ := s["next_digest_at"].(string)
			if s["digest_time"] == "10:00" {
				got, err := time.Parse(time.RFC3339, at)
				if err != nil || !got.Equal(clk.at(1, 10, 0)) {
					return "event next_digest_at = " + at
				}
				return ""
			}
		}
		return "no settings event for 10:00"
	})
}

func TestTheTimeZoneIsTheServersZoneNameNotAnAbbreviation(t *testing.T) {
	t.Setenv("TZ", "America/Sao_Paulo")
	e, _ := newDigestE2E(t)
	if got := e.settings().TimeZone; got != "America/Sao_Paulo" {
		t.Errorf("time_zone = %q, want America/Sao_Paulo", got)
	}
	if strings.HasPrefix(e.settings().TimeZone, "-") {
		t.Error("time_zone is an abbreviation")
	}
}
