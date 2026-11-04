package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.GitIgnore = (*Code)(nil)

// IgnoredPaths asks git which of paths it ignores, all in one `check-ignore`
// call whatever their number. A path git tracks is not ignored.
func (c Code) IgnoredPaths(ctx context.Context, dir string, paths []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(paths) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.d.read())
	defer cancel()
	// check-ignore refuses --literal-pathspecs; the names arrive on stdin, never as pathspecs.
	cmd := exec.CommandContext(ctx, c.d.bin(), "-c", "core.quotePath=false", "-c", "core.fsmonitor=false", "--no-pager", "check-ignore", "-z", "--stdin")
	cmd.Dir = dir
	cmd.Env = localEnv("GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = waitDelay
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := runTimed(ctx, cmd); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return out, nil // none of them is ignored
		}
		return nil, fmt.Errorf("git check-ignore: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	for p := range strings.SplitSeq(stdout.String(), "\x00") {
		if p != "" {
			out[p] = true
		}
	}
	return out, nil
}
