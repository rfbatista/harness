package gitcli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// localOverrides keep a git command that works on a checkout from ever
// authenticating, should it reach a remote (a lazy fetch of a partial clone,
// say): every credential helper is reset at command-line precedence, ssh
// gets no config, agent or key, and nothing prompts. The machine's other
// config still applies, so output (ignored files, quoting) is as the person
// sees it. Remote operations use the harness's own credentials instead (see
// AuthBroker).
var localOverrides = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_ASKPASS=", "SSH_ASKPASS=", "GIT_SSH=", "SSH_AUTH_SOCK=",
	"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
	"GIT_SSH_COMMAND=ssh -F /dev/null -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes -o IdentityFile=/dev/null",
}

// localEnv is this process's environment with localOverrides, then extra.
func localEnv(extra ...string) []string {
	env := append(os.Environ(), localOverrides...)
	return append(env, extra...)
}

// gitCmd is git args in dir ("" keeps the working directory) with localEnv,
// killed when ctx ends. Its output pipes are let go waitDelay after the kill.
func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = localEnv()
	cmd.WaitDelay = waitDelay
	return cmd
}

// startupOutput is the output of a git run made once while the server starts,
// bounded by DefaultReadDeadline.
func startupOutput(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultReadDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.WaitDelay = waitDelay
	out, err := cmd.Output()
	return out, cut(ctx, args[0], err)
}

// MachineExcludesFile is the machine's global core.excludesFile, with ~
// expanded; "" when it has none. It is a path, not a credential: sessions
// keep ignoring what the person ignores everywhere.
func MachineExcludesFile() string {
	out, err := startupOutput("config", "--global", "--path", "--get", "core.excludesFile")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// minGit is the oldest git with GIT_CONFIG_GLOBAL and GIT_CONFIG_COUNT,
// which the harness's credential isolation relies on.
var minGit = [2]int{2, 32}

// CheckVersion is an error when git is missing or older than 2.32.
func CheckVersion() error {
	out, err := startupOutput("version")
	if err != nil {
		return fmt.Errorf("git version: %w", err)
	}
	// "git version 2.50.1 (Apple Git-155)"
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return fmt.Errorf("git version: unexpected %q", out)
	}
	parts := strings.SplitN(fields[2], ".", 3)
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < minGit[0] || major == minGit[0] && minor < minGit[1] {
		return fmt.Errorf("git %s is older than %d.%d: stored git credentials cannot be isolated from the machine's", fields[2], minGit[0], minGit[1])
	}
	return nil
}
