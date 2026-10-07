package projects

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The projects list reads the same on first paint as live:
// web/testdata/views/project-summary.json pins this and
// web/src/modules/projects/presentation/view.js together.
func TestSummaryRowsReadLikeTheBrowser(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/project-summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Repos      int    `json:"repository_count"`
			Open       int    `json:"open_task_count"`
			Running    int    `json:"running_session_count"`
			MinutesAgo *int   `json:"minutes_ago"`
			State      string `json:"state"`
			Word       string `json:"word"`
			Meta       string `json:"meta"`
		} `json:"cases"`
		Totals []struct {
			Projects int    `json:"projects"`
			Running  int    `json:"running"`
			Line     string `json:"line"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	for _, c := range fixture.Cases {
		s := ports.ProjectSummary{Project: &domain.Project{ID: "p1", Name: "x"}, RepositoryCount: c.Repos, OpenTaskCount: c.Open, RunningSessionCount: c.Running}
		if c.MinutesAgo != nil {
			at := now.Add(-time.Duration(*c.MinutesAgo) * time.Minute)
			s.LastActivityAt = &at
		}
		row := NewListView(shell.Frame{}, []ports.ProjectSummary{s}, now).Rows[0]
		if row.State != c.State || row.Word != c.Word || row.Meta != c.Meta {
			t.Errorf("%+v reads %q %q %q, want %q %q %q", c, row.State, row.Word, row.Meta, c.State, c.Word, c.Meta)
		}
	}
	for _, c := range fixture.Totals {
		if got := totalLine(c.Projects, c.Running); got != c.Line {
			t.Errorf("totalLine(%d, %d) = %q, want %q", c.Projects, c.Running, got, c.Line)
		}
	}
}

func TestListViewLinksEachProjectAndSeedsTheAPIShape(t *testing.T) {
	at := time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)
	v := NewListView(shell.Frame{}, []ports.ProjectSummary{
		{Project: &domain.Project{ID: "p 1", Name: "harness", RootDir: "/src/harness"}, RepositoryCount: 1, LastActivityAt: &at},
	}, at)
	if r := v.Rows[0]; r.Href != "/projects/p%201" || r.SettingsHref != "/projects/p%201/settings" || r.RootDir != "/src/harness" {
		t.Errorf("row = %+v", r)
	}
	seed, _ := json.Marshal(v.Seed)
	want := `{"summaries":[{"project":{"id":"p 1","name":"harness","root_dir":"/src/harness"},"repository_count":1,"open_task_count":0,"running_session_count":0,"last_activity_at":"2026-10-07T11:00:00Z"}]}`
	if string(seed) != want {
		t.Errorf("seed =\n%s\nwant\n%s", seed, want)
	}
	empty, _ := json.Marshal(NewListView(shell.Frame{}, nil, at).Seed)
	if string(empty) != `{"summaries":[]}` {
		t.Errorf("no projects seeds %s, want an empty list", empty)
	}
}

func TestSettingsViewSeedsTheProjectAndLinksItsEnvFiles(t *testing.T) {
	p := &domain.Project{ID: "p1", Name: "harness", RootDir: "/src/harness", IgnoredPaths: []string{"dist"}, Repositories: []domain.Repository{{ID: "stale"}}}
	v := NewSettingsView(shell.Frame{}, p, []*domain.Repository{{ID: "r1", ProjectID: "p1", RootDir: "/src/harness/api"}})
	if v.RepositoriesHref != "/projects/p1/repositories" || len(v.Repositories) != 1 || v.Repositories[0] != (SettingsRepository{"api", "/projects/p1/repositories/r1/env"}) {
		t.Errorf("view = %+v", v)
	}
	seed, _ := json.Marshal(v.Seed)
	want := `{"project":{"id":"p1","name":"harness","root_dir":"/src/harness","ignored_paths":["dist"]},"repositories":[{"id":"r1","project_id":"p1","name":"","url":"","root_dir":"/src/harness/api"}]}`
	if string(seed) != want {
		t.Errorf("seed =\n%s\nwant\n%s", seed, want)
	}
	if none := NewSettingsView(shell.Frame{}, &domain.Project{ID: "p1"}, nil); none.Seed.Repositories == nil {
		t.Error("the seed must list [], never null")
	}
}
