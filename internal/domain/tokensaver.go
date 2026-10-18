package domain

import (
	"slices"
	"strings"
)

// TokenSaver is a third-party tool the harness wires into agent sessions to
// cut the tokens they spend (see the Glossary's "Token savers").
type TokenSaver string

// The token savers the harness knows.
const (
	TokenSaverRTK       TokenSaver = "rtk"
	TokenSaverCaveman   TokenSaver = "caveman"
	TokenSaverCodegraph TokenSaver = "codegraph"
)

// TokenSavers lists every token saver, in the order the pages show them.
var TokenSavers = []TokenSaver{TokenSaverRTK, TokenSaverCaveman, TokenSaverCodegraph}

// ParseTokenSaver is INVALID_INPUT for a name that is not a token saver.
func ParseTokenSaver(s string) (TokenSaver, error) {
	if !slices.Contains(TokenSavers, TokenSaver(s)) {
		return "", &StructuredError{Code: "INVALID_INPUT", Message: "unknown token saver " + s + ": use rtk, caveman or codegraph"}
	}
	return TokenSaver(s), nil
}

// ActivationLevel is where an activation is set.
type ActivationLevel string

// The levels, from the least specific.
const (
	ActivationLevelProject    ActivationLevel = "project"
	ActivationLevelRepository ActivationLevel = "repository"
	ActivationLevelTask       ActivationLevel = "task"
)

// ParseActivationLevel is INVALID_INPUT for anything but project, repository
// or task.
func ParseActivationLevel(s string) (ActivationLevel, error) {
	switch l := ActivationLevel(s); l {
	case ActivationLevelProject, ActivationLevelRepository, ActivationLevelTask:
		return l, nil
	}
	return "", &StructuredError{Code: "INVALID_INPUT", Message: "unknown activation level " + s + ": use project, repository or task"}
}

// ActivationValue is one level's setting of a token saver. A project is on
// or off; a repository or task may also inherit.
type ActivationValue string

// The values. A level with nothing stored reads as ActivationInherit, and a
// project with nothing stored as ActivationOff.
const (
	ActivationInherit ActivationValue = "inherit"
	ActivationOn      ActivationValue = "on"
	ActivationOff     ActivationValue = "off"
)

// ParseActivationValue is INVALID_INPUT for a value the level does not take:
// a project cannot inherit.
func ParseActivationValue(level ActivationLevel, s string) (ActivationValue, error) {
	switch v := ActivationValue(s); v {
	case ActivationOn, ActivationOff:
		return v, nil
	case ActivationInherit:
		if level != ActivationLevelProject {
			return v, nil
		}
		return "", &StructuredError{Code: "INVALID_INPUT", Message: "a project's token saver is on or off; it has nothing to inherit from"}
	}
	return "", &StructuredError{Code: "INVALID_INPUT", Message: "unknown activation value " + s + ": use inherit, on or off"}
}

// ActivationOwner is the project, repository or task an activation belongs to.
type ActivationOwner struct {
	Level ActivationLevel `json:"level"`
	ID    string          `json:"owner_id"`
}

// Activation is one stored setting of one token saver at one level.
// ProjectID is the owner's project, so a deleted project takes the
// activations of its repositories and tasks with it.
type Activation struct {
	Saver     TokenSaver
	Owner     ActivationOwner
	ProjectID string
	Value     ActivationValue
}

// Resolution is what a token saver resolves to and from which level. For a
// task's activation, RepositoryID and RepositoryName say which repository's
// sessions it is for (empty when the project has none).
type Resolution struct {
	Value          ActivationValue `json:"value"`
	Level          ActivationLevel `json:"level"`
	RepositoryID   string          `json:"repository_id,omitempty"`
	RepositoryName string          `json:"repository_name,omitempty"`
}

// TokenSaverActivation is one token saver as an owner's page shows it: its
// own value there, what it resolves to, and that said in one line
// (Summarize).
type TokenSaverActivation struct {
	Saver    TokenSaver      `json:"saver"`
	Value    ActivationValue `json:"value"`
	Resolved []Resolution    `json:"resolved"`
	Summary  string          `json:"summary"`
}

// Summarize says the activation in one line: "inherit — resolves on
// (project)", or for a task whose repositories resolve differently "inherit
// — resolves off (repository) in api; on (project) in web".
func (a TokenSaverActivation) Summarize() string {
	line := string(a.Value) + " — resolves "
	if len(a.Resolved) == 0 {
		return line + string(ActivationOff) + " (" + string(ActivationLevelProject) + ")"
	}
	first := a.Resolved[0]
	same := true
	for _, r := range a.Resolved[1:] {
		same = same && r.Value == first.Value && r.Level == first.Level
	}
	if same {
		return line + string(first.Value) + " (" + string(first.Level) + ")"
	}
	parts := make([]string, 0, len(a.Resolved))
	for _, r := range a.Resolved {
		parts = append(parts, string(r.Value)+" ("+string(r.Level)+") in "+r.RepositoryName)
	}
	return line + strings.Join(parts, "; ")
}

// ResolvedTokenSaver is one token saver resolved for a session: on or off,
// and the level that decided it.
type ResolvedTokenSaver struct {
	Saver TokenSaver
	Value ActivationValue
	Level ActivationLevel
}
