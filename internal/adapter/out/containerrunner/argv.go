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

// RunArgs is the argument list of `<runtime> run` for c, named name: the
// flags the harness chooses, then the image reference, then the image's
// arguments. Every value is its own element, so nothing is parsed by a
// shell, and the only flags are the ones written here: no volume, mount,
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
		args = append(args, "-p", strconv.Itoa(p.Host)+":"+strconv.Itoa(p.Container)+"/"+p.Protocol)
	}
	for _, kv := range c.Env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || !domain.ValidEnvKey(k) {
			return nil, errors.New("an environment variable name is not usable")
		}
		args = append(args, "-e", kv)
	}
	args = append(args, c.Image)
	return append(args, c.Args...), nil
}

// Describe is the command line for the log: the runtime with RunArgs, an
// env entry shown by its name only.
func Describe(name string, c ports.ServiceContainer) string {
	args, err := RunArgs(name, c)
	if err != nil {
		return c.Runtime + " run (refused: " + err.Error() + ")"
	}
	out := []string{c.Runtime}
	for i := 0; i < len(args); i++ {
		out = append(out, args[i])
		if args[i] == "-e" && i+1 < len(args) {
			i++
			k, _, _ := strings.Cut(args[i], "=")
			out = append(out, k)
		}
	}
	return strings.Join(out, " ")
}
