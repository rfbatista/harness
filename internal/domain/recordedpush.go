package domain

import (
	"regexp"
	"time"
)

// Who made a recorded push.
const (
	PushByAgent  = "agent"
	PushByPerson = "person"
)

// RecordedPush is one successful push the harness made to a remote: the branch
// that went, where it landed and the commit it carried (ADR 0071). The
// Branches list reads these to say where a branch was pushed.
type RecordedPush struct {
	// RequestID is the branch request the push was approved under, "" for the
	// person's own Push.
	RequestID    string
	SessionID    string
	RepositoryID string
	// Branch is the local branch pushed.
	Branch       string
	Remote       string
	RemoteBranch string
	// Commit is the head the push carried.
	Commit string
	At     time.Time
	// By is PushByAgent or PushByPerson.
	By string
}

var urlUserinfo = regexp.MustCompile(`(://)[^/\s@'"]+@`)

// RedactURL is url without the credentials it carries ("https://user:token@host/x"
// becomes "https://host/x"); a URL with none is returned as configured.
func RedactURL(url string) string { return urlUserinfo.ReplaceAllString(url, "$1") }
