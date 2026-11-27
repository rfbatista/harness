package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
)

func benchHandler(t *testing.T, db string) *Handler {
	t.Helper()
	conn, err := sqlite.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	svc := bench.New(bench.Deps{
		Cases:    sqlite.NewBenchCaseRepository(conn),
		Projects: agentSystemProjects{"p1": {ID: "p1"}, "p2": {ID: "p2"}},
	})
	return NewHandler(Services{Bench: svc})
}

const benchCaseBody = `{
  "name": "reads the readme",
  "prompt": "Read the README and say what the project is.",
  "tools": [{"name": "read_file", "result": "# Demo project"}],
  "expect": {
    "tools": [{"tool": "read_file", "params": {"path": "README.md"}}],
    "ordered": true,
    "reference": "A demo project."
  }
}`

func benchDelete(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api"+path, nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	return rec
}

func TestBenchCaseIsCreatedListedAndDeletedPerProject(t *testing.T) {
	h := benchHandler(t, ":memory:")

	rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var created domain.BenchCase
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.ProjectID != "p1" || created.Source != domain.BenchSourceDefinition {
		t.Fatalf("created = %+v", created)
	}

	var list struct {
		Cases []domain.BenchCase `json:"cases"`
	}
	rec = getJSON(t, h, "/projects/p1/bench/cases")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Cases) != 1 || list.Cases[0].ID != created.ID {
		t.Fatalf("list = %s (%v)", rec.Body.String(), err)
	}

	rec = getJSON(t, h, "/projects/p2/bench/cases")
	list.Cases = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Cases) != 0 {
		t.Fatalf("p2 sees p1's cases: %s", rec.Body.String())
	}

	if rec := benchDelete(t, h, "/projects/p2/bench/cases/"+created.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("delete from another project = %d, want 404", rec.Code)
	}
	if rec := benchDelete(t, h, "/projects/p1/bench/cases/"+created.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(t, h, "/projects/p1/bench/cases")
	list.Cases = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Cases) != 0 {
		t.Fatalf("case still listed: %s", rec.Body.String())
	}
}

func TestBenchCaseDefinitionsAreRefusedWithTheFieldPath(t *testing.T) {
	h := benchHandler(t, ":memory:")
	cases := []struct {
		name, body, field, code string
	}{
		{"no prompt", `{"tools":[],"expect":{"reference":"x"}}`, "prompt", "REQUIRED"},
		{"expectation names an unmocked tool", `{"prompt":"p","tools":[{"name":"a","result":"r"}],"expect":{"tools":[{"tool":"b"}]}}`, "expect.tools[0].tool", "UNKNOWN_TOOL"},
		{"no expectation", `{"prompt":"p","tools":[{"name":"a","result":"r"}],"expect":{}}`, "expect", "NO_EXPECTATION"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(tc.body))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			var body agentSystemErrors
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) == 0 {
				t.Fatalf("body = %s (%v)", rec.Body.String(), err)
			}
			if got := body.Errors[0]; got.Field != tc.field || got.Code != tc.code {
				t.Fatalf("error = %+v, want %s %s", got, tc.field, tc.code)
			}
		})
	}
}

func TestBenchCasesOfAnUnknownProjectAreNotFound(t *testing.T) {
	h := benchHandler(t, ":memory:")
	if rec := getJSON(t, h, "/projects/nope/bench/cases"); rec.Code != http.StatusNotFound {
		t.Fatalf("list = %d, want 404", rec.Code)
	}
	if rec := postJSON(t, h, "/projects/nope/bench/cases", json.RawMessage(benchCaseBody)); rec.Code != http.StatusNotFound {
		t.Fatalf("create = %d, want 404", rec.Code)
	}
}

func TestBenchCaseIsReadBackIdenticalAfterTheStoreIsReopened(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bench.db")
	h := benchHandler(t, file)
	rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created domain.BenchCase
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	reopened := benchHandler(t, file)
	var list struct {
		Cases []domain.BenchCase `json:"cases"`
	}
	rec = getJSON(t, reopened, "/projects/p1/bench/cases")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Cases) != 1 {
		t.Fatalf("list = %s (%v)", rec.Body.String(), err)
	}
	want, _ := json.Marshal(created)
	got, _ := json.Marshal(list.Cases[0])
	if string(want) != string(got) {
		t.Fatalf("read back\n%s\nwant\n%s", got, want)
	}
}

func benchPatch(t *testing.T, h *Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	return rec
}

func TestBenchCaseExpectationIsEditedInPlace(t *testing.T) {
	h := benchHandler(t, ":memory:")
	rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	var created domain.BenchCase
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	path := "/projects/p1/bench/cases/" + created.ID

	rec = benchPatch(t, h, path, `{"expect": {"tools": [{"tool": "read_file"}], "ordered": false, "reference": "The demo."}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d: %s", rec.Code, rec.Body.String())
	}
	var updated domain.BenchCase
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Prompt != created.Prompt || updated.Expect.Reference != "The demo." || updated.Expect.Ordered || updated.Expect.Tools[0].Params != nil {
		t.Fatalf("updated = %+v", updated)
	}
	var list struct {
		Cases []domain.BenchCase `json:"cases"`
	}
	if err := json.Unmarshal(getJSON(t, h, "/projects/p1/bench/cases").Body.Bytes(), &list); err != nil || len(list.Cases) != 1 || list.Cases[0].Expect.Reference != "The demo." {
		t.Fatalf("stored = %+v (%v)", list.Cases, err)
	}
}

func TestBenchCaseExpectationRefusedLikeCreate(t *testing.T) {
	h := benchHandler(t, ":memory:")
	rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	var created domain.BenchCase
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	path := "/projects/p1/bench/cases/" + created.ID

	rec = benchPatch(t, h, path, `{"expect": {"tools": [{"tool": "write_file"}]}}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "UNKNOWN_TOOL") || !strings.Contains(rec.Body.String(), "expect.tools[0].tool") {
		t.Fatalf("refused patch = %d: %s", rec.Code, rec.Body.String())
	}
	rec = benchPatch(t, h, path, `{"expect": {}}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "NO_EXPECTATION") {
		t.Fatalf("empty expectation = %d: %s", rec.Code, rec.Body.String())
	}
	// Nothing was stored by the refused edits.
	var list struct {
		Cases []domain.BenchCase `json:"cases"`
	}
	_ = json.Unmarshal(getJSON(t, h, "/projects/p1/bench/cases").Body.Bytes(), &list)
	if list.Cases[0].Expect.Reference != "A demo project." {
		t.Fatalf("a refused edit changed the case: %+v", list.Cases[0].Expect)
	}
}

func TestBenchCaseExpectationOfAnUnknownCaseOrProjectIsNotFound(t *testing.T) {
	h := benchHandler(t, ":memory:")
	rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	var created domain.BenchCase
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	body := `{"expect": {"reference": "x"}}`

	if rec := benchPatch(t, h, "/projects/p1/bench/cases/nope", body); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown case = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := benchPatch(t, h, "/projects/p2/bench/cases/"+created.ID, body); rec.Code != http.StatusNotFound {
		t.Fatalf("another project's case = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := benchPatch(t, h, "/projects/nope/bench/cases/"+created.ID, body); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown project = %d: %s", rec.Code, rec.Body.String())
	}
}
