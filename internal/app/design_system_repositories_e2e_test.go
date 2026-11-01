//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
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

// apply sets the repository's design system (nil: none); path "" sends none.
func (s *designServer) apply(id string, designSystemID any, path string, confirmMove bool) (int, map[string]any) {
	body := map[string]any{"repository_id": id, "design_system_id": designSystemID}
	if path != "" {
		body["path"] = path
	}
	if confirmMove {
		body["confirm_move"] = true
	}
	return s.post("set_repository_design_system", body)
}

func (s *designServer) mustApply(id string, designSystemID any, path string, confirmMove bool) map[string]any {
	s.t.Helper()
	status, out := s.apply(id, designSystemID, path, confirmMove)
	if status != http.StatusOK {
		s.t.Fatalf("apply: %d %v", status, out)
	}
	return out["repository"].(map[string]any)
}

func (s *designServer) repoRow(list []any, name string) map[string]any {
	s.t.Helper()
	for _, r := range list {
		if m := r.(map[string]any); m["name"] == name {
			return m
		}
	}
	s.t.Fatalf("no repository %q in %v", name, list)
	return nil
}

func TestDesignSystem_ApplyingStoresTheDefaultOrACustomPathAndRemovingKeepsIt(t *testing.T) {
	s := startDesignServer(t)
	id := repoID(t, s.addRepo("spillway"))

	repo := s.addRepo("fresh")["repository"].(map[string]any)
	if v, present := repo["design_system_id"]; !present || v != nil || repo["design_system_path"] != ".harness/design-system/" {
		t.Errorf("a new repository: %v", repo)
	}
	if _, old := repo["uses_design_system"]; old {
		t.Errorf("the yes/no flag is still there: %v", repo)
	}

	repo = s.mustApply(id, s.ds, "", false)
	if repo["id"] != id || repo["design_system_id"] != s.ds || repo["design_system_path"] != ".harness/design-system/" {
		t.Errorf("apply with no path: %v", repo)
	}

	if repo = s.mustApply(id, s.ds, "ui/tokens/", false); repo["design_system_path"] != "ui/tokens/" || repo["design_system_id"] != s.ds {
		t.Errorf("custom path, same design system, no move to confirm: %v", repo)
	}

	repo = s.mustApply(id, nil, "", false)
	if repo["design_system_id"] != nil || repo["design_system_path"] != "ui/tokens/" {
		t.Errorf("removing keeps the path: %v", repo)
	}

	got := s.mustGet("get_repository?repository_id=" + id)["repository"].(map[string]any)
	if got["design_system_id"] != nil || got["design_system_path"] != "ui/tokens/" {
		t.Errorf("read back: %v", got)
	}
}

func TestDesignSystem_ApplyingAnotherToARepositoryNeedsTheMoveConfirmedAndNamesTheCurrentOne(t *testing.T) {
	s := startDesignServer(t)
	second := s.create("brand", "Brand")
	id := repoID(t, s.addRepo("spillway"))
	s.mustApply(id, s.ds, "ui/", false)

	status, out := s.apply(id, second, "", false)
	if status != http.StatusConflict || out["code"] != "MOVE_NOT_CONFIRMED" {
		t.Fatalf("%d %v", status, out)
	}
	cur, _ := out["details"].(map[string]any)["current"].(map[string]any)
	if cur["design_system_id"] != s.ds || cur["name"] != "Main" {
		t.Errorf("details.current = %v", out["details"])
	}
	if got := s.mustGet("get_repository?repository_id=" + id)["repository"].(map[string]any); got["design_system_id"] != s.ds {
		t.Errorf("a refused move changed the repository: %v", got)
	}

	repo := s.mustApply(id, second, "", true)
	if repo["design_system_id"] != second || repo["design_system_path"] != "ui/" {
		t.Errorf("moved: %v", repo)
	}
}

func TestDesignSystem_AnUnknownDesignSystemOrOneOfAnotherProjectIsRefused(t *testing.T) {
	s := startDesignServer(t)
	id := repoID(t, s.addRepo("spillway"))
	other, err := s.cat.Projects.CreateProject(context.Background(), "gadgets", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign := s.mustPost("create_design_system", map[string]any{"project_id": other.ID, "name": "Theirs", "slug": "theirs"})["design_system_id"]

	for _, ds := range []any{"nope", foreign} {
		status, out := s.apply(id, ds, "", true)
		if status != http.StatusNotFound || out["code"] != "DESIGN_SYSTEM_NOT_FOUND" {
			t.Errorf("%v: %d %v", ds, status, out)
		}
	}
	if got := s.mustGet("get_repository?repository_id=" + id)["repository"].(map[string]any); got["design_system_id"] != nil {
		t.Errorf("a refused apply changed the repository: %v", got)
	}
}

func TestDesignSystem_APathThatIsNotRelativeInsideTheRepositoryEndingInASlashIsRefused(t *testing.T) {
	s := startDesignServer(t)
	id := repoID(t, s.addRepo("spillway"))
	for _, path := range []string{"/etc/design/", "ui/tokens", "../outside/", "ui/../../outside/", "a/../../b/", "..", ".git/", "ui/.GIT/hooks/", "a/.Git/", "ui\x00/", "ui\n/", "ui\t/"} {
		status, out := s.apply(id, s.ds, path, false)
		if status != http.StatusBadRequest || out["code"] != "INVALID_INPUT" {
			t.Errorf("path %q: %d %v", path, status, out)
		}
	}
	got := s.mustGet("get_repository?repository_id=" + id)["repository"].(map[string]any)
	if got["design_system_id"] != nil || got["design_system_path"] != ".harness/design-system/" {
		t.Errorf("a refused path changed the repository: %v", got)
	}
}

func TestDesignSystem_ApplyingToAnUnknownRepositoryIsNotFound(t *testing.T) {
	s := startDesignServer(t)
	status, out := s.apply("nope", s.ds, "", false)
	if status != http.StatusNotFound || out["code"] != "REPOSITORY_NOT_FOUND" {
		t.Errorf("%d %v", status, out)
	}
}

func TestDesignSystem_TheRepositoryListHasEveryRepositoryOnceWithItsAppliedDesignSystem(t *testing.T) {
	s := startDesignServer(t)
	second := s.create("brand", "Brand")
	a := repoID(t, s.addRepo("spillway"))
	b := repoID(t, s.addRepo("docs"))
	s.addRepo("plain")
	s.mustApply(a, s.ds, "ui/", false)
	s.mustApply(b, second, "", false)

	list := s.mustPost("list_design_system_repositories", map[string]any{})["repositories"].([]any)
	if len(list) != 3 {
		t.Fatalf("%v", list)
	}
	for _, r := range list {
		if v, present := r.(map[string]any)["export"]; !present || v != nil {
			t.Errorf("export = %v (present %v), want null", v, present)
		}
	}
	r := s.repoRow(list, "spillway")
	if ds, _ := r["design_system"].(map[string]any); r["repository_id"] != a || r["applied_here"] != true || r["path"] != "ui/" || ds["design_system_id"] != s.ds || ds["name"] != "Main" {
		t.Errorf("spillway: %v", r)
	}
	r = s.repoRow(list, "docs")
	if ds, _ := r["design_system"].(map[string]any); r["applied_here"] != false || ds["design_system_id"] != second || ds["name"] != "Brand" {
		t.Errorf("docs uses another: %v", r)
	}
	r = s.repoRow(list, "plain")
	if v, present := r["design_system"]; !present || v != nil || r["applied_here"] != false || r["path"] != ".harness/design-system/" {
		t.Errorf("plain: %v", r)
	}

	asked := s.mustPost("list_design_system_repositories", map[string]any{"design_system_id": second})["repositories"].([]any)
	if r := s.repoRow(asked, "docs"); r["applied_here"] != true {
		t.Errorf("asked about the second: %v", r)
	}
	if r := s.repoRow(asked, "spillway"); r["applied_here"] != false {
		t.Errorf("asked about the second: %v", r)
	}

	status, body := s.post("list_design_system_repositories", map[string]any{"design_system_id": "nope"})
	if status != http.StatusNotFound || body["code"] != "DESIGN_SYSTEM_NOT_FOUND" {
		t.Errorf("unknown design system: %d %v", status, body)
	}
}

func TestDesignSystem_DeletingIsRefusedWhileARepositoryUsesItAndAllowedAfterItIsRemovedOrMoved(t *testing.T) {
	s := startDesignServer(t)
	second := s.create("brand", "Brand")
	a := repoID(t, s.addRepo("spillway"))
	b := repoID(t, s.addRepo("docs"))
	s.mustApply(a, s.ds, "", false)
	s.mustApply(b, s.ds, "", false)

	status, out := s.post("delete_design_system", map[string]any{"confirm_name": "Main"})
	if status != http.StatusConflict || out["code"] != "DESIGN_SYSTEM_IN_USE" {
		t.Fatalf("%d %v", status, out)
	}
	used, _ := out["details"].(map[string]any)["repositories"].([]any)
	names := []string{}
	for _, u := range used {
		m := u.(map[string]any)
		names = append(names, m["name"].(string))
		if m["repository_id"] != a && m["repository_id"] != b {
			t.Errorf("repository %v", m)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"docs", "spillway"}) {
		t.Errorf("details.repositories = %v", used)
	}
	s.get() // still there

	s.mustApply(a, nil, "", false)
	s.mustApply(b, second, "", true)
	s.mustPost("delete_design_system", map[string]any{"confirm_name": "Main"})
}

func TestDesignSystem_TheDesignSystemListShowsTheRepositoriesThatUseEach(t *testing.T) {
	s := startDesignServer(t)
	second := s.create("brand", "Brand")
	a := repoID(t, s.addRepo("spillway"))
	s.addRepo("docs")
	s.mustApply(a, s.ds, "", false)

	var main, brand map[string]any
	for _, d := range s.list() {
		switch d["design_system_id"] {
		case s.ds:
			main = d
		case second:
			brand = d
		}
	}
	repos, _ := main["repositories"].([]any)
	if len(repos) != 1 {
		t.Fatalf("main: %v", main)
	}
	if r := repos[0].(map[string]any); r["repository_id"] != a || r["name"] != "spillway" || r["export_state"] != "never_exported" {
		t.Errorf("entry: %v", r)
	}
	if repos, ok := brand["repositories"].([]any); !ok || len(repos) != 0 {
		t.Errorf("brand: %v", brand)
	}
}

func TestDesignSystem_AddingARepositoryAsksOnlyOnceAndOnlyWhenTheProjectHasADesignSystem(t *testing.T) {
	s := startDesignServer(t)
	// startDesignServer already made one; start from a project with none.
	s.mustPost("delete_design_system", map[string]any{"confirm_name": "Main"})
	if first := s.addRepo("before"); first["ask_design_system"] == true {
		t.Errorf("no design system yet: %v", first)
	}

	s.ds = s.create("main", "Main")
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
	if repo := added["repository"].(map[string]any); repo["design_system_id"] != nil {
		t.Errorf("adding applies nothing: %v", repo)
	}
}

// Applying a design system to a repository and deleting that design system,
// at the same time, never leaves the repository pointing at a missing one.
func TestDesignSystem_ApplyAndDeleteRacingNeverLeaveARepositoryOnAMissingDesignSystem(t *testing.T) {
	s := startDesignServer(t)
	ctx := context.Background()
	const pairs = 25
	type pair struct{ ds, name, repo string }
	ps := make([]pair, pairs)
	for i := range ps {
		name := fmt.Sprintf("Race %d", i)
		ps[i] = pair{ds: s.create(fmt.Sprintf("race-%d", i), name), name: name, repo: repoID(t, s.addRepo(fmt.Sprintf("repo-%d", i)))}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, p := range ps {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _ = s.cat.Projects.SetRepositoryDesignSystem(ctx, p.repo, &p.ds, "", true)
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = s.cat.DesignSystems.DeleteDesignSystem(ctx, p.ds, p.name)
		}()
	}
	close(start)
	wg.Wait()
	for _, p := range ps {
		repo, err := s.cat.Projects.GetRepository(ctx, p.repo)
		if err != nil {
			t.Fatal(err)
		}
		if repo.DesignSystemID == nil {
			continue
		}
		if id, err := s.store.Identity(ctx, *repo.DesignSystemID); err != nil || id == nil {
			t.Errorf("repository %s uses the missing design system %s (%v)", p.repo, *repo.DesignSystemID, err)
		}
	}
}
