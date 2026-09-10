package catalog

import (
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/domain"
)

func projectsFixture() (*backend.Fake, Model) {
	f := backend.NewFake()
	f.Projects = []*domain.Project{{ID: "p1", Name: "coding_pool", RootDir: "/src/cp", IgnoredPaths: []string{"node_modules"}}}
	f.Repositories = []*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "main-repo", RootDir: "/src/cp"}}
	f.BoundedContexts = []*domain.BoundedContext{{ID: "bc1", ProjectID: "p1", Name: "Billing", Purpose: "money", UbiquitousLanguage: []domain.LanguageTerm{{Term: "Invoice", Definition: "a bill"}}}}
	m := withSnap(New(ctx(), Projects, f), f)
	return f, m
}

func TestProjectsNewCreatesWithRoot(t *testing.T) {
	f, m := projectsFixture()
	m, _ = press(m, ch('n'))
	m = typeKeys(m, "demo")
	m, _ = press(m, tab())
	m = typeKeys(m, "/src/demo")
	_, root := press(m, enter())
	if len(f.Projects) != 2 || f.Projects[1].RootDir != "/src/demo" || !hasRefresh(root) {
		t.Fatalf("project not created: %+v", f.Projects)
	}
}

func TestProjectsEnterOpensDetailWithTabs(t *testing.T) {
	_, m := projectsFixture()
	m, _ = press(m, enter())
	v := m.View()
	for _, want := range []string{"Repositories", "Bounded contexts", "Ignored paths", "main-repo"} {
		if !strings.Contains(v, want) {
			t.Fatalf("detail missing %q:\n%s", want, v)
		}
	}
	m, _ = press(m, ch('l'))
	if !strings.Contains(m.View(), "Billing") || !strings.Contains(m.View(), "1 term") {
		t.Fatalf("contexts tab:\n%s", m.View())
	}
	m, _ = press(m, ch('l'))
	if !strings.Contains(m.View(), "node_modules") {
		t.Fatalf("ignored tab:\n%s", m.View())
	}
	m, _ = press(m, esc())
	if !strings.Contains(m.View(), "Project") || strings.Contains(m.View(), "Repositories") {
		t.Fatalf("esc should return to the list:\n%s", m.View())
	}
}

func TestProjectsDetailAddsRepositoryScopedToProject(t *testing.T) {
	f, m := projectsFixture()
	m, _ = press(m, enter())
	m, _ = press(m, ch('n'))
	m = typeKeys(m, "docs-repo")
	m, _ = press(m, tab()) // description
	m, _ = press(m, tab()) // url
	m, _ = press(m, tab()) // root
	m = typeKeys(m, "/src/docs")
	m, root := press(m, enter())
	if len(f.Repositories) != 2 || f.Repositories[1].ProjectID != "p1" || f.Repositories[1].RootDir != "/src/docs" {
		t.Fatalf("repo not created under p1: %+v", f.Repositories)
	}
	if !hasRefresh(root) {
		t.Fatal("refresh expected")
	}
	m = withSnap(m, f)
	if !strings.Contains(m.View(), "docs-repo") {
		t.Fatalf("detail should list the new repo after refresh:\n%s", m.View())
	}
}

func TestProjectsDetailDeletesBoundedContext(t *testing.T) {
	f, m := projectsFixture()
	m, _ = press(m, enter())
	m, _ = press(m, ch('l'))
	m, _ = press(m, ch('D'))
	if !strings.Contains(m.View(), "Billing") {
		t.Fatalf("confirm should name the context:\n%s", m.View())
	}
	_, _ = press(m, ch('y'))
	if len(f.BoundedContexts) != 0 {
		t.Fatalf("context should be deleted: %+v", f.BoundedContexts)
	}
}

func TestProjectsDetailEditsBoundedContextTermsAsLines(t *testing.T) {
	f, m := projectsFixture()
	m, _ = press(m, enter())
	m, _ = press(m, ch('l'))
	m, _ = press(m, ch('e'))
	v := m.View()
	if !strings.Contains(v, "Billing") || !strings.Contains(v, "1 lines") {
		t.Fatalf("edit form should prefill name and show the terms summary:\n%s", v)
	}
	// Simulate the editor returning two "term: definition" lines.
	m, _ = m.Update(editorDone("terms", "Invoice: a bill\nLedger: the book"))
	_, _ = press(m, enter())
	if got := f.BoundedContexts[0].UbiquitousLanguage; len(got) != 2 || got[1].Term != "Ledger" || got[1].Definition != "the book" {
		t.Fatalf("terms not parsed: %+v", got)
	}
}

func TestProjectsDetailIgnoredPathAddAndRemove(t *testing.T) {
	f, m := projectsFixture()
	m, _ = press(m, enter())
	m, _ = press(m, ch('l'))
	m, _ = press(m, ch('l'))
	m, _ = press(m, ch('n'))
	m = typeKeys(m, "dist")
	m, _ = press(m, enter())
	if len(f.Projects[0].IgnoredPaths) != 2 {
		t.Fatalf("ignored path not added: %+v", f.Projects[0].IgnoredPaths)
	}
	m = withSnap(m, f)
	m, _ = press(m, ch('D'))
	_, _ = press(m, ch('y'))
	if len(f.Projects[0].IgnoredPaths) != 1 {
		t.Fatalf("ignored path not removed: %+v", f.Projects[0].IgnoredPaths)
	}
}

func TestProjectsWorkspacesRootEdit(t *testing.T) {
	f, m := projectsFixture()
	m, _ = press(m, ch('w'))
	if !strings.Contains(m.View(), "Workspaces root") {
		t.Fatalf("settings form:\n%s", m.View())
	}
	m = typeKeys(m, "/tmp/wt")
	_, root := press(m, enter())
	if f.Settings[domain.SettingWorkspacesRoot] != "/tmp/wt" || !hasRefresh(root) {
		t.Fatalf("setting not saved: %v", f.Settings)
	}
}

func TestProjectsDeleteCascadesViaBackend(t *testing.T) {
	f, m := projectsFixture()
	m, _ = press(m, ch('D'))
	_, _ = press(m, ch('y'))
	if len(f.Projects) != 0 {
		t.Fatalf("project should be deleted: %+v", f.Projects)
	}
}
