package catalog

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/domain"
)

// skillsSpec lists skills: n creates from an inline SKILL.md, i imports a
// folder, e edits metadata, p/u publish and unpublish, Enter opens the file
// browser.
type skillsSpec struct{}

// opPublish tags the publish write so a PUBLISH_TARGET_EXISTS conflict can
// be retried with force for the same skill.
const opPublish = "skill.publish:"

// importCheckedMsg is the outcome of validating an import path before asking
// the user to confirm the import.
type importCheckedMsg struct {
	path   string
	result *domain.SkillPathValidation
	err    error
}

func (skillsSpec) columns() []components.Column {
	return []components.Column{{Title: "Skill", Width: 28}, {Title: "Description"}, {Title: "Files", Width: 9}, {Title: "State", Width: 12}}
}

func (skillsSpec) entries(m *Model) []entry {
	th := m.ctx.Theme
	var out []entry
	for _, s := range m.snap.Skills {
		state := th.Meta().Render("valid")
		if len(s.Validation) > 0 {
			state = th.Error().Render("invalid")
		}
		if s.PublishedPath != "" {
			state += th.Meta().Render(" ↑")
		}
		out = append(out, entry{s.ID, []string{s.Name, s.Description, plural(len(s.Files), "file"), state}})
	}
	return out
}

func (skillsSpec) summary() string   { return "Agent Skills catalog" }
func (skillsSpec) emptyText() string { return "No skills yet. Press n to create one or i to import." }

func (skillsSpec) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "↵", Desc: "files"}, {Key: "n", Desc: "new"}, {Key: "i", Desc: "import"}, {Key: "e", Desc: "edit"}, {Key: "p/u", Desc: "publish/unpublish"}, {Key: "D", Desc: "delete"}}
}

func (skillsSpec) skill(m *Model, id string) *domain.Skill {
	for _, s := range m.snap.Skills {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (s skillsSpec) key(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	th, be := m.ctx.Theme, m.be
	switch k.String() {
	case "n":
		m.overlays.OpenForm(components.NewForm(th, "New skill",
			components.TextField("name", "Name", "").Required().Placeholder("kebab-case, e.g. tdd-workflow"),
			components.TextField("description", "Description", "").Required(),
			components.TextField("license", "License", ""),
			components.TextField("compatibility", "Compatibility", ""),
			components.TextField("allowed_tools", "Allowed tools", ""),
			components.MultilineField("body", "SKILL.md body", "# Skill\n\nDescribe when and how to use this skill.\n"),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			in := skillInput(sub.Values)
			in.Files = []domain.SkillFile{{Path: domain.SkillFileName, Content: skillMarkdown(sub.Values)}}
			return components.Op("skill.create", "Skill created", func() error {
				_, err := be.CreateSkill(in)
				return err
			})
		})
		return nil, true
	case "e":
		sk := s.skill(m, m.Selected())
		if sk == nil {
			return nil, true
		}
		id := sk.ID
		m.overlays.OpenForm(components.NewForm(th, "Edit skill metadata",
			components.TextField("name", "Name", sk.Name).Required(),
			components.TextField("description", "Description", sk.Description),
			components.TextField("license", "License", sk.License),
			components.TextField("compatibility", "Compatibility", sk.Compatibility),
			components.TextField("allowed_tools", "Allowed tools", sk.AllowedTools),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			in := skillInput(sub.Values)
			return components.Op("skill.update", "Skill updated", func() error {
				_, err := be.UpdateSkill(id, in)
				return err
			})
		})
		return nil, true
	case "i":
		m.overlays.OpenForm(components.NewForm(th, "Import skill from path",
			components.TextField("path", "Path to a skill folder or its SKILL.md", "").Required().Placeholder("/absolute/path/to/skill"),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			// Validate first; the outcome comes back as importCheckedMsg and
			// either keeps the form open with the issue or asks to confirm.
			path := sub.Values["path"]
			return func() tea.Msg {
				res, err := be.ValidateSkillPath(path)
				return importCheckedMsg{path: path, result: res, err: err}
			}
		})
		return nil, true
	case "p":
		sk := s.skill(m, m.Selected())
		if sk == nil {
			return nil, true
		}
		id := sk.ID
		return components.Op(opPublish+id, "Skill published", func() error {
			_, err := be.PublishSkill(id, false)
			return err
		}), true
	case "u":
		sk := s.skill(m, m.Selected())
		if sk == nil {
			return nil, true
		}
		id := sk.ID
		return components.Op("skill.unpublish", "Skill unpublished", func() error {
			_, err := be.UnpublishSkill(id)
			return err
		}), true
	case "D":
		sk := s.skill(m, m.Selected())
		if sk == nil {
			return nil, true
		}
		id := sk.ID
		m.overlays.OpenConfirm(th, "Delete skill",
			fmt.Sprintf("Delete %q and its files? Agents linked to it lose the capability.", sk.Name),
			func() tea.Cmd {
				return components.Op("skill.delete", "Skill deleted", func() error { return be.DeleteSkill(id) })
			})
		return nil, true
	case "enter":
		sk := s.skill(m, m.Selected())
		if sk == nil {
			return nil, true
		}
		m.detail = newSkillDetail(m.ctx, m.be, sk.ID)
		return nil, true
	}
	return nil, false
}

// result turns a validated import path into a confirm with the preview, or
// keeps the form open with the first issue when the path is not a skill.
func (skillsSpec) result(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	chk, ok := msg.(importCheckedMsg)
	if !ok {
		return nil, false
	}
	if chk.err != nil {
		cmd, _ := m.overlays.Done(components.OpDoneMsg{Tag: "skill.import", Err: chk.err})
		return cmd, true
	}
	res := chk.result
	if !res.Valid || res.Preview == nil {
		issue := "path is not a valid skill"
		if len(res.Validation) > 0 {
			issue = res.Validation[0].Message
		}
		if m.overlays.Form != nil {
			m.overlays.Form.SetFieldError("path", issue)
		}
		return nil, true
	}
	be, path := m.be, chk.path
	m.overlays.OpenConfirm(m.ctx.Theme, "Import skill",
		fmt.Sprintf("Import %q from %s?\n%s · %s", res.Preview.Name, res.SkillRoot, plural(len(res.Preview.Files), "file"), truncate(res.Preview.Description, 80)),
		func() tea.Cmd {
			return components.Op("skill.import", "Skill imported", func() error {
				_, err := be.ImportSkillFromPath(path)
				return err
			})
		})
	return nil, true
}

// conflict offers to overwrite an existing publish target.
func (s skillsSpec) conflict(m *Model, op components.OpDoneMsg) bool {
	if !strings.HasPrefix(op.Tag, opPublish) {
		return false
	}
	id := strings.TrimPrefix(op.Tag, opPublish)
	name := id
	if sk := s.skill(m, id); sk != nil {
		name = sk.Name
	}
	be := m.be
	m.overlays.OpenConfirm(m.ctx.Theme, "Publish target already exists",
		fmt.Sprintf("A folder for %q already exists under the publish root. Overwrite it?", name),
		func() tea.Cmd {
			return components.Op("skill.publish.force", "Skill published", func() error {
				_, err := be.PublishSkill(id, true)
				return err
			})
		})
	return true
}

// skillInput reads the metadata fields shared by the new and edit forms.
func skillInput(v map[string]string) domain.SkillInput {
	return domain.SkillInput{
		Name: v["name"], Description: v["description"], License: v["license"],
		Compatibility: v["compatibility"], AllowedTools: v["allowed_tools"],
	}
}

// skillMarkdown assembles a SKILL.md from the form: Agent Skills frontmatter
// followed by the body.
func skillMarkdown(v map[string]string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", v["name"])
	fmt.Fprintf(&b, "description: %s\n", v["description"])
	for _, kv := range [][2]string{{"license", v["license"]}, {"compatibility", v["compatibility"]}, {"allowed-tools", v["allowed_tools"]}} {
		if strings.TrimSpace(kv[1]) != "" {
			fmt.Fprintf(&b, "%s: %s\n", kv[0], kv[1])
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimLeft(v["body"], "\n"))
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
