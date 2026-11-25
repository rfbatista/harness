package productowner

import (
	"errors"
	"testing"
	"time"
)

func TestLocalZoneName(t *testing.T) {
	at := func(offset int) time.Time {
		return time.Date(2030, 1, 1, 12, 0, 0, 0, time.FixedZone("x", offset))
	}
	link := func(target string) func(string) (string, error) {
		return func(string) (string, error) { return target, nil }
	}
	noLink := func(string) (string, error) { return "", errors.New("not a link") }
	env := func(v string, set bool) func(string) (string, bool) {
		return func(string) (string, bool) { return v, set }
	}
	h := func(hours, mins int) int { return hours*3600 + mins*60 }

	tests := []struct {
		name     string
		env      func(string) (string, bool)
		readlink func(string) (string, error)
		local    string
		now      time.Time
		want     string
	}{
		{"TZ names a zone", env("America/Sao_Paulo", true), noLink, "Local", at(0), "America/Sao_Paulo"},
		{"TZ with a colon", env(":Europe/Paris", true), noLink, "Local", at(0), "Europe/Paris"},
		{"TZ is not a zone, the link answers", env("Nowhere/Land", true), link("/usr/share/zoneinfo/Asia/Tokyo"), "Local", at(0), "Asia/Tokyo"},
		{"TZ empty is unset", env("", true), link("/var/db/timezone/zoneinfo/Europe/Lisbon"), "Local", at(0), "Europe/Lisbon"},
		{"the link is not under zoneinfo", env("", false), link("/etc/other/UTC"), "Europe/Berlin", at(h(1, 0)), "Europe/Berlin"},
		{"not a link, a named location", env("", false), noLink, "Australia/Sydney", at(h(11, 0)), "Australia/Sydney"},
		{"Local is no name: a negative offset", env("", false), noLink, "Local", at(-h(3, 0)), "-03:00"},
		{"empty location: a positive half-hour offset", env("", false), noLink, "", at(h(5, 30)), "+05:30"},
		{"zero offset", env("", false), noLink, "Local", at(0), "+00:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := localZoneName(tc.env, tc.readlink, tc.local, tc.now); got != tc.want {
				t.Errorf("localZoneName = %q, want %q", got, tc.want)
			}
		})
	}
}
