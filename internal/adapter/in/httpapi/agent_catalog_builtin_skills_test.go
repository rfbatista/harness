package httpapi

import (
	"reflect"
	"strings"
	"testing"
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
