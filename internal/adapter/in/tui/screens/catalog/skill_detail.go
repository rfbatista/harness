package catalog

import (
	"fmt"
	"path"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/domain"
)

// skillDetail is the file browser for one skill: the tree on the left, a
// preview on the right, edits delegated to $EDITOR.
type skillDetail struct {
	ctx      core.Context
	be       backend.Backend
	skillID  string
	skill    *domain.Skill
	cursor   int
	overlays components.Overlays
}

// fileEditTag prefixes the editor tag of a file edit, so the result can be
// told apart from a form field edited in $EDITOR.
const fileEditTag = "file:"

func newSkillDetail(ctx core.Context, be backend.Backend, id string) *skillDetail {
	d := &skillDetail{ctx: ctx, be: be, skillID: id}
	d.reload()
	return d
}

func (d *skillDetail) reload() { d.skill = d.be.GetSkill(d.skillID) }

// files returns the tree sorted by path, folders marked with a trailing slash.
func (d *skillDetail) files() []domain.SkillFile {
	if d.skill == nil {
		return nil
	}
	out := append([]domain.SkillFile(nil), d.skill.Files...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (d *skillDetail) selected() *domain.SkillFile {
	fs := d.files()
	if d.cursor >= 0 && d.cursor < len(fs) {
		return &fs[d.cursor]
	}
	return nil
}

func (d *skillDetail) clamp() {
	n := len(d.files())
	if d.cursor >= n {
		d.cursor = n - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

func (d *skillDetail) capturing() bool { return d.overlays.Active() }

func (d *skillDetail) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "esc", Desc: "back"}, {Key: "e", Desc: "edit in $EDITOR"}, {Key: "a", Desc: "add file"}, {Key: "A", Desc: "add folder"}, {Key: "R", Desc: "rename"}, {Key: "D", Desc: "delete"}, {Key: "j/k", Desc: "move"}}
}

func (d *skillDetail) update(msg tea.Msg) (detail, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ContextMsg:
		d.ctx = msg.Ctx
		return d, nil
	case core.SnapshotMsg:
		d.reload()
		if d.skill == nil {
			return d, closeDetail
		}
		d.clamp()
		return d, nil
	case components.EditorDoneMsg:
		if strings.HasPrefix(msg.Tag, fileEditTag) {
			return d, d.writeFile(strings.TrimPrefix(msg.Tag, fileEditTag), msg)
		}
	}
	if cmd, handled := d.overlays.Update(msg); handled {
		return d, cmd
	}
	switch msg := msg.(type) {
	case components.OpDoneMsg:
		cmd, _ := d.overlays.Done(msg)
		return d, cmd
	case tea.KeyPressMsg:
		return d, d.key(msg)
	}
	return d, nil
}

func (d *skillDetail) writeFile(p string, msg components.EditorDoneMsg) tea.Cmd {
	if msg.Err != nil {
		return components.ErrorToast(msg.Err.Error())
	}
	be, id, content := d.be, d.skillID, msg.Content
	return components.Op("skill.file.save", "Saved "+p, func() error {
		_, err := be.PutSkillFile(id, domain.SkillFile{Path: p, Content: content})
		return err
	})
}

func (d *skillDetail) key(k tea.KeyPressMsg) tea.Cmd {
	th, be, id := d.ctx.Theme, d.be, d.skillID
	switch k.String() {
	case "esc", "q":
		return closeDetail
	case "j", "down":
		d.cursor++
		d.clamp()
	case "k", "up":
		d.cursor--
		d.clamp()
	case "g":
		d.cursor = 0
	case "G":
		d.cursor = len(d.files()) - 1
		d.clamp()
	case "e", "enter":
		f := d.selected()
		if f == nil || f.Dir {
			return nil
		}
		if f.Encoding == "base64" {
			return components.ErrorToast("binary files cannot be edited here")
		}
		return components.EditInEditor(f.Content, path.Ext(f.Path), fileEditTag+f.Path)
	case "a":
		d.overlays.OpenForm(components.NewForm(th, "New file",
			components.TextField("path", "Path inside the skill", d.dirPrefix()).Required().Placeholder("docs/notes.md"),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			p := strings.TrimSpace(sub.Values["path"])
			return components.Op("skill.file.add", "File added", func() error {
				_, err := be.PutSkillFile(id, domain.SkillFile{Path: p})
				return err
			})
		})
	case "A":
		d.overlays.OpenForm(components.NewForm(th, "New folder",
			components.TextField("path", "Folder path inside the skill", d.dirPrefix()).Required().Placeholder("scripts"),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			p := strings.TrimSpace(sub.Values["path"])
			return components.Op("skill.folder.add", "Folder added", func() error {
				_, err := be.PutSkillFile(id, domain.SkillFile{Path: p, Dir: true})
				return err
			})
		})
	case "R":
		f := d.selected()
		if f == nil {
			return nil
		}
		old := f.Path
		d.overlays.OpenForm(components.NewForm(th, "Rename",
			components.TextField("path", "New path", f.Path).Required(),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			p := strings.TrimSpace(sub.Values["path"])
			return components.Op("skill.file.rename", "Renamed", func() error {
				_, err := be.RenameSkillFile(id, old, p)
				return err
			})
		})
	case "D":
		f := d.selected()
		if f == nil {
			return nil
		}
		if f.Path == domain.SkillFileName {
			return components.ErrorToast("SKILL.md cannot be deleted")
		}
		p := f.Path
		d.overlays.OpenConfirm(th, "Delete file", fmt.Sprintf("Delete %q from the skill?", p), func() tea.Cmd {
			return components.Op("skill.file.delete", "File deleted", func() error {
				_, err := be.DeleteSkillFile(id, p)
				return err
			})
		})
	}
	return nil
}

// dirPrefix suggests the selected folder as the starting path for new files.
func (d *skillDetail) dirPrefix() string {
	f := d.selected()
	if f == nil {
		return ""
	}
	if f.Dir {
		return strings.TrimSuffix(f.Path, "/") + "/"
	}
	if dir := path.Dir(f.Path); dir != "." {
		return dir + "/"
	}
	return ""
}

func (d *skillDetail) view() string {
	th := d.ctx.Theme
	if d.skill == nil {
		return ""
	}
	header := th.Title().Render(d.skill.Name) + "  " + th.Subtitle().Render(d.skill.Description)
	if d.skill.PublishedPath != "" {
		header += "  " + th.Meta().Render("↑ "+d.skill.PublishedPath)
	}
	treeW := 34
	if d.ctx.Width < 80 {
		treeW = d.ctx.Width / 3
	}
	previewW := d.ctx.Width - treeW - 3
	bodyH := d.ctx.Height - 3

	var tree []string
	for i, f := range d.files() {
		label := f.Path
		if f.Dir {
			label = strings.TrimSuffix(label, "/") + "/"
		}
		if i == d.cursor {
			label = th.Selected().Render(label)
		}
		tree = append(tree, label)
	}
	if len(tree) == 0 {
		tree = append(tree, th.Subtitle().Render("No files. Press a to add one."))
	}
	left := th.Panel().Width(treeW).Height(bodyH).Render(strings.Join(tree, "\n"))
	right := th.Panel().Width(previewW).Height(bodyH).Render(d.preview(previewW - 4))
	body := lipgloss.JoinVertical(lipgloss.Left, header, lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right))
	if d.overlays.Active() {
		return components.OverlayOn(body, d.overlays.View(overlayWidth(d.ctx)), d.ctx.Width, d.ctx.Height)
	}
	return body
}

func (d *skillDetail) preview(width int) string {
	th := d.ctx.Theme
	f := d.selected()
	switch {
	case f == nil:
		return ""
	case f.Dir:
		return th.Subtitle().Render("folder")
	case f.Encoding == "base64":
		return th.Subtitle().Render("binary file")
	case f.Content == "":
		return th.Subtitle().Render("(empty) — press e to edit")
	case strings.HasSuffix(strings.ToLower(f.Path), ".md"):
		return th.Markdown(stripFrontmatter(f.Content), width)
	}
	return f.Content
}

// stripFrontmatter hides the YAML header from the rendered preview; it is
// metadata, and the list already shows it.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[4:]
	if i := strings.Index(rest, "\n---"); i >= 0 {
		return strings.TrimLeft(rest[i+4:], "\n")
	}
	return s
}
