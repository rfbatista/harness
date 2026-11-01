//go:build !windows

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"operators-mcp/internal/ports"
)

// The project design system in the virtual shell: a read-only view of the
// current version for sessions whose repository uses it (contract "Harness
// server ↔ Agent sessions — design-system view, draft and proposals").

const designSystemDir = "project/design-system"

func shellDesignRecord() map[string]any {
	token := func(group, name, typ string, value any, note string) map[string]any {
		return map[string]any{"group": group, "name": name, "type": typ, "value": value, "note": note}
	}
	return map[string]any{
		"tokens": []any{
			token("colors", "signal", "color", "oklch(0.5 0.095 182)", "Primary action"),
			token("spacing", "md", "dimension", "1rem", ""),
			token("colors", "canvas", "color", "#fff", "Page background: \"white\""),
			token("typography", "headline", "typography", map[string]any{"fontSize": "1.5rem", "fontFamily": "Inter, sans-serif", "fontWeight": "600"}, "Page titles"),
		},
		"principles": "Calm surfaces, one signal colour.\nNo decoration.\n\n## Colour\n\nUse signal sparingly.\n",
		"components": []any{},
	}
}

func (e *shellE2E) saveDesignSystem(base int, record map[string]any) {
	e.t.Helper()
	e.post("save_design_system", map[string]any{"project_id": e.project, "base_version": base, "record": record}, nil)
}

func (e *shellE2E) useDesignSystem(repo string, uses bool) {
	e.t.Helper()
	e.post("set_repository_design_system", map[string]any{"repository_id": repo, "uses_design_system": uses}, nil)
}

// designSystemDate is the day the current version was saved, as its files say it.
func (e *shellE2E) designSystemDate() string {
	e.t.Helper()
	ds, err := e.cat.DesignSystems.GetDesignSystem(e.ctx, e.project)
	if err != nil {
		e.t.Fatal(err)
	}
	return ds.UpdatedAt.UTC().Format("2006-01-02")
}

func (e *shellE2E) listing(s runningSession, dir string) []string {
	e.t.Helper()
	entries, err := os.ReadDir(filepath.Join(s.shell, filepath.FromSlash(dir)))
	if err != nil {
		return nil
	}
	var names []string
	for _, ent := range entries {
		names = append(names, ent.Name())
	}
	sort.Strings(names)
	return names
}

func (e *shellE2E) designFile(s runningSession, name string) string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.shell, "project", "design-system", name))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

// otherRepo is a second repository of the project, which never opts in.
func (e *shellE2E) otherRepo() string {
	e.t.Helper()
	dir := filepath.Join(e.t.TempDir(), "other")
	gitInit(e.t, dir)
	r, err := e.cat.Projects.CreateRepository(e.ctx, e.project, "other", "", "https://example.com/other.git", dir)
	if err != nil {
		e.t.Fatal(err)
	}
	return r.ID
}

func TestDesignSystemView_OnlyAnOptedInRepositoryHasTheFolderAndItsReadmeSaysSo(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	e.saveDesignSystem(0, shellDesignRecord())
	other := e.otherRepo()
	e.useDesignSystem(e.repo, true)

	in := e.start(e.task("Opted in"))
	out := e.startWith(ports.InteractiveRequest{ProjectID: e.project, RepositoryID: other, TicketID: e.task("Not opted in"), Mode: "implement"})

	if got := strings.Join(e.listing(in, designSystemDir), ","); got != "DESIGN.md,README.md,tokens.json" {
		t.Errorf("opted in lists %q", got)
	}
	if _, err := os.Stat(filepath.Join(out.shell, "project", "design-system")); err == nil {
		t.Error("a repository that does not use the design system has the folder")
	}
	if indexRow(t, out.shell, designSystemDir+"/DESIGN.md") != nil {
		t.Error("the other session's index lists the files")
	}
	if text, _ := os.ReadFile(filepath.Join(out.shell, "README.md")); strings.Contains(string(text), "design-system") {
		t.Errorf("the other session's README mentions the design system")
	}
	if text, _ := os.ReadFile(filepath.Join(in.shell, "README.md")); !strings.Contains(string(text), "project/design-system/") {
		t.Errorf("the opted-in session's README does not mention the folder")
	}

	// Opting out and in changes the next listing.
	e.useDesignSystem(e.repo, false)
	if got := e.listing(e.start(e.task("Opted out")), designSystemDir); got != nil {
		t.Errorf("after opting out the next shell lists %v", got)
	}
	e.useDesignSystem(other, true)
	if got := e.listing(e.startWith(ports.InteractiveRequest{ProjectID: e.project, RepositoryID: other, TicketID: e.task("Now in"), Mode: "implement"}), designSystemDir); len(got) != 3 {
		t.Errorf("after opting in the next shell lists %v", got)
	}
}

func TestDesignSystemView_TheFilesAreTheContractsFormatsForAFixedRecord(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	e.saveDesignSystem(0, shellDesignRecord())
	e.useDesignSystem(e.repo, true)
	a := e.start(e.task("Formats"))
	date := e.designSystemDate()

	wantDesign := `---
name: "harness"
description: "Calm surfaces, one signal colour. No decoration."
colors:
  signal: "oklch(0.5 0.095 182)"
  canvas: "#fff"
spacing:
  md: "1rem"
typography:
  headline:
    fontFamily: "Inter, sans-serif"
    fontSize: "1.5rem"
    fontWeight: "600"
---
<!-- design system version 1 · ` + date + ` -->

Calm surfaces, one signal colour.
No decoration.

## Colour

Use signal sparingly.
`
	if got := e.designFile(a, "DESIGN.md"); got != wantDesign {
		t.Errorf("DESIGN.md:\n%s\n--- want ---\n%s", got, wantDesign)
	}

	wantTokens := `{
  "$version": 1,
  "$date": "` + date + `",
  "colors": {
    "signal": {
      "$value": "oklch(0.5 0.095 182)",
      "$type": "color",
      "$description": "Primary action"
    },
    "canvas": {
      "$value": "#fff",
      "$type": "color",
      "$description": "Page background: \"white\""
    }
  },
  "spacing": {
    "md": {
      "$value": "1rem",
      "$type": "dimension",
      "$description": ""
    }
  },
  "typography": {
    "headline": {
      "$value": {
        "fontFamily": "Inter, sans-serif",
        "fontSize": "1.5rem",
        "fontWeight": "600"
      },
      "$type": "typography",
      "$description": "Page titles"
    }
  }
}
`
	if got := e.designFile(a, "tokens.json"); got != wantTokens {
		t.Errorf("tokens.json:\n%s\n--- want ---\n%s", got, wantTokens)
	}

	// Both parse.
	var tokens map[string]any
	if err := json.Unmarshal([]byte(e.designFile(a, "tokens.json")), &tokens); err != nil {
		t.Errorf("tokens.json is not JSON: %v", err)
	}
	front := strings.SplitN(e.designFile(a, "DESIGN.md"), "---\n", 3)[1]
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(front), &parsed); err != nil || parsed["name"] != "harness" {
		t.Errorf("the frontmatter is not YAML: %v %v", err, parsed)
	}

	readme := e.designFile(a, "README.md")
	for _, want := range []string{
		"# Project design system\n",
		"It is version 1, saved " + date + ".",
		"- `tokens.json`: the tokens (colors, spacing, type, radii, shadows, durations) with their type and note, in the W3C Design Tokens format.\n- `DESIGN.md`: the same tokens as YAML frontmatter, then the principles: when and why to use what.\n",
		"`--typography-headline-font-family`",
		"## Changing it\n",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md lacks %q:\n%s", want, readme)
		}
	}
}

func TestDesignSystemView_AnEmptyDesignSystemHasOnlyAReadmeThatSaysSo(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	e.useDesignSystem(e.repo, true)
	a := e.start(e.task("Empty"))
	if got := strings.Join(e.listing(a, designSystemDir), ","); got != "README.md" {
		t.Fatalf("lists %q", got)
	}
	if text := e.designFile(a, "README.md"); !strings.Contains(text, "no design system yet") || strings.Contains(text, "tokens.json") {
		t.Errorf("README.md = %q", text)
	}
}

func TestDesignSystemView_TheIndexListsTheFilesAsReadOnlyProjectDesignSystemFiles(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	e.saveDesignSystem(0, shellDesignRecord())
	e.useDesignSystem(e.repo, true)
	a := e.start(e.task("Index"))
	for _, name := range []string{"README.md", "DESIGN.md", "tokens.json"} {
		row := indexRow(t, a.shell, designSystemDir+"/"+name)
		if row == nil {
			t.Fatalf("no index row for %s", name)
		}
		if row["kind"] != "design-system" || row["scope"] != "project" || row["writable"] == true {
			t.Errorf("%s row = %v", name, row)
		}
		if info, _ := os.Stat(filepath.Join(a.shell, designSystemDir, name)); info.Mode().Perm()&0o222 != 0 {
			t.Errorf("%s is writable: %v", name, info.Mode())
		}
	}
}

func TestDesignSystemView_AnyChangeUnderTheFolderIsRefusedAsReadOnlyAndNamesADesignSession(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	e.saveDesignSystem(0, shellDesignRecord())
	e.useDesignSystem(e.repo, true)
	a := e.start(e.task("Read only"))
	file := designSystemDir + "/DESIGN.md"
	for name, c := range map[string]struct {
		op    string
		paths []string
	}{
		"write":  {"write", []string{file}},
		"create": {"write", []string{designSystemDir + "/new.md"}},
		"rm":     {"rm", []string{file}},
		"mv out": {"mv", []string{file, "project/docs/x.md"}},
		"mv in":  {"mv", []string{"project/docs/x.md", file}},
		"touch":  {"touch", []string{designSystemDir + "/tokens.json"}},
		"mkdir":  {"mkdir", []string{designSystemDir + "/sub"}},
	} {
		ans, raw := e.shellCall(a, "check", c.op, c.paths...)
		if ans.Allow || !strings.HasPrefix(ans.Reason, "READ_ONLY: ") ||
			!strings.Contains(ans.Reason, "only through a design session started by the person") || !strings.Contains(ans.Reason, "web UI") {
			t.Errorf("%s: %s", name, raw)
		}
	}
	// The hook says the same.
	if out := runHook(t, a, e.srv.URL, "PreToolUse", editTools, editInput("Write", filepath.Join(a.shell, filepath.FromSlash(file)))); !strings.Contains(out, "READ_ONLY") {
		t.Errorf("PreToolUse Write = %s", out)
	}
}

func TestDesignSystemView_ASaveOrARenameRefreshesARunningSessionsView(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	e.saveDesignSystem(0, shellDesignRecord())
	e.useDesignSystem(e.repo, true)
	a := e.start(e.task("Refresh"))

	changed := shellDesignRecord()
	changed["tokens"] = append(changed["tokens"].([]any), map[string]any{"group": "radii", "name": "md", "type": "dimension", "value": "6px", "note": ""})
	e.saveDesignSystem(1, changed)
	eventually(t, "the saved version in the running view", func() error {
		if !strings.Contains(e.designFile(a, "DESIGN.md"), "version 2 · ") || !strings.Contains(e.designFile(a, "tokens.json"), `"radii"`) ||
			!strings.Contains(e.designFile(a, "README.md"), "version 2,") {
			return os.ErrNotExist
		}
		return nil
	})
	if row := indexRow(t, a.shell, designSystemDir+"/tokens.json"); row == nil || row["version"] == "" {
		t.Errorf("row = %v", row)
	}

	e.post("rename_design_token", map[string]any{"project_id": e.project, "base_version": 2, "group": "radii", "name": "md", "new_group": "radius", "new_name": "md"}, nil)
	eventually(t, "the rename in the running view", func() error {
		if !strings.Contains(e.designFile(a, "DESIGN.md"), "radius:") || !strings.Contains(e.designFile(a, "DESIGN.md"), "version 3 · ") {
			return os.ErrNotExist
		}
		return nil
	})
}
