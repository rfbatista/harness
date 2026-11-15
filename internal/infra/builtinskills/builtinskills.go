// Package builtinskills owns the skills embedded in the harness server and
// the session modes that carry them (ADR 0088). The set is read once per
// process; the agent catalog lists it and orchestration delivers it.
package builtinskills

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/domain"
)

//go:embed skills
var skillFS embed.FS

// modeSkillNames lists, per mode, the skill directories it adds. The first one
// is the skill the mode's first message names.
var modeSkillNames = map[domain.SessionMode][]string{
	domain.SessionModeArchitect: {"task-architecture", "task-grill-with-docs", "task-to-spec"},
	domain.SessionModeImplement: {"task-implement-spec", "task-tdd", "task-code-review"},
	domain.SessionModeDesign:    {"design-artifacts"},
	domain.SessionModeProduct:   {"task-product"},
	domain.SessionModeReview:    {"task-review", "task-code-review"},
	domain.SessionModeTest:      {"task-test", "task-tdd"},
	domain.SessionModeQA:        {"task-qa"},
	domain.SessionModeOperate:   {"task-operate", "task-run-app"},
}

// Skill is one embedded skill: its files, its description from the SKILL.md
// frontmatter, and the modes that carry it (never nil).
type Skill struct {
	Name        string
	Description string
	Files       []skill.File
	Modes       []string
}

var (
	once   sync.Once
	loaded map[string]Skill
	sorted []Skill
	err    error
)

func load() {
	loaded = map[string]Skill{}
	entries, e := skillFS.ReadDir("skills")
	if e != nil {
		err = e
		return
	}
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		files, e := readFiles(d.Name())
		if e != nil {
			err = fmt.Errorf("built-in skill %s: %w", d.Name(), e)
			return
		}
		s := Skill{Name: d.Name(), Files: files, Modes: []string{}}
		for _, f := range files {
			if f.Path == skill.FileName {
				doc, _ := skill.ParseDocument([]byte(f.Content))
				s.Description = doc.Description
			}
		}
		loaded[s.Name] = s
	}
	for mode, names := range modeSkillNames {
		for _, n := range names {
			s, ok := loaded[n]
			if !ok {
				err = fmt.Errorf("mode %s names the skill %s, which is not embedded", mode, n)
				return
			}
			s.Modes = append(s.Modes, string(mode))
			loaded[n] = s
		}
	}
	for _, s := range loaded {
		slices.Sort(s.Modes)
		loaded[s.Name] = s
		sorted = append(sorted, s)
	}
	slices.SortFunc(sorted, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
}

// readFiles reads every file of the skill directory skills/<name>, with
// SKILL.md required, so a skill's reference files ship along with it.
func readFiles(name string) ([]skill.File, error) {
	root := "skills/" + name
	if _, err := fs.Stat(skillFS, path.Join(root, skill.FileName)); err != nil {
		return nil, err
	}
	var files []skill.File
	err := fs.WalkDir(skillFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := skillFS.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, skill.File{Path: strings.TrimPrefix(p, root+"/"), Content: string(body)})
		return nil
	})
	return files, err
}

// All is every built-in skill, sorted by name. The slice and the files are
// shared: callers must not change them.
func All() []Skill {
	once.Do(load)
	return sorted
}

// Get is the built-in skill named name.
func Get(name string) (Skill, bool) {
	once.Do(load)
	s, ok := loaded[name]
	return s, ok
}

// ForMode is the skills a mode adds, in the mode's order.
func ForMode(mode domain.SessionMode) []Skill {
	once.Do(load)
	var out []Skill
	for _, n := range modeSkillNames[mode] {
		out = append(out, loaded[n])
	}
	return out
}

// ModeSkillNames is the skill names each mode adds, in order.
func ModeSkillNames() map[domain.SessionMode][]string { return modeSkillNames }

// Err is why the embedded set failed to load, if it did.
func Err() error {
	once.Do(load)
	return err
}
