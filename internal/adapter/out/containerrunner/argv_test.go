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
		Runtime: "docker", ServiceID: "svc1", Instance: "inst1", Image: "repo/img:1$(id);--privileged", EnvDir: "/h/folders/svc1",
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
	want = append(want, "--env-file")
	slices.Sort(want)
	slices.Sort(flags)
	if !slices.Equal(flags, want) {
		t.Fatalf("flags = %q, want only %q", flags, want)
	}
	at := slices.Index(args[:image], "--env-file")
	if at < 0 || args[at+1] != EnvFilePath("harness-svc-x-y", c) {
		t.Fatalf("the env file is --env-file's one argument: %q", args)
	}
	for i, kv := range c.Env {
		k, v, _ := strings.Cut(kv, "=")
		for _, a := range args[:image] {
			if a == v || a == kv || a == k {
				t.Fatalf("env %q of entry %d is on the command line: %q", kv, i, args)
			}
		}
		if slices.Contains(RunEnv([]string{"PATH=/bin"}), kv) {
			t.Fatalf("env %q reaches the runtime's own environment", kv)
		}
	}
	if env := RunEnv([]string{"PATH=/bin"}); !slices.Equal(env, []string{"PATH=/bin"}) {
		t.Fatalf("the runtime's environment is the base only: %q", env)
	}
	if !slices.Contains(args, "127.0.0.1:5432:5432/tcp") || !slices.Contains(args, "127.0.0.1:53:5353/udp") {
		t.Fatalf("port mappings missing: %q", args)
	}
	for i, a := range args[:image] {
		if a == "-p" && !strings.HasPrefix(args[i+1], "127.0.0.1:") {
			t.Fatalf("a port is published beyond loopback: %q", args[i+1])
		}
	}
}

func TestRunArgsRefuseWhatValidationShouldHave(t *testing.T) {
	for _, c := range []ports.ServiceContainer{
		{Image: "-v"}, {Image: ""},
		{Image: "i", EnvDir: "/h", Env: []string{"--privileged=1"}},
		{Image: "i", Env: []string{"A=1"}},
		{Image: "i", Ports: []domain.ServicePort{{Host: 1, Container: 1, Protocol: "tcp -v /:/h"}}},
	} {
		if _, err := RunArgs("n", c); err == nil {
			t.Fatalf("%+v should be refused", c)
		}
	}
}

func TestDescribeNamesEnvKeysNeverValues(t *testing.T) {
	line := Describe("n", ports.ServiceContainer{Runtime: "podman", Image: "pg", EnvDir: "/h/f", Env: []string{"PASS=hunter2"}, Args: []string{"-c", "x"}})
	if strings.Contains(line, "hunter2") || strings.Contains(line, "PASS") || !strings.Contains(line, "--env-file /h/f/n.env pg -c x") || !strings.HasPrefix(line, "podman run ") {
		t.Fatal(line)
	}
}
