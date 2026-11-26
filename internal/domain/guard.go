package domain

import "slices"

// The Guard layer's view of a session (ADR 0012, 0014): the guard part of the
// effective agent system configuration, taken once when the session starts or
// resumes and kept with it, and the rule of which sessions are gated.

// SessionGated says whether a session in mode runs the gates: implement and
// test sessions, and a mode-less session with a diff against its base. Every
// other mode is never gated.
func SessionGated(mode SessionMode, hasDiff bool) bool {
	switch mode {
	case SessionModeImplement, SessionModeTest:
		return true
	case SessionModeDefault:
		return hasDiff
	default:
		return false
	}
}

// GuardGate is one gate a session runs, as kept at its start.
type GuardGate struct {
	Kind           GateKind `json:"kind"`
	Command        string   `json:"command"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// GuardConfig is what a session keeps of the guard configuration: a plain
// value no later save reaches. Scope lists the protected globs the architect
// unlocked for this session alone.
type GuardConfig struct {
	Mode                    SessionMode `json:"mode"`
	Gates                   []GuardGate `json:"gates"`
	MissingRequiredGates    []GateKind  `json:"missing_required_gates,omitempty"`
	ProtectedPaths          []string    `json:"protected_paths"`
	LockfileRule            bool        `json:"lockfile_rule"`
	RetryLimit              int         `json:"retry_limit"`
	TestsFailFirstAvailable bool        `json:"tests_fail_first_available"`
	Scope                   []string    `json:"scope,omitempty"`
	// Unresolved is why the settings store could not give this session a
	// configuration; the session runs as before and is not gated.
	Unresolved string `json:"unresolved,omitempty"`
}

// Gated says whether the session runs the gates under this configuration;
// hasDiff is whether a mode-less session has a diff against its base.
func (c GuardConfig) Gated(hasDiff bool) bool {
	return c.Unresolved == "" && SessionGated(c.Mode, hasDiff)
}

// NewGuardConfig keeps the guard part of eff for a session in mode, sharing no
// memory with it.
func NewGuardConfig(eff EffectiveAgentSystem, mode SessionMode) GuardConfig {
	g := eff.Guard
	cfg := GuardConfig{
		Mode:                    mode,
		MissingRequiredGates:    slices.Clone(eff.MissingRequiredGates),
		ProtectedPaths:          slices.Clone(g.ProtectedPaths.Value),
		LockfileRule:            g.LockfileRule.Enabled.Value,
		RetryLimit:              g.RetryLimit.Value,
		TestsFailFirstAvailable: g.TestsFailFirst.Available.Value,
	}
	for _, gate := range g.Gates {
		cfg.Gates = append(cfg.Gates, GuardGate{Kind: gate.Kind, Command: gate.Command.Value, TimeoutSeconds: gate.TimeoutSeconds.Value})
	}
	return cfg
}
