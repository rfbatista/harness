package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNotificationUnreadUntilReadOrResolved(t *testing.T) {
	now := time.Now()
	if n := (&Notification{}); !n.Unread() {
		t.Fatal("a fresh notification is unread")
	}
	if n := (&Notification{ReadAt: &now}); n.Unread() {
		t.Fatal("a read notification is not unread")
	}
	if n := (&Notification{ResolvedAt: &now}); n.Unread() {
		t.Fatal("a resolved notification is not unread")
	}
}

func TestNotificationLevels(t *testing.T) {
	for _, s := range []string{"info", "action"} {
		if _, err := ParseNotificationLevel(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if _, err := ParseNotificationLevel("urgent"); err == nil {
		t.Fatal("urgent is not a level")
	}
	cases := []struct {
		l, min NotificationLevel
		want   bool
	}{
		{NotificationInfo, NotificationInfo, true},
		{NotificationAction, NotificationInfo, true},
		{NotificationAction, NotificationAction, true},
		{NotificationInfo, NotificationAction, false},
	}
	for _, c := range cases {
		if got := c.l.AtLeast(c.min); got != c.want {
			t.Errorf("%s.AtLeast(%s) = %v", c.l, c.min, got)
		}
	}
}

func TestNotificationTextLimitsCountCharacters(t *testing.T) {
	if err := ValidateNotificationText(strings.Repeat("é", 120), strings.Repeat("ü", 2000)); err != nil {
		t.Fatalf("at the limits: %v", err)
	}
	for name, c := range map[string][2]string{
		"empty title": {"", ""},
		"long title":  {strings.Repeat("a", 121), ""},
		"long body":   {"t", strings.Repeat("b", 2001)},
	} {
		if err := ValidateNotificationText(c[0], c[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestHarnessLinks(t *testing.T) {
	for _, ok := range []string{"/", "/projects/p1", "/projects/p1/tasks/t1?tab=design#x"} {
		if err := ValidateHarnessLink(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "projects/p1", "https://evil.example", "//evil.example/x", "/\\evil", "/a\nb", "javascript:alert(1)"} {
		if err := ValidateHarnessLink(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := TaskLink("p 1", "t1"); got != "/projects/p%201/tasks/t1" {
		t.Errorf("TaskLink = %q", got)
	}
	if got := TaskLink("p1", ""); got != "/projects/p1" {
		t.Errorf("TaskLink without a task = %q", got)
	}
}

func TestNotificationWireShape(t *testing.T) {
	b, _ := json.Marshal(Notification{ID: "n1", ProjectID: "p1", ProjectName: "P", Source: NotificationFromSystem,
		Kind: NotificationTaskReview, Level: NotificationAction, Title: "T", Link: "/projects/p1", CauseKey: "task_review:t1"})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, absent := range []string{"task_id", "task_title", "session_id", "body", "read_at", "resolved_at", "CauseKey", "cause_key"} {
		if _, ok := m[absent]; ok {
			t.Errorf("%s is on the wire: %s", absent, b)
		}
	}
	for _, present := range []string{"id", "project_id", "project_name", "source", "kind", "level", "title", "link", "created_at"} {
		if _, ok := m[present]; !ok {
			t.Errorf("%s is missing: %s", present, b)
		}
	}
}

func TestProviderSettingsAreValidated(t *testing.T) {
	for _, ok := range [][2]string{
		{NotificationProviderEnabledKey("log"), "true"}, {NotificationProviderEnabledKey("log"), "false"},
		{NotificationProviderLevelKey("log"), "action"}, {NotificationProviderLevelKey("log"), ""},
	} {
		if err := ValidateSetting(ok[0], ok[1]); err != nil {
			t.Errorf("%s=%q: %v", ok[0], ok[1], err)
		}
	}
	for _, bad := range [][2]string{
		{"notifications.log.enabled", "yes"}, {"notifications.log.level", "urgent"},
	} {
		if err := ValidateSetting(bad[0], bad[1]); err == nil {
			t.Errorf("%s=%q accepted", bad[0], bad[1])
		}
	}
}
