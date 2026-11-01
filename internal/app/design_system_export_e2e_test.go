//go:build !windows

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportRepo is a repository of the design server on a real checkout whose
// origin is a local bare repository, so a test can prove an export never
// pushes.
type exportRepo struct {
	id       string
	checkout string
	bare     string
}

// addExportRepo makes a repository with a checkout on main and an origin, and
// applies the server's design system to it (path "": the default).
func (s *designServer) addExportRepo(name, path string) *exportRepo {
	s.t.Helper()
	dir := s.t.TempDir()
	r := &exportRepo{checkout: filepath.Join(dir, name), bare: filepath.Join(dir, "origin.git")}
	gitInit(s.t, r.checkout)
	gitE2E(s.t, dir, "init", "-q", "--bare", r.bare)
	gitE2E(s.t, r.checkout, "remote", "add", "origin", r.bare)
	gitE2E(s.t, r.checkout, "push", "-q", "origin", "main")
	repo, err := s.cat.Projects.CreateRepository(s.ctx, s.project, name, "", "file://"+r.checkout, r.checkout)
	if err != nil {
		s.t.Fatal(err)
	}
	r.id = repo.ID
	s.mustApply(r.id, s.ds, path, false)
	return r
}

func (s *designServer) export(r *exportRepo, confirm bool) (int, map[string]any) {
	body := map[string]any{"repository_id": r.id}
	if confirm {
		body["confirm_edited_outside"] = true
	}
	return s.post("export_design_system", body)
}

func (s *designServer) mustExport(r *exportRepo, confirm bool) map[string]any {
	s.t.Helper()
	status, out := s.export(r, confirm)
	if status != http.StatusOK {
		s.t.Fatalf("export: %d %v", status, out)
	}
	return out
}

func filesOfHead(t *testing.T, checkout string) string {
	t.Helper()
	return gitE2E(t, checkout, "show", "--name-only", "--format=", "HEAD")
}

func TestDesignSystemExport_CommitsTheViewFilesWithoutReadmeAtTheDefaultAndACustomPathAndPushesNothing(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	for _, c := range []struct{ name, path, folder string }{
		{"spillway", "", ".harness/design-system/"},
		{"harbor", "ui/design/", "ui/design/"},
	} {
		r := s.addExportRepo(c.name, c.path)
		before := gitE2E(t, r.checkout, "rev-parse", "HEAD")
		remoteBefore := gitE2E(t, r.bare, "rev-parse", "main")

		got := s.mustExport(r, false)
		head := gitE2E(t, r.checkout, "rev-parse", "HEAD")
		if got["commit"] != head || got["version"] != float64(1) || got["state"] != "in_sync" || head == before {
			t.Errorf("%s: %v head %s before %s", c.name, got, head, before)
		}
		if n := gitE2E(t, r.checkout, "rev-list", "--count", before+"..HEAD"); n != "1" {
			t.Errorf("%s: %s commits", c.name, n)
		}
		if msg := gitE2E(t, r.checkout, "log", "-1", "--format=%s"); msg != "design system: export version 1" {
			t.Errorf("%s: message %q", c.name, msg)
		}
		want := strings.Join([]string{c.folder + "DESIGN.md", c.folder + "components/button.html", c.folder + "tokens.json"}, "\n")
		if files := filesOfHead(t, r.checkout); files != want {
			t.Errorf("%s: files\n%s\nwant\n%s", c.name, files, want)
		}
		tokens, err := os.ReadFile(filepath.Join(r.checkout, c.folder, "tokens.json"))
		if err != nil || !strings.Contains(string(tokens), `"$version": 1`) {
			t.Errorf("%s: tokens.json %q %v", c.name, tokens, err)
		}
		if remote := gitE2E(t, r.bare, "rev-parse", "main"); remote != remoteBefore {
			t.Errorf("%s: the export pushed", c.name)
		}
		if gitE2E(t, r.checkout, "status", "--porcelain") != "" {
			t.Errorf("%s: checkout not clean", c.name)
		}
		s.sawOnFeed("design_system_export", `"repository_id":"`+r.id+`"`, `"design_system_id":"`+s.ds+`"`, `"state":"in_sync"`)
	}
}

func TestDesignSystemExport_DeletesTheFileOfAComponentNoLongerInTheRecord(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	r := s.addExportRepo("spillway", "")
	s.mustExport(r, false)
	file := filepath.Join(r.checkout, ".harness/design-system/components/button.html")
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	s.mustSave(1, dsRecord())

	got := s.mustExport(r, false)
	if got["version"] != float64(2) {
		t.Errorf("%v", got)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the component file is still there: %v", err)
	}
	if files := filesOfHead(t, r.checkout); !strings.Contains(files, "components/button.html") || !strings.Contains(files, "tokens.json") {
		t.Errorf("the commit names %q", files)
	}
	if gitE2E(t, r.checkout, "status", "--porcelain") != "" {
		t.Error("checkout not clean")
	}
}

func TestDesignSystemExport_ExportingTheSameVersionAgainMakesNoSecondCommit(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	r := s.addExportRepo("spillway", "")
	first := s.mustExport(r, false)
	head := gitE2E(t, r.checkout, "rev-parse", "HEAD")

	second := s.mustExport(r, false)
	if second["commit"] != first["commit"] || second["state"] != "in_sync" || gitE2E(t, r.checkout, "rev-parse", "HEAD") != head {
		t.Errorf("first %v second %v head %s", first, second, head)
	}
}

func TestDesignSystemExport_IsRefusedWithAReasonAndWritesNothing(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	r := s.addExportRepo("spillway", "")
	untouched := func(why string) {
		t.Helper()
		if gitE2E(t, r.checkout, "log", "--oneline", "-5") != gitE2E(t, r.checkout, "log", "--oneline", "-1") {
			t.Errorf("%s: a commit was made", why)
		}
		if _, err := os.Stat(filepath.Join(r.checkout, ".harness")); !os.IsNotExist(err) {
			t.Errorf("%s: the folder was written: %v", why, err)
		}
	}

	if status, out := s.export(&exportRepo{id: "nope"}, false); status != http.StatusNotFound || out["code"] != "REPOSITORY_NOT_FOUND" {
		t.Errorf("unknown repository: %d %v", status, out)
	}

	if err := os.WriteFile(filepath.Join(r.checkout, "wip.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, out := s.export(r, false); status != http.StatusConflict || out["code"] != "CHECKOUT_DIRTY" {
		t.Errorf("dirty: %d %v", status, out)
	}
	untouched("dirty")
	if err := os.Remove(filepath.Join(r.checkout, "wip.txt")); err != nil {
		t.Fatal(err)
	}

	gitE2E(t, r.checkout, "checkout", "-q", "-b", "feature")
	status, out := s.export(r, false)
	if status != http.StatusConflict || out["code"] != "WRONG_BRANCH" || !strings.Contains(out["error"].(string), "feature") {
		t.Errorf("wrong branch: %d %v", status, out)
	}
	gitE2E(t, r.checkout, "checkout", "-q", "main")
	untouched("wrong branch")

	s.mustApply(r.id, nil, "", false)
	if status, out := s.export(r, false); status != http.StatusConflict || out["code"] != "NO_DESIGN_SYSTEM" {
		t.Errorf("no design system: %d %v", status, out)
	}
	untouched("no design system")
}

// exportOf is the export object of the repository's row in the Repositories
// tab of the design system ds.
func (s *designServer) exportOf(ds string, r *exportRepo) map[string]any {
	s.t.Helper()
	out := s.mustPost("list_design_system_repositories", map[string]any{"design_system_id": ds})
	for _, row := range out["repositories"].([]any) {
		if m := row.(map[string]any); m["repository_id"] == r.id {
			exp, _ := m["export"].(map[string]any)
			return exp
		}
	}
	s.t.Fatalf("repository %s not listed", r.id)
	return nil
}

func (s *designServer) stateOf(r *exportRepo) string {
	s.t.Helper()
	exp := s.exportOf(s.ds, r)
	if exp == nil {
		s.t.Fatal("no export object")
	}
	return exp["state"].(string)
}

func TestDesignSystemExport_StateIsNeverExportedInSyncBehindAndEditedOutside(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	r := s.addExportRepo("spillway", "")
	if exp := s.exportOf(s.ds, r); exp["state"] != "never_exported" || exp["current_version"] != float64(1) || exp["exported_version"] != nil {
		t.Errorf("before: %v", exp)
	}

	got := s.mustExport(r, false)
	exp := s.exportOf(s.ds, r)
	if exp["state"] != "in_sync" || exp["exported_version"] != float64(1) || exp["exported_commit"] != got["commit"] || exp["exported_at"] == nil {
		t.Errorf("after export: %v", exp)
	}

	s.mustSave(1, dsRecord(buttonComponent()))
	if exp := s.exportOf(s.ds, r); exp["state"] != "behind" || exp["exported_version"] != float64(1) || exp["current_version"] != float64(2) {
		t.Errorf("after a save: %v", exp)
	}
	s.mustExport(r, false)

	file := filepath.Join(r.checkout, ".harness/design-system/tokens.json")
	if err := os.WriteFile(file, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, r.checkout, "commit", "-q", "-am", "hand edit")
	exp = s.exportOf(s.ds, r)
	files, _ := exp["changed_files"].([]any)
	if exp["state"] != "edited_outside" || len(files) != 1 || files[0] != "tokens.json" {
		t.Errorf("after an edit: %v", exp)
	}

	if err := os.Remove(filepath.Join(r.checkout, ".harness/design-system/components/button.html")); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, r.checkout, "commit", "-q", "-am", "drop a component")
	if exp := s.exportOf(s.ds, r); exp["state"] != "edited_outside" || len(exp["changed_files"].([]any)) != 2 {
		t.Errorf("a missing file: %v", exp)
	}
}

func TestDesignSystemExport_OverEditedOutsideFilesNeedsConfirmationAndShowsTheDifferingLines(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	r := s.addExportRepo("spillway", "")
	s.mustExport(r, false)
	file := filepath.Join(r.checkout, ".harness/design-system/DESIGN.md")
	original, _ := os.ReadFile(file)
	if err := os.WriteFile(file, append(original, []byte("hand written line\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, r.checkout, "commit", "-q", "-am", "hand edit")
	head := gitE2E(t, r.checkout, "rev-parse", "HEAD")

	preview := s.mustPost("preview_design_system_export", map[string]any{"repository_id": r.id})["files"].([]any)
	status := map[string]string{}
	var diff string
	for _, f := range preview {
		m := f.(map[string]any)
		status[m["path"].(string)] = m["status"].(string)
		if strings.HasSuffix(m["path"].(string), "DESIGN.md") {
			diff, _ = m["diff"].(string)
		}
	}
	if status[".harness/design-system/DESIGN.md"] != "changed" || status[".harness/design-system/tokens.json"] != "unchanged" || status[".harness/design-system/components/button.html"] != "unchanged" {
		t.Errorf("preview statuses %v", status)
	}
	if !strings.Contains(diff, "-hand written line") {
		t.Errorf("the diff does not show the differing line:\n%s", diff)
	}
	if gitE2E(t, r.checkout, "rev-parse", "HEAD") != head {
		t.Error("the preview committed")
	}

	st, out := s.export(r, false)
	details, _ := out["details"].(map[string]any)
	files, _ := details["changed_files"].([]any)
	if st != http.StatusConflict || out["code"] != "EDITED_OUTSIDE" || len(files) != 1 || files[0] != "DESIGN.md" {
		t.Errorf("unconfirmed: %d %v", st, out)
	}
	if gitE2E(t, r.checkout, "rev-parse", "HEAD") != head {
		t.Error("a refused export committed")
	}

	got := s.mustExport(r, true)
	if got["state"] != "in_sync" || gitE2E(t, r.checkout, "rev-parse", "HEAD") == head {
		t.Errorf("confirmed: %v", got)
	}
	if now, _ := os.ReadFile(file); string(now) != string(original) {
		t.Error("the hand edit was not replaced")
	}
	if s.stateOf(r) != "in_sync" {
		t.Error("not in sync after the confirmed export")
	}
}

func TestDesignSystemExport_AfterAMoveTheOldFilesAreEditedOutsideAgainstTheNewDesignSystem(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	r := s.addExportRepo("spillway", "")
	s.mustExport(r, false)

	other := s.create("other", "Other")
	status, out := s.post("save_design_system", map[string]any{"design_system_id": other, "base_version": 0, "record": dsRecord()})
	if status != http.StatusOK {
		t.Fatalf("%d %v", status, out)
	}
	s.mustApply(r.id, other, "", true)

	if exp := s.exportOf(s.ds, r); exp != nil {
		t.Errorf("the old design system still reports an export: %v", exp)
	}
	exp := s.exportOf(other, r)
	if exp["state"] != "edited_outside" || exp["exported_version"] != float64(1) {
		t.Errorf("against the new design system: %v", exp)
	}
	st, body := s.post("export_design_system", map[string]any{"repository_id": r.id})
	if st != http.StatusConflict || body["code"] != "EDITED_OUTSIDE" {
		t.Errorf("unconfirmed: %d %v", st, body)
	}
	s.mustExport(r, true)
	if exp := s.exportOf(other, r); exp["state"] != "in_sync" {
		t.Errorf("after exporting: %v", exp)
	}
	if _, err := os.Stat(filepath.Join(r.checkout, ".harness/design-system/components/button.html")); !os.IsNotExist(err) {
		t.Errorf("the old design system's component file stayed: %v", err)
	}
}
