package agentcatalog

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rfbatista/harnesskit/skill"
	"gopkg.in/yaml.v3"

	"operators-mcp/internal/domain"
)

const maxSkillDescription = 1024

// cleanSkillPath is a file path inside a skill folder, slash-separated and
// clean. It refuses a path that is absolute, leaves the folder, or names
// SKILL.md, which the server writes from the form's fields.
func cleanSkillPath(p string) (string, error) {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || filepath.IsAbs(p) {
		return "", invalid("files", "a skill file path is relative to its folder: "+p)
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", invalid("files", "a skill file path may not leave its folder: "+p)
	}
	if clean == skillFileName {
		return "", invalid("files", skillFileName+" is written from the name, description and body")
	}
	return clean, nil
}

// skillFiles is every file of a skill folder by path, SKILL.md included.
func (c *Catalog) skillFiles(name string) (map[string][]byte, error) {
	dir := filepath.Join(c.root, skillsDir, name)
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	return out, err
}

// frontmatterBlock is SKILL.md's frontmatter text: the form's name and
// description, then every other key the file already had.
func frontmatterBlock(name, description string, old []byte) (string, error) {
	head, err := yaml.Marshal(struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}{name, description})
	if err != nil {
		return "", err
	}
	text := string(old)
	var rest []string
	if after, ok := strings.CutPrefix(text, "---"); ok {
		after = strings.TrimLeft(after, "\r\n")
		if end := strings.Index(after, "\n---"); end >= 0 {
			skipping := false
			for _, line := range strings.Split(after[:end], "\n") {
				key, _, isKey := strings.Cut(line, ":")
				topLevel := isKey && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")
				switch {
				case topLevel && (key == "name" || key == "description"):
					skipping = true
				case topLevel || strings.TrimSpace(line) == "" && !skipping:
					skipping = false
					rest = append(rest, line)
				case skipping:
					// A continuation line of a key being replaced.
				default:
					rest = append(rest, line)
				}
			}
		}
	}
	block := string(head)
	if len(rest) > 0 {
		block += strings.TrimRight(strings.Join(rest, "\n"), "\n") + "\n"
	}
	return block, nil
}

// skillMarkdown is the SKILL.md text for the form's fields. When nothing it
// holds changed, the old bytes stay.
func skillMarkdown(name, description, body string, old []byte) ([]byte, error) {
	if old != nil {
		if doc, err := skill.ParseDocument(old); err == nil && strings.HasPrefix(string(old), "---") &&
			doc.Name == name && doc.Description == strings.TrimSpace(description) && doc.Body == strings.TrimSpace(body) {
			return old, nil
		}
	}
	block, err := frontmatterBlock(name, description, old)
	if err != nil {
		return nil, err
	}
	out := "---\n" + block + "---\n\n" + strings.TrimSpace(body) + "\n"
	return []byte(out), nil
}

// SaveCatalogSkill creates, updates or renames a skill: its SKILL.md, its
// files, and who gets it.
func (c *Catalog) SaveCatalogSkill(in domain.SaveCatalogSkill) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t, err := c.begin()
	if err != nil {
		return nil, err
	}
	sk := in.Skill
	if !slugRe.MatchString(sk.Name) {
		return nil, invalid("name", "a skill name is lower-case letters, digits and dashes, up to 64")
	}
	if strings.TrimSpace(sk.Description) == "" {
		return nil, invalid("description", "a skill needs a description")
	}
	if len([]rune(sk.Description)) > maxSkillDescription {
		return nil, invalid("description", fmt.Sprintf("a skill description is at most %d characters", maxSkillDescription))
	}
	prev := in.PreviousName
	if prev != "" && !c.skillExists(prev) {
		return nil, &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill " + prev + " not found"}
	}
	if (prev == "" || prev != sk.Name) && c.skillExists(sk.Name) {
		return nil, nameTaken("skill", sk.Name, c.skillExists)
	}

	// The skill's files as they will be: the old ones, minus the deleted,
	// plus the given.
	files := map[string][]byte{}
	var oldFiles map[string][]byte
	if prev != "" {
		if oldFiles, err = c.skillFiles(prev); err != nil {
			return nil, err
		}
		for p, data := range oldFiles {
			files[p] = data
		}
	}
	for _, d := range sk.DeletedFiles {
		clean, err := cleanSkillPath(d)
		if err != nil {
			return nil, err
		}
		delete(files, clean)
	}
	given := map[string]bool{}
	for _, f := range sk.Files {
		clean, err := cleanSkillPath(f.Path)
		if err != nil {
			return nil, err
		}
		data := []byte(f.Content)
		switch f.Encoding {
		case "", "utf-8":
		case "base64":
			if data, err = base64.StdEncoding.DecodeString(f.Content); err != nil {
				return nil, invalid("files", "the content of "+f.Path+" is not valid base64")
			}
		default:
			return nil, invalid("files", "unknown encoding "+f.Encoding+" for "+f.Path)
		}
		files[clean] = append([]byte{}, data...)
		given[clean] = true
	}
	md, err := skillMarkdown(sk.Name, sk.Description, sk.Body, oldFiles[skillFileName])
	if err != nil {
		return nil, err
	}
	files[skillFileName] = md

	// Edits: the new folder's files first, then the old folder's removal.
	dest := skillsDir + "/" + sk.Name + "/"
	for _, p := range sortedPaths(files) {
		before, err := os.ReadFile(filepath.Join(c.root, skillsDir, sk.Name, filepath.FromSlash(p)))
		if err != nil {
			before = nil
		}
		if prev != sk.Name && prev != "" {
			before = nil
		}
		if before != nil && bytes.Equal(before, files[p]) {
			continue
		}
		t.edits = append(t.edits, &fileEdit{path: dest + p, before: before, after: files[p]})
	}
	if prev == sk.Name {
		for _, p := range sortedPaths(oldFiles) {
			if _, kept := files[p]; !kept {
				t.edits = append(t.edits, &fileEdit{path: dest + p, before: oldFiles[p]})
			}
		}
	} else if prev != "" {
		for _, p := range sortedPaths(oldFiles) {
			t.edits = append(t.edits, &fileEdit{path: skillsDir + "/" + prev + "/" + p, before: oldFiles[p]})
		}
	}

	refs := 0
	if prev != "" && prev != sk.Name {
		refs = t.rewriteEntries(func(e string) (string, bool) {
			if e == prev {
				return sk.Name, true
			}
			return e, true
		})
	}
	touched, err := t.applyAddTo(sk.Name, in.AddTo)
	if err != nil {
		return nil, err
	}

	verb := "Created"
	switch {
	case prev == sk.Name:
		verb = "Saved"
	case prev != "":
		verb = fmt.Sprintf("Renamed %s to %s (%d references updated) and saved", prev, sk.Name, refs)
	}
	summary := fmt.Sprintf("%s skills/%s — %d files", verb, sk.Name, len(files))
	if touched > 0 {
		summary += fmt.Sprintf("; %d agents and bundles changed", touched)
	}
	return t.commit(in.CatalogWriteOptions, summary)
}

func sortedPaths(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// applyAddTo makes the named agents and bundles the only ones that list the
// skill directly. It says how many it changed. A nil addTo changes nothing.
func (t *txn) applyAddTo(skillName string, addTo *domain.CatalogAddTo) (int, error) {
	if addTo == nil {
		return 0, nil
	}
	var unknown []string
	for _, a := range addTo.Agents {
		if t.agentsMap().get(a) == nil {
			unknown = append(unknown, a)
		}
	}
	for _, b := range addTo.Bundles {
		if t.bundlesMap().get(b) == nil {
			unknown = append(unknown, "@"+b)
		}
	}
	if len(unknown) > 0 {
		return 0, unknownRefs(unknown)
	}
	changed := 0
	set := func(list *node, wanted bool) {
		if setMembership(list, skillName, wanted) {
			changed++
		}
	}
	inList := func(names []string, n string) bool {
		for _, x := range names {
			if x == n {
				return true
			}
		}
		return false
	}
	for _, m := range t.agentsMap().members {
		list := m.val.get("skills")
		wanted := inList(addTo.Agents, m.key)
		if list == nil {
			if !wanted {
				continue
			}
			list = newArray()
			m.val.set("skills", list)
		}
		set(list, wanted)
	}
	for _, m := range t.bundlesMap().members {
		set(m.val, inList(addTo.Bundles, m.key))
	}
	return changed, nil
}

// setMembership makes item present in or absent from a list of strings. It
// says whether the list changed.
func setMembership(list *node, item string, wanted bool) bool {
	has := false
	var items []*node
	for _, it := range list.items {
		if it.kind == 's' && it.str() == item {
			has = true
			if !wanted {
				continue
			}
		}
		items = append(items, it)
	}
	list.items = items
	switch {
	case wanted && !has:
		list.items = append(list.items, scalarOf(item))
		return true
	case !wanted && has:
		return true
	}
	return false
}

// DeleteCatalogSkill removes a skill's folder and every entry that names it.
func (c *Catalog) DeleteCatalogSkill(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t, err := c.begin()
	if err != nil {
		return nil, err
	}
	if !slugRe.MatchString(in.Name) || !c.skillExists(in.Name) {
		return nil, &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill " + in.Name + " not found"}
	}
	files, err := c.skillFiles(in.Name)
	if err != nil {
		return nil, err
	}
	for _, p := range sortedPaths(files) {
		t.edits = append(t.edits, &fileEdit{path: skillsDir + "/" + in.Name + "/" + p, before: files[p]})
	}
	refs := t.rewriteEntries(func(e string) (string, bool) { return e, e != in.Name })
	return t.commit(in.CatalogWriteOptions, fmt.Sprintf("Deleted skills/%s (%d references removed)", in.Name, refs))
}
