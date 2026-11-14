package httpapi

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/agentcatalog"
)

var builtInSkillNames = []string{
	"design-artifacts", "task-architecture", "task-code-review", "task-grill-with-docs", "task-implement-spec",
	"task-product", "task-run-app", "task-tdd", "task-to-spec",
}

func TestBuiltInSkills_AreListedBesideTheCatalogsWithTheirModes(t *testing.T) {
	f := newCatalogFixture(t)
	f.skill("zeta", "A catalog skill")
	cat := f.get("/api/get_agent_catalog", 200)

	var builtIn []string
	for _, s := range list(cat["skills"]) {
		m := s.(map[string]any)
		if m["built_in"] == true {
			builtIn = append(builtIn, m["name"].(string))
		}
	}
	if !reflect.DeepEqual(builtIn, builtInSkillNames) {
		t.Fatalf("built-in skills = %v", builtIn)
	}
	run := byName(t, cat["skills"], "task-run-app")
	if !reflect.DeepEqual(strList(run["modes"]), []string{"architect"}) || run["description"] == "" ||
		run["has_extra_files"] != true || run["file_count"].(float64) < 2 {
		t.Errorf("task-run-app = %v", run)
	}
	if len(list(run["problems"])) != 0 || run["problems"] == nil {
		t.Errorf("a built-in skill has no problems: %v", run["problems"])
	}
	zeta := byName(t, cat["skills"], "zeta")
	if zeta["built_in"] != false || zeta["modes"] == nil || len(list(zeta["modes"])) != 0 {
		t.Errorf("catalog skill built_in/modes = %v %v", zeta["built_in"], zeta["modes"])
	}
	for path := range cat["versions"].(map[string]any) {
		if strings.HasPrefix(path, "skills/") {
			t.Errorf("versions names %s: the read hashes no skill folder", path)
		}
	}
}

func TestBuiltInSkills_AreReadWithTheirBodyAndFilesButNoVersion(t *testing.T) {
	f := newCatalogFixture(t)
	got := f.get("/api/get_catalog_skill?name=task-run-app", 200)
	sk := got["skill"].(map[string]any)
	if sk["built_in"] != true || !reflect.DeepEqual(strList(sk["modes"]), []string{"architect"}) {
		t.Errorf("skill = %v %v", sk["built_in"], sk["modes"])
	}
	if sk["body"] == "" {
		t.Errorf("body = %q", sk["body"])
	}
	if len(list(sk["files"])) == 0 {
		t.Error("a built-in skill is read with its files")
	}
	if len(got["versions"].(map[string]any)) != 0 {
		t.Errorf("versions = %v", got["versions"])
	}
	f.get("/api/get_catalog_skill?name=nope", 404)
}

func TestBuiltInSkills_ACatalogSkillWithTheSameNameIsListedOnceAsTheBuiltInOne(t *testing.T) {
	f := newCatalogFixture(t)
	f.skill("task-tdd", "Mine")
	cat := f.get("/api/get_agent_catalog", 200)
	count := 0
	for _, s := range list(cat["skills"]) {
		if s.(map[string]any)["name"] == "task-tdd" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("task-tdd listed %d times", count)
	}
	tdd := byName(t, cat["skills"], "task-tdd")
	if tdd["built_in"] != true || tdd["description"] == "Mine" {
		t.Errorf("the catalog skill won: %v", tdd)
	}
	if got := problemCodes(tdd); !reflect.DeepEqual(got, []string{"SHADOWED_BY_BUILT_IN"}) {
		t.Errorf("problems = %v", got)
	}
	one := f.get("/api/get_catalog_skill?name=task-tdd", 200)["skill"].(map[string]any)
	if one["built_in"] != true || one["description"] == "Mine" {
		t.Errorf("get returned the catalog one: %v", one)
	}
}

func TestBuiltInSkills_EditingRenamingOrDeletingOneIsRefusedAndWritesNothing(t *testing.T) {
	f := newCatalogFixture(t)
	f.skill("mine", "Mine")
	for _, c := range []struct {
		name, path string
		body       map[string]any
	}{
		{"edit", "/api/save_catalog_skill", map[string]any{"previous_name": "task-run-app", "skill": skillBody("task-run-app", "Mine")}},
		{"rename from", "/api/save_catalog_skill", map[string]any{"previous_name": "task-run-app", "skill": skillBody("run-app", "Mine")}},
		{"delete", "/api/delete_catalog_skill", map[string]any{"name": "task-run-app"}},
	} {
		got := f.post(c.path, c.body, 409)
		msg, _ := got["error"].(string)
		if got["code"] != "SKILL_BUILT_IN" || !strings.Contains(msg, "ships with the server") || !strings.Contains(msg, "architect") {
			t.Errorf("%s: body = %v", c.name, got)
		}
	}
	if _, err := os.Stat(filepath.Join(f.dir, "skills", "task-run-app")); err == nil {
		t.Fatal("a refused write created a folder")
	}
}

func TestBuiltInSkills_ACatalogSkillCannotTakeABuiltInName(t *testing.T) {
	f := newCatalogFixture(t)
	f.skill("mine", "Mine")
	before := f.read("skills/mine/SKILL.md")
	for name, body := range map[string]map[string]any{
		"create": {"skill": skillBody("task-tdd", "Mine")},
		"rename": {"previous_name": "mine", "skill": skillBody("task-tdd", "Mine")},
	} {
		got := f.post("/api/save_catalog_skill", body, 409)
		msg, _ := got["error"].(string)
		if got["code"] != "NAME_TAKEN" || got["suggestion"] != "task-tdd-2" || !strings.Contains(msg, "built-in") {
			t.Errorf("%s: body = %v", name, got)
		}
	}
	if f.read("skills/mine/SKILL.md") != before {
		t.Fatal("a refused rename changed the skill")
	}
}

func TestBuiltInSkills_TheLegacyWritesRefuseTheSameWay(t *testing.T) {
	f := legacyFixture(t)
	if got := f.post("/api/create_skill", map[string]any{"name": "task-tdd", "description": "Mine"}, 409); got["code"] != "NAME_TAKEN" {
		t.Errorf("create: %v", got)
	}
	if got := f.post("/api/update_skill", map[string]any{"skill_id": "task-tdd", "description": "Mine"}, 409); got["code"] != "SKILL_BUILT_IN" {
		t.Errorf("update: %v", got)
	}
	rec := f.postRaw("/api/delete_skill", map[string]any{"skill_id": "task-tdd"})
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "SKILL_BUILT_IN") {
		t.Errorf("delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBuiltInSkills_DeletingTheShadowClearsTheClashAndKeepsTheBuiltInOne(t *testing.T) {
	f := newCatalogFixture(t)
	f.skill("task-tdd", "Mine")
	f.write("agents.json", `{"agents":{"a":{"description":"A","skills":["task-tdd"]}}}`)
	f.post("/api/save_catalog_skill", map[string]any{"previous_name": "task-tdd", "skill": skillBody("task-tdd", "Mine")}, 409)
	f.post("/api/delete_catalog_skill", map[string]any{"name": "task-tdd"}, 200)
	if _, err := os.Stat(filepath.Join(f.dir, "skills", "task-tdd")); err == nil {
		t.Fatal("the shadow folder is still there")
	}
	cat := f.get("/api/get_agent_catalog", 200)
	tdd := byName(t, cat["skills"], "task-tdd")
	if tdd["built_in"] != true || len(problemCodes(tdd)) != 0 {
		t.Errorf("task-tdd = %v", tdd)
	}
	if got := strList(tdd["agents"]); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("the agent lost its reference: %v", got)
	}
	// With no shadow left, a delete is refused again.
	f.post("/api/delete_catalog_skill", map[string]any{"name": "task-tdd"}, 409)
}

func TestBuiltInSkills_TheImportCopiesNoneIntoTheCatalogFolder(t *testing.T) {
	repo := t.TempDir()
	for _, name := range []string{"task-tdd", "mine"} {
		p := filepath.Join(repo, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: "+name+"\ndescription: D\n---\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, rep := importInto(t, agentcatalog.ImportSource{RepoDir: repo})
	if rep.Skills != 1 {
		t.Errorf("report = %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "skills", "task-tdd")); err == nil {
		t.Error("the import copied a built-in name into the catalog")
	}
	if f.read("skills/mine/SKILL.md") == "" {
		t.Error("the import lost a catalog skill")
	}
}

func TestBuiltInSkills_AnAgentOrABundleMayNameOneAndTheyAreCounted(t *testing.T) {
	f := newCatalogFixture(t)
	f.post("/api/save_catalog_bundle", map[string]any{"bundle": map[string]any{"name": "ops", "entries": []string{"task-run-app"}}}, 200)
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{
		"name": "runner", "description": "Runs apps", "entries": []string{"task-run-app"},
	}}, 200)
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{
		"name": "via-bundle", "description": "Runs apps too", "entries": []string{"@ops"},
	}}, 200)

	cat := f.get("/api/get_agent_catalog", 200)
	runner := byName(t, cat["agents"], "runner")
	if got := strList(runner["resolved_skills"]); !reflect.DeepEqual(got, []string{"task-run-app"}) {
		t.Errorf("resolved_skills = %v", got)
	}
	if len(list(runner["missing_skills"])) != 0 || len(problemCodes(runner)) != 0 {
		t.Errorf("runner missing/problems = %v %v", runner["missing_skills"], runner["problems"])
	}
	if got := strList(byName(t, cat["agents"], "via-bundle")["resolved_skills"]); !reflect.DeepEqual(got, []string{"task-run-app"}) {
		t.Errorf("via-bundle resolved_skills = %v", got)
	}
	run := byName(t, cat["skills"], "task-run-app")
	if !reflect.DeepEqual(strList(run["agents"]), []string{"runner"}) || !reflect.DeepEqual(strList(run["bundles"]), []string{"ops"}) ||
		run["agent_count"] != float64(2) {
		t.Errorf("task-run-app agents/bundles/count = %v %v %v", run["agents"], run["bundles"], run["agent_count"])
	}
	one := f.get("/api/get_catalog_skill?name=task-run-app", 200)["skill"].(map[string]any)
	if !reflect.DeepEqual(strList(one["agents"]), []string{"runner"}) || one["agent_count"] != float64(2) {
		t.Errorf("get_catalog_skill agents/count = %v %v", one["agents"], one["agent_count"])
	}
}

func TestBuiltInSkills_ANameThatIsNeitherIsStillAnUnknownReference(t *testing.T) {
	f := newCatalogFixture(t)
	got := f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{
		"name": "x", "description": "X", "entries": []string{"task-run-app", "ghost"},
	}}, 400)
	if got["code"] != "UNKNOWN_REFERENCE" || !reflect.DeepEqual(strList(got["names"]), []string{"ghost"}) {
		t.Errorf("body = %v", got)
	}
}
