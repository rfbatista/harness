package containerrunner

import (
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The one place a pure function is the right seam: whatever the service
// holds, the runtime never gets a flag that reaches the host.
func TestRunArgsNeverHoldAForbiddenFlagForHostileInput(t *testing.T) {
	hostile := []string{"--privileged", "-v", "/:/host", "--network=host", "--device=/dev/mem", "; rm -rf /", "$(id)", "`id`", "a b", "--mount=type=bind,src=/,dst=/h"}
	c := ports.ServiceContainer{
		Runtime: "docker", ServiceID: "svc1", Instance: "inst1", Image: "repo/img:1$(id);--privileged",
		Args:  hostile,
		Ports: []domain.ServicePort{{Host: 5432, Container: 5432, Protocol: "tcp"}, {Host: 53, Container: 5353, Protocol: "udp"}},
	}
	for _, h := range hostile {
		c.Env = append(c.Env, "K"+strings.Repeat("X", len(c.Env))+"="+h)
	}
	args, err := RunArgs("harness-svc-x-y", c)
	if err != nil {
		t.Fatal(err)
	}
	image := slices.Index(args, c.Image)
	if image < 0 || !slices.Equal(args[image+1:], hostile) {
		t.Fatalf("image then its args as separate entries: %q", args)
	}
	var flags []string
	for _, a := range args[:image] {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		}
	}
	want := []string{"--rm", "--name", "--label", "--label"}
	for range c.Ports {
		want = append(want, "-p")
	}
	for range c.Env {
		want = append(want, "-e")
	}
	slices.Sort(want)
	slices.Sort(flags)
	if !slices.Equal(flags, want) {
		t.Fatalf("flags = %q, want only %q", flags, want)
	}
	for i, kv := range c.Env {
		k, v, _ := strings.Cut(kv, "=")
		at := slices.Index(args[:image], k)
		if at < 1 || args[at-1] != "-e" {
			t.Fatalf("env name %q is not its own -e entry of %q", k, args)
		}
		for _, a := range args[:image] {
			if a == v || a == kv {
				t.Fatalf("env value %q of entry %d is on the command line: %q", v, i, args)
			}
		}
		if !slices.Contains(RunEnv([]string{"PATH=/bin"}, c), kv) {
			t.Fatalf("env %q does not arrive in the runtime's environment", kv)
		}
	}
	if env := RunEnv([]string{"PATH=/bin"}, c); env[0] != "PATH=/bin" || len(env) != 1+len(c.Env) {
		t.Fatalf("the base environment comes first: %q", env)
	}
	if !slices.Contains(args, "5432:5432/tcp") || !slices.Contains(args, "53:5353/udp") {
		t.Fatalf("port mappings missing: %q", args)
	}
}

func TestNoEnvValueIsOnTheCommandLineAnywhere(t *testing.T) {
	c := ports.ServiceContainer{Runtime: "docker", Image: "pg", Env: []string{"PASS=hunter2", "TOKEN=s3cr3t=with=equals"}}
	args, err := RunArgs("n", c)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if strings.Contains(a, "hunter2") || strings.Contains(a, "s3cr3t") {
			t.Fatalf("a value is on the command line: %q", args)
		}
	}
	if !slices.Contains(args, "PASS") || !slices.Contains(args, "TOKEN") {
		t.Fatalf("names are: %q", args)
	}
}

func TestRunArgsRefuseWhatValidationShouldHave(t *testing.T) {
	for _, c := range []ports.ServiceContainer{
		{Image: "-v"}, {Image: ""},
		{Image: "i", Env: []string{"--privileged=1"}},
		{Image: "i", Ports: []domain.ServicePort{{Host: 1, Container: 1, Protocol: "tcp -v /:/h"}}},
	} {
		if _, err := RunArgs("n", c); err == nil {
			t.Fatalf("%+v should be refused", c)
		}
	}
}

func TestDescribeNamesEnvKeysNeverValues(t *testing.T) {
	line := Describe("n", ports.ServiceContainer{Runtime: "podman", Image: "pg", Env: []string{"PASS=hunter2"}, Args: []string{"-c", "x"}})
	if strings.Contains(line, "hunter2") || !strings.Contains(line, "-e PASS pg -c x") || !strings.HasPrefix(line, "podman run ") {
		t.Fatal(line)
	}
}
