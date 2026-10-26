//go:build !windows

package archrules

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"

	"operators-mcp/internal/domain"
)

// ShellRunner runs command with sh in its own process group, so a timeout
// kills what the command started too.
func ShellRunner(ctx context.Context, dir string, env []string, command string, timeout time.Duration) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir, cmd.Env = dir, env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	b := out.Bytes()
	if len(b) > domain.MaxRuleEventDetail {
		b = b[:domain.MaxRuleEventDetail]
	}
	return string(b), err == nil
}
