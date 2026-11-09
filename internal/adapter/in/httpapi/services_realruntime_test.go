package httpapi

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/commandrunner"
	"operators-mcp/internal/adapter/out/containerrunner"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The real container runtime, when this machine has one: skipped unless docker
// (or podman) is on PATH, its daemon answers and a tiny image is
// already cached (nothing is pulled).

// tinyImages are small images, in order of preference, the test uses when
// one is cached already.
var tinyImages = []string{"busybox", "busybox:latest", "alpine", "alpine:latest"}

func realRuntime(t *testing.T) (runtime, image string) {
	t.Helper()
	var why []string
	for _, rt := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(rt); err != nil {
			why = append(why, rt+" is not on PATH")
			continue
		}
		if out, err := exec.Command(rt, "info").CombinedOutput(); err != nil {
			why = append(why, rt+" info failed (no daemon?): "+firstLine(string(out)))
			continue
		}
		for _, img := range tinyImages {
			if exec.Command(rt, "image", "inspect", img).Run() == nil {
				return rt, img
			}
		}
		why = append(why, "none of "+strings.Join(tinyImages, ", ")+" is cached by "+rt+", and the test never pulls")
	}
	t.Skip("skipped: " + strings.Join(why, "; "))
	return "", ""
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func TestServicesRealRuntime_ImageServiceRunsStopsAndBootRemovesALabelledLeftover(t *testing.T) {
	runtime, image := realRuntime(t)
	f := newServicesFixture(t, services.Options{
		Runners: map[domain.ServiceKind]ports.ServiceProcessRunner{
			domain.ServiceKindCommand: commandrunner.Runner{}, domain.ServiceKindImage: containerrunner.Runner{},
		},
		LookPath: exec.LookPath,
		Sweeper:  containerrunner.Runner{},
		Instance: "real-test-instance",
	})
	if code, out := f.setRuntime(runtime); code != 200 {
		t.Fatalf("set runtime = %d %v", code, out)
	}
	id := f.image("sleeper", map[string]any{"image": map[string]any{
		"ref": image, "args": []string{"sleep", "300"}, "ports": []map[string]any{{"host": 18765, "container": 8080}},
	}})

	if code, out := f.operate("start_service", id); code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	eventually(t, "the container to run", func() bool { return f.status(id) == "running" })
	out, err := exec.Command(runtime, "ps", "--filter", "label=harness.service="+id, "--format", "{{.Ports}}").Output()
	if err != nil || !strings.Contains(string(out), "18765->8080") {
		t.Fatalf("port mapping: %q %v", out, err)
	}
	if code, o := f.operate("stop_service", id); code != 200 || o["service"].(map[string]any)["status"] != "stopped" {
		t.Fatalf("stop = %d %v", code, o)
	}

	leftover, foreign := "harness-svc-test-leftover", "harness-svc-test-foreign"
	for name, instance := range map[string]string{leftover: "real-test-instance", foreign: "another-harness"} {
		t.Cleanup(func() { _ = exec.Command(runtime, "rm", "-f", name).Run() })
		if out, err := exec.Command(runtime, "run", "-d", "--name", name, "--label", "harness.service=test",
			"--label", "harness.instance="+instance, image, "sleep", "300").CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", name, err, out)
		}
	}
	if err := f.svc.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the leftover to be removed", func() bool {
		out, _ := exec.Command(runtime, "ps", "-a", "--filter", "name="+leftover, "--format", "{{.Names}}").Output()
		return strings.TrimSpace(string(out)) == ""
	})
	if out, _ := exec.Command(runtime, "ps", "-a", "--filter", "name="+foreign, "--format", "{{.Names}}").Output(); strings.TrimSpace(string(out)) != foreign {
		t.Fatalf("another harness's container must survive the sweep, ps = %q", out)
	}
}
