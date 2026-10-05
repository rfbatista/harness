package projects

import (
	"testing"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

func TestRepositoriesViewReadsLikeTheBrowser(t *testing.T) {
	v := NewRepositoriesView(shell.Frame{}, &domain.Project{ID: "p1", Name: "coding_pool"}, []*domain.Repository{
		{ID: "r1", Name: "harness", URL: "git@github.com:me/harness.git", RootDir: "/src/harness"},
		{ID: "r2", URL: "file:///src/kit", RootDir: "/src/kit/"},
		{ID: "r3", URL: "https://example.com/x.git"},
	})
	got := []RepositoryRow{}
	got = append(got, v.Rows...)
	want := []RepositoryRow{
		{"r1", "harness", "/src/harness", "git@github.com:me/harness.git", "/projects/p1/repositories/r1/env", "/projects/p1/repositories/r1/history"},
		{"r2", "kit", "/src/kit/", "local only", "/projects/p1/repositories/r2/env", "/projects/p1/repositories/r2/history"},
		{"r3", "https://example.com/x.git", "no local path: sessions cannot run here", "https://example.com/x.git", "/projects/p1/repositories/r3/env", "/projects/p1/repositories/r3/history"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if v.Seed.ProjectID != "p1" || len(v.Seed.Repositories) != 3 {
		t.Errorf("seed = %+v", v.Seed)
	}
	if empty := NewRepositoriesView(shell.Frame{}, &domain.Project{ID: "p1"}, nil); empty.Seed.Repositories == nil {
		t.Error("the seed must list [], never null")
	}
}
