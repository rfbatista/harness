package domain

import (
	"slices"
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
