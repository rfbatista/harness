package httpapi

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func skillBody(name, description string) map[string]any {
	return map[string]any{"name": name, "description": description, "body": "# " + name + "\n\nSteps.", "files": []any{}, "deleted_files": []any{}}
}

func TestCatalogSkills_CreateWritesSkillMarkdownFilesAndWhoGetsIt(t *testing.T) {
	f := catalogWithSkills(t)
	skill := skillBody("review", "Review: code, carefully")
	skill["files"] = []map[string]any{
		{"path": "refs/guide.md", "content": "guide"},
		{"path": "bin/blob", "content": "//4A", "encoding": "base64"},
	}
	out := f.post("/api/save_catalog_skill", map[string]any{
		"skill":  skill,
		"add_to": map[string]any{"agents": []string{"zeta"}, "bundles": []string{"core"}},
	}, 200)

	if got, want := f.read("skills/review/SKILL.md"), "---\nname: review\ndescription: 'Review: code, carefully'\n---\n\n# review\n\nSteps.\n"; got != want {
		t.Fatalf("SKILL.md = %q, want %q", got, want)
	}
	if f.read("skills/review/refs/guide.md") != "guide" {
		t.Fatal("guide.md not written")
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "skills/review/bin/blob")); string(b) != "\xff\xfe\x00" {
		t.Fatalf("blob = %q", b)
	}
	cat := f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "zeta")["entries"]); !reflect.DeepEqual(got, []string{"tdd", "review"}) {
		t.Fatalf("zeta entries = %v", got)
	}
	if got := strList(byName(t, cat["bundles"], "core")["entries"]); !reflect.DeepEqual(got, []string{"tdd", "review"}) {
		t.Fatalf("core entries = %v", got)
	}
	sk := byName(t, cat["skills"], "review")
	if sk["description"] != "Review: code, carefully" || sk["file_count"] != float64(3) {
		t.Fatalf("skill = %v", sk)
	}
	if got := problemCodes(sk); len(got) != 0 {
		t.Fatalf("problems = %v", got)
	}

	writes := map[string]map[string]any{}
	for _, w := range list(out["change"].(map[string]any)["writes"]) {
		m := w.(map[string]any)
		writes[m["path"].(string)] = m
	}
	for _, p := range []string{"skills/review/SKILL.md", "skills/review/refs/guide.md", "skills/review/bin/blob", "agents.json"} {
		if writes[p] == nil {
			t.Fatalf("no write for %s: %v", p, writes)
		}
	}
	if writes["skills/review/bin/blob"]["encoding"] != "base64" {
		t.Fatalf("blob write = %v", writes["skills/review/bin/blob"])
	}
	if out["versions"].(map[string]any)["skills/review"] == nil {
		t.Fatalf("versions = %v", out["versions"])
	}
}

func TestCatalogSkills_AddToIsTheFullListOfWhoGetsIt(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/save_catalog_skill", map[string]any{
		"previous_name": "tdd", "skill": skillBody("tdd", "Test first"),
		"add_to": map[string]any{"agents": []string{}, "bundles": []string{}},
	}, 200)
	cat := f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "zeta")["entries"]); len(got) != 0 {
		t.Fatalf("zeta still has it: %v", got)
	}
	if got := strList(byName(t, cat["bundles"], "core")["entries"]); len(got) != 0 {
		t.Fatalf("core still has it: %v", got)
	}

	// Without add_to, who gets it stays as it is.
	f.post("/api/save_catalog_skill", map[string]any{
		"previous_name": "design", "skill": skillBody("design", "Design more"),
	}, 200)
	if !strings.Contains(f.read("skills/design/SKILL.md"), "Design more") {
		t.Fatal("the description was not saved")
	}

	bad := f.post("/api/save_catalog_skill", map[string]any{
		"previous_name": "design", "skill": skillBody("design", "d"),
		"add_to": map[string]any{"agents": []string{"ghost"}, "bundles": []string{"phantom"}},
	}, 400)
	if bad["code"] != "UNKNOWN_REFERENCE" || !reflect.DeepEqual(strList(bad["names"]), []string{"ghost", "@phantom"}) {
		t.Fatalf("body = %v", bad)
	}
}

func TestCatalogSkills_UpdateKeepsOtherFrontmatterAndEditsFiles(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Old\nlicense: MIT\nallowed-tools: Read\n---\n\nold body\n")
	f.write("skills/tdd/old.md", "old")
	f.write("skills/tdd/keep.md", "keep")
	skill := skillBody("tdd", "New description")
	skill["files"] = []map[string]any{{"path": "new.md", "content": "new"}}
	skill["deleted_files"] = []string{"old.md"}
	f.post("/api/save_catalog_skill", map[string]any{"previous_name": "tdd", "skill": skill}, 200)

	if got, want := f.read("skills/tdd/SKILL.md"), "---\nname: tdd\ndescription: New description\nlicense: MIT\nallowed-tools: Read\n---\n\n# tdd\n\nSteps.\n"; got != want {
		t.Fatalf("SKILL.md = %q, want %q", got, want)
	}
	if f.exists("skills/tdd/old.md") || f.read("skills/tdd/new.md") != "new" || f.read("skills/tdd/keep.md") != "keep" {
		t.Fatal("files were not edited as asked")
	}
}

func TestCatalogSkills_RenameMovesTheFolderAndRewritesReferences(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("skills/tdd/refs/deep.md", "deep")
	skill := skillBody("tdd-loop", "Test first")
	out := f.post("/api/save_catalog_skill", map[string]any{
		"previous_name": "tdd", "skill": skill,
		"add_to": map[string]any{"agents": []string{"zeta"}, "bundles": []string{"core"}},
	}, 200)
	if f.exists("skills/tdd") {
		t.Fatal("the old folder is still there")
	}
	if f.read("skills/tdd-loop/refs/deep.md") != "deep" {
		t.Fatal("the folder's files did not move")
	}
	cat := f.get("/api/get_agent_catalog", 200)
	if got := strList(byName(t, cat["agents"], "zeta")["entries"]); !reflect.DeepEqual(got, []string{"tdd-loop"}) {
		t.Fatalf("zeta entries = %v", got)
	}
	if !strings.Contains(out["change"].(map[string]any)["summary"].(string), "Renamed tdd to tdd-loop") {
		t.Fatalf("summary = %v", out["change"])
	}
	taken := f.post("/api/save_catalog_skill", map[string]any{"previous_name": "tdd-loop", "skill": skillBody("design", "d")}, 409)
	if taken["code"] != "NAME_TAKEN" || taken["suggestion"] != "design-2" {
		t.Fatalf("body = %v", taken)
	}
}

func TestCatalogSkills_DeleteRemovesTheFolderAndEveryReferenceAndUndoBringsItBack(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("skills/tdd/refs/deep.md", "deep")
	f.write("skills/tdd/bin", "\xff\x00\xfe")
	agentsBefore := f.read("agents.json")
	out := f.post("/api/delete_catalog_skill", map[string]any{"name": "tdd"}, 200)
	if f.exists("skills/tdd") {
		t.Fatal("the skill folder is still there")
	}
	if strings.Contains(f.read("agents.json"), `"tdd"`) {
		t.Fatal("a reference to the skill is left")
	}
	f.post("/api/delete_catalog_skill", map[string]any{"name": "tdd"}, 404)

	f.post("/api/undo_catalog_change", map[string]any{"change_id": out["change"].(map[string]any)["id"]}, 200)
	if f.read("skills/tdd/refs/deep.md") != "deep" || f.read("skills/tdd/bin") != "\xff\x00\xfe" || f.read("agents.json") != agentsBefore {
		t.Fatal("the undo did not bring everything back")
	}
	if !f.exists("skills/tdd/SKILL.md") {
		t.Fatal("SKILL.md was not restored")
	}
}

func TestCatalogSkills_RefuseBadInput(t *testing.T) {
	f := catalogWithSkills(t)
	long := strings.Repeat("x", 1025)
	cases := []struct {
		name  string
		skill map[string]any
		field string
	}{
		{"bad name", skillBody("Bad Name", "d"), "name"},
		{"no description", skillBody("ok", " "), "description"},
		{"description too long", skillBody("ok", long), "description"},
		{"dotdot path", withFile(skillBody("ok", "d"), "../escape.md"), "files"},
		{"nested dotdot path", withFile(skillBody("ok", "d"), "a/../../escape.md"), "files"},
		{"absolute path", withFile(skillBody("ok", "d"), "/etc/passwd"), "files"},
		{"skill markdown path", withFile(skillBody("ok", "d"), "SKILL.md"), "files"},
		{"bad base64", func() map[string]any {
			s := skillBody("ok", "d")
			s["files"] = []map[string]any{{"path": "a", "content": "***", "encoding": "base64"}}
			return s
		}(), "files"},
	}
	for _, c := range cases {
		got := f.post("/api/save_catalog_skill", map[string]any{"skill": c.skill}, 400)
		if got["code"] != "INVALID_INPUT" || got["field"] != c.field {
			t.Errorf("%s: body = %v", c.name, got)
		}
	}
	if f.exists("skills/ok") || f.exists("escape.md") || f.exists("skills/escape.md") {
		t.Fatal("a refused save wrote files")
	}
	f.post("/api/save_catalog_skill", map[string]any{"previous_name": "ghost", "skill": skillBody("x", "d")}, 404)
}

func withFile(skill map[string]any, path string) map[string]any {
	skill["files"] = []map[string]any{{"path": path, "content": "x"}}
	return skill
}

func TestCatalogSkills_AFailurePartWayLeavesTheCatalogAsItWas(t *testing.T) {
	f := catalogWithSkills(t)
	// "refs" is a file, so refs/one.md cannot be written: the save fails after
	// the files before it were written.
	f.write("skills/tdd/refs", "a file where a folder is needed")
	skill := skillBody("tdd", "Changed description")
	skill["files"] = []map[string]any{{"path": "aaa.md", "content": "a"}, {"path": "refs/one.md", "content": "b"}}
	before := f.read("skills/tdd/SKILL.md")
	rec := f.postRaw("/api/save_catalog_skill", map[string]any{"previous_name": "tdd", "skill": skill})
	if rec.Code < 400 {
		t.Fatalf("the save succeeded: %d %s", rec.Code, rec.Body.String())
	}
	if f.read("skills/tdd/SKILL.md") != before || f.exists("skills/tdd/aaa.md") || f.read("skills/tdd/refs") != "a file where a folder is needed" {
		t.Fatal("the failed save left a partial write")
	}
}

func TestCatalogSkills_DryRunListsEveryFile(t *testing.T) {
	f := catalogWithSkills(t)
	skill := withFile(skillBody("preview", "d"), "x.md")
	out := f.post("/api/save_catalog_skill", map[string]any{"dry_run": true, "skill": skill}, 200)
	paths := []string{}
	for _, w := range list(out["change"].(map[string]any)["writes"]) {
		paths = append(paths, w.(map[string]any)["path"].(string))
	}
	if !reflect.DeepEqual(paths, []string{"skills/preview/SKILL.md", "skills/preview/x.md"}) {
		t.Fatalf("paths = %v", paths)
	}
	if f.exists("skills/preview") {
		t.Fatal("a dry run wrote")
	}
}

func TestCatalogSkills_AStaleSkillVersionIsRefused(t *testing.T) {
	f := catalogWithSkills(t)
	opened := f.get("/api/get_catalog_skill?name=tdd", 200)["versions"]
	f.write("skills/tdd/extra.md", "added by hand")
	got := f.post("/api/save_catalog_skill", map[string]any{
		"base_versions": opened, "previous_name": "tdd", "skill": skillBody("tdd", "Changed"),
	}, 409)
	if got["code"] != "CATALOG_CHANGED" {
		t.Fatalf("body = %v", got)
	}
}
