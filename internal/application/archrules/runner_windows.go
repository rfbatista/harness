//go:build windows

package archrules

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"operators-mcp/internal/domain"
)

// ShellRunner runs command with cmd.exe.
func ShellRunner(ctx context.Context, dir string, env []string, command string, timeout time.Duration) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "cmd", "/C", command)
	cmd.Dir, cmd.Env = dir, env
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
