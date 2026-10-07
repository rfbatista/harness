package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"
)

func sess(id, parent string, mode SessionMode, status SessionStatus, ageMin int) *Session {
	return &Session{ID: id, ParentSessionID: parent, Mode: mode, Status: status,
		CreatedAt: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC).Add(-time.Duration(ageMin) * time.Minute)}
}

func TestTaskArchitect(t *testing.T) {
	cases := []struct {
		name     string
		sessions []*Session
		want     string
	}{
		{"none", []*Session{sess("a", "", "", SessionRunning, 1)}, ""},
		{"newest wins", []*Session{sess("old", "", SessionModeArchitect, SessionRunning, 10), sess("new", "", SessionModeArchitect, SessionRunning, 1)}, "new"},
		{"running beats a newer ended one", []*Session{sess("live", "", SessionModeArchitect, SessionRunning, 10), sess("gone", "", SessionModeArchitect, SessionStopped, 1)}, "live"},
		{"an ended one is still the architect when alone", []*Session{sess("gone", "", SessionModeArchitect, SessionStopped, 1)}, "gone"},
		{"design mode is not an architect", []*Session{sess("d", "", SessionModeDesign, SessionRunning, 1)}, ""},
	}
	for _, c := range cases {
		got := TaskArchitect(c.sessions)
		id := ""
		if got != nil {
			id = got.ID
		}
		if id != c.want {
			t.Errorf("%s: architect %q, want %q", c.name, id, c.want)
		}
	}
}

func TestRoleOf(t *testing.T) {
	all := []*Session{
		sess("arch", "", SessionModeArchitect, SessionRunning, 10),
		sess("child", "arch", "", SessionRunning, 5),
		sess("grandchild", "child", "", SessionRunning, 4),
		sess("peer", "", "", SessionRunning, 3),
		sess("peerchild", "peer", "", SessionRunning, 2),
		sess("cycleA", "cycleB", "", SessionRunning, 2),
		sess("cycleB", "cycleA", "", SessionRunning, 2),
		sess("orphan", "deleted", "", SessionRunning, 2),
	}
	want := map[string]SessionRole{
		"arch": RoleArchitect, "child": RoleDelegate, "grandchild": RoleDelegate,
		"peer": RolePeer, "peerchild": RolePeer, "cycleA": RolePeer, "orphan": RolePeer, "unknown": RolePeer,
	}
	for id, role := range want {
		got, arch := RoleOf(id, all)
		if got != role || arch != "arch" {
			t.Errorf("RoleOf(%s) = %q, %q; want %q, arch", id, got, arch, role)
		}
	}
	if role, arch := RoleOf("a", []*Session{sess("a", "", "", SessionRunning, 1)}); role != RolePeer || arch != "" {
		t.Errorf("no architect: %q, %q", role, arch)
	}
}

func TestTaskMessageValidate(t *testing.T) {
	ok := []TaskMessage{
		{Kind: MessageQuestion, Body: "why?"},
		{Kind: MessageStatusReport, Body: "on it", Status: ReportWorking},
		{Kind: MessageReply, Body: "fine", Verdict: VerdictApproved},
		{Kind: MessageReviewRequest, Body: "look"},
	}
	for _, m := range ok {
		if err := m.Validate(); err != nil {
			t.Errorf("%+v: %v", m, err)
		}
	}
	bad := map[string]TaskMessage{
		"kind":                 {Kind: "shout", Body: "x"},
		"body":                 {Kind: MessageQuestion, Body: "  "},
		"status on a question": {Kind: MessageQuestion, Body: "x", Status: ReportDone},
		"unknown status":       {Kind: MessageStatusReport, Body: "x", Status: "sleeping"},
		"verdict on a report":  {Kind: MessageStatusReport, Body: "x", Verdict: VerdictApproved},
		"unknown verdict":      {Kind: MessageReply, Body: "x", Verdict: "meh"},
	}
	for name, m := range bad {
		if err := m.Validate(); errs.Code(err) != "INVALID_INPUT" {
			t.Errorf("%s: want INVALID_INPUT, got %v", name, err)
		}
	}
}

func TestValidStatusCheckMinutes(t *testing.T) {
	for _, n := range []int{0, 2, 10, 240} {
		if err := ValidStatusCheckMinutes(n); err != nil {
			t.Errorf("%d: %v", n, err)
		}
	}
	for _, n := range []int{-1, 1, 241} {
		if ValidStatusCheckMinutes(n) == nil {
			t.Errorf("%d must be refused", n)
		}
	}
}

// The derived fields are always on the wire: role as "" for a peer, the
// architect and status check as null when there is none.
func TestSessionAndTicketWireCarryDerivedFields(t *testing.T) {
	b, _ := json.Marshal(&Session{ID: "s"})
	for _, k := range []string{`"role":""`, `"architect_session_id":null`, `"status_check":null`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("session JSON lacks %s: %s", k, b)
		}
	}
	b, _ = json.Marshal(&Ticket{ID: "t"})
	for _, k := range []string{`"architect_session_id":null`, `"pending_reviews":0`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("ticket JSON lacks %s: %s", k, b)
		}
	}
	b, _ = json.Marshal(&TaskMessage{ID: "m", Queued: true})
	if strings.Contains(string(b), "queued") {
		t.Errorf("queued is server bookkeeping, not wire: %s", b)
	}
}
