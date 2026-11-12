package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/commandrunner"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeSandboxSwitch bool

func (f fakeSandboxSwitch) Enabled() bool { return bool(f) }

type servicesFixture struct {
	t      *testing.T
	srv    *httptest.Server
	cat    catalog.Catalog
	svc    *services.Service
	store  ports.ServiceStore
	logDir string
	// worktreeRoot is where service worktrees are made, when the test set one.
	worktreeRoot string
	projectID    string
	repoID       string
}

// newServicesFixture serves the services module over a real in-memory store,
// with one project that has one repository.
func newServicesFixture(t *testing.T, opts services.Options) *servicesFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	deps := catalog.SQLiteDeps(db)
	deps.EnvFiles = sqlite.NewEnvFileRepository(db)
	cat := catalog.New(deps)
	if opts.LogDir == "" {
		opts.LogDir = t.TempDir()
	}
	if opts.Runners == nil {
		opts.Runners = map[domain.ServiceKind]ports.ServiceProcessRunner{domain.ServiceKindCommand: commandrunner.Runner{}}
	}
	if opts.EnvFiles == nil {
		opts.EnvFiles = cat.Projects
	}
	if opts.FolderDir == "" {
		opts.FolderDir = t.TempDir()
	}
	store := sqlite.NewServiceRepository(db)
	svc := services.New(store, cat.Projects, cat.Projects, opts)
	s := servicesOf(cat)
	s.Services = svc
	srv := httptest.NewServer(NewRouter(NewHandler(s)))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	p, err := cat.Projects.CreateProject(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := cat.Projects.CreateRepository(ctx, p.ID, "api", "", "https://example.com/api.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &servicesFixture{t: t, srv: srv, cat: cat, svc: svc, store: store, logDir: opts.LogDir, worktreeRoot: opts.WorktreeRoot, projectID: p.ID, repoID: r.ID}
}

func (f *servicesFixture) call(method, path string, body any) (int, map[string]any) {
	f.t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (f *servicesFixture) create(in map[string]any) (int, map[string]any) {
	return f.call("POST", "/api/create_service", map[string]any{"project_id": f.projectID, "service": in})
}

func commandInput(name string) map[string]any {
	return map[string]any{"name": name, "kind": "command", "command": map[string]any{"line": "make air"}}
}

func imageInput(name string) map[string]any {
	return map[string]any{"name": name, "kind": "image", "image": map[string]any{"ref": "postgres:16"}}
}

func errorOf(out map[string]any) (code, message string) {
	code, _ = out["code"].(string)
	message, _ = out["error"].(string)
	return
}

func TestServices_DefineReadEditAndDelete(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	if err := os.MkdirAll(filepath.Join(f.repoDir(), "cmd/web"), 0o755); err != nil {
		t.Fatal(err)
	}

	code, out := f.create(map[string]any{
		"name": "web", "kind": "command", "repository_id": f.repoID, "export_repository_env": true,
		"env":     "PORT=8080\nLOG_LEVEL=debug",
		"command": map[string]any{"line": "make air", "subdir": "cmd/web", "ports": []int{8080}},
	})
	if code != 200 {
		t.Fatalf("create = %d %v", code, out)
	}
	web := out["service"].(map[string]any)
	if web["status"] != "stopped" || web["run"] != nil || web["id"] == "" || web["project_id"] != f.projectID {
		t.Fatalf("a new service is stopped and never ran: %v", web)
	}
	if web["image"] != nil || web["export_repository_env"] != true || web["env"] != "PORT=8080\nLOG_LEVEL=debug" {
		t.Fatalf("service = %v", web)
	}

	code, out = f.create(map[string]any{
		"name": "db", "kind": "image", "env": "POSTGRES_PASSWORD=x",
		"image": map[string]any{"ref": "postgres:16", "args": []string{"-c", "log_statement=all"},
			"ports": []map[string]any{{"host": 5432, "container": 5432}}},
	})
	if code != 200 {
		t.Fatalf("create image = %d %v", code, out)
	}
	db := out["service"].(map[string]any)
	img := db["image"].(map[string]any)
	if db["command"] != nil || img["ports"].([]any)[0].(map[string]any)["protocol"] != "tcp" {
		t.Fatalf("an image service has no command and its ports default to tcp: %v", db)
	}

	code, out = f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	if code != 200 {
		t.Fatalf("list = %d %v", code, out)
	}
	list := out["services"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["name"] != "db" || list[1].(map[string]any)["name"] != "web" {
		t.Fatalf("services are sorted by name: %v", list)
	}
	if out["seq"] != float64(2) {
		t.Fatalf("seq = %v, want 2 (one per create)", out["seq"])
	}
	if out["sandbox"] == nil || out["runtime"].(map[string]any)["name"] != "docker" {
		t.Fatalf("list carries runtime and sandbox: %v", out)
	}

	code, out = f.call("GET", "/api/get_service?service_id="+web["id"].(string), nil)
	if code != 200 || out["service"].(map[string]any)["name"] != "web" {
		t.Fatalf("get = %d %v", code, out)
	}

	code, out = f.call("POST", "/api/update_service", map[string]any{"service_id": web["id"], "service": map[string]any{
		"name": "web2", "kind": "command", "command": map[string]any{"line": "make run"},
	}})
	if code != 200 || out["restart_needed"] != false {
		t.Fatalf("update = %d %v", code, out)
	}
	upd := out["service"].(map[string]any)
	if upd["name"] != "web2" || upd["repository_id"] != "" || upd["export_repository_env"] != false ||
		upd["command"].(map[string]any)["line"] != "make run" || upd["created_at"] != web["created_at"] {
		t.Fatalf("update replaces the definition and keeps the rest: %v", upd)
	}

	if code, out = f.call("POST", "/api/delete_service", map[string]any{"service_id": web["id"]}); code != 200 || len(out) != 0 {
		t.Fatalf("delete = %d %v", code, out)
	}
	if code, out = f.call("GET", "/api/get_service?service_id="+web["id"].(string), nil); code != 404 {
		t.Fatalf("a deleted service is gone: %d %v", code, out)
	}
}

func TestServices_RefuseInvalidFieldsNamingThem(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	cases := []struct {
		field string
		in    map[string]any
	}{
		{"name", commandInput("Bad Name")},
		{"name", commandInput("")},
		{"name", commandInput("-x")},
		{"kind", map[string]any{"name": "a", "kind": "vm"}},
		{"command.line", map[string]any{"name": "a", "kind": "command", "command": map[string]any{"line": "  "}}},
		{"command.line", map[string]any{"name": "a", "kind": "command"}},
		{"image.ref", map[string]any{"name": "a", "kind": "image", "image": map[string]any{"ref": ""}}},
		{"image.ref", map[string]any{"name": "a", "kind": "image", "image": map[string]any{"ref": "--privileged"}}},
		{"command.subdir", map[string]any{"name": "a", "kind": "command", "command": map[string]any{"line": "x", "subdir": "/etc"}}},
		{"command.subdir", map[string]any{"name": "a", "kind": "command", "command": map[string]any{"line": "x", "subdir": "a/../../b"}}},
		{"command.ports", map[string]any{"name": "a", "kind": "command", "command": map[string]any{"line": "x", "ports": []int{70000}}}},
		{"image.ports[0].host", map[string]any{"name": "a", "kind": "image", "image": map[string]any{"ref": "x", "ports": []map[string]any{{"host": 0, "container": 80}}}}},
		{"image.ports[0].container", map[string]any{"name": "a", "kind": "image", "image": map[string]any{"ref": "x", "ports": []map[string]any{{"host": 80, "container": 65536}}}}},
		{"image.ports[0].protocol", map[string]any{"name": "a", "kind": "image", "image": map[string]any{"ref": "x", "ports": []map[string]any{{"host": 80, "container": 80, "protocol": "sctp"}}}}},
		{"image.ports[1].host", map[string]any{"name": "a", "kind": "image", "image": map[string]any{"ref": "x", "ports": []map[string]any{{"host": 80, "container": 80}, {"host": 80, "container": 81}}}}},
		{"env", map[string]any{"name": "a", "kind": "command", "env": "OK=1\nnot a dotenv line", "command": map[string]any{"line": "x"}}},
	}
	for _, c := range cases {
		code, out := f.create(c.in)
		ec, msg := errorOf(out)
		if code != 400 || ec != "INVALID_INPUT" || !strings.Contains(msg, c.field) {
			t.Errorf("%s: got %d %s %q", c.field, code, ec, msg)
		}
	}

	if _, out := f.create(map[string]any{"name": "a", "kind": "command", "env": "SECRET=hunter2\nhunter3 oops", "command": map[string]any{"line": "x"}}); strings.Contains(out["error"].(string), "hunter") {
		t.Errorf("a refusal never quotes an env line: %v", out["error"])
	}

	_, out := f.create(commandInput("web"))
	id := out["service"].(map[string]any)["id"]
	code, out := f.call("POST", "/api/update_service", map[string]any{"service_id": id, "service": imageInput("web")})
	if ec, msg := errorOf(out); code != 400 || ec != "INVALID_INPUT" || !strings.Contains(msg, "kind") {
		t.Errorf("a kind change on update = %d %s %q", code, ec, msg)
	}
	code, out = f.call("POST", "/api/update_service", map[string]any{"service_id": id, "service": commandInput("Bad Name")})
	if ec, _ := errorOf(out); code != 400 || ec != "INVALID_INPUT" {
		t.Errorf("an invalid update = %d %s", code, ec)
	}
}

func TestServices_NamedRefusals(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	_, out := f.create(commandInput("web"))
	webID := out["service"].(map[string]any)["id"]
	_, out = f.create(commandInput("worker"))
	workerID := out["service"].(map[string]any)["id"]

	check := func(what string, code int, out map[string]any, wantStatus int, wantCode string) {
		t.Helper()
		if ec, _ := errorOf(out); code != wantStatus || ec != wantCode {
			t.Errorf("%s = %d %s, want %d %s", what, code, ec, wantStatus, wantCode)
		}
	}
	code, out := f.create(commandInput("web"))
	check("a taken name", code, out, 409, "SERVICE_NAME_TAKEN")
	code, out = f.call("POST", "/api/update_service", map[string]any{"service_id": workerID, "service": commandInput("web")})
	check("renaming onto a taken name", code, out, 409, "SERVICE_NAME_TAKEN")
	code, out = f.call("POST", "/api/update_service", map[string]any{"service_id": webID, "service": commandInput("web")})
	if code != 200 {
		t.Errorf("keeping its own name is fine: %d %v", code, out)
	}

	in := commandInput("api")
	in["repository_id"] = "nope"
	code, out = f.create(in)
	check("an unknown repository", code, out, 404, "REPOSITORY_NOT_FOUND")

	other, err := f.cat.Projects.CreateProject(context.Background(), "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := f.cat.Projects.CreateRepository(context.Background(), other.ID, "x", "", "https://example.com/x.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in["repository_id"] = foreign.ID
	code, out = f.create(in)
	check("another project's repository", code, out, 404, "REPOSITORY_NOT_FOUND")

	code, out = f.call("POST", "/api/create_service", map[string]any{"project_id": "nope", "service": commandInput("a")})
	check("an unknown project", code, out, 404, "PROJECT_NOT_FOUND")
	code, out = f.call("GET", "/api/list_services?project_id=nope", nil)
	check("listing an unknown project", code, out, 404, "PROJECT_NOT_FOUND")
	code, out = f.call("GET", "/api/get_service?service_id=nope", nil)
	check("an unknown service", code, out, 404, "SERVICE_NOT_FOUND")
	code, out = f.call("POST", "/api/delete_service", map[string]any{"service_id": "nope"})
	check("deleting an unknown service", code, out, 404, "SERVICE_NOT_FOUND")
	code, out = f.call("POST", "/api/update_service", map[string]any{"service_id": "nope", "service": commandInput("a")})
	check("updating an unknown service", code, out, 404, "SERVICE_NOT_FOUND")
}

func TestServices_RepositoryEnvExportReadsFalseWithoutARepository(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	in := commandInput("web")
	in["export_repository_env"] = true
	_, out := f.create(in)
	if out["service"].(map[string]any)["export_repository_env"] != false {
		t.Fatalf("export_repository_env with no repository: %v", out)
	}
	in = commandInput("api")
	in["export_repository_env"], in["repository_id"] = true, f.repoID
	_, out = f.create(in)
	if out["service"].(map[string]any)["export_repository_env"] != true {
		t.Fatalf("export_repository_env with a repository: %v", out)
	}
}

func TestServices_ListReportsRuntimeAndSandbox(t *testing.T) {
	driver := func() ports.SandboxDriver { return nil }
	cases := []struct {
		name      string
		opts      services.Options
		available bool
		sandbox   string
	}{
		{"setting off", services.Options{Sandbox: fakeSandboxSwitch(false), Driver: driver,
			LookPath: func(string) (string, error) { return "/usr/bin/docker", nil }}, true, "off"},
		{"setting on and no driver", services.Options{Sandbox: fakeSandboxSwitch(true), Driver: driver,
			LookPath: func(string) (string, error) { return "", errors.New("not found") }}, false, "unavailable"},
		{"setting on with a driver", services.Options{Sandbox: fakeSandboxSwitch(true),
			Driver: func() ports.SandboxDriver { return stubDriver{} }}, false, "on"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newServicesFixture(t, c.opts)
			_, out := f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
			rt := out["runtime"].(map[string]any)
			if rt["name"] != "docker" || rt["available"] != c.available || out["sandbox"] != c.sandbox {
				t.Fatalf("runtime %v sandbox %v", rt, out["sandbox"])
			}
			if out["services"] == nil || len(out["services"].([]any)) != 0 {
				t.Fatalf("an empty project lists no services, as an array: %v", out["services"])
			}
		})
	}
}

type stubDriver struct{}

func (stubDriver) Name() string { return "stub" }
func (stubDriver) Wrap(ports.SandboxPolicy, ports.SandboxCommand) (ports.SandboxCommand, error) {
	return ports.SandboxCommand{}, nil
}

func TestServices_DeleteRemovesItsLog(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	_, out := f.create(commandInput("web"))
	id := out["service"].(map[string]any)["id"].(string)
	logPath := f.svc.LogPath(id)
	if err := os.WriteFile(logPath, []byte("[harness] ran\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := f.call("POST", "/api/delete_service", map[string]any{"service_id": id}); code != 200 {
		t.Fatalf("delete = %d %v", code, out)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("the log survived the delete: %v", err)
	}
}

func TestServices_RunningCountIsZeroWhileNothingRuns(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	f.create(commandInput("web"))
	n, err := f.svc.RunningServiceCount(context.Background(), f.projectID)
	if err != nil || n != 0 {
		t.Fatalf("running count = %d, %v", n, err)
	}
}

// sseEvents reads the `data:` lines of an event stream into a channel.
func sseEvents(t *testing.T, url string) <-chan map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("GET %s = %d", url, res.StatusCode)
	}
	events := make(chan map[string]any, 32)
	go func() {
		defer res.Body.Close()
		defer close(events)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var e map[string]any
				if json.Unmarshal([]byte(data), &e) == nil {
					events <- e
				}
			}
		}
	}()
	return events
}

func nextEvent(t *testing.T, events <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case e, ok := <-events:
		if !ok {
			t.Fatal("the feed closed")
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event in time")
		return nil
	}
}

// Create, update and delete are announced on the feed with a sequence number
// that grows by one per event within a project, whether it is followed on
// its own SSE endpoint or on the tab's stream.
func TestServices_FeedAnnouncesEveryChange(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	other, err := f.cat.Projects.CreateProject(context.Background(), "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	own := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)

	// The tab's stream, following the same feed by path.
	stream := sseEvents(t, f.srv.URL+"/api/stream")
	streamID := nextEvent(t, stream)["stream"].(string)
	code, out := f.call("POST", "/api/stream/follow", map[string]any{"stream": streamID, "path": "/services/events?project_id=" + f.projectID})
	if code != 200 {
		t.Fatalf("follow = %d %v", code, out)
	}
	// An event in another project is not on this feed and has its own count.
	if code, out := f.call("POST", "/api/create_service", map[string]any{"project_id": other.ID, "service": commandInput("elsewhere")}); code != 200 {
		t.Fatalf("create in other = %d %v", code, out)
	}

	_, out = f.create(commandInput("web"))
	id := out["service"].(map[string]any)["id"].(string)
	f.call("POST", "/api/update_service", map[string]any{"service_id": id, "service": commandInput("web")})
	f.call("POST", "/api/delete_service", map[string]any{"service_id": id})

	wantKinds := []string{"service_changed", "service_changed", "service_deleted"}
	for i, kind := range wantKinds {
		e := nextEvent(t, own)
		if e["kind"] != kind || e["seq"] != float64(i+1) || e["running_count"] != float64(0) {
			t.Fatalf("event %d = %v", i, e)
		}
		if kind == "service_changed" && e["service"].(map[string]any)["id"] != id {
			t.Fatalf("event %d names another service: %v", i, e)
		}
		if kind == "service_deleted" && e["service_id"] != id {
			t.Fatalf("event %d = %v", i, e)
		}
		msg := nextEvent(t, stream)
		data, _ := msg["data"].(map[string]any)
		if msg["path"] != "/services/events?project_id="+f.projectID || data["kind"] != kind || data["seq"] != float64(i+1) {
			t.Fatalf("stream message %d = %v", i, msg)
		}
	}

	// list_services reports the sequence number its read is as of.
	_, out = f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	if out["seq"] != float64(3) {
		t.Fatalf("seq = %v, want 3", out["seq"])
	}

	if code, _ := f.call("GET", "/api/services/events?project_id=nope", nil); code != 404 {
		t.Fatalf("following an unknown project = %d", code)
	}
}

func TestServices_NotConfiguredAnswers503(t *testing.T) {
	srv := httptest.NewServer(NewRouter(NewHandler(Services{})))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/api/list_services?project_id=p")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatalf("status = %d", res.StatusCode)
	}
}
