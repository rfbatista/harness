// Package vshell is what makes an agent's process a shell on a session's
// virtual file system (ADR 0050): the edit proxy's hooks, the env that names
// the folder, and the start and stop of the agent's shell around the
// session. It depends on the virtual file system through the ports it
// serves, never the reverse.
package vshell

import (
	"context"
	"fmt"

	"operators-mcp/internal/ports"
)

// Shell is an agent's virtual shell: its resources root's folder, made and
// filled, until Stop.
type Shell struct {
	// Dir is the resources root's absolute path: what the agent's
	// $HARNESS_SHELL names and its process is given with --add-dir.
	Dir string

	stop func()
}

// Start makes the session's resources root on its virtual file system and
// answers the shell on it.
func Start(ctx context.Context, roots ports.ResourcesRoots, sessionID string, scope ports.ShellScope) (*Shell, error) {
	dir, err := roots.Open(ctx, sessionID, scope)
	if err != nil {
		return nil, fmt.Errorf("make the session's virtual shell: %w", err)
	}
	return &Shell{Dir: dir, stop: func() { roots.Close(sessionID, dir) }}, nil
}

// Env is the environment the agent's process names its folder by.
func (s *Shell) Env() []string { return []string{ports.ShellDirEnv + "=" + s.Dir} }

// Stop ends the shell: its resources root is closed.
func (s *Shell) Stop() { s.stop() }
