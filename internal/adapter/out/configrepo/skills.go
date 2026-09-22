package configrepo

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rfbatista/harnesskit/skill"
)

// SkillStore implements skill.Store (ports.SkillRepository) by reading skill
// directories live from disk on every call — Root is normally
// "<DotfilesAgentsDir>/skills". Create/Update/Delete always fail: the files
// are the source of truth, not a store this process writes to.
type SkillStore struct {
	Root string
}

// NewSkillStore returns a SkillStore reading skill directories under root.
func NewSkillStore(root string) *SkillStore {
	return &SkillStore{Root: root}
}

// List re-reads every skill directory under Root, in name order.
func (s *SkillStore) List() []*skill.Skill {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil
	}
	var out []*skill.Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if sk, ok := s.load(e.Name()); ok {
			out = append(out, sk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get re-reads one skill by its "cfg:"-prefixed id, or nil if id isn't one of
// this store's ids or the directory can no longer be read/parsed.
func (s *SkillStore) Get(id string) *skill.Skill {
	dirName, ok := stripIDPrefix(id)
	if !ok {
		return nil
	}
	sk, ok := s.load(dirName)
	if !ok {
		return nil
	}
	return sk
}

func (s *SkillStore) load(dirName string) (*skill.Skill, bool) {
	files, err := skill.ImportTreeFromPath(filepath.Join(s.Root, dirName))
	if err != nil {
		return nil, false
	}
	doc, ok := skill.RootDocument(files)
	if !ok {
		return nil, false
	}
	return &skill.Skill{
		ID:            IDPrefix + dirName,
		Name:          doc.Name,
		Description:   doc.Description,
		Files:         files,
		License:       doc.License,
		Compatibility: doc.Compatibility,
		Metadata:      doc.Metadata,
		AllowedTools:  doc.AllowedTools,
	}, true
}

// ListFiles returns the skill's file tree, sorted by path — the one read
// operation skill.Store declares beyond Get/List, so it's implemented for
// real rather than refused like the mutations below.
func (s *SkillStore) ListFiles(skillID string) ([]skill.File, error) {
	sk := s.Get(skillID)
	if sk == nil {
		return nil, errNotFound("skill")
	}
	files := append([]skill.File(nil), sk.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (s *SkillStore) Create(skill.Input) (*skill.Skill, error) { return nil, errReadOnly("skill") }

func (s *SkillStore) Update(string, skill.Input) (*skill.Skill, error) {
	return nil, errReadOnly("skill")
}

func (s *SkillStore) Delete(string) error { return errReadOnly("skill") }

func (s *SkillStore) PutFile(string, skill.File) error        { return errReadOnly("skill") }
func (s *SkillStore) RenameFile(string, string, string) error { return errReadOnly("skill") }
func (s *SkillStore) DeleteFile(string, string) error         { return errReadOnly("skill") }

func (s *SkillStore) SetPublishState(string, skill.PublishState) error {
	return errReadOnly("skill")
}
