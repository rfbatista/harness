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
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Dir       string `json:"dir"`
	// Worktree names the run worktree Dir belongs to.
	Worktree  RunWorktreeRef `json:"worktree"`
	Status    AppRunStatus   `json:"status"`
	ExitCode  int            `json:"exit_code"`
	StartedAt time.Time      `json:"started_at"`
	EndedAt   *time.Time     `json:"ended_at,omitempty"`
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

// RunWorktreeKind says whose worktree a run worktree is.
type RunWorktreeKind string

const (
	// RunWorktreeOwn is the worktree of the session that starts the run.
	RunWorktreeOwn RunWorktreeKind = "own"
	// RunWorktreeMain is the repository's main checkout.
	RunWorktreeMain RunWorktreeKind = "main"
	// RunWorktreeSession is another session's worktree of the same repository.
	RunWorktreeSession RunWorktreeKind = "session"
)

// RunWorktree is a directory a session can run its application in. Its Key
// is opaque to clients: "own", "main" or "session:<session id>".
type RunWorktree struct {
	Key  string          `json:"key"`
	Kind RunWorktreeKind `json:"kind"`
	// Branch is the branch checked out there; empty when detached.
	Branch string `json:"branch"`
	Dir    string `json:"dir"`
	// SessionID is set for the own and session kinds.
	SessionID string `json:"session_id,omitempty"`
	// Agent and SessionStatus are set for the session kind.
	Agent         string        `json:"agent,omitempty"`
	SessionStatus SessionStatus `json:"session_status,omitempty"`
}

// RunWorktreeRef is how a run names its run worktree.
type RunWorktreeRef struct {
	Key       string          `json:"key"`
	Kind      RunWorktreeKind `json:"kind"`
	Branch    string          `json:"branch"`
	SessionID string          `json:"session_id"`
}

// Ref is the run worktree as a run records it.
func (w RunWorktree) Ref() RunWorktreeRef {
	return RunWorktreeRef{Key: w.Key, Kind: w.Kind, Branch: w.Branch, SessionID: w.SessionID}
}
