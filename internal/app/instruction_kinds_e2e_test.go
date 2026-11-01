//go:build !windows

package app

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// The record's instruction kinds are memory, skill and subagent; the old
// names are read as the new ones.
func TestInstructionKinds_OldKindsAreStoredAsTheNewOnes(t *testing.T) {
	s := startArchRecordServer(t)
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		s.agent(a)
	}
	rec := clone(sample())
	rec["instructions"] = []any{
		map[string]any{"path": "CLAUDE.md", "kind": "rules", "applies_to": []any{"all"}, "loaded": "always"},
		map[string]any{"path": ".claude/skills/a/SKILL.md", "kind": "skill", "applies_to": []any{"all"}, "loaded": "on_demand"},
		map[string]any{"path": ".claude/agents/r.md", "kind": "subagent_prompt", "applies_to": []any{"all"}, "loaded": "on_demand"},
	}
	if status, body := s.save(0, rec); status != http.StatusOK {
		t.Fatalf("save: %d %v", status, body)
	}
	var kinds []string
	for _, in := range s.get()["record"].(map[string]any)["instructions"].([]any) {
		kinds = append(kinds, in.(map[string]any)["kind"].(string))
	}
	if strings.Join(kinds, ",") != "memory,skill,subagent" {
		t.Errorf("kinds = %v", kinds)
	}

	rec["instructions"].([]any)[0].(map[string]any)["kind"] = "bogus"
	status, body := s.save(1, rec)
	if status != http.StatusBadRequest {
		t.Fatalf("bogus kind: %d %v", status, body)
	}
	if code := errorPaths(body)["/instructions/0/kind"]; code != string(domain.ArchInvalidValue) {
		t.Errorf("code = %q", code)
	}
}

func TestInstructionKinds_SchemaListsOnlyTheNewKinds(t *testing.T) {
	s := startArchRecordServer(t)
	res, err := http.Get(s.srv.URL + "/api/architecture.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(b), `"memory"`) || strings.Contains(string(b), "subagent_prompt") {
		t.Errorf("schema = %s", b)
	}
}
