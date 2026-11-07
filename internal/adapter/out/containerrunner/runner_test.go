package containerrunner

import (
	"context"
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
run) echo "K=$K" >> "` + dir + `/envseen"; ` + runBody + ` ;;
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
		Container: &ports.ServiceContainer{Runtime: "docker", ServiceID: "svc1", Instance: "inst1", Image: "postgres:16", Env: []string{"K=v"}},
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

	h, err := Runner{}.Start(context.Background(), spec(dir))
	if err != nil {
		t.Fatal(err)
	}
	rh := h.(ports.ServiceReadyHandle)
	select {
	case <-rh.Ready():
		t.Fatal("ready while the image is still being pulled")
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case <-rh.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("never ready")
	}
	h.Stop(10 * time.Second)
	h.Wait()
	calls := read(t, filepath.Join(dir, "calls"))
	if !strings.Contains(calls, "run --rm --name harness-svc-x-y --label harness.service=svc1 --label harness.instance=inst1 -e K postgres:16\n") ||
		!strings.Contains(calls, "stop -t 10 harness-svc-x-y\n") {
		t.Fatalf("calls:\n%s", calls)
	}
	if seen := read(t, filepath.Join(dir, "envseen")); seen != "K=v\n" {
		t.Fatalf("the container's value reaches the runtime through its environment, not its argv: %q", seen)
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
