package domain

import (
	"regexp"
	"strings"
	"time"
)

// RunCommand is a named way to run a repository's application, such as
// "server: make air": saved per repository, run from any of its sessions'
// worktrees.
type RunCommand struct {
	RepositoryID string    `json:"repository_id"`
	Name         string    `json:"name"`
	Command      string    `json:"command"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AppRunStatus is where a run of the application is.
type AppRunStatus string

const (
	AppRunRunning AppRunStatus = "running"
	AppRunExited  AppRunStatus = "exited"
	AppRunStopped AppRunStatus = "stopped"
)

// AppRun is one run of a command in a session's worktree, in a terminal on
// the server. Runs live as long as the server does.
type AppRun struct {
	ID        string       `json:"id"`
	SessionID string       `json:"session_id"`
	Name      string       `json:"name"`
	Command   string       `json:"command"`
	Dir       string       `json:"dir"`
	Status    AppRunStatus `json:"status"`
	ExitCode  int          `json:"exit_code"`
	StartedAt time.Time    `json:"started_at"`
	EndedAt   *time.Time   `json:"ended_at,omitempty"`
}

// MaxRunCommandLength bounds a command line.
const MaxRunCommandLength = 4096

var runCommandName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,39}$`)

// CleanRunCommand validates a saved command's name and command line and
// returns them trimmed: a short name (letters, digits, space . _ -) and a
// non-empty command.
func CleanRunCommand(name, command string) (string, string, error) {
	name = strings.TrimSpace(name)
	command = strings.TrimSpace(command)
	if !runCommandName.MatchString(name) {
		return "", "", &StructuredError{Code: "INVALID_NAME", Message: "a run command needs a short name: letters, digits, spaces, . _ - (up to 40)"}
	}
	if command == "" {
		return "", "", &StructuredError{Code: "INVALID_INPUT", Message: "the command to run is required"}
	}
	if len(command) > MaxRunCommandLength {
		return "", "", &StructuredError{Code: "INVALID_INPUT", Message: "the command is too long"}
	}
	return name, command, nil
}
