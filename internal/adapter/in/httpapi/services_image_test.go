package httpapi

import (
	"context"
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

// Running an image service through the HTTP API: the container runner is
// the fake that stands in for docker, taking the argument list the real
// builder makes; status changes are read on the services feed and the
// runtime's output on the log.

// imageFixture serves the module with the fake container runner and a PATH
// that holds the commands named in found.
func newImageFixture(t *testing.T, found ...string) (*servicesFixture, *containerrunnertest.Fake) {
	t.Helper()
	fake := &containerrunnertest.Fake{}
	f := newServicesFixture(t, services.Options{
		Runners: map[domain.ServiceKind]ports.ServiceProcessRunner{domain.ServiceKindCommand: commandrunner.Runner{}, domain.ServiceKindImage: fake},
		LookPath: func(c string) (string, error) {
			if slices.Contains(found, c) {
				return "/usr/bin/" + c, nil
			}
			return "", os.ErrNotExist
		},
		StopGrace: 10 * time.Second,
	})
	return f, fake
}

func (f *servicesFixture) image(name string, in map[string]any) string {
	f.t.Helper()
	in["name"], in["kind"] = name, "image"
	return f.createID(in)
}

func (f *servicesFixture) setRuntime(name string) (int, map[string]any) {
	return f.call("POST", "/api/set_services_settings", map[string]any{"container_runtime": name})
}

func TestServicesImage_TheRuntimeSettingIsDockerUntilPodmanIsPicked(t *testing.T) {
	f, _ := newImageFixture(t, "podman")

	code, out := f.call("GET", "/api/get_services_settings", nil)
	if code != 200 || out["container_runtime"] != "docker" || out["available"] != false {
		t.Fatalf("default = %d %v", code, out)
	}
	code, out = f.setRuntime("podman")
	if code != 200 || out["container_runtime"] != "podman" || out["available"] != true {
		t.Fatalf("set = %d %v", code, out)
	}
	_, list := f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	if rt := list["runtime"].(map[string]any); rt["name"] != "podman" || rt["available"] != true {
		t.Fatalf("list_services reports the setting: %v", rt)
	}
	for _, bad := range []string{"", "containerd", "docker --privileged", "Docker"} {
		code, out = f.setRuntime(bad)
		if c, _ := errorOf(out); code != 400 || c != "INVALID_INPUT" {
			t.Fatalf("runtime %q = %d %v", bad, code, out)
		}
	}
	if _, out = f.call("GET", "/api/get_services_settings", nil); out["container_runtime"] != "podman" {
		t.Fatalf("a refused value changes nothing: %v", out)
	}
}

func TestServicesImage_StartsShowStartingUntilTheContainerRunsThenRunning(t *testing.T) {
	f, fake := newImageFixture(t, "docker")
	fake.Hold = true
	id := f.image("db", map[string]any{
		"repository_id": f.repoID,
		"image": map[string]any{"ref": "postgres:16", "args": []string{"-c", "log_statement=all"},
			"ports": []map[string]any{{"host": 5432, "container": 5432}, {"host": 5353, "container": 53, "protocol": "udp"}}},
		"env": "POSTGRES_PASSWORD=hunter2",
	})
	events := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)

	code, out := f.operate("start_service", id)
	svc := out["service"].(map[string]any)
	if code != 200 || out["already_running"] != false || svc["status"] != "starting" {
		t.Fatalf("an image service is starting while it pulls: %d %v", code, out)
	}
	run := svc["run"].(map[string]any)
	where := run["where"].(map[string]any)
	if where["kind"] != "container" || where["dir"] != "" || run["sandboxed"] != true {
		t.Fatalf("run = %v", run)
	}
	runs := fake.Runs()
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
	r := runs[0]
	short := func(s string) string { return s[:8] }
	wantName := "harness-svc-" + short(id) + "-" + short(strings.TrimPrefix(run["id"].(string), "run-"))
	if r.Runtime != "docker" || !r.Has("--name", wantName) || !r.Has("--label", "harness.service="+id) {
		t.Fatalf("name and label: %q", r.Argv)
	}
	if !slices.ContainsFunc(r.Argv, func(a string) bool {
		return strings.HasPrefix(a, "harness.instance=") && len(a) > len("harness.instance=")
	}) {
		t.Fatalf("instance label: %q", r.Argv)
	}
	for _, want := range []string{"5432:5432/tcp", "5353:53/udp", "POSTGRES_PASSWORD"} {
		if !slices.Contains(r.Argv, want) {
			t.Fatalf("%q missing from %q", want, r.Argv)
		}
	}
	if !slices.Contains(r.Env, "POSTGRES_PASSWORD=hunter2") {
		t.Fatalf("the value reaches the runtime's environment: %q", r.Env)
	}
	for _, a := range r.Argv {
		if strings.Contains(a, "hunter2") {
			t.Fatalf("an env value is on the command line: %q", r.Argv)
		}
	}
	if tail := r.Argv[len(r.Argv)-3:]; !slices.Equal(tail, []string{"postgres:16", "-c", "log_statement=all"}) {
		t.Fatalf("the image then its args close the command: %q", r.Argv)
	}
	f.awaitLog(id, "pulling")

	r.Release()
	got := f.awaitStatus(id, "running")
	if got["run"].(map[string]any)["where"].(map[string]any)["kind"] != "container" {
		t.Fatalf("running = %v", got)
	}
	// starting, then running, on the feed.
	var seen []string
	for _, e := range serviceChanges(t, events, id, 2) {
		seen = append(seen, e["service"].(map[string]any)["status"].(string))
	}
	if !slices.Equal(seen, []string{"starting", "running"}) {
		t.Fatalf("feed statuses = %v", seen)
	}

	r.Say("ready to accept connections\n")
	log := f.awaitLog(id, "ready to accept connections")
	if !strings.Contains(log, "[harness] command: docker run --rm --name "+wantName) || !strings.Contains(log, "-e POSTGRES_PASSWORD postgres:16 -c log_statement=all") {
		t.Fatalf("the runtime command is in the log: %q", log)
	}
	if strings.Contains(log, "hunter2") {
		t.Fatalf("an env value is in the log: %q", log)
	}
}

func TestServicesImage_NoInputReachesTheRuntimeAsAFlagOrAShell(t *testing.T) {
	f, fake := newImageFixture(t, "docker")
	hostile := []string{"--privileged", "-v", "/:/host", "--network=host", "--device=/dev/mem", "; touch /tmp/pwned", "$(id)", "`id`", "a b", "--mount=type=bind,src=/,dst=/h"}
	env := "X1=--privileged\nX2=-v /:/host\nX3=$(id)\nX4=a;b\nX5='--network=host'"
	id := f.image("evil", map[string]any{"env": env, "image": map[string]any{"ref": "reg.io/a/b:1@sha256:abc;touch$IFS/tmp/p", "args": hostile}})

	if code, out := f.operate("start_service", id); code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	f.awaitStatus(id, "running")
	argv := fake.Runs()[0].Argv
	image := slices.Index(argv, "reg.io/a/b:1@sha256:abc;touch$IFS/tmp/p")
	if image < 0 || !slices.Equal(argv[image+1:], hostile) {
		t.Fatalf("image and args are separate entries after every flag: %q", argv)
	}
	for _, a := range argv[:image] {
		for _, forbidden := range []string{"-v", "--volume", "--mount", "--privileged", "--network", "--net", "--device", "--pid", "--cap-add", "--security-opt", "--userns"} {
			if a == forbidden || strings.HasPrefix(a, forbidden+"=") {
				t.Fatalf("forbidden flag %q in %q", a, argv)
			}
		}
	}
	run := fake.Runs()[0]
	for _, kv := range []string{"X1=--privileged", "X2=-v /:/host", "X3=$(id)", "X4=a;b", "X5=--network=host"} {
		k, _, _ := strings.Cut(kv, "=")
		i := slices.Index(argv, k)
		if i < 1 || argv[i-1] != "-e" {
			t.Fatalf("%q is one -e entry: %q", k, argv)
		}
		if !slices.Contains(run.Env, kv) || slices.Contains(argv, kv) {
			t.Fatalf("%q is in the environment, not on the command line", kv)
		}
	}
	log := f.logText(id)
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "[harness] ") && (strings.Contains(line, "--privileged=") || strings.Contains(line, "$(id)") && strings.Contains(line, "X3=")) {
			t.Fatalf("an env value is in a [harness] line: %q", line)
		}
	}
}

func TestServicesImage_ARuntimeErrorEndsTheRunFailedWithItsMessage(t *testing.T) {
	f, fake := newImageFixture(t, "docker")
	fake.Fail = "docker: Error response from daemon: driver failed programming external connectivity: Bind for 0.0.0.0:5432 failed: port is already allocated."
	id := f.image("db", map[string]any{"image": map[string]any{"ref": "postgres:16"}, "env": "SECRET=s3cr3t"})
	events := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)

	code, out := f.operate("start_service", id)
	if code != 200 {
		t.Fatalf("a failure after the run was created is not a refusal: %d %v", code, out)
	}
	got := f.awaitStatus(id, "failed")
	run := got["run"].(map[string]any)
	if run["error"] != fake.Fail || run["ended_at"] == "" || run["exit_code"] != nil {
		t.Fatalf("run = %v", run)
	}
	log := f.awaitLog(id, "[harness] failed: ")
	if !strings.Contains(log, "port is already allocated") || strings.Contains(log, "s3cr3t") {
		t.Fatalf("the runtime's message is in the log, no env value: %q", log)
	}
	last := serviceChanges(t, events, id, 2)
	if last[len(last)-1]["service"].(map[string]any)["status"] != "failed" {
		t.Fatalf("feed = %v", last)
	}
	// A new start is allowed after a failed run, and the fault can clear.
	fake.Fail = ""
	if code, out := f.operate("start_service", id); code != 200 || out["already_running"] != false {
		t.Fatalf("restart after failure = %d %v", code, out)
	}
	f.awaitStatus(id, "running")
}

func TestServicesImage_AMissingRuntimeRefusesTheStartAndStartsNothing(t *testing.T) {
	f, fake := newImageFixture(t) // no docker, no podman
	id := f.image("db", map[string]any{"image": map[string]any{"ref": "postgres:16"}})

	code, out := f.operate("start_service", id)
	if code != 503 || errCode(out) != "RUNTIME_UNAVAILABLE" {
		t.Fatalf("start = %d %v", code, out)
	}
	if len(fake.Runs()) != 0 || f.status(id) != "stopped" || f.service(id)["run"] != nil {
		t.Fatalf("nothing started: runs %d, %v", len(fake.Runs()), f.service(id))
	}
	if code, out := f.operate("restart_service", id); errCode(out) != "RUNTIME_UNAVAILABLE" {
		t.Fatalf("restart = %d %v", code, out)
	}
}

func TestServicesImage_ARefusedRestartLeavesTheRunningServiceRunning(t *testing.T) {
	f, fake := newImageFixture(t, "docker") // podman is not installed
	id := f.image("db", map[string]any{"image": map[string]any{"ref": "postgres:16"}})
	f.operate("start_service", id)
	f.awaitStatus(id, "running")

	f.setRuntime("podman")
	if code, out := f.operate("restart_service", id); code != 503 || errCode(out) != "RUNTIME_UNAVAILABLE" {
		t.Fatalf("restart = %d %v", code, out)
	}
	if f.status(id) != "running" || len(fake.Runs()[0].Stops()) != 0 || len(fake.Runs()) != 1 {
		t.Fatalf("a refused restart stops nothing: %s, stops %v, runs %d", f.status(id), fake.Runs()[0].Stops(), len(fake.Runs()))
	}
}

func errCode(out map[string]any) string { c, _ := errorOf(out); return c }

func TestServicesImage_AStopUsesTheRuntimeTheRunStartedWith(t *testing.T) {
	f, fake := newImageFixture(t, "docker", "podman")
	id := f.image("db", map[string]any{"image": map[string]any{"ref": "postgres:16"}})
	f.operate("start_service", id)
	f.awaitStatus(id, "running")

	f.setRuntime("podman")
	if f.status(id) != "running" {
		t.Fatal("the setting does not touch a running service")
	}
	code, out := f.operate("stop_service", id)
	if code != 200 || out["service"].(map[string]any)["status"] != "stopped" {
		t.Fatalf("stop = %d %v", code, out)
	}
	first := fake.Runs()[0]
	name := first.Spec.Name
	if got := first.Stops(); !slices.Equal(got, []string{"docker stop -t 10 " + name}) {
		t.Fatalf("stops = %v", got)
	}
	if !strings.Contains(f.logText(id), "[harness] stopped") {
		t.Fatalf("log: %q", f.logText(id))
	}

	// The next start takes the new runtime; restart stops with it too.
	f.operate("start_service", id)
	f.awaitStatus(id, "running")
	second := fake.Runs()[1]
	if second.Runtime != "podman" || !strings.Contains(f.logText(id), "[harness] command: podman run ") {
		t.Fatalf("the next start uses podman: %q", f.logText(id))
	}
	f.setRuntime("docker")
	if code, out := f.operate("restart_service", id); code != 200 {
		t.Fatalf("restart = %d %v", code, out)
	}
	if got := second.Stops(); len(got) != 1 || !strings.HasPrefix(got[0], "podman stop -t 10 ") {
		t.Fatalf("restart stops with the runtime the run started with: %v", got)
	}
	f.awaitStatus(id, "running")
	if third := fake.Runs()[2]; third.Runtime != "docker" {
		t.Fatalf("third run = %s", third.Runtime)
	}
}

func TestServicesImage_ARunningServiceIsReturnedAndRacingStartsShareOneRun(t *testing.T) {
	f, fake := newImageFixture(t, "docker")
	id := f.image("db", map[string]any{"image": map[string]any{"ref": "postgres:16"}})

	var wg sync.WaitGroup
	var mu sync.Mutex
	fresh := 0
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, out := f.operate("start_service", id); out["already_running"] == false {
				mu.Lock()
				fresh++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if fresh != 1 || len(fake.Runs()) != 1 {
		t.Fatalf("one run per service: %d fresh starts, %d container runs", fresh, len(fake.Runs()))
	}
	f.awaitStatus(id, "running")
	if _, out := f.operate("start_service", id); out["already_running"] != true || out["service"].(map[string]any)["status"] != "running" {
		t.Fatalf("again = %v", out)
	}
	if n, _ := f.svc.RunningServiceCount(context.Background(), f.projectID); n != 1 {
		t.Fatalf("running count = %d", n)
	}
	if len(fake.Runs()) != 1 {
		t.Fatal("already running starts no second container")
	}
}

func TestServicesImage_AStopWhileThePullGoesEndsStopped(t *testing.T) {
	f, fake := newImageFixture(t, "docker")
	fake.Hold = true
	id := f.image("db", map[string]any{"image": map[string]any{"ref": "postgres:16"}})
	f.operate("start_service", id)
	if f.status(id) != "starting" {
		t.Fatal("pulling")
	}
	code, out := f.operate("stop_service", id)
	if code != 200 || out["service"].(map[string]any)["status"] != "stopped" {
		t.Fatalf("stop = %d %v", code, out)
	}
}

func TestServicesImage_TheRepositoryEnvIsExportedAsForACommandService(t *testing.T) {
	f, fake := newImageFixture(t, "docker")
	t.Setenv("HARNESS_TEST_SERVER_SECRET", "server-secret-value")
	if _, err := f.cat.Projects.SaveEnvFile(context.Background(), f.repoID, ".env", "A=from-dotenv\nB=from-dotenv\n"); err != nil {
		t.Fatal(err)
	}
	start := func(name string, export bool) *containerrunnertest.Run {
		id := f.image(name, map[string]any{"repository_id": f.repoID, "export_repository_env": export, "env": "B=from-service\nC=service-only",
			"image": map[string]any{"ref": "postgres:16"}})
		f.operate("start_service", id)
		f.awaitStatus(id, "running")
		runs := fake.Runs()
		return runs[len(runs)-1]
	}
	envOf := func(r *containerrunnertest.Run) []string {
		var env []string
		for i, a := range r.Argv {
			if a != "-e" {
				continue
			}
			for _, kv := range slices.Backward(r.Env) {
				if strings.HasPrefix(kv, r.Argv[i+1]+"=") {
					env = append(env, kv)
					break
				}
			}
		}
		return env
	}

	if got := envOf(start("exported", true)); !slices.Equal(got, []string{"A=from-dotenv", "B=from-service", "C=service-only"}) {
		t.Fatalf("service env beats the .env; the base and the server's env stay out of the container: %v", got)
	}
	if got := envOf(start("plain", false)); !slices.Equal(got, []string{"B=from-service", "C=service-only"}) {
		t.Fatalf("the .env is exported only when asked: %v", got)
	}
	for _, r := range fake.Runs() {
		for _, e := range r.Spec.Env {
			if strings.HasPrefix(e, "A=") || strings.HasPrefix(e, "B=") {
				t.Fatalf("container env leaked into the runtime command's own: %v", r.Spec.Env)
			}
		}
	}
}
