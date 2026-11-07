package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/commandrunner"
	"operators-mcp/internal/adapter/out/containerrunner/containerrunnertest"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// What happens to services when the harness stops, starts again, or loses
// their project: nothing outlives the server, nothing starts by itself, and
// deleting a project leaves no service behind.

// fakeSweeper stands in for the runtime's labelled containers.
type fakeSweeper struct {
	mu        sync.Mutex
	found     []ports.ServiceContainerRef
	listErr   error
	asked     []string // runtimes asked, in order
	removed   []string
	removeErr error
}

func (s *fakeSweeper) ListServiceContainers(_ context.Context, runtime string) ([]ports.ServiceContainerRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, runtime)
	return s.found, s.listErr
}

func (s *fakeSweeper) RemoveServiceContainer(_ context.Context, _, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removed = append(s.removed, name)
	return s.removeErr
}

func (s *fakeSweeper) Removed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.removed)
}

func (s *fakeSweeper) Asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

func lookPathOf(found ...string) func(string) (string, error) {
	return func(c string) (string, error) {
		if slices.Contains(found, c) {
			return "/usr/bin/" + c, nil
		}
		return "", os.ErrNotExist
	}
}

func TestServicesLifecycle_ShutdownStopsEveryRunWithinTheGraceAndLeavesNoProcess(t *testing.T) {
	fake := &containerrunnertest.Fake{}
	f := newServicesFixture(t, services.Options{
		Runners: map[domain.ServiceKind]ports.ServiceProcessRunner{
			domain.ServiceKindCommand: commandrunner.Runner{}, domain.ServiceKindImage: fake,
		},
		LookPath:  lookPathOf("docker"),
		StopGrace: 300 * time.Millisecond,
	})
	polite := f.command("polite", "echo pid=$$; exec sleep 60")
	stubborn := f.command("stubborn", "trap '' TERM; echo pid=$$; while true; do sleep 0.05; done")
	db := f.image("db", map[string]any{"image": map[string]any{"ref": "postgres:16"}})
	for _, id := range []string{polite, stubborn, db} {
		if code, out := f.operate("start_service", id); code != 200 {
			t.Fatalf("start = %d %v", code, out)
		}
		f.awaitStatus(id, "running")
	}
	pids := []int{pidIn(t, f.awaitLog(polite, "\npid="), "pid"), pidIn(t, f.awaitLog(stubborn, "\npid="), "pid")}
	events := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)

	began := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.svc.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("the runs were stopped in parallel within the escalation: took %v", took)
	}
	for _, pid := range pids {
		if alive(pid) {
			t.Fatalf("process %d outlived the shutdown", pid)
		}
	}
	for _, id := range []string{polite, stubborn, db} {
		if s := f.status(id); s != "stopped" {
			t.Fatalf("%s is %s after shutdown", id, s)
		}
	}
	if stops := fake.Runs()[0].Stops(); len(stops) != 1 {
		t.Fatalf("the container was stopped through the runtime: %v", stops)
	}
	last := map[string]any{}
	for range 3 {
		e := nextEvent(t, events)
		last = e
		if e["kind"] != "service_changed" {
			t.Fatalf("event = %v", e)
		}
	}
	if last["running_count"] != float64(0) {
		t.Fatalf("the feed ends with nothing running: %v", last)
	}
	if code, out := f.operate("start_service", polite); code == 200 {
		t.Fatalf("a stopped harness starts nothing: %v", out)
	}
}

// restarted serves a second module over the first one's store and logs, as a
// restarted server does.
func restarted(t *testing.T, f *servicesFixture, opts services.Options) *servicesFixture {
	t.Helper()
	opts.LogDir = f.logDir
	if opts.Runners == nil {
		opts.Runners = map[domain.ServiceKind]ports.ServiceProcessRunner{domain.ServiceKindCommand: commandrunner.Runner{}}
	}
	svc := services.New(f.store, f.cat.Projects, f.cat.Projects, opts)
	s := servicesOf(f.cat)
	s.Services = svc
	srv := httptest.NewServer(NewRouter(NewHandler(s)))
	t.Cleanup(srv.Close)
	g := *f
	g.svc, g.srv = svc, srv
	return &g
}

// leaveRunning writes what a server that died mid-run leaves in the store.
func leaveRunning(t *testing.T, f *servicesFixture, id string, status domain.ServiceStatus) {
	t.Helper()
	svc, err := f.store.GetService(id)
	if err != nil {
		t.Fatal(err)
	}
	svc.Status = status
	svc.Run = &domain.ServiceRun{ID: "run-old", StartedAt: "2026-11-07T10:00:00Z", Where: domain.ServiceWhere{Kind: "main", Dir: "/x"}}
	if err := f.store.SaveService(svc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.svc.LogPath(id), []byte("[harness] command: serve\nlistening\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestServicesLifecycle_BootStopsWhatWasLeftRunningAndStartsNothing(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	running := f.command("web", "exec sleep 60")
	starting := f.command("api", "exec sleep 60")
	idle := f.command("idle", "exec sleep 60")
	leaveRunning(t, f, running, domain.ServiceRunning)
	leaveRunning(t, f, starting, domain.ServiceStarting)

	g := restarted(t, f, services.Options{})
	events := sseEvents(t, g.srv.URL+"/api/services/events?project_id="+f.projectID)
	if err := g.svc.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{running, starting} {
		svc := g.service(id)
		run := svc["run"].(map[string]any)
		if svc["status"] != "stopped" || run["ended_at"] == "" || run["ended_at"] == nil || run["id"] != "run-old" {
			t.Fatalf("%s after boot = %v", id, svc)
		}
		text := g.logText(id)
		if !strings.HasPrefix(text, "[harness] command: serve\nlistening\n") || !strings.HasSuffix(text, "[harness] the harness restarted\n") {
			t.Fatalf("log = %q", text)
		}
	}
	if g.status(idle) != "stopped" || g.service(idle)["run"] != nil {
		t.Fatalf("a service that never ran is untouched: %v", g.service(idle))
	}
	counts := []float64{}
	for range 2 {
		e := nextEvent(t, events)
		if e["kind"] != "service_changed" || e["service"].(map[string]any)["status"] != "stopped" {
			t.Fatalf("event = %v", e)
		}
		counts = append(counts, e["running_count"].(float64))
	}
	if !slices.Equal(counts, []float64{1, 0}) {
		t.Fatalf("running_count falls as each is stopped: %v", counts)
	}
	_, list := g.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	for _, s := range list["services"].([]any) {
		if s.(map[string]any)["status"] != "stopped" {
			t.Fatalf("nothing runs after boot: %v", s)
		}
	}
	if err := g.svc.Recover(context.Background()); err != nil {
		t.Fatalf("a second recover is a no-op: %v", err)
	}
	if got := strings.Count(g.logText(running), "the harness restarted"); got != 1 {
		t.Fatalf("restart line written %d times", got)
	}
}

func TestServicesLifecycle_BootRemovesTheHarnessLabelledContainers(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	sweeper := &fakeSweeper{found: []ports.ServiceContainerRef{
		{Name: "harness-svc-aaaa-1", ServiceID: "a", Instance: "old"}, {Name: "harness-svc-bbbb-2", ServiceID: "b", Instance: "other"},
	}}
	g := restarted(t, f, services.Options{LookPath: lookPathOf("podman"), Sweeper: sweeper})
	g.setRuntime("podman")

	if err := g.svc.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sweeper.Removed(); !slices.Equal(got, []string{"harness-svc-aaaa-1", "harness-svc-bbbb-2"}) {
		t.Fatalf("removed = %v", got)
	}
	if got := sweeper.Asked(); !slices.Equal(got, []string{"podman"}) {
		t.Fatalf("asked the configured runtime: %v", got)
	}
}

func TestServicesLifecycle_BootCarriesOnWithoutARuntimeOrWhenTheSweepFails(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	leaveRunning(t, f, f.command("web", "true"), domain.ServiceRunning)

	none := &fakeSweeper{found: []ports.ServiceContainerRef{{Name: "c"}}}
	g := restarted(t, f, services.Options{LookPath: lookPathOf(), Sweeper: none})
	if err := g.svc.Recover(context.Background()); err != nil {
		t.Fatalf("no runtime: %v", err)
	}
	if len(none.Asked()) != 0 {
		t.Fatalf("a missing runtime is not asked: %v", none.Asked())
	}

	for name, sw := range map[string]*fakeSweeper{
		"list fails":   {listErr: errors.New("daemon is not running")},
		"remove fails": {found: []ports.ServiceContainerRef{{Name: "a"}, {Name: "b"}}, removeErr: errors.New("busy")},
	} {
		h := restarted(t, f, services.Options{LookPath: lookPathOf("docker"), Sweeper: sw})
		if err := h.svc.Recover(context.Background()); err != nil {
			t.Fatalf("%s: boot must not fail: %v", name, err)
		}
		if name == "remove fails" && len(sw.Removed()) != 2 {
			t.Fatalf("one container that stays does not stop the rest: %v", sw.Removed())
		}
	}
}

func TestServicesLifecycle_DeletingAProjectStopsItsServicesAndDeletesThemWithTheirLogs(t *testing.T) {
	f := newServicesFixture(t, services.Options{StopGrace: 300 * time.Millisecond})
	f.svc.Subscribe(f.cat.Bus)
	running := f.command("web", "echo pid=$$; exec sleep 60")
	idle := f.command("idle", "true")
	f.operate("start_service", running)
	pid := pidIn(t, f.awaitLog(running, "\npid="), "pid")
	f.operate("start_service", idle)
	f.awaitStatus(idle, "exited")
	other, err := f.cat.Projects.CreateProject(context.Background(), "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if code, out := f.call("POST", "/api/create_service", map[string]any{"project_id": other.ID, "service": commandInput("keep")}); code != 200 {
		t.Fatalf("create in other = %d %v", code, out)
	}
	events := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)
	logs := []string{f.svc.LogPath(running), f.svc.LogPath(idle)}

	if code, out := f.call("POST", "/api/delete_project", map[string]any{"project_id": f.projectID}); code != 204 {
		t.Fatalf("delete_project = %d %v", code, out)
	}

	if alive(pid) {
		t.Fatal("the service's process outlived its project")
	}
	deleted := map[string]bool{}
	for len(deleted) < 2 {
		e := nextEvent(t, events)
		if e["kind"] == "service_deleted" {
			deleted[e["service_id"].(string)] = true
			if e["running_count"] != float64(0) && len(deleted) == 2 {
				t.Fatalf("running_count = %v", e["running_count"])
			}
		}
	}
	if !deleted[running] || !deleted[idle] {
		t.Fatalf("deleted = %v", deleted)
	}
	for _, id := range []string{running, idle} {
		if _, err := f.store.GetService(id); err == nil {
			t.Fatalf("service %s is still stored", id)
		}
	}
	for _, p := range logs {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("log %s is still on disk: %v", p, err)
		}
	}
	if list, err := f.store.ListServices(other.ID); err != nil || len(list) != 1 {
		t.Fatalf("another project's services stay: %v %v", list, err)
	}
}
