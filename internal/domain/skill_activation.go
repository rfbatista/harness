package domain

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// A skill's activation rule lives in the spec-standard `metadata` map of its
// SKILL.md (ADR 0013's Learn layer), so the skill format stays harnesskit's:
//
//	metadata:
//	  activation_paths: "web/**/*.templ, internal/domain/*.go"
//	  activation_task_types: "implement, review"
const (
	SkillActivationPathsKey     = "activation_paths"
	SkillActivationTaskTypesKey = "activation_task_types"
)

// SkillActivation is a skill's activation rule: its body loads into a session
// the first time a touched file matches one of Paths, or at start when the
// session's task type is one of TaskTypes. The zero value is no rule.
type SkillActivation struct {
	Paths     []string
	TaskTypes []string
}

// Empty reports whether the skill has no activation rule.
func (a SkillActivation) Empty() bool { return len(a.Paths) == 0 && len(a.TaskTypes) == 0 }

// ParseSkillActivation reads the activation rule out of a skill's metadata.
// Metadata with no activation keys is no rule; an invalid value is an error
// naming the field.
func ParseSkillActivation(metadata map[string]string) (SkillActivation, error) {
	var a SkillActivation
	if raw, ok := metadata[SkillActivationPathsKey]; ok {
		paths := splitList(raw)
		if len(paths) == 0 {
			return a, fmt.Errorf("%s is empty: give at least one path pattern", SkillActivationPathsKey)
		}
		for _, p := range paths {
			if msg := activationGlobProblem(p); msg != "" {
				return a, fmt.Errorf("%s: %q %s", SkillActivationPathsKey, p, msg)
			}
		}
		a.Paths = paths
	}
	if raw, ok := metadata[SkillActivationTaskTypesKey]; ok {
		types := splitList(raw)
		if len(types) == 0 {
			return a, fmt.Errorf("%s is empty: give at least one task type", SkillActivationTaskTypesKey)
		}
		for _, t := range types {
			if !knownTaskType(t) {
				return a, fmt.Errorf("%s: unknown task type %q (one of %s)", SkillActivationTaskTypesKey, t, strings.Join(SkillTaskTypes(), ", "))
			}
		}
		a.TaskTypes = types
	}
	return a, nil
}

// SkillTaskTypes are the task types an activation rule can name: the session
// modes a task's sessions run in.
func SkillTaskTypes() []string {
	return []string{
		string(SessionModeArchitect), string(SessionModeImplement), string(SessionModeDesign),
		string(SessionModeProduct), string(SessionModeProductOwner), string(SessionModeDesignSystem),
		string(SessionModeReview), string(SessionModeTest), string(SessionModeQA), string(SessionModeOperate),
	}
}

func knownTaskType(t string) bool {
	for _, k := range SkillTaskTypes() {
		if k == t {
			return true
		}
	}
	return false
}

// MatchesTaskType reports whether a session in mode is one the rule names.
func (a SkillActivation) MatchesTaskType(mode SessionMode) bool {
	for _, t := range a.TaskTypes {
		if t == string(mode) {
			return true
		}
	}
	return false
}

// MatchesPath reports whether file (a slash path relative to the repository)
// matches one of the rule's path patterns. `**` stands for any number of
// directories; the rest is path.Match.
func (a SkillActivation) MatchesPath(file string) bool {
	segs := strings.Split(file, "/")
	for _, p := range a.Paths {
		if matchSegments(strings.Split(p, "/"), segs) {
			return true
		}
	}
	return false
}

func activationGlobProblem(p string) string {
	switch {
	case strings.HasPrefix(p, "/") || strings.Contains(p, "\\"):
		return "must be relative to the repository, with / separators"
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "must not contain .."
		}
		if _, err := path.Match(seg, ""); err != nil {
			return "is not a valid pattern: " + err.Error()
		}
	}
	return ""
}

func splitList(raw string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' }) {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// SkillActivationTypeProblem says what is wrong with the type of an
// activation key in the metadata of a SKILL.md: each must be one string (a
// comma-separated list), not a YAML list, number, boolean or mapping, which
// the metadata reader would drop or fail on without naming the field. "" when
// every activation key is a string or absent.
func SkillActivationTypeProblem(markdown []byte) string {
	text := strings.ReplaceAll(string(markdown), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return ""
	}
	block, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		return ""
	}
	var front struct {
		Metadata map[string]any `yaml:"metadata"`
	}
	if yaml.Unmarshal([]byte(block), &front) != nil {
		return ""
	}
	for _, key := range []string{SkillActivationPathsKey, SkillActivationTaskTypesKey} {
		v, present := front.Metadata[key]
		if _, isString := v.(string); present && !isString {
			return fmt.Sprintf("%s must be one string, a comma-separated list such as \"a, b\", not %s", key, yamlKind(v))
		}
	}
	return ""
}

func yamlKind(v any) string {
	switch v.(type) {
	case []any:
		return "a YAML list"
	case map[string]any:
		return "a YAML mapping"
	case bool:
		return "a boolean"
	case int, int64, uint64, float64:
		return "a number"
	case nil:
		return "an empty value"
	}
	return "a non-string value"
}
