package ports

import (
	"context"
	"time"
)

// A person's terminal is a virtual shell opened on a folder of a session's
// virtual file system, as the person: their login shell, their environment
// minus the harness's (ADR 0050, ADR 0052). See "Contract: Harness server ↔
// Web UI — person's terminals".

// PersonTerminal is one open terminal of a person.
type PersonTerminal struct {
	// ID is in its own namespace (prefix pt_), never a session or run id.
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	// Root is "worktree" or "resources".
	Root string `json:"root"`
	// Dir is the folder, relative to the root; "" is the root itself.
	Dir string `json:"dir"`
	// Label names the terminal's folder for its tab.
	Label    string    `json:"label"`
	State    string    `json:"state"`
	OpenedAt time.Time `json:"opened_at"`
}

// PersonTerminalRunning is the state of every listed terminal: one that
// ended is not listed.
const PersonTerminalRunning = "running"

// PersonTerminalOpen is what a person asks to open.
type PersonTerminalOpen struct {
	Root string
	Dir  string
	Size TermSize
}

// PersonTerminals opens and keeps the person's terminals of sessions.
type PersonTerminals interface {
	// Open starts the person's shell on the folder. Refusals are
	// SESSION_NOT_FOUND, INVALID_INPUT, ROOT_GONE, PATH_NOT_FOUND and
	// TERMINAL_READ_ONLY_UNAVAILABLE.
	Open(ctx context.Context, sessionID string, req PersonTerminalOpen) (PersonTerminal, error)
	// List is the session's open terminals, oldest first; SESSION_NOT_FOUND
	// for an unknown session.
	List(ctx context.Context, sessionID string) ([]PersonTerminal, error)
	// Attach returns the terminal's screen to attach to, or
	// TERMINAL_NOT_FOUND.
	Attach(id string) (Terminal, error)
	// Close hangs up the shell. A terminal that is not there is not an error.
	Close(id string) error
}

// PersonTerminalKind is the AgentSpec.Kind a terminal host's launcher serves
// for a person's shell.
const PersonTerminalKind = "person"
