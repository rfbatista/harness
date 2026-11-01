//go:build !windows

package app

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// importFixture is a git repository whose default branch holds a copy of the
// harness's own DESIGN.md and design-system/tokens.json.
func (s *designServer) importFixture(name string, files map[string]string) (repositoryID, checkout string) {
	s.t.Helper()
	checkout = gitFixture(s.t, name, files)
	repo, err := s.cat.Projects.CreateRepository(s.ctx, s.project, name, "", "file://"+checkout, checkout)
	if err != nil {
		s.t.Fatal(err)
	}
	return repo.ID, checkout
}

// gitFixture is a git checkout of the given files, committed on main.
func gitFixture(t *testing.T, name string, files map[string]string) (checkout string) {
	t.Helper()
	checkout = filepath.Join(t.TempDir(), name)
	gitInit(t, checkout)
	for path, content := range files {
		full := filepath.Join(checkout, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=e2e@test", "-c", "user.name=e2e", "commit", "-q", "--allow-empty", "-m", "files"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return checkout
}

func harnessFiles(t *testing.T) map[string]string {
	t.Helper()
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join("testdata", "design-import", p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	return map[string]string{"DESIGN.md": read("DESIGN.md"), "design-system/tokens.json": read("design-system/tokens.json")}
}

func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain=v1")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}

func skippedPaths(out map[string]any) map[string]string {
	got := map[string]string{}
	for _, e := range out["skipped"].([]any) {
		m := e.(map[string]any)
		got[m["path"].(string)] = m["reason"].(string)
	}
	return got
}

func (s *designServer) designSystemSlugs() []string {
	var slugs []string
	for _, d := range s.mustPost("list_design_systems", map[string]any{})["design_systems"].([]any) {
		slugs = append(slugs, d.(map[string]any)["slug"].(string))
	}
	return slugs
}

func TestDesignImport_SourcesListTheRepositoriesHoldingDesignFiles(t *testing.T) {
	s := startDesignServer(t)
	harness, _ := s.importFixture("harness", harnessFiles(t))
	s.importFixture("plain", map[string]string{"README.md": "# plain\n"})

	out := s.mustPost("find_design_system_sources", map[string]any{})
	repos := out["repositories"].([]any)
	if len(repos) != 1 {
		t.Fatalf("sources = %v", repos)
	}
	got := repos[0].(map[string]any)
	if got["repository_id"] != harness || got["name"] != "harness" || got["design_md"] != "DESIGN.md" || got["tokens"] != "design-system/tokens.json" {
		t.Errorf("source = %v", got)
	}
}

func TestDesignImport_PreviewCountsWhatParsedAndListsWhatWasSkippedWithoutWriting(t *testing.T) {
	s := startDesignServer(t)
	harness, checkout := s.importFixture("harness", harnessFiles(t))

	out := s.mustPost("preview_design_system_import", map[string]any{"repository_id": harness, "tokens_path": "design-system/tokens.json"})

	found := out["found"].(map[string]any)
	if found["design_md"] != true || found["tokens"] != true || found["components"] != float64(0) {
		t.Errorf("found = %v", found)
	}
	counts := out["counts"].(map[string]any)
	if counts["colors"] != float64(23) {
		t.Errorf("colors = %v; counts = %v", counts["colors"], counts)
	}
	if counts["font"] != float64(2) || counts["typography"] == nil {
		t.Errorf("counts = %v", counts)
	}
	skipped := skippedPaths(out)
	for _, path := range []string{"$comment", "themes.dark", "utilities"} {
		if skipped[path] == "" {
			t.Errorf("%s is not reported as skipped with a reason: %v", path, skipped)
		}
	}
	record := out["record"].(map[string]any)
	if !strings.Contains(record["principles"].(string), "## 1. Overview") {
		t.Errorf("principles = %.80q", record["principles"])
	}
	var sans map[string]any
	for _, tok := range record["tokens"].([]any) {
		if m := tok.(map[string]any); m["group"] == "font" && m["name"] == "sans" {
			sans = m
		}
	}
	if sans == nil || sans["type"] != "font" {
		t.Errorf("font.sans = %v", sans)
	}

	if got := s.designSystemSlugs(); len(got) != 1 {
		t.Errorf("a preview stored a design system: %v", got)
	}
	if st := gitStatus(t, checkout); st != "" {
		t.Errorf("a preview changed the checkout: %q", st)
	}
}

func TestDesignImport_AFileThatCannotBeReadIsNamedAndTheImportStoresNothing(t *testing.T) {
	s := startDesignServer(t)
	harness, _ := s.importFixture("harness", harnessFiles(t))
	broken, _ := s.importFixture("broken", map[string]string{"DESIGN.md": "---\nname: [unclosed\n---\n", "tokens.json": "{ nope"})

	out := s.mustPost("preview_design_system_import", map[string]any{"repository_id": harness, "tokens_path": "missing/tokens.json"})
	if out["found"].(map[string]any)["tokens"] != false || skippedPaths(out)["missing/tokens.json"] == "" {
		t.Errorf("a missing tokens file is not named: %v", out)
	}

	status, out := s.post("preview_design_system_import", map[string]any{"repository_id": broken})
	if status != http.StatusBadRequest || out["code"] != "IMPORT_UNREADABLE" {
		t.Fatalf("%d %v", status, out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "tokens.json") || !strings.Contains(msg, "DESIGN.md") {
		t.Errorf("the message does not name the files: %q", msg)
	}

	half := harnessFiles(t)
	half["design-system/tokens.json"] = "{ nope"
	halfBroken, _ := s.importFixture("half-broken", half)
	status, out = s.post("import_design_system", map[string]any{"repository_id": halfBroken, "tokens_path": "design-system/tokens.json", "name": "Half", "slug": "half"})
	if msg, _ := out["error"].(string); status != http.StatusBadRequest || out["code"] != "IMPORT_UNREADABLE" || !strings.Contains(msg, "design-system/tokens.json") {
		t.Fatalf("one file unparseable, the other fine: %d %v", status, out)
	}

	status, out = s.post("import_design_system", map[string]any{"repository_id": broken, "name": "Broken", "slug": "broken"})
	if status != http.StatusBadRequest || out["code"] != "IMPORT_UNREADABLE" {
		t.Fatalf("import: %d %v", status, out)
	}
	if got := s.designSystemSlugs(); len(got) != 1 {
		t.Errorf("a failed import stored a design system: %v", got)
	}
}

func TestDesignImport_ImportCreatesANewDesignSystemAtVersionOneAndNeverOverwrites(t *testing.T) {
	s := startDesignServer(t)
	harness, _ := s.importFixture("harness", harnessFiles(t))
	body := func(slug, name string) map[string]any {
		return map[string]any{"repository_id": harness, "tokens_path": "design-system/tokens.json", "name": name, "slug": slug, "description": "from harness"}
	}

	out := s.mustPost("import_design_system", body("harness-look", "Harness look"))
	id, _ := out["design_system_id"].(string)
	if id == "" || out["version"] != float64(1) {
		t.Fatalf("import = %v", out)
	}
	got := s.mustPost("get_design_system", map[string]any{"design_system_id": id})
	if got["name"] != "Harness look" || got["description"] != "from harness" || got["version"] != float64(1) || got["updated_by"] != "import" {
		t.Errorf("design system = name %v, description %v, version %v, by %v", got["name"], got["description"], got["version"], got["updated_by"])
	}
	if n := len(got["record"].(map[string]any)["tokens"].([]any)); n < 50 {
		t.Errorf("%d tokens imported", n)
	}

	versions := s.mustPost("list_design_system_versions", map[string]any{"design_system_id": id})["versions"].([]any)
	v1 := versions[0].(map[string]any)
	if len(versions) != 1 || v1["version"] != float64(1) || v1["source"] != "import" || v1["by"] != "import" {
		t.Fatalf("versions = %v", versions)
	}
	if repo, _ := v1["repository"].(map[string]any); repo["repository_id"] != harness || repo["name"] != "harness" {
		t.Errorf("version 1 does not name the repository: %v", v1)
	}

	second := s.mustPost("import_design_system", body("harness-copy", "Harness copy"))
	if second["design_system_id"] == id {
		t.Error("a second import reused the first design system")
	}

	status, refused := s.post("import_design_system", body("harness-look", "Another name"))
	if status != http.StatusConflict || refused["code"] != "SLUG_TAKEN" {
		t.Fatalf("%d %v", status, refused)
	}
	if after := s.mustPost("get_design_system", map[string]any{"design_system_id": id}); after["name"] != "Harness look" || after["version"] != float64(1) {
		t.Errorf("a refused import changed the first design system: %v", after)
	}
	if slugs := s.designSystemSlugs(); len(slugs) != 3 {
		t.Errorf("slugs = %v", slugs)
	}
}

func TestDesignImport_ATokenNameMayStartWithADigitAndEasingIsAType(t *testing.T) {
	s := startDesignServer(t)
	record := map[string]any{
		"tokens": []any{
			dsToken("space", "2xl", "dimension", "2rem"),
			dsToken("ease", "out", "easing", "cubic-bezier(0.25, 1, 0.5, 1)"),
		},
		"principles": "", "components": []any{},
	}
	s.mustSave(0, record)

	if status, out := s.post("save_design_system", map[string]any{"base_version": 1, "record": map[string]any{
		"tokens": []any{dsToken("space", "-xl", "dimension", "1rem")}, "principles": "", "components": []any{},
	}}); status != http.StatusBadRequest || out["code"] != "DESIGN_SYSTEM_INVALID" {
		t.Errorf("a name that starts with a dash: %d %v", status, out)
	}
}

func TestDesignImport_TheHarnessFixtureKeepsItsDigitNamesAndEasingTokens(t *testing.T) {
	s := startDesignServer(t)
	harness, _ := s.importFixture("harness", harnessFiles(t))

	out := s.mustPost("preview_design_system_import", map[string]any{"repository_id": harness, "tokens_path": "design-system/tokens.json"})

	kept := map[string]string{}
	for _, tok := range out["record"].(map[string]any)["tokens"].([]any) {
		m := tok.(map[string]any)
		kept[m["group"].(string)+"."+m["name"].(string)] = m["type"].(string)
	}
	for key, typ := range map[string]string{"space.2xl": "dimension", "space.3xs": "dimension", "text.2xl": "dimension", "spacing.3xl": "dimension", "ease.out": "easing", "ease.out-expo": "easing"} {
		if kept[key] != typ {
			t.Errorf("%s = %q, want %q", key, kept[key], typ)
		}
	}
	skipped := skippedPaths(out)
	for path := range skipped {
		if strings.HasPrefix(path, "space.") || strings.HasPrefix(path, "ease.") || strings.HasPrefix(path, "text.") {
			t.Errorf("%s is still skipped: %s", path, skipped[path])
		}
	}
}

func TestDesignImport_AFailingVersionWriteLeavesNoDesignSystem(t *testing.T) {
	s := startDesignServer(t)
	harness, _ := s.importFixture("harness", harnessFiles(t))
	project, err := s.cat.Projects.GetProject(s.ctx, s.project)
	if err != nil {
		t.Fatal(err)
	}
	// A second connection to the server's database makes the version insert fail.
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(project.RootDir, "harness.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_version BEFORE INSERT ON design_system_versions BEGIN SELECT RAISE(ABORT, 'no versions today'); END`).Error; err != nil {
		t.Fatal(err)
	}

	status, out := s.post("import_design_system", map[string]any{"repository_id": harness, "tokens_path": "design-system/tokens.json", "name": "Harness look", "slug": "harness-look"})
	if status < 500 {
		t.Fatalf("a failing version write answered %d %v", status, out)
	}
	if got := s.designSystemSlugs(); len(got) != 1 {
		t.Errorf("the failed import left a design system behind: %v", got)
	}

	if err := db.Exec(`DROP TRIGGER fail_version`).Error; err != nil {
		t.Fatal(err)
	}
	s.mustPost("import_design_system", map[string]any{"repository_id": harness, "tokens_path": "design-system/tokens.json", "name": "Harness look", "slug": "harness-look"})
}
