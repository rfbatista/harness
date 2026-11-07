package containerrunner

import (
	"errors"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Labels the harness puts on every container it starts.
const (
	LabelService  = "harness.service"
	LabelInstance = "harness.instance"
)

// RunEnv is the environment of the `<runtime> run` command: the base the
// runtime needs and nothing of the container's own variables (a DOCKER_HOST
// among them would aim the runtime elsewhere). Those go in the file of
// WriteEnvFile, named by --env-file in RunArgs; a value is never on the
// command line, where any process of the machine could read it.
func RunEnv(base []string) []string {
	return append([]string(nil), base...)
}

// RunArgs is the argument list of `<runtime> run` for c, named name: the
// flags the harness chooses, then the image reference, then the image's
// arguments. Every value is its own element, so nothing is parsed by a
// shell, the variables are one file (`--env-file`, see WriteEnvFile), every
// port is published on loopback only, and the only flags are the ones written here: no volume, mount,
// privileged, host network or device flag exists for any input. The image
// reference comes after all flags and may not look like one.
func RunArgs(name string, c ports.ServiceContainer) ([]string, error) {
	if c.Image == "" || strings.HasPrefix(c.Image, "-") {
		return nil, errors.New("the image reference is not usable")
	}
	args := []string{
		"run", "--rm", "--name", name,
		"--label", LabelService + "=" + c.ServiceID,
		"--label", LabelInstance + "=" + c.Instance,
	}
	for _, p := range c.Ports {
		if p.Protocol != "tcp" && p.Protocol != "udp" {
			return nil, errors.New("port protocol must be tcp or udp")
		}
		args = append(args, "-p", "127.0.0.1:"+strconv.Itoa(p.Host)+":"+strconv.Itoa(p.Container)+"/"+p.Protocol)
	}
	for _, kv := range c.Env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || !domain.ValidEnvKey(k) {
			return nil, errors.New("an environment variable name is not usable")
		}
	}
	if len(c.Env) > 0 {
		if c.EnvDir == "" {
			return nil, errors.New("the folder for the environment file is missing")
		}
		args = append(args, "--env-file", EnvFilePath(name, c))
	}
	args = append(args, c.Image)
	return append(args, c.Args...), nil
}

// Describe is the command line for the log: the runtime with RunArgs, which
// names the env file, never a value.
func Describe(name string, c ports.ServiceContainer) string {
	args, err := RunArgs(name, c)
	if err != nil {
		return c.Runtime + " run (refused: " + err.Error() + ")"
	}
	out := []string{c.Runtime}
	out = append(out, args...)
	return strings.Join(out, " ")
}
