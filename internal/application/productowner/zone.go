package productowner

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// serverZoneName is the server's local time zone as the settings show it.
func serverZoneName(now time.Time) string {
	return localZoneName(os.LookupEnv, os.Readlink, time.Local.String(), now)
}

// localZoneName is the server's time zone as an IANA name when it can tell
// one: the TZ environment variable when it names a zone, else the zone that
// /etc/localtime links into, else the name of the local location. When none
// gives a name it is the UTC offset of now, "+HH:MM" or "-HH:MM". The browser
// reads the name across daylight-saving changes, which an abbreviation such as
// "-03" does not allow. Its inputs are parameters so each way can be tested.
func localZoneName(lookupEnv func(string) (string, bool), readlink func(string) (string, error), local string, now time.Time) string {
	if tz, ok := lookupEnv("TZ"); ok {
		if name := strings.TrimPrefix(tz, ":"); isZoneName(name) {
			return name
		}
	}
	if target, err := readlink("/etc/localtime"); err == nil {
		if _, name, found := strings.Cut(target, "zoneinfo/"); found && isZoneName(name) {
			return name
		}
	}
	if local != "" && local != "Local" && isZoneName(local) {
		return local
	}
	_, offset := now.Zone()
	sign := '+'
	if offset < 0 {
		sign, offset = '-', -offset
	}
	return fmt.Sprintf("%c%02d:%02d", sign, offset/3600, offset%3600/60)
}

func isZoneName(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}
