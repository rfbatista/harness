package domain

import "slices"

// The agent system settings (ADR 0011, the Glossary's "Sistema agêntico"): the
// person's configuration of the four layers, kept in one store with project
// defaults and per-repository overrides. EffectiveAgentSystem is what a
// repository runs on; every layer reads it and nothing else.

// AgentSystemSource is the level an effective value came from.
type AgentSystemSource string

const (
	SourceRepository AgentSystemSource = "repository"
	SourceProject    AgentSystemSource = "project"
	SourceHarness    AgentSystemSource = "harness"
)

// GateKind is one kind of gate (Glossary "Gate").
type GateKind string

const (
	GateBuild          GateKind = "build"
	GateTest           GateKind = "test"
	GateLint           GateKind = "lint"
	GateSecretScan     GateKind = "secret_scan"
	GateLockfile       GateKind = "lockfile"
	GateTypecheck      GateKind = "typecheck"
	GateSAST           GateKind = "sast"
	GateDependencyScan GateKind = "dependency_scan"
)

// RequiredGateKinds can never be removed or disabled at any level.
var RequiredGateKinds = []GateKind{GateBuild, GateTest, GateLint, GateSecretScan, GateLockfile}

// OptionalGateKinds may be added, changed and removed.
var OptionalGateKinds = []GateKind{GateTypecheck, GateSAST, GateDependencyScan}

// IsRequired says whether k is a required gate kind.
func (k GateKind) IsRequired() bool { return slices.Contains(RequiredGateKinds, k) }

// The harness defaults that do not depend on the stack.
const (
	DefaultGateTimeoutSeconds = 600
	DefaultRetryLimit         = 3
	MinRetryLimit             = 1
	MaxRetryLimit             = 10
)

// DefaultProtectedPaths are the paths an agent may not change: the gate
// scripts and the CI configuration. (The gates config itself lives in the
// harness store, outside the checkout.)
func DefaultProtectedPaths() []string {
	return []string{
		"check.sh", ".github/workflows/**", ".gitlab-ci.yml", ".circleci/**",
		".buildkite/**", "Jenkinsfile", "azure-pipelines.yml",
	}
}

// DefaultCriticStandards are what the critic reads, as sources.
func DefaultCriticStandards() []string { return []string{"AGENTS.md", "CLAUDE.md", "context map"} }

// AgentSystemField is one effective value with the level it came from. Reason
// says why for a harness default that was derived ("detected from go.mod").
type AgentSystemField[T any] struct {
	Value  T                 `json:"value"`
	Source AgentSystemSource `json:"source"`
	Reason string            `json:"reason,omitempty"`
}

// VisualQAUI is the UI a repository declares for visual QA.
type VisualQAUI struct {
	StartCommand string   `json:"start_command"`
	URL          string   `json:"url"`
	Routes       []string `json:"routes"`
	Viewports    []string `json:"viewports"` // "1280x800"
	UIPaths      []string `json:"ui_paths"`
}

// EffectiveGate is one gate kind as a repository runs it.
type EffectiveGate struct {
	Kind           GateKind                 `json:"kind"`
	Command        AgentSystemField[string] `json:"command"`
	TimeoutSeconds AgentSystemField[int]    `json:"timeout_seconds"`
}

type EffectiveToggle struct {
	Enabled AgentSystemField[bool] `json:"enabled"`
}

type EffectiveTaskMemory struct {
	RequireUpdate AgentSystemField[bool] `json:"require_update"`
}

type EffectiveLearn struct {
	ContextMap EffectiveToggle     `json:"context_map"`
	TaskMemory EffectiveTaskMemory `json:"task_memory"`
}

type EffectiveTestsFailFirst struct {
	Available AgentSystemField[bool] `json:"available"`
}

type EffectiveGuard struct {
	Gates          []EffectiveGate            `json:"gates"`
	ProtectedPaths AgentSystemField[[]string] `json:"protected_paths"`
	LockfileRule   EffectiveToggle            `json:"lockfile_rule"`
	RetryLimit     AgentSystemField[int]      `json:"retry_limit"`
	TestsFailFirst EffectiveTestsFailFirst    `json:"tests_fail_first"`
}

type EffectiveCritic struct {
	Enabled   AgentSystemField[bool]     `json:"enabled"`
	Model     AgentSystemField[string]   `json:"model"`
	Standards AgentSystemField[[]string] `json:"standards"`
}

type EffectiveVisualQA struct {
	UI AgentSystemField[*VisualQAUI] `json:"ui"`
}

type EffectiveReview struct {
	Critic   EffectiveCritic   `json:"critic"`
	VisualQA EffectiveVisualQA `json:"visual_qa"`
}

type EffectiveTrajectory struct {
	Default AgentSystemField[bool] `json:"default"`
}

type EffectiveScore struct {
	TrajectoryRecording EffectiveTrajectory `json:"trajectory_recording"`
}

// EffectiveAgentSystem is a repository's effective configuration: each field's
// value and source, and the required gates with no command at any level. It is
// a plain value: a layer takes it when a session starts and keeps it.
type EffectiveAgentSystem struct {
	Learn                EffectiveLearn  `json:"learn"`
	Guard                EffectiveGuard  `json:"guard"`
	Review               EffectiveReview `json:"review"`
	Score                EffectiveScore  `json:"score"`
	MissingRequiredGates []GateKind      `json:"missing_required_gates,omitempty"`
}

// GateDefault is a gate command the harness detected, with why.
type GateDefault struct {
	Command string
	Reason  string
}

// AgentSystemHarnessDefaults are the inputs to the resolver that the harness
// supplies: the gate commands detected from the stack, and the review-mode
// session model.
type AgentSystemHarnessDefaults struct {
	Gates       map[GateKind]GateDefault
	ReviewModel string
}

func harnessField[T any](v T) AgentSystemField[T] {
	return AgentSystemField[T]{Value: v, Source: SourceHarness}
}

// ResolveAgentSystem is the effective configuration for the harness defaults.
// It is a pure function of its inputs.
func ResolveAgentSystem(defaults AgentSystemHarnessDefaults) EffectiveAgentSystem {
	var out EffectiveAgentSystem
	out.Learn.ContextMap.Enabled = harnessField(true)
	out.Learn.TaskMemory.RequireUpdate = harnessField(true)
	out.Guard.ProtectedPaths = harnessField(DefaultProtectedPaths())
	out.Guard.LockfileRule.Enabled = harnessField(true)
	out.Guard.RetryLimit = harnessField(DefaultRetryLimit)
	out.Guard.TestsFailFirst.Available = harnessField(false)
	out.Review.Critic.Enabled = harnessField(true)
	out.Review.Critic.Model = harnessField(defaults.ReviewModel)
	out.Review.Critic.Standards = harnessField(DefaultCriticStandards())
	out.Review.VisualQA.UI = harnessField[*VisualQAUI](nil)
	out.Score.TrajectoryRecording.Default = harnessField(false)

	for _, kind := range RequiredGateKinds {
		d := defaults.Gates[kind]
		g := EffectiveGate{Kind: kind, Command: harnessField(d.Command), TimeoutSeconds: harnessField(DefaultGateTimeoutSeconds)}
		g.Command.Reason = d.Reason
		out.Guard.Gates = append(out.Guard.Gates, g)
		if d.Command == "" {
			out.MissingRequiredGates = append(out.MissingRequiredGates, kind)
		}
	}
	return out
}
