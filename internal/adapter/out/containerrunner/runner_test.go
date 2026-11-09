package containerrunner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

// fakeDocker writes a `docker` script that plays the runtime: `run` pulls,
// then holds a container until `stop`; every call is appended to calls.
func fakeDocker(t *testing.T, runBody string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := `#!/bin/sh
echo "$@" >> "` + dir + `/calls"
case "$1" in
run) f=; while [ $# -gt 0 ]; do [ "$1" = --env-file ] && f=$2; shift; done
  cat "$f" >> "` + dir + `/envseen"; ls -l "$f" | cut -c1-10 >> "` + dir + `/modeseen"; env > "` + dir + `/runenv"; ` + runBody + ` ;;
inspect) [ -f "` + dir + `/up" ] && echo true || exit 1 ;;
stop) rm -f "` + dir + `/up"; kill $(cat "` + dir + `/pid") ;;
ps) printf 'harness-svc-a-b\tsvc1\tinst1\nharness-svc-c-d\tsvc2\tinst2\n' ;;
rm) ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func spec(dir string) ports.ServiceProcessSpec {
	return ports.ServiceProcessSpec{
		Name: "harness-svc-x-y", Env: []string{"PATH=" + dir + ":/usr/bin:/bin"},
		Container: &ports.ServiceContainer{Runtime: "docker", ServiceID: "svc1", Instance: "inst1", Image: "postgres:16",
			EnvDir: filepath.Join(dir, "folder", "svc1"), Env: []string{"K=v", "DOCKER_HOST=tcp://evil:1", "PATH=/evil"}},
	}
}

func read(t *testing.T, p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func TestARunIsReadyOnceTheRuntimeReportsTheContainerAndStopsWithStop(t *testing.T) {
	dir := fakeDocker(t, `echo pulling; echo $$ > PIDFILE; sleep 0.5; touch UPFILE; trap 'exit 0' TERM; while :; do sleep 0.1; done`)
	script := strings.NewReplacer("PIDFILE", `"`+dir+`/pid"`, "UPFILE", `"`+dir+`/up"`).Replace(read(t, filepath.Join(dir, "docker")))
	_ = os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	envFile := filepath.Join(dir, "folder", "svc1", "harness-svc-x-y.env")
	h, err := Runner{ReadyPoll: 10 * time.Millisecond}.Start(context.Background(), spec(dir))
	if err != nil {
		t.Fatal(err)
	}
	rh := h.(ports.ServiceReadyHandle)
	select {
	case <-rh.Ready():
		if _, err := os.Stat(filepath.Join(dir, "up")); err != nil {
			t.Fatal("ready while the image is still being pulled")
		}
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-rh.Ready():
	case <-time.After(30 * time.Second):
		t.Fatal("never ready")
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("the env file is gone once the container runs: %v", err)
	}
	h.Stop(10 * time.Second)
	h.Wait()
	calls := read(t, filepath.Join(dir, "calls"))
	if !strings.Contains(calls, "run --rm --name harness-svc-x-y --label harness.service=svc1 --label harness.instance=inst1 --env-file "+envFile+" postgres:16\n") ||
		!strings.Contains(calls, "stop -t 10 harness-svc-x-y\n") {
		t.Fatalf("calls:\n%s", calls)
	}
	if strings.Contains(calls, "hunter") || strings.Contains(calls, "evil") {
		t.Fatalf("a value is on the command line:\n%s", calls)
	}
	if seen := read(t, filepath.Join(dir, "envseen")); seen != "K=v\nDOCKER_HOST=tcp://evil:1\nPATH=/evil\n" {
		t.Fatalf("the container's env is in the file when the runtime starts: %q", seen)
	}
	if mode := read(t, filepath.Join(dir, "modeseen")); mode != "-rw-------\n" {
		t.Fatalf("mode = %q", mode)
	}
	runenv := read(t, filepath.Join(dir, "runenv"))
	if strings.Contains(runenv, "K=v") || strings.Contains(runenv, "DOCKER_HOST") || strings.Contains(runenv, "PATH=/evil") {
		t.Fatalf("the service's env reached the runtime command's environment:\n%s", runenv)
	}
}

func TestTheEnvFileIsGoneWhenTheRuntimeFailsOrCannotStart(t *testing.T) {
	dir := fakeDocker(t, `echo "Error: pull access denied" >&2; exit 125`)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	s := spec(dir)
	envFile := EnvFilePath(s.Name, *s.Container)
	h, err := Runner{}.Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	h.Wait()
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("env file left after a failed run: %v", err)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "envseen")), "K=v") {
		t.Fatal("the file was there while the runtime started")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := (Runner{}).Start(context.Background(), s); err == nil {
		t.Fatal("no docker on that PATH")
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("env file left after a start error: %v", err)
	}
}

func TestTheEnvFileHoldsHostileValuesUnchangedAndRefusesNewlines(t *testing.T) {
	var logs strings.Builder
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	c := ports.ServiceContainer{Image: "i", EnvDir: filepath.Join(t.TempDir(), "f"), Env: []string{
		"A=a b", `B="quoted"`, "C=$(id) `id` ${X}", "D=#not a comment", "E=it's", "F=line1\nB=injected", "G=x\ry", "H=", "A=last",
	}}
	remove, err := WriteEnvFile("n", c)
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, EnvFilePath("n", c))
	want := "A=last\nB=\"quoted\"\nC=$(id) `id` ${X}\nD=#not a comment\nE=it's\nH=\n"
	if got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if st, _ := os.Stat(c.EnvDir); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode().Perm())
	}
	if l := logs.String(); !strings.Contains(l, "F") || strings.Contains(l, "injected") || strings.Contains(l, "line1") || strings.Contains(l, "a b") {
		t.Fatalf("the log names the key only: %q", l)
	}
	remove()
	remove()
	if _, err := os.Stat(EnvFilePath("n", c)); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
}

func TestARunTheRuntimeEndsBeforeTheContainerStartedIsNeverReady(t *testing.T) {
	dir := fakeDocker(t, `echo "Error: pull access denied" >&2; exit 125`)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	h, err := Runner{}.Start(context.Background(), spec(dir))
	if err != nil {
		t.Fatal(err)
	}
	if code := h.Wait(); code != 125 {
		t.Fatalf("code = %d", code)
	}
	select {
	case <-h.(ports.ServiceReadyHandle).Ready():
		t.Fatal("ready")
	default:
	}
}

func TestStartRefusesWhatIsNotARuntimeOrACleanRun(t *testing.T) {
	s := spec(t.TempDir())
	s.Container.Runtime = "sh"
	if _, err := (Runner{}).Start(context.Background(), s); err == nil {
		t.Fatal("only docker and podman are run")
	}
	s = spec(t.TempDir())
	s.Container.Image = "--privileged"
	if _, err := (Runner{}).Start(context.Background(), s); err == nil {
		t.Fatal("an image that looks like a flag is refused")
	}
}

func TestTheHarnessContainersAreListedByLabelAndRemoved(t *testing.T) {
	dir := fakeDocker(t, `true`)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	refs, err := Runner{}.ListServiceContainers(context.Background(), "docker")
	if err != nil || len(refs) != 2 || refs[1] != (ports.ServiceContainerRef{Name: "harness-svc-c-d", ServiceID: "svc2", Instance: "inst2"}) {
		t.Fatalf("refs = %v, %v", refs, err)
	}
	if err := (Runner{}).RemoveServiceContainer(context.Background(), "docker", "harness-svc-a-b"); err != nil {
		t.Fatal(err)
	}
	calls := read(t, filepath.Join(dir, "calls"))
	if !strings.Contains(calls, "ps -a --filter label=harness.service --format") || !strings.Contains(calls, "rm -f harness-svc-a-b\n") {
		t.Fatalf("calls:\n%s", calls)
	}
	if (Runner{}).RemoveServiceContainer(context.Background(), "docker", "-f") == nil {
		t.Fatal("a name that looks like a flag is refused")
	}
}
