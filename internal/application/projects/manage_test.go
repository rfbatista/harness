package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeTasks map[string]ports.TaskActivity

func (f fakeTasks) TaskActivityByProject(context.Context) (map[string]ports.TaskActivity, error) {
	return f, nil
}

type fakeSessions struct {
	activity map[string]ports.SessionActivity
	live     map[string][]domain.ProjectSessionRef
}

func (f *fakeSessions) SessionActivityByProject(context.Context) (map[string]ports.SessionActivity, error) {
	return f.activity, nil
}

func (f *fakeSessions) LiveProjectSessions(_ context.Context, projectID string) ([]domain.ProjectSessionRef, error) {
	return f.live[projectID], nil
}

type recordingBus struct{ published []domain.Event }

func (b *recordingBus) Publish(_ context.Context, events ...domain.Event) error {
	b.published = append(b.published, events...)
	return nil
}

type manageFixture struct {
	svc      *Service
	store    *sqlite.ProjectRepository
	repos    *sqlite.RepositoryRepository
	sessions *fakeSessions
	tasks    fakeTasks
	bus      *recordingBus
}

func newManageFixture(t *testing.T) *manageFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	f := &manageFixture{
		store:    sqlite.NewProjectRepository(db),
		repos:    sqlite.NewRepositoryRepository(db),
		sessions: &fakeSessions{activity: map[string]ports.SessionActivity{}, live: map[string][]domain.ProjectSessionRef{}},
		tasks:    fakeTasks{},
		bus:      &recordingBus{},
	}
	f.svc = NewService(f.store, f.repos, f.bus)
	f.svc.UseActivity(f.tasks, f.sessions)
	return f
}

func wantErrCode(t *testing.T, err error, code string) {
	t.Helper()
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != code {
		t.Fatalf("err = %v, want %s", err, code)
	}
}

func TestCreateProject_TrimsNameAndCleansRoot(t *testing.T) {
	f := newManageFixture(t)
	dir := t.TempDir()
	p, err := f.svc.CreateProject(context.Background(), "  Harness  ", dir+"/./")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Harness" || p.RootDir != dir {
		t.Fatalf("project = %q at %q, want Harness at %q", p.Name, p.RootDir, dir)
	}
}

func TestCreateProject_ExpandsHome(t *testing.T) {
	f := newManageFixture(t)
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "code"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	p, err := f.svc.CreateProject(context.Background(), "p", "~/code")
	if err != nil {
		t.Fatal(err)
	}
	if p.RootDir != filepath.Join(home, "code") {
		t.Fatalf("root = %q, want %q", p.RootDir, filepath.Join(home, "code"))
	}
}

func TestCreateProject_Refusals(t *testing.T) {
	f := newManageFixture(t)
	ctx := context.Background()
	if _, err := f.svc.CreateProject(ctx, "Harness", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, root, code string }{
		{"   ", t.TempDir(), "INVALID_INPUT"},
		{" harness ", t.TempDir(), "PROJECT_NAME_TAKEN"},
		{"new", "relative/dir", "PROJECT_ROOT_INVALID"},
		{"new", "", "PROJECT_ROOT_INVALID"},
		{"new", filepath.Join(t.TempDir(), "missing"), "PROJECT_ROOT_INVALID"},
		{"new", file, "PROJECT_ROOT_INVALID"},
	} {
		_, err := f.svc.CreateProject(ctx, c.name, c.root)
		wantErrCode(t, err, c.code)
	}
	if list, _ := f.svc.ListProjects(ctx); len(list) != 1 {
		t.Fatalf("%d projects after refusals, want 1", len(list))
	}
}

func TestUpdateProject_Rules(t *testing.T) {
	f := newManageFixture(t)
	ctx := context.Background()
	a, _ := f.svc.CreateProject(ctx, "Alpha", t.TempDir())
	if _, err := f.svc.CreateProject(ctx, "Beta", t.TempDir()); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.UpdateProject(ctx, a.ID, "BETA", "")
	wantErrCode(t, err, "PROJECT_NAME_TAKEN")
	_, err = f.svc.UpdateProject(ctx, a.ID, "", "not/absolute")
	wantErrCode(t, err, "PROJECT_ROOT_INVALID")
	_, err = f.svc.UpdateProject(ctx, "missing", "x", "")
	wantErrCode(t, err, "PROJECT_NOT_FOUND")
	_, err = f.svc.UpdateProject(ctx, "", "x", "")
	wantErrCode(t, err, "INVALID_INPUT")

	got, err := f.svc.UpdateProject(ctx, a.ID, " ALPHA ", "")
	if err != nil || got.Name != "ALPHA" || got.RootDir != a.RootDir {
		t.Fatalf("recase own name = %+v, %v", got, err)
	}
	dir := t.TempDir()
	got, err = f.svc.UpdateProject(ctx, a.ID, "", dir)
	if err != nil || got.Name != "ALPHA" || got.RootDir != dir {
		t.Fatalf("re-point = %+v, %v", got, err)
	}
}

// Projects stored before the rules — a colliding name, a root that is gone —
// still list, have their other field edited, and delete.
func TestUpdateProject_LegacyProjectsStayEditable(t *testing.T) {
	f := newManageFixture(t)
	ctx := context.Background()
	gone := filepath.Join(t.TempDir(), "gone")
	a, _ := f.store.Create("Same", gone)
	b, _ := f.store.Create("same", gone)

	if list, err := f.svc.ListProjectSummaries(ctx); err != nil || len(list) != 2 {
		t.Fatalf("summaries = %d, %v", len(list), err)
	}
	dir := t.TempDir()
	got, err := f.svc.UpdateProject(ctx, a.ID, "Same", dir)
	if err != nil || got.RootDir != dir {
		t.Fatalf("re-point legacy (both fields sent) = %+v, %v", got, err)
	}
	got, err = f.svc.UpdateProject(ctx, b.ID, "Renamed", gone)
	if err != nil || got.Name != "Renamed" || got.RootDir != gone {
		t.Fatalf("rename legacy with missing root (both fields sent) = %+v, %v", got, err)
	}
	if err := f.svc.DeleteProject(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteProject_RefusedWhileSessionsRun(t *testing.T) {
	f := newManageFixture(t)
	ctx := context.Background()
	p, _ := f.svc.CreateProject(ctx, "Busy", t.TempDir())
	live := []domain.ProjectSessionRef{{ID: "s1", TicketID: "t1", Agent: "go-developer"}, {ID: "s2"}}
	f.sessions.live[p.ID] = live

	err := f.svc.DeleteProject(ctx, p.ID)
	wantErrCode(t, err, "PROJECT_HAS_RUNNING_SESSIONS")
	var de *domain.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("err = %T, want *domain.DetailedError", err)
	}
	if want := map[string]any{"sessions": live}; !reflect.DeepEqual(de.Details, want) {
		t.Fatalf("details = %#v, want %#v", de.Details, want)
	}
	want := `PROJECT_HAS_RUNNING_SESSIONS: project "Busy" has 2 running sessions (s1 on task t1 by go-developer, s2); stop them first`
	if de.Message != want {
		t.Fatalf("message = %q, want %q", de.Message, want)
	}
	if _, err := f.svc.GetProject(ctx, p.ID); err != nil {
		t.Fatalf("project gone after a refused delete: %v", err)
	}
	if len(f.bus.published) != 0 {
		t.Fatalf("published %v on a refused delete", f.bus.published)
	}
}

func TestDeleteProject_WithOnlyFinishedSessionsCascades(t *testing.T) {
	f := newManageFixture(t)
	ctx := context.Background()
	p, _ := f.svc.CreateProject(ctx, "Done", t.TempDir())
	if _, err := f.svc.CreateRepository(ctx, p.ID, "api", "", "https://example.com/api", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	f.sessions.activity[p.ID] = ports.SessionActivity{LastActivityAt: time.Now()}

	if err := f.svc.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if repos, _ := f.svc.ListRepositories(ctx, p.ID); len(repos) != 0 {
		t.Fatalf("%d repositories left", len(repos))
	}
	if want := []domain.Event{domain.ProjectDeleted{ProjectID: p.ID}}; !reflect.DeepEqual(f.bus.published, want) {
		t.Fatalf("published %v, want %v", f.bus.published, want)
	}
	wantErrCode(t, f.svc.DeleteProject(ctx, ""), "INVALID_INPUT")
}

func TestListProjectSummaries(t *testing.T) {
	f := newManageFixture(t)
	ctx := context.Background()
	if list, err := f.svc.ListProjectSummaries(ctx); err != nil || list == nil || len(list) != 0 {
		t.Fatalf("empty = %#v, %v; want [] (not nil)", list, err)
	}
	b, _ := f.svc.CreateProject(ctx, "beta", t.TempDir())
	a, _ := f.svc.CreateProject(ctx, "Alpha", t.TempDir())
	c, _ := f.svc.CreateProject(ctx, "Charlie", t.TempDir())
	for range 2 {
		if _, err := f.svc.CreateRepository(ctx, b.ID, "r", "", "https://example.com/r", t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	older, newer := time.UnixMilli(1_700_000_000_000), time.UnixMilli(1_700_000_500_000)
	f.tasks[b.ID] = ports.TaskActivity{OpenCount: 3, LastUpdatedAt: older}
	f.sessions.activity[b.ID] = ports.SessionActivity{LiveCount: 1, LastActivityAt: newer}
	f.tasks[a.ID] = ports.TaskActivity{OpenCount: 0, LastUpdatedAt: newer}
	f.sessions.activity[a.ID] = ports.SessionActivity{LastActivityAt: older}

	list, err := f.svc.ListProjectSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Project.ID != a.ID || list[1].Project.ID != b.ID || list[2].Project.ID != c.ID {
		t.Fatalf("order = %v, want Alpha, beta, Charlie", names(list))
	}
	if s := list[1]; s.RepositoryCount != 2 || s.OpenTaskCount != 3 || s.RunningSessionCount != 1 || !s.LastActivityAt.Equal(newer) {
		t.Fatalf("beta = %+v", s)
	}
	if s := list[0]; s.RepositoryCount != 0 || s.OpenTaskCount != 0 || !s.LastActivityAt.Equal(newer) {
		t.Fatalf("Alpha = %+v", s)
	}
	if s := list[2]; s.LastActivityAt != nil || s.RepositoryCount != 0 {
		t.Fatalf("Charlie = %+v, want no activity", s)
	}
}

func names(list []ports.ProjectSummary) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Project.Name
	}
	return out
}

func TestFollowProjects_AnnouncesEachWriteInOrder(t *testing.T) {
	f := newManageFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := f.svc.FollowProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}

	p, _ := f.svc.CreateProject(ctx, "Feed", t.TempDir())
	if _, err := f.svc.CreateProject(ctx, "feed", t.TempDir()); err == nil {
		t.Fatal("duplicate name accepted")
	}
	f.svc.UpdateProject(ctx, p.ID, "Fed", "")
	f.svc.AddIgnoredPath(ctx, p.ID, "node_modules")
	f.svc.RemoveIgnoredPath(ctx, p.ID, "node_modules")
	if err := f.svc.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}

	type seen struct {
		name    string
		ignored int
		deleted bool
	}
	var got []seen
	for range 5 {
		select {
		case c := <-changes:
			if c.Project.ID != p.ID {
				t.Fatalf("change for %q, want %q", c.Project.ID, p.ID)
			}
			got = append(got, seen{c.Project.Name, len(c.Project.IgnoredPaths), c.Deleted})
		case <-time.After(time.Second):
			t.Fatalf("only %d changes: %v", len(got), got)
		}
	}
	want := []seen{{"Feed", 0, false}, {"Fed", 0, false}, {"Fed", 1, false}, {"Fed", 0, false}, {"", 0, true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	select {
	case c := <-changes:
		t.Fatalf("unexpected change %+v (a refused write was announced?)", c)
	default:
	}

	cancel()
	select {
	case _, ok := <-changes:
		if ok {
			t.Fatal("change after the follower left")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed when ctx ended")
	}
}

func TestFollowProjects_DropsAFollowerThatFellBehind(t *testing.T) {
	f := newManageFixture(t)
	ctx := context.Background()
	slow, _ := f.svc.FollowProjects(ctx)
	p, _ := f.svc.CreateProject(ctx, "Busy", t.TempDir())
	for i := range followerBuffer {
		f.svc.AddIgnoredPath(ctx, p.ID, filepath.Join("dir", string(rune('a'+i%26)), time.Duration(i).String()))
	}
	n := 0
	for range slow {
		n++
	}
	if n != followerBuffer {
		t.Fatalf("slow follower got %d changes before its channel closed, want %d", n, followerBuffer)
	}
}
