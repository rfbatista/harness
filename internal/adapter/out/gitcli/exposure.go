package gitcli

import (
	"context"
	"errors"
	"os/exec"
)

// HomeExposedToGit says dir lies inside a git work tree where git does not
// ignore it, so a plaintext login kept there could be committed. It runs git
// ("" is "git" from PATH) once, bounded by DefaultReadDeadline; any failure
// (no git, a missing dir, a timeout) is false, never a warning.
func HomeExposedToGit(git, dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultReadDeadline)
	defer cancel()
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, gitBin(git), append([]string{"-C", dir}, args...)...)
		cmd.WaitDelay = waitDelay
		return cmd.Run()
	}
	if run("rev-parse", "--show-toplevel") != nil {
		return false // outside any work tree, or git failed
	}
	// check-ignore exits 0 when ignored, 1 when not, 128 on error.
	err := run("check-ignore", "-q", ".")
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == 1
}
