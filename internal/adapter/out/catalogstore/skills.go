package catalogstore

import (
	"sort"
	"strings"

	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Skills is the capabilities context's skill repository, over the catalog. It
// implements skill.Store.
type Skills struct{ cat editor }

var _ skill.Store = (*Skills)(nil)

// NewSkills is the skill repository over the catalog.
func NewSkills(cat ports.CatalogEditor) *Skills { return &Skills{cat: cat} }

// Get returns the skill named id, with its whole tree, or nil.
func (r *Skills) Get(id string) *skill.Skill {
	view, err := r.cat.ReadCatalogSkillContent(id)
	if err != nil {
		return nil
	}
	return skillOf(view.Skill)
}

func skillOf(s domain.CatalogSkill) *skill.Skill {
	files := []skill.File{{Path: skill.FileName, Content: s.Markdown}}
	for _, f := range s.Files {
		if f.Dir {
			files = append(files, skill.File{Path: f.Path, Dir: true})
			continue
		}
		enc := f.Encoding
		if enc == "utf-8" {
			enc = ""
		}
		files = append(files, skill.File{Path: f.Path, Content: f.Content, Encoding: enc})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	doc, _ := skill.ParseDocument([]byte(s.Markdown))
	return &skill.Skill{
		ID: s.Name, Name: s.Name, Description: s.Description, Files: files,
		License: doc.License, Compatibility: doc.Compatibility, Metadata: doc.Metadata, AllowedTools: doc.AllowedTools,
	}
}

// List returns every skill, without its tree: a list of hundreds of trees is
// not what the callers want. Get has the tree.
func (r *Skills) List() []*skill.Skill {
	view, err := r.cat.ReadCatalog()
	if err != nil {
		return nil
	}
	out := make([]*skill.Skill, 0, len(view.Skills))
	for _, s := range view.Skills {
		out = append(out, &skill.Skill{ID: s.Name, Name: s.Name, Description: s.Description})
	}
	return out
}

// save writes a whole tree: SKILL.md as given, every other file, and the
// removal of the files of the old tree it no longer has.
func (r *Skills) save(previous string, in skill.Input) error {
	var markdown *string
	var body string
	var files []domain.CatalogSkillFileInput
	keep := map[string]bool{}
	for _, f := range skill.NormalizeFiles(in.Files) {
		if f.Dir {
			continue
		}
		if isMarkdown(f.Path) {
			m := f.Content
			markdown = &m
			doc, _ := skill.ParseDocument([]byte(f.Content))
			body = doc.Body
			continue
		}
		keep[f.Path] = true
		files = append(files, domain.CatalogSkillFileInput{Path: f.Path, Content: f.Content, Encoding: f.Encoding})
	}
	if markdown == nil && in.Content != "" {
		body = in.Content
	}
	// SKILL.md is written as given only when it already says the name and
	// description being saved; otherwise they are written from the fields and
	// the body is kept.
	var raw *string
	if markdown != nil {
		if doc, err := skill.ParseDocument([]byte(*markdown)); err == nil &&
			doc.Name == strings.TrimSpace(in.Name) && doc.Description == strings.TrimSpace(in.Description) {
			raw = markdown
		}
	}
	var deleted []string
	if previous != "" {
		view, err := r.cat.ReadCatalogSkillContent(previous)
		if err != nil {
			return err
		}
		for _, f := range view.Skill.Files {
			if !f.Dir && !keep[f.Path] {
				deleted = append(deleted, f.Path)
			}
		}
	}
	_, err := r.cat.SaveCatalogSkill(domain.SaveCatalogSkill{
		PreviousName: previous,
		Skill: domain.CatalogSkillInput{
			Name: strings.TrimSpace(in.Name), Description: in.Description, Body: body, Files: files, DeletedFiles: deleted,
		},
		RawMarkdown: raw,
	})
	return err
}

// Create adds a skill.
func (r *Skills) Create(in skill.Input) (*skill.Skill, error) {
	if err := r.save("", in); err != nil {
		return nil, err
	}
	return r.Get(strings.TrimSpace(in.Name)), nil
}

// Update replaces a skill's description and tree, and renames it when the
// name changed. An empty name or description stays as it is.
func (r *Skills) Update(id string, in skill.Input) (*skill.Skill, error) {
	cur := r.Get(id)
	if cur == nil {
		return nil, errNotFound("SKILL_NOT_FOUND", "skill "+id)
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = cur.Name
	}
	if strings.TrimSpace(in.Description) == "" {
		in.Description = cur.Description
	}
	if err := r.save(id, in); err != nil {
		return nil, err
	}
	return r.Get(strings.TrimSpace(in.Name)), nil
}

// Delete removes a skill and every reference to it.
func (r *Skills) Delete(id string) error {
	_, err := r.cat.DeleteCatalogSkill(domain.DeleteCatalogItem{Name: id})
	return err
}

// ListFiles returns the skill's tree, sorted by path.
func (r *Skills) ListFiles(id string) ([]skill.File, error) {
	sk := r.Get(id)
	if sk == nil {
		return nil, errNotFound("SKILL_NOT_FOUND", "skill "+id)
	}
	return sk.Files, nil
}

// edit saves the skill with its tree changed by fn.
func (r *Skills) edit(id string, fn func(files []skill.File) ([]skill.File, error)) error {
	sk := r.Get(id)
	if sk == nil {
		return errNotFound("SKILL_NOT_FOUND", "skill "+id)
	}
	files, err := fn(sk.Files)
	if err != nil {
		return err
	}
	return r.save(id, skill.Input{Name: sk.Name, Description: sk.Description, Files: files})
}

// PutFile creates or replaces the node at f.Path.
func (r *Skills) PutFile(id string, f skill.File) error {
	return r.edit(id, func(files []skill.File) ([]skill.File, error) {
		path, ok := skill.NormalizePath(f.Path)
		if !ok {
			return nil, invalid("path", "a skill file path is relative to its folder")
		}
		f.Path = path
		out := make([]skill.File, 0, len(files)+1)
		for _, x := range files {
			if x.Path != path {
				out = append(out, x)
			}
		}
		return append(out, f), nil
	})
}

// RenameFile moves oldPath to newPath, descendants with it.
func (r *Skills) RenameFile(id, oldPath, newPath string) error {
	return r.edit(id, func(files []skill.File) ([]skill.File, error) {
		from, ok1 := skill.NormalizePath(oldPath)
		to, ok2 := skill.NormalizePath(newPath)
		if !ok1 || !ok2 {
			return nil, invalid("path", "a skill file path is relative to its folder")
		}
		moved := false
		out := make([]skill.File, 0, len(files))
		for _, f := range files {
			switch {
			case f.Path == from:
				f.Path, moved = to, true
			case strings.HasPrefix(f.Path, from+"/"):
				f.Path, moved = to+strings.TrimPrefix(f.Path, from), true
			}
			out = append(out, f)
		}
		if !moved {
			return nil, &domain.StructuredError{Code: "SKILL_FILE_NOT_FOUND", Message: "nothing at " + oldPath}
		}
		return out, nil
	})
}

// DeleteFile removes the node at path and its descendants.
func (r *Skills) DeleteFile(id, path string) error {
	return r.edit(id, func(files []skill.File) ([]skill.File, error) {
		p, ok := skill.NormalizePath(path)
		if !ok {
			return nil, invalid("path", "a skill file path is relative to its folder")
		}
		removed := false
		out := make([]skill.File, 0, len(files))
		for _, f := range files {
			if f.Path == p || strings.HasPrefix(f.Path, p+"/") {
				removed = true
				continue
			}
			out = append(out, f)
		}
		if !removed {
			return nil, &domain.StructuredError{Code: "SKILL_FILE_NOT_FOUND", Message: "nothing at " + path}
		}
		return out, nil
	})
}

// SetPublishState is refused: a skill of the catalog already is a folder.
func (r *Skills) SetPublishState(string, skill.PublishState) error {
	return &domain.StructuredError{Code: "UNAVAILABLE", Message: "a skill in the agent catalog is already a folder; it is not published"}
}
