package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/ports"
)

// checkShCheckout is a repository's default branch holding only the files given.
type checkShCheckout struct {
	ports.ArchitectureGit
	files map[string]string
}

func (checkShCheckout) DefaultBranch(context.Context, string) (string, error) { return "main", nil }
func (c checkShCheckout) Files(context.Context, string, string) ([]string, error) {
	var out []string
	for p := range c.files {
		out = append(out, p)
	}
	return out, nil
}
func (checkShCheckout) LastCommit(context.Context, string, string, string) (string, error) {
	return "c1", nil
}
func (c checkShCheckout) ReadFile(_ context.Context, _, _, p string) ([]byte, bool, error) {
	body, ok := c.files[p]
	return []byte(body), ok, nil
}

func checkShHandler(t *testing.T, files map[string]string) *Handler {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(Services{AgentSystem: agentsystem.New(agentsystem.Deps{
		Store:        sqlite.NewAgentSystemRepository(db),
		Projects:     agentSystemProjects{"p1": {ID: "p1"}},
		Repositories: agentSystemRepositories{"r1": {ID: "r1", ProjectID: "p1", RootDir: "/repo"}, "r2": {ID: "r2", ProjectID: "other"}},
		ReviewModel:  func() string { return "opus" },
		Detector:     agentsystem.NewDetector(checkShCheckout{files: files}, func(string) (string, error) { return "/bin/gitleaks", nil }),
	})})
}

type checkShAnswer struct {
	Gates []struct {
		Kind    string `json:"kind"`
		Command string `json:"command"`
		Reason  string `json:"reason"`
	} `json:"gates"`
}

func importFromCheckSh(t *testing.T, h *Handler, path string) (int, checkShAnswer) {
	t.Helper()
	rec := getJSON(t, h, path)
	var answer checkShAnswer
	_ = json.Unmarshal(rec.Body.Bytes(), &answer)
	return rec.Code, answer
}

const importPath = "/projects/p1/repositories/r1/agent-system/check-sh-import"

func TestImportFromCheckShAnswersEachKindTheScriptNames(t *testing.T) {
	h := checkShHandler(t, map[string]string{
		"check.sh": "#!/bin/sh\ncase \"$1\" in\n  build) go build ./... ;;\n  lint) go vet ./... ;;\nesac\n",
		"go.mod":   "module x",
	})
	code, answer := importFromCheckSh(t, h, importPath)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := map[string]string{}
	for _, g := range answer.Gates {
		got[g.Kind] = g.Command
		if g.Reason != "detected from check.sh" {
			t.Errorf("%s reason = %q", g.Kind, g.Reason)
		}
	}
	if len(got) != 2 || got["build"] != "./check.sh build" || got["lint"] != "./check.sh lint" {
		t.Fatalf("gates = %v, want build and lint from check.sh only (the stack's go test is not imported)", got)
	}
}

func TestImportFromCheckShOfAScriptThatDoesNotDispatchFillsTest(t *testing.T) {
	h := checkShHandler(t, map[string]string{"check.sh": "#!/bin/sh\nmake all\n"})
	code, answer := importFromCheckSh(t, h, importPath)
	if code != http.StatusOK || len(answer.Gates) != 1 || answer.Gates[0].Kind != "test" || answer.Gates[0].Command != "./check.sh" {
		t.Fatalf("answer = %d %+v", code, answer)
	}
}

func TestImportFromCheckShWithNoScriptAnswersAnEmptyList(t *testing.T) {
	h := checkShHandler(t, map[string]string{"go.mod": "module x"})
	rec := getJSON(t, h, importPath)
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"gates\":[]}\n" {
		t.Fatalf("answer = %d %q", rec.Code, rec.Body.String())
	}
}

func TestImportFromCheckShStoresNothing(t *testing.T) {
	h := checkShHandler(t, map[string]string{"check.sh": "make all\n"})
	importFromCheckSh(t, h, importPath)
	var view struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	rec := getJSON(t, h, "/projects/p1/repositories/r1/agent-system")
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil || len(view.Values) != 0 {
		t.Fatalf("values after import = %v (%v)", view.Values, err)
	}
}

func TestImportFromCheckShOfAnUnknownProjectOrRepositoryIs404(t *testing.T) {
	h := checkShHandler(t, map[string]string{"check.sh": "make all\n"})
	for _, path := range []string{
		"/projects/nope/repositories/r1/agent-system/check-sh-import",
		"/projects/p1/repositories/nope/agent-system/check-sh-import",
		"/projects/p1/repositories/r2/agent-system/check-sh-import", // another project's
	} {
		if code, _ := importFromCheckSh(t, h, path); code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, code)
		}
	}
}

