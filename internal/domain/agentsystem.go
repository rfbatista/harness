package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

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

// AgentSystemValues are the values one level sets, by field path, each as the
// JSON it was saved as. A field a level does not set is absent.
type AgentSystemValues map[string]json.RawMessage

// AgentSystemLevel is one level's values, for the resolver.
type AgentSystemLevel struct {
	Source AgentSystemSource
	Values AgentSystemValues
}

// pickField is the value of path at the first level that sets it, else def.
func pickField[T any](path string, levels []AgentSystemLevel, def T) AgentSystemField[T] {
	for _, l := range levels {
		raw, ok := l.Values[path]
		if !ok {
			continue
		}
		var v T
		if json.Unmarshal(raw, &v) == nil { // stored values were validated; a bad one counts as unset
			return AgentSystemField[T]{Value: v, Source: l.Source}
		}
	}
	return harnessField(def)
}

// ResolveAgentSystem is the effective configuration: per field, the first of
// levels (highest priority first) that sets it, else the harness default. It
// is a pure function of its inputs.
func ResolveAgentSystem(defaults AgentSystemHarnessDefaults, levels ...AgentSystemLevel) EffectiveAgentSystem {
	var out EffectiveAgentSystem
	out.Learn.ContextMap.Enabled = pickField(FieldContextMapEnabled, levels, true)
	out.Learn.TaskMemory.RequireUpdate = pickField(FieldTaskMemoryRequireUpdate, levels, true)
	out.Guard.ProtectedPaths = pickField(FieldProtectedPaths, levels, DefaultProtectedPaths())
	out.Guard.LockfileRule.Enabled = pickField(FieldLockfileRuleEnabled, levels, true)
	out.Guard.RetryLimit = pickField(FieldRetryLimit, levels, DefaultRetryLimit)
	out.Guard.TestsFailFirst.Available = pickField(FieldTestsFailFirstAvailable, levels, false)
	out.Review.Critic.Enabled = pickField(FieldCriticEnabled, levels, true)
	out.Review.Critic.Model = pickField(FieldCriticModel, levels, defaults.ReviewModel)
	out.Review.Critic.Standards = pickField(FieldCriticStandards, levels, DefaultCriticStandards())
	out.Review.VisualQA.UI = pickField[*VisualQAUI](FieldVisualQAUI, levels, nil)
	out.Score.TrajectoryRecording.Default = pickField(FieldTrajectoryRecordingDefault, levels, false)

	for _, kind := range append(slices.Clone(RequiredGateKinds), OptionalGateKinds...) {
		d := defaults.Gates[kind]
		g := EffectiveGate{
			Kind:           kind,
			Command:        pickField(GateField(kind, GateAttrCommand), levels, d.Command),
			TimeoutSeconds: pickField(GateField(kind, GateAttrTimeout), levels, DefaultGateTimeoutSeconds),
		}
		if g.Command.Source == SourceHarness {
			g.Command.Reason = d.Reason
		}
		if kind.IsRequired() {
			out.Guard.Gates = append(out.Guard.Gates, g)
			if g.Command.Value == "" {
				out.MissingRequiredGates = append(out.MissingRequiredGates, kind)
			}
			continue
		}
		if g.Command.Value != "" && pickField(GateField(kind, GateAttrEnabled), levels, true).Value {
			out.Guard.Gates = append(out.Guard.Gates, g)
		}
	}
	return out
}

// The field paths of the contract's schema.
const (
	FieldContextMapEnabled          = "learn.context_map.enabled"
	FieldTaskMemoryRequireUpdate    = "learn.task_memory.require_update"
	FieldProtectedPaths             = "guard.protected_paths"
	FieldLockfileRuleEnabled        = "guard.lockfile_rule.enabled"
	FieldRetryLimit                 = "guard.retry_limit"
	FieldTestsFailFirstAvailable    = "guard.tests_fail_first.available"
	FieldCriticEnabled              = "review.critic.enabled"
	FieldCriticModel                = "review.critic.model"
	FieldCriticStandards            = "review.critic.standards"
	FieldVisualQAUI                 = "review.visual_qa.ui"
	FieldTrajectoryRecordingDefault = "score.trajectory_recording.default"
)

// The attributes of a gate a level can set, under "guard.gates.<kind>.".
const (
	GateAttrCommand = "command"
	GateAttrTimeout = "timeout_seconds"
	GateAttrEnabled = "enabled"
)

const gateFieldPrefix = "guard.gates."

// GateField is the field path of one attribute of one gate kind.
func GateField(kind GateKind, attr string) string { return gateFieldPrefix + string(kind) + "." + attr }

// gateField reads a gate field path; ok is false when path is not one of a
// known kind and attribute.
func gateField(path string) (kind GateKind, attr string, ok bool) {
	rest, found := strings.CutPrefix(path, gateFieldPrefix)
	if !found {
		return "", "", false
	}
	k, attr, found := strings.Cut(rest, ".")
	kind = GateKind(k)
	if !found || !(kind.IsRequired() || slices.Contains(OptionalGateKinds, kind)) {
		return "", "", false
	}
	return kind, attr, attr == GateAttrCommand || attr == GateAttrTimeout || attr == GateAttrEnabled
}

// checkGateField validates one gate attribute of kind.
func checkGateField(kind GateKind, attr string, raw json.RawMessage) (json.RawMessage, *AgentSystemFieldError) {
	switch attr {
	case GateAttrTimeout:
		return checkIntRange(1, 1<<31-1)(raw)
	case GateAttrEnabled:
		canon, e := checkBool(raw)
		if e == nil && string(canon) == "false" && kind.IsRequired() {
			return nil, &AgentSystemFieldError{Code: CodeRequiredGateDisabled, Message: string(kind) + " is a required gate and cannot be disabled"}
		}
		return canon, e
	}
	canon, e := checkString(raw)
	if e != nil {
		return nil, e
	}
	var cmd string
	_ = json.Unmarshal(canon, &cmd)
	if strings.TrimSpace(cmd) == "" {
		if kind.IsRequired() {
			return nil, &AgentSystemFieldError{Code: CodeRequiredGateMissing, Message: string(kind) + " is a required gate and needs a command"}
		}
		return nil, &AgentSystemFieldError{Code: CodeOutOfRange, Message: "the command must not be empty; reset it to remove the gate"}
	}
	return canon, nil
}

// Codes of a refused save (contract "REST API").
const (
	CodeRequiredGateMissing  = "REQUIRED_GATE_MISSING"
	CodeRequiredGateDisabled = "REQUIRED_GATE_DISABLED"
	CodeInvalidGlob          = "INVALID_GLOB"
	CodeModelUnavailable     = "MODEL_UNAVAILABLE"
	CodeInvalidRoute         = "INVALID_ROUTE"
	CodeOutOfRange           = "OUT_OF_RANGE"
	CodeUnknownField         = "UNKNOWN_FIELD"
)

// AgentSystemFieldError is one refusal of a save, naming the field.
type AgentSystemFieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AgentSystemValidationError is a refused save: nothing was stored.
type AgentSystemValidationError struct{ Errors []AgentSystemFieldError }

func (e *AgentSystemValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "invalid agent system settings"
	}
	return fmt.Sprintf("invalid agent system settings: %s: %s", e.Errors[0].Field, e.Errors[0].Message)
}

// fieldChecks validate one field's value and answer the canonical JSON to
// store. A wrong type is OUT_OF_RANGE: the contract has no other code for a
// value that does not fit the field.
var fieldChecks = map[string]func(raw json.RawMessage) (json.RawMessage, *AgentSystemFieldError){
	FieldContextMapEnabled:          checkBool,
	FieldTaskMemoryRequireUpdate:    checkBool,
	FieldProtectedPaths:             checkStringList,
	FieldLockfileRuleEnabled:        checkBool,
	FieldRetryLimit:                 checkIntRange(MinRetryLimit, MaxRetryLimit),
	FieldTestsFailFirstAvailable:    checkBool,
	FieldCriticEnabled:              checkBool,
	FieldCriticModel:                checkString,
	FieldCriticStandards:            checkStringList,
	FieldVisualQAUI:                 checkVisualQAUI,
	FieldTrajectoryRecordingDefault: checkBool,
}

// ValidateAgentSystemPatch checks a save: every field is a field of the
// schema and its value fits. A null value resets the field. It answers what to
// store and what to remove, or an *AgentSystemValidationError naming every
// refused field.
func ValidateAgentSystemPatch(patch map[string]json.RawMessage) (set AgentSystemValues, reset []string, err error) {
	set = AgentSystemValues{}
	var bad []AgentSystemFieldError
	paths := make([]string, 0, len(patch))
	for p := range patch {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, path := range paths {
		raw := patch[path]
		check, known := fieldChecks[path]
		if kind, attr, ok := gateField(path); ok {
			check, known = func(raw json.RawMessage) (json.RawMessage, *AgentSystemFieldError) {
				return checkGateField(kind, attr, raw)
			}, true
		}
		if !known {
			bad = append(bad, AgentSystemFieldError{Field: path, Code: CodeUnknownField, Message: "no such setting"})
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			reset = append(reset, path)
			continue
		}
		canon, e := check(raw)
		if e != nil {
			e.Field = path
			bad = append(bad, *e)
			continue
		}
		set[path] = canon
	}
	if len(bad) > 0 {
		return nil, nil, &AgentSystemValidationError{Errors: bad}
	}
	return set, reset, nil
}

func wrongType(want string) *AgentSystemFieldError {
	return &AgentSystemFieldError{Code: CodeOutOfRange, Message: "expected " + want}
}

func checkBool(raw json.RawMessage) (json.RawMessage, *AgentSystemFieldError) {
	var v bool
	if json.Unmarshal(raw, &v) != nil {
		return nil, wrongType("true or false")
	}
	return canonical(v)
}

func checkString(raw json.RawMessage) (json.RawMessage, *AgentSystemFieldError) {
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return nil, wrongType("a string")
	}
	return canonical(v)
}

func checkStringList(raw json.RawMessage) (json.RawMessage, *AgentSystemFieldError) {
	var v []string
	if json.Unmarshal(raw, &v) != nil {
		return nil, wrongType("a list of strings")
	}
	if v == nil {
		v = []string{}
	}
	return canonical(v)
}

func checkIntRange(lo, hi int) func(json.RawMessage) (json.RawMessage, *AgentSystemFieldError) {
	return func(raw json.RawMessage) (json.RawMessage, *AgentSystemFieldError) {
		var v int
		if json.Unmarshal(raw, &v) != nil {
			return nil, &AgentSystemFieldError{Code: CodeOutOfRange, Message: fmt.Sprintf("expected a whole number from %d to %d", lo, hi)}
		}
		if v < lo || v > hi {
			return nil, &AgentSystemFieldError{Code: CodeOutOfRange, Message: fmt.Sprintf("must be from %d to %d", lo, hi)}
		}
		return canonical(v)
	}
}

func checkVisualQAUI(raw json.RawMessage) (json.RawMessage, *AgentSystemFieldError) {
	var v VisualQAUI
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&v) != nil {
		return nil, wrongType("an object with start_command, url, routes, viewports and ui_paths")
	}
	return canonical(v)
}

// AgentSystemView is one level as the settings UI reads it: what the level
// sets itself, and the effective configuration with every source.
type AgentSystemView struct {
	Effective EffectiveAgentSystem `json:"effective"`
	Values    AgentSystemValues    `json:"values"`
}

// AgentSystemSettingsChanged: agent system settings were saved. RepositoryID
// is empty for a project-level save. A layer's cache or a UI follows it.
type AgentSystemSettingsChanged struct {
	ProjectID    string
	RepositoryID string
}

// EventName implements Event.
func (AgentSystemSettingsChanged) EventName() string { return "agent_system.settings_changed" }

// canonical is v as the JSON that is stored.
func canonical(v any) (json.RawMessage, *AgentSystemFieldError) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, wrongType("a value that can be stored")
	}
	return raw, nil
}
