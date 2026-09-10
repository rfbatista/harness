package domain

// Skills are implemented by github.com/rfbatista/harnesskit/skill, which owns
// the Agent Skills format (https://agentskills.io/specification).
//
// The declarations below are type aliases, not new types: domain.Skill and
// skill.Skill are the same type, so every existing construction site, method
// call, interface implementation and JSON tag keeps working untouched. Adding a
// field or method to one of these types therefore means changing harnesskit,
// not this file.

import (
	"github.com/rfbatista/harnesskit/skill"
)

type (
	// Skill is a reusable capability attached to agents, owning a directory
	// tree with a required root SKILL.md.
	Skill = skill.Skill
	// SkillFile is one node in a skill's tree.
	SkillFile = skill.File
	// SkillInput holds persistable skill fields for create and update.
	SkillInput = skill.Input
	// SkillPublishState is the persisted publishing state of a skill.
	SkillPublishState = skill.PublishState
	// SkillDocument is the parsed content of a SKILL.md.
	SkillDocument = skill.Document
	// SkillInspection is the result of inspecting an on-disk skill.
	SkillInspection = skill.Inspection
	// SkillPathValidation is the result of validating a path before create.
	SkillPathValidation = skill.PathValidation
	// ValidationIssue describes a skill validation warning or error.
	ValidationIssue = skill.ValidationIssue
)

// SkillFileName is the required name of a skill's root document.
const SkillFileName = skill.FileName

// The wrappers below keep this package's existing call sites compiling. They
// are plain functions rather than `var F = skill.F` so that go doc and IDE
// navigation still show them as functions.

func NormalizeSkillPath(p string) (string, bool)            { return skill.NormalizePath(p) }
func NormalizeSkillFiles(f []SkillFile) []SkillFile         { return skill.NormalizeFiles(f) }
func ValidateSkillFiles(f []SkillFile) []ValidationIssue    { return skill.ValidateFiles(f) }
func RenamedSkillPath(p, from, to string) (string, bool)    { return skill.RenamedPath(p, from, to) }
func RootSkillDocument(f []SkillFile) (SkillDocument, bool) { return skill.RootDocument(f) }
func SkillResourcesFromFiles(f []SkillFile) []string        { return skill.ResourcesFromFiles(f) }
func SkillMarkdownFor(name, description, body string) string {
	return skill.MarkdownFor(name, description, body)
}
func ImportSkillTreeFromPath(path string) ([]SkillFile, error) {
	return skill.ImportTreeFromPath(path)
}

func ParseSkillDocument(markdown []byte) (SkillDocument, error) { return skill.ParseDocument(markdown) }
func SkillRootDir(path string) (string, error)                  { return skill.RootDir(path) }

func ValidateSkillName(name, dirName string) []ValidationIssue {
	return skill.ValidateName(name, dirName)
}
func ValidateSkillDescription(description string) []ValidationIssue {
	return skill.ValidateDescription(description)
}
func ValidateSkillDocument(doc SkillDocument, skillRoot string) []ValidationIssue {
	return skill.ValidateDocument(doc, skillRoot)
}
func HasValidationErrors(issues []ValidationIssue) bool      { return skill.HasErrors(issues) }
func ListSkillResources(skillRoot string) ([]string, error)  { return skill.ListResources(skillRoot) }
func InspectSkillPath(path string) (*SkillInspection, error) { return skill.InspectPath(path) }
func ValidateSkillPathForCreate(path string) (*SkillPathValidation, error) {
	return skill.ValidatePathForCreate(path)
}

func ExpandSkillPath(path string) (string, error)       { return skill.ExpandPath(path) }
func ValidateSkillPath(path string) error               { return skill.ValidatePath(path) }
func SkillMarkdownPath(path string) (string, error)     { return skill.MarkdownPath(path) }
func ResolveSkillPluginDir(path string) (string, error) { return skill.ResolvePluginDir(path) }
func LoadSkillFromPath(path string) (Skill, error)      { return skill.LoadFromPath(path) }

func ExpandUserPath(path string) string          { return skill.ExpandUserPath(path) }
func SkillFileBytes(f SkillFile) ([]byte, error) { return skill.FileBytes(f) }
func WriteSkillTree(destDir string, files []SkillFile) error {
	return skill.WriteTree(destDir, files)
}

func HydrateSkill(stored *Skill) *Skill { return skill.Hydrate(stored) }

// NormalizeSkillPathEquals reports whether a raw path normalizes to want.
func NormalizeSkillPathEquals(raw, want string) bool { return skill.NormalizePathEquals(raw, want) }
