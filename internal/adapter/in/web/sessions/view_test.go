package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// The browser's view tests read the same fixture, so first paint (here) and
// live updates (web/src/modules/sessions/presentation/view.js) agree.
func TestStatusViewMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/session-status.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Status           domain.SessionStatus `json:"status"`
			PendingApprovals int                  `json:"pending_approvals"`
			State, Word      string
			NeedsYou         bool `json:"needs_you"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range fixture.Cases {
		s := &domain.Session{Status: c.Status, PendingApprovals: c.PendingApprovals}
		if got := statusOf(s); got != (StatusView{c.State, c.Word}) {
			t.Errorf("%s+%d: got %+v, want {%s %s}", c.Status, c.PendingApprovals, got, c.State, c.Word)
		}
		if got := NeedsYou(s); got != c.NeedsYou {
			t.Errorf("%s+%d: needsYou = %v, want %v", c.Status, c.PendingApprovals, got, c.NeedsYou)
		}
	}
}

func TestPageViewGroupsForTriageAndSelectsTheFirstRow(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return now.Add(time.Duration(-m) * time.Minute) }
	list := []*domain.Session{
		{ID: "old-run", Task: "a", Status: domain.SessionRunning, UpdatedAt: at(30)},
		{ID: "done", Task: "b", Status: domain.SessionDone, UpdatedAt: at(1)},
		{ID: "new-run", Task: "", AgentID: "backend", Status: domain.SessionThinking, UpdatedAt: at(5)},
		{ID: "turn", Task: "d", Status: domain.SessionIdle, UpdatedAt: at(90)},
	}
	v := NewPageView(shell.Frame{}, &domain.Project{ID: "p1", Name: "coding_pool"}, &domain.Ticket{ID: "t1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}, list,
		[]*domain.Agent{{ID: "backend", Name: "Backend dev"}}, []*domain.Repository{{ID: "r1", Name: "harness"}}, now)

	var got [][]string
	for _, g := range v.Groups {
		ids := []string{g.Label}
		for _, r := range g.Rows {
			ids = append(ids, r.ID)
		}
		got = append(got, ids)
	}
	want := [][]string{{"Needs you", "turn"}, {"Running", "new-run", "old-run"}, {"Earlier", "done"}}
	if len(got) != len(want) {
		t.Fatalf("groups = %v, want %v", got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("groups = %v, want %v", got, want)
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("groups = %v, want %v", got, want)
			}
		}
	}

	if !v.Groups[0].Rows[0].Selected || v.Groups[0].Tone != "attention" {
		t.Errorf("the first row of Needs you should start selected with an attention tone: %+v", v.Groups[0])
	}
	newRun := v.Groups[1].Rows[0]
	if newRun.Title != "Untitled session" || newRun.Meta != "Backend dev · 5m" {
		t.Errorf("row = %+v", newRun)
	}
	if v.Summary != "4 sessions · 1 waiting" {
		t.Errorf("summary = %q", v.Summary)
	}
	if v.NewSession.DefaultRepository() != "r1" || !v.NewSession.CanStart() || v.Seed.AgentNames["backend"] != "Backend dev" {
		t.Errorf("new session form = %+v, names = %v", v.NewSession, v.Seed.AgentNames)
	}
	if v.TaskTitle != "Add SSE feed" || v.TaskStatus != "in progress" || v.Seed.TicketID != "t1" {
		t.Errorf("task = %q / %q / seed %q", v.TaskTitle, v.TaskStatus, v.Seed.TicketID)
	}
}

func TestSeedIsNeverNull(t *testing.T) {
	v := NewPageView(shell.Frame{}, &domain.Project{ID: "p1"}, &domain.Ticket{ID: "t1"}, nil, nil, nil, time.Now())
	b, err := json.Marshal(v.Seed)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"project_id":"p1","ticket_id":"t1","sessions":[],"agent_names":{},"repository_names":{}}` {
		t.Errorf("seed = %s", b)
	}
	if !v.Empty() {
		t.Error("a project without sessions is empty")
	}
	if v.NewSession.CanStart() {
		t.Error("a project without a repository cannot start a session")
	}
}

func TestAgentLabel(t *testing.T) {
	names := map[string]string{"a1": "Reviewer"}
	for id, want := range map[string]string{"a1": "Reviewer", "": "plain claude", "gone": "gone"} {
		if got := AgentLabel(id, names); got != want {
			t.Errorf("AgentLabel(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestRunsAs(t *testing.T) {
	names := map[string]string{"a1": "Reviewer"}
	for _, c := range []struct {
		s    *domain.Session
		want string
	}{
		{&domain.Session{AgentID: "a1"}, "Reviewer"},
		{&domain.Session{Mode: domain.SessionModeArchitect}, "plain claude as architect"},
		{&domain.Session{AgentID: "a1", Mode: domain.SessionModeArchitect}, "Reviewer as architect"},
		{&domain.Session{AgentID: "a1", Mode: domain.SessionMode("design")}, "Reviewer as design"},
	} {
		if got := RunsAs(c.s, names); got != c.want {
			t.Errorf("RunsAs(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestRelativeTimeMatchesTheBrowser(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		30 * time.Second: "now",
		4 * time.Minute:  "4m",
		3 * time.Hour:    "3h",
		50 * time.Hour:   "2d",
		-5 * time.Second: "now",
	}
	for ago, want := range cases {
		if got := relativeTime(now.Add(-ago), now); got != want {
			t.Errorf("%v ago: got %q, want %q", ago, got, want)
		}
	}
}

func TestStartedByNamesTheParentsAgent(t *testing.T) {
	lead := &domain.Session{ID: "lead", AgentID: "backend"}
	others := map[string]*domain.Session{"lead": lead}
	names := map[string]string{"backend": "Backend dev"}
	for _, c := range []struct {
		s    *domain.Session
		want string
	}{
		{&domain.Session{ParentSessionID: "lead"}, "Backend dev"},
		{&domain.Session{ParentSessionID: "gone"}, "another session"},
		{lead, ""},
	} {
		if got := StartedBy(c.s, others, names); got != c.want {
			t.Errorf("StartedBy(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
	if got := meta("plain claude", "Backend dev", "now"); got != "plain claude · started by Backend dev · now" {
		t.Errorf("meta = %q", got)
	}
}

// The browser's domain tests read the same fixture, so the architect's group
// reads the same on first paint and after a live update.
func TestGroupingByRoleMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/session-roles.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string `json:"name"`
			Sessions []struct {
				ID     string               `json:"id"`
				Role   domain.SessionRole   `json:"role"`
				Parent string               `json:"parent"`
				Status domain.SessionStatus `json:"status"`
				Ago    int                  `json:"ago"`
			} `json:"sessions"`
			Groups [][]json.RawMessage `json:"groups"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	for _, c := range fixture.Cases {
		var list []*domain.Session
		for _, s := range c.Sessions {
			list = append(list, &domain.Session{ID: s.ID, Role: s.Role, ParentSessionID: s.Parent, Status: s.Status, UpdatedAt: now.Add(-time.Duration(s.Ago) * time.Minute)})
		}
		var want []string
		for _, g := range c.Groups {
			var key string
			var members [][]any
			if err := json.Unmarshal(g[0], &key); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(g[1], &members); err != nil {
				t.Fatal(err)
			}
			for _, m := range members {
				want = append(want, fmt.Sprintf("%s/%s@%v", key, m[0], m[1]))
			}
		}
		var got []string
		for _, g := range group(list) {
			for _, s := range g.sessions {
				got = append(got, fmt.Sprintf("%s/%s@%d", g.key, s.ID, g.depth[s.ID]))
			}
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s:\n got  %v\n want %v", c.Name, got, want)
		}
	}
}

func TestArchitectRowIsBadgedAndDelegatesIndented(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	list := []*domain.Session{
		{ID: "arch", Task: "Shape the work", Role: domain.RoleArchitect, Mode: domain.SessionModeArchitect, Status: domain.SessionRunning, UpdatedAt: now},
		{ID: "d1", Task: "Server", Role: domain.RoleDelegate, ParentSessionID: "arch", Status: domain.SessionRunning, UpdatedAt: now},
		{ID: "d2", Task: "Tests", Role: domain.RoleDelegate, ParentSessionID: "d1", Status: domain.SessionRunning, UpdatedAt: now},
		{ID: "d3", Task: "More", Role: domain.RoleDelegate, ParentSessionID: "d2", Status: domain.SessionRunning, UpdatedAt: now},
		{ID: "d4", Task: "Deeper", Role: domain.RoleDelegate, ParentSessionID: "d3", Status: domain.SessionRunning, UpdatedAt: now},
	}
	v := NewPageView(shell.Frame{}, &domain.Project{ID: "p1"}, &domain.Ticket{ID: "t1"}, list, nil, nil, now)
	if v.Groups[0].Label != "Architect" || v.Groups[0].Tone != "" {
		t.Fatalf("first group = %+v, want the Architect group, untoned", v.Groups[0])
	}
	var got []string
	for _, r := range v.Groups[0].Rows {
		got = append(got, fmt.Sprintf("%s:%s:%d:%s", r.ID, r.Role, r.Depth, r.Badge))
	}
	want := "arch:architect:0:architect d1:delegate:1: d2:delegate:2: d3:delegate:3: d4:delegate:3:"
	if strings.Join(got, " ") != want {
		t.Errorf("rows = %v, want %s", got, want)
	}
}

func TestStatusCheckViewMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/status-check.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			State                 domain.StatusCheckState `json:"state"`
			Every                 int                     `json:"every"`
			NextIn                *int                    `json:"next_in"`
			LastAgo               *int                    `json:"last_ago"`
			Fired                 int                     `json:"fired"`
			Status, Short, Detail string
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	at := func(m *int, sign int) *time.Time {
		if m == nil {
			return nil
		}
		t := now.Add(time.Duration(sign**m) * time.Minute)
		return &t
	}
	for _, c := range fixture.Cases {
		check := &domain.StatusCheck{State: c.State, EveryMinutes: c.Every, NextAt: at(c.NextIn, 1), LastFiredAt: at(c.LastAgo, -1), FiredCount: c.Fired}
		got := StatusCheckOf(check, now)
		if got != (CheckView{c.Status, c.Short, c.Detail}) {
			t.Errorf("%s every %d: got %+v, want {%s %s %s}", c.State, c.Every, got, c.Status, c.Short, c.Detail)
		}
	}
}

func TestDelegateRowSaysItsNextCheckInsteadOfWhoStartedIt(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	next := now.Add(6 * time.Minute)
	list := []*domain.Session{
		{ID: "arch", AgentID: "go", Role: domain.RoleArchitect, Mode: domain.SessionModeArchitect, Status: domain.SessionRunning, UpdatedAt: now.Add(-9 * time.Minute)},
		{ID: "d1", AgentID: "go", Role: domain.RoleDelegate, ParentSessionID: "arch", Status: domain.SessionRunning, UpdatedAt: now.Add(-8 * time.Minute),
			StatusCheck: &domain.StatusCheck{State: domain.StatusCheckActive, EveryMinutes: 10, NextAt: &next}},
		{ID: "d2", AgentID: "go", Role: domain.RoleDelegate, ParentSessionID: "d1", Status: domain.SessionRunning, UpdatedAt: now.Add(-7 * time.Minute)},
	}
	v := NewPageView(shell.Frame{}, &domain.Project{ID: "p1"}, &domain.Ticket{ID: "t1"}, list, []*domain.Agent{{ID: "go", Name: "go-developer"}}, nil, now)
	got := map[string]string{}
	for _, r := range v.Groups[0].Rows {
		got[r.ID] = r.Meta
	}
	want := map[string]string{"arch": "go-developer as architect · 9m", "d1": "go-developer · check in 6m", "d2": "go-developer · 7m"}
	for id, m := range want {
		if got[id] != m {
			t.Errorf("%s meta = %q, want %q", id, got[id], m)
		}
	}
}
