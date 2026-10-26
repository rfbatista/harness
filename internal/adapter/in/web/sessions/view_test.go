package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
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
		{ID: "old-run", TicketID: "t1", Task: "a", Status: domain.SessionRunning, UpdatedAt: at(30)},
		{ID: "done", TicketID: "t1", Task: "b", Status: domain.SessionDone, UpdatedAt: at(1)},
		{ID: "new-run", TicketID: "t1", Task: "", AgentID: "backend", Status: domain.SessionThinking, UpdatedAt: at(5)},
		{ID: "turn", TicketID: "t1", Task: "d", Status: domain.SessionIdle, UpdatedAt: at(90)},
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
	if newRun.Agent != "Backend dev" || newRun.When != "5m" || newRun.Detail != nil {
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
	if string(b) != `{"project_id":"p1","ticket_id":"t1","sessions":[],"agent_names":{},"repository_names":{},"artifacts":[],"tasks":[]}` {
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

func TestRunsOn(t *testing.T) {
	names := map[string]string{"a1": "Reviewer"}
	for _, c := range []struct {
		s    *domain.Session
		want string
	}{
		{&domain.Session{AgentID: "a1"}, "Reviewer"},
		{&domain.Session{Mode: domain.SessionModeArchitect, Model: "opus"}, "plain claude as architect on opus"},
		{&domain.Session{AgentID: "a1", Model: "sonnet"}, "Reviewer on sonnet"},
	} {
		if got := RunsOn(c.s, names); got != c.want {
			t.Errorf("RunsOn(%+v) = %q, want %q", c.s, got, c.want)
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

func TestDelegateRowSaysItsNextCheckBeforeTheMessagesLoad(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	next := now.Add(6 * time.Minute)
	list := []*domain.Session{
		{ID: "arch", AgentID: "go", Role: domain.RoleArchitect, Mode: domain.SessionModeArchitect, Status: domain.SessionRunning, UpdatedAt: now.Add(-9 * time.Minute)},
		{ID: "d1", AgentID: "go", Role: domain.RoleDelegate, ParentSessionID: "arch", Status: domain.SessionRunning, UpdatedAt: now.Add(-8 * time.Minute),
			StatusCheck: &domain.StatusCheck{State: domain.StatusCheckActive, EveryMinutes: 10, NextAt: &next}},
		{ID: "d2", AgentID: "go", Role: domain.RoleDelegate, ParentSessionID: "d1", Status: domain.SessionRunning, UpdatedAt: now.Add(-7 * time.Minute)},
	}
	v := NewPageView(shell.Frame{}, &domain.Project{ID: "p1"}, &domain.Ticket{ID: "t1"}, list, []*domain.Agent{{ID: "go", Name: "go-developer"}}, nil, now)
	got := map[string]*Detail{}
	for _, r := range v.Groups[0].Rows {
		got[r.ID] = r.Detail
	}
	want := map[string]*Detail{"arch": nil, "d1": {Kind: "none", Check: "check in 6m"}, "d2": nil}
	for id, d := range want {
		if !reflect.DeepEqual(got[id], d) {
			t.Errorf("%s detail = %+v, want %+v", id, got[id], d)
		}
	}
}

// The browser's view test reads the same fixture (view.test.js). The server
// never has a task's messages, so the cases where they are loaded are the
// browser's alone.
func TestRowMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/session-row.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		AgentNames map[string]string `json:"agent_names"`
		Cases      []struct {
			Name           string `json:"name"`
			TaskTitle      string `json:"task_title"`
			MessagesLoaded bool   `json:"messages_loaded"`
			Row            string `json:"row"`
			Sessions       []struct {
				ID               string               `json:"id"`
				AgentID          string               `json:"agent_id"`
				Role             domain.SessionRole   `json:"role"`
				Parent           string               `json:"parent"`
				Mode             domain.SessionMode   `json:"mode"`
				Model            string               `json:"model"`
				Status           domain.SessionStatus `json:"status"`
				PendingApprovals int                  `json:"pending_approvals"`
				Ago              int                  `json:"ago"`
				TicketID         *string              `json:"ticket_id"`
				Task             string               `json:"task"`
				Sandboxed        *bool                `json:"sandboxed"`
				Check            *struct {
					State  domain.StatusCheckState `json:"state"`
					Every  int                     `json:"every"`
					NextIn int                     `json:"next_in"`
				} `json:"check"`
			} `json:"sessions"`
			Expect struct {
				Agent          string `json:"agent"`
				LeadsWithTitle bool   `json:"leadsWithTitle"`
				Mode           string `json:"mode"`
				Model          string `json:"model"`
				Badge          string `json:"badge"`
				Chip           *struct {
					Word string  `json:"word"`
					Tone *string `json:"tone"`
				} `json:"chip"`
				Word            string  `json:"word"`
				State           string  `json:"state"`
				When            string  `json:"when"`
				Detail          *Detail `json:"detail"`
				DetailText      string  `json:"detailText"`
				ResumeLabel     string  `json:"resumeLabel"`
				UnsandboxedAttr *string `json:"unsandboxedAttr"`
			} `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("empty fixture")
	}
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	for _, c := range fixture.Cases {
		if c.MessagesLoaded {
			continue
		}
		others := map[string]*domain.Session{}
		for _, f := range c.Sessions {
			s := &domain.Session{ID: f.ID, AgentID: f.AgentID, Role: f.Role, ParentSessionID: f.Parent, Mode: f.Mode, Model: f.Model,
				Status: f.Status, PendingApprovals: f.PendingApprovals, Task: f.Task, TicketID: "t1", UpdatedAt: now.Add(-time.Duration(f.Ago) * time.Minute)}
			if f.TicketID != nil {
				s.TicketID = *f.TicketID
			}
			if f.Sandboxed != nil {
				s.Sandbox = &domain.SandboxFacts{Sandboxed: *f.Sandboxed, Visibility: domain.SandboxUnrestricted}
			}
			if f.Check != nil {
				next := now.Add(time.Duration(f.Check.NextIn) * time.Minute)
				s.StatusCheck = &domain.StatusCheck{State: f.Check.State, EveryMinutes: f.Check.Every, NextAt: &next}
			}
			others[f.ID] = s
		}
		got := toRow(others[c.Row], "", fixture.AgentNames, now, others, 0, c.TaskTitle)
		want := c.Expect
		var wantChip *Chip
		if want.Chip != nil {
			wantChip = &Chip{Word: want.Chip.Word}
			if want.Chip.Tone != nil {
				wantChip.Tone = *want.Chip.Tone
			}
		}
		if got.Agent != want.Agent || got.LeadsWithTitle != want.LeadsWithTitle || got.Mode != want.Mode || got.Model != want.Model ||
			got.Badge != want.Badge || got.Word != want.Word || got.State != want.State || got.When != want.When ||
			got.DetailText != want.DetailText || got.ResumeLabel != want.ResumeLabel || got.Unsandboxed != (want.UnsandboxedAttr != nil) {
			t.Errorf("%s: got %+v, want %+v", c.Name, got, want)
		}
		if !reflect.DeepEqual(got.Chip, wantChip) || !reflect.DeepEqual(got.Detail, want.Detail) {
			t.Errorf("%s: chip %+v detail %+v, want chip %+v detail %+v", c.Name, got.Chip, got.Detail, wantChip, want.Detail)
		}
	}
}

func TestNewSessionFormCarriesEachAgentsDescription(t *testing.T) {
	agents := []*domain.Agent{
		{ID: "ux", Name: "ux-designer", Description: "Variants, behaviour, wireframes"},
		{ID: "dev", Name: "developer"},
	}
	view := NewPageView(shell.Frame{}, &domain.Project{ID: "p"}, &domain.Ticket{ID: "t"}, nil, agents, nil, time.Now())
	want := []Option{
		{ID: "ux", Name: "ux-designer", Description: "Variants, behaviour, wireframes"},
		{ID: "dev", Name: "developer"},
	}
	if fmt.Sprint(view.NewSession.Agents) != fmt.Sprint(want) {
		t.Errorf("Agents = %+v, want %+v", view.NewSession.Agents, want)
	}
}

func TestRowTitleMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/session-title.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			T      string `json:"task"`
			Ticket string `json:"ticket_id"`
			Want   string `json:"title"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("empty fixture")
	}
	for _, c := range fixture.Cases {
		if got := RowTitle(&domain.Session{Task: c.T, TicketID: c.Ticket}); got != c.Want {
			t.Errorf("RowTitle(task=%q, ticket=%q) = %q, want %q", c.T, c.Ticket, got, c.Want)
		}
	}
}

func TestProjectPageViewHasNoTaskAndLocksTheFormToProduct(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	list := []*domain.Session{{ID: "b1", Task: "", Mode: "product", Status: domain.SessionIdle, UpdatedAt: now}}
	v := NewPageView(shell.Frame{}, &domain.Project{ID: "p1", Name: "coding_pool"}, nil, list, nil, []*domain.Repository{{ID: "r1", Name: "harness"}}, now)
	if v.HasTask() || v.Task != nil || v.TaskTitle != BrainstormsTitle || v.LockedMode() != "product" {
		t.Errorf("project page view = %+v", v)
	}
	if v.ListLabel() != "Feature brainstorms of this project" {
		t.Errorf("list label = %q", v.ListLabel())
	}
	b, err := json.Marshal(v.Seed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ticket_id":""`) {
		t.Errorf("the seed must carry an empty ticket id, so the browser keeps ticket-less sessions only: %s", b)
	}
	if v.Groups[0].Rows[0].Agent != "Feature brainstorm" || !v.Groups[0].Rows[0].LeadsWithTitle {
		t.Errorf("row = %+v", v.Groups[0].Rows[0])
	}
	task := NewPageView(shell.Frame{}, &domain.Project{ID: "p1"}, &domain.Ticket{ID: "t1", Title: "Add SSE feed"}, nil, nil, nil, now)
	if !task.HasTask() || task.LockedMode() != "" || task.ListLabel() != "Sessions of this task" {
		t.Errorf("task page view = %+v", task)
	}
}

// Only a session that started with the sandbox off is marked in the list; one
// from before the sandbox existed is not.
func TestOnlyAnUnsandboxedSessionsRowSaysSo(t *testing.T) {
	now := time.Now()
	row := func(f *domain.SandboxFacts) Row {
		s := &domain.Session{ID: "s", UpdatedAt: now, Sandbox: f}
		return toRow(s, "", nil, now, map[string]*domain.Session{"s": s}, 0, "")
	}
	for name, tc := range map[string]struct {
		facts *domain.SandboxFacts
		want  bool
	}{
		"before the sandbox": {nil, false},
		"sandboxed":          {&domain.SandboxFacts{Sandboxed: true, Visibility: domain.SandboxOwnRepo}, false},
		"sandbox off":        {&domain.SandboxFacts{Visibility: domain.SandboxUnrestricted}, true},
	} {
		r := row(tc.facts)
		if r.Unsandboxed != tc.want || strings.Contains(r.DetailText, " · unsandboxed") != tc.want {
			t.Errorf("%s: unsandboxed=%v meta=%q, want %v", name, r.Unsandboxed, r.DetailText, tc.want)
		}
	}
}

// The server's first paint renders the contract's session row: the agent
// leads, the facts are tags, the detail sits under it, and no row repeats
// the task's title.
func TestFirstPaintRendersTheSessionRowMarkup(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	next := now.Add(6 * time.Minute)
	list := []*domain.Session{
		{ID: "arch", AgentID: "go", Role: domain.RoleArchitect, Mode: domain.SessionModeArchitect, Model: "opus", Status: domain.SessionRunning, Task: "AI Helper", TicketID: "t1", UpdatedAt: now.Add(-9 * time.Minute)},
		{ID: "d1", AgentID: "go", Role: domain.RoleDelegate, ParentSessionID: "arch", Mode: domain.SessionModeImplement, Model: "sonnet", Status: domain.SessionRunning, Task: "AI Helper", TicketID: "t1", UpdatedAt: now.Add(-8 * time.Minute),
			StatusCheck: &domain.StatusCheck{State: domain.StatusCheckActive, EveryMinutes: 10, NextAt: &next}},
		{ID: "peer", AgentID: "go", Status: domain.SessionFailed, Task: "fix the flake", TicketID: "t1", UpdatedAt: now.Add(-20 * time.Minute)},
	}
	v := NewPageView(shell.Frame{}, &domain.Project{ID: "p1"}, &domain.Ticket{ID: "t1", Title: "AI Helper"}, list, []*domain.Agent{{ID: "go", Name: "go-developer"}}, nil, now)
	var buf strings.Builder
	if err := ssrRows(v).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	// The classes read "[ agent ]" in the source (the design system's
	// bracket convention), and templ leaves a space between sibling elements.
	html := regexp.MustCompile(`>\s+<`).ReplaceAllString(strings.NewReplacer(`"[ `, `"`, ` ]"`, `"`).Replace(buf.String()), "><")
	for _, want := range []string{
		`data-layout="session"`,
		`<span class="agent">go-developer</span>`,
		`<span class="tag" data-kind="mode">implement</span>`,
		`<span class="tag" data-kind="model">sonnet</span>`,
		`<span class="badge" data-tone="danger" aria-hidden="true">failed</span>`,
		`<span class="detail" data-report="none"><span class="check">check in 6m</span></span>`,
		`<span class="detail" data-report="prompt"><span class="lead">prompt</span><span class="quote">“fix the flake”</span></span>`,
		`data-dot-only`,
		`<time class="when">9m</time>`,
		`title="go-developer · implement · sonnet · check in 6m · running · 8m"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("first paint lacks %s\n%s", want, html)
		}
	}
	for _, unwanted := range []string{`class="title"`, `class="meta"`, `data-stacked`, `AI Helper`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("first paint has %s", unwanted)
		}
	}
}

// A session whose commit check is off shows it on its row, with the reason,
// and shows nothing once the check is repaired (ADR 0040).
func TestFirstPaintShowsCommitCheckOffWithItsReasonOnlyWhileItIsOff(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	render := func(off string) string {
		list := []*domain.Session{{ID: "s1", AgentID: "go", Status: domain.SessionRunning, Task: "t", TicketID: "t1", UpdatedAt: now, CommitCheckOff: off}}
		v := NewPageView(shell.Frame{}, &domain.Project{ID: "p1"}, &domain.Ticket{ID: "t1"}, list, []*domain.Agent{{ID: "go", Name: "go-developer"}}, nil, now)
		var buf strings.Builder
		if err := ssrRows(v).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	html := render("the hooks folder /x/githooks/s1 is missing")
	for _, want := range []string{`data-commit-check-off`, `title="the hooks folder /x/githooks/s1 is missing"`, `Commit check off`} {
		if !strings.Contains(html, want) {
			t.Errorf("a row with the commit check off lacks %s\n%s", want, html)
		}
	}
	if html := render(""); strings.Contains(html, "Commit check off") {
		t.Errorf("a guarded session's row says the commit check is off\n%s", html)
	}
}
