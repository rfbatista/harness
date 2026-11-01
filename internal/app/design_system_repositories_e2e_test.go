//go:build !windows

package app

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// addRepo adds a repository to the project over the API and answers the whole
// create_repository response.
func (s *designServer) addRepo(name string) map[string]any {
	s.t.Helper()
	return s.mustPost("create_repository", map[string]any{"name": name, "url": "https://example.com/acme/" + name + ".git"})
}

func (s *designServer) mustGet(path string) map[string]any {
	s.t.Helper()
	res, err := http.Get(s.srv.URL + "/api/" + path)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		s.t.Fatalf("%s: %d %s", path, res.StatusCode, b)
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func repoID(t *testing.T, resp map[string]any) string {
	t.Helper()
	repo, ok := resp["repository"].(map[string]any)
	if !ok {
		t.Fatalf("no repository in %v", resp)
	}
	return repo["id"].(string)
}

func (s *designServer) setUse(id string, uses bool, path string) (int, map[string]any) {
	body := map[string]any{"repository_id": id, "uses_design_system": uses}
	if path != "" {
		body["path"] = path
	}
	return s.post("set_repository_design_system", body)
}

func TestDesignSystem_OptingInStoresTheDefaultOrACustomPathAndOptingOutKeepsIt(t *testing.T) {
	s := startDesignServer(t)
	id := repoID(t, s.addRepo("spillway"))

	repo := s.addRepo("fresh")["repository"].(map[string]any)
	if repo["uses_design_system"] != false || repo["design_system_path"] != ".harness/design-system/" {
		t.Errorf("a new repository: %v", repo)
	}

	status, out := s.setUse(id, true, "")
	if status != http.StatusOK {
		t.Fatalf("%d %v", status, out)
	}
	repo = out["repository"].(map[string]any)
	if repo["id"] != id || repo["uses_design_system"] != true || repo["design_system_path"] != ".harness/design-system/" {
		t.Errorf("opt in with no path: %v", repo)
	}

	_, out = s.setUse(id, true, "ui/tokens/")
	if repo = out["repository"].(map[string]any); repo["design_system_path"] != "ui/tokens/" {
		t.Errorf("custom path: %v", repo)
	}

	_, out = s.setUse(id, false, "")
	repo = out["repository"].(map[string]any)
	if repo["uses_design_system"] != false || repo["design_system_path"] != "ui/tokens/" {
		t.Errorf("opt out keeps the path: %v", repo)
	}

	got := s.mustGet("get_repository?repository_id=" + id)["repository"].(map[string]any)
	if got["uses_design_system"] != false || got["design_system_path"] != "ui/tokens/" {
		t.Errorf("read back: %v", got)
	}
}

func TestDesignSystem_APathThatIsNotRelativeInsideTheRepositoryEndingInASlashIsRefused(t *testing.T) {
	s := startDesignServer(t)
	id := repoID(t, s.addRepo("spillway"))
	for _, path := range []string{"/etc/design/", "ui/tokens", "../outside/", "ui/../../outside/", "a/../../b/", ".."} {
		status, out := s.setUse(id, true, path)
		if status != http.StatusBadRequest || out["code"] != "INVALID_INPUT" {
			t.Errorf("path %q: %d %v", path, status, out)
		}
	}
	got := s.mustGet("get_repository?repository_id=" + id)["repository"].(map[string]any)
	if got["uses_design_system"] != false || got["design_system_path"] != ".harness/design-system/" {
		t.Errorf("a refused path changed the repository: %v", got)
	}
}

func TestDesignSystem_OptingAnUnknownRepositoryIsNotFound(t *testing.T) {
	s := startDesignServer(t)
	status, out := s.setUse("nope", true, "")
	if status != http.StatusNotFound || out["code"] != "REPOSITORY_NOT_FOUND" {
		t.Errorf("%d %v", status, out)
	}
}

func TestDesignSystem_TheProjectsRepositoriesAreListedWithTheirOptIn(t *testing.T) {
	s := startDesignServer(t)
	a := repoID(t, s.addRepo("spillway"))
	s.addRepo("docs")
	s.mustPost("set_repository_design_system", map[string]any{"repository_id": a, "uses_design_system": true, "path": "ui/"})

	out := s.mustPost("list_design_system_repositories", map[string]any{})
	list := out["repositories"].([]any)
	if len(list) != 2 {
		t.Fatalf("%v", out)
	}
	byName := map[string]map[string]any{}
	for _, r := range list {
		m := r.(map[string]any)
		byName[m["name"].(string)] = m
		if v, present := m["export"]; !present || v != nil {
			t.Errorf("export = %v (present %v), want null", v, present)
		}
	}
	if r := byName["spillway"]; r["repository_id"] != a || r["uses_design_system"] != true || r["path"] != "ui/" {
		t.Errorf("spillway: %v", r)
	}
	if r := byName["docs"]; r["uses_design_system"] != false || r["path"] != ".harness/design-system/" {
		t.Errorf("docs: %v", r)
	}

	status, body := s.post("list_design_system_repositories", map[string]any{"project_id": "nope"})
	if status != http.StatusNotFound || body["code"] != "PROJECT_NOT_FOUND" {
		t.Errorf("unknown project: %d %v", status, body)
	}
}

func TestDesignSystem_AddingARepositoryAsksOnlyOnceAndOnlyWhenTheProjectHasADesignSystem(t *testing.T) {
	s := startDesignServer(t)
	if first := s.addRepo("before"); first["ask_design_system"] == true {
		t.Errorf("no design system yet: %v", first)
	}

	s.mustSave(0, dsRecord())
	added := s.addRepo("after")
	if added["ask_design_system"] != true {
		t.Fatalf("the project has a design system: %v", added)
	}
	id := repoID(t, added)

	if read := s.mustGet("get_repository?repository_id=" + id); read["ask_design_system"] != nil {
		t.Errorf("a later read asks again: %v", read)
	}
	if list := s.mustGet("list_repositories?project_id=" + s.project); list["ask_design_system"] != nil {
		t.Errorf("a list asks: %v", list)
	}
	if repo := added["repository"].(map[string]any); repo["uses_design_system"] != false {
		t.Errorf("adding opts nothing in: %v", repo)
	}
}
