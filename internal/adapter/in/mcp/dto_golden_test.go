package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The JSON shape of these entities is the contract with the Flutter client and
// with anything reading the raw MCP tool results. Since the entities now live
// in harnesskit, a struct-tag change there would silently alter this wire
// format. These goldens are the tripwire: if one fails after a harnesskit
// bump, decide deliberately whether the client changes too.

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestGolden_SkillDTO(t *testing.T) {
	at := time.Date(2026, 8, 23, 10, 30, 0, 0, time.UTC)
	sk := &domain.Skill{
		ID: "sk1", Name: "Code Review", Description: "reviews code",
		Files: []domain.SkillFile{
			{Path: "SKILL.md", Content: "# demo"},
			{Path: "scripts", Dir: true},
			{Path: "assets/logo.png", Content: "iVBOR", Encoding: "base64"},
		},
		License: "MIT", Compatibility: "claude-code",
		Metadata: map[string]string{"team": "docs"}, AllowedTools: "Read, Bash",
		Resources:     []string{"scripts/run.py"},
		Validation:    []domain.ValidationIssue{{Level: "warning", Field: "name", Message: "too long"}},
		PublishedSlug: "code-review", PublishedPath: "/root/code-review",
		PublishedAt: &at, PublishError: "",
	}

	want := `{"id":"sk1","name":"Code Review","description":"reviews code",` +
		`"files":[{"path":"SKILL.md","content":"# demo"},{"path":"scripts","dir":true},` +
		`{"path":"assets/logo.png","content":"iVBOR","encoding":"base64"}],` +
		`"license":"MIT","compatibility":"claude-code","metadata":{"team":"docs"},` +
		`"allowed_tools":"Read, Bash","resources":["scripts/run.py"],` +
		`"validation":[{"level":"warning","field":"name","message":"too long"}],` +
		`"published_path":"/root/code-review","published_at":"2026-08-23T10:30:00Z"}`

	if got := marshal(t, sk); got != want {
		t.Errorf("domain.Skill JSON drifted\n got: %s\nwant: %s", got, want)
	}
}

// PublishedSlug is persistence-facing and must never reach a client.
func TestGolden_SkillHidesPublishedSlug(t *testing.T) {
	got := marshal(t, &domain.Skill{ID: "sk1", Name: "n", PublishedSlug: "secret-slug"})
	if got != `{"id":"sk1","name":"n"}` {
		t.Errorf("PublishedSlug leaked or the zero-value shape drifted: %s", got)
	}
}

func TestGolden_MCPServerDTO(t *testing.T) {
	at := time.Date(2026, 8, 23, 10, 30, 0, 0, time.UTC)
	m := &domain.MCPServer{
		ID: "m1", Name: "alpha", Description: "the alpha server",
		Transport: "stdio", Command: "npx", Args: []string{"-y", "alpha"},
		URL: "", Env: map[string]string{"K": "V"}, Headers: map[string]string{"H": "1"},
		LastProbeAt: &at, LastProbeStatus: "ok", LastProbeError: "",
		ToolCount: 3, ResourceCount: 1, PromptCount: 2,
		AgentCount: 2, AgentNames: []string{"reviewer", "planner"},
	}

	want := `{"id":"m1","name":"alpha","description":"the alpha server","transport":"stdio",` +
		`"command":"npx","args":["-y","alpha"],"env":{"K":"V"},"headers":{"H":"1"},` +
		`"last_probe_at":"2026-08-23T10:30:00Z","last_probe_status":"ok",` +
		`"tool_count":3,"resource_count":1,"prompt_count":2,` +
		`"agent_count":2,"agent_names":["reviewer","planner"]}`

	if got := marshal(t, m); got != want {
		t.Errorf("domain.MCPServer JSON drifted\n got: %s\nwant: %s", got, want)
	}
}

func TestGolden_MCPProbeResult(t *testing.T) {
	r := domain.MCPProbeResult{
		Status: "ok", ToolCount: 2, ResourceCount: 0, PromptCount: 1,
		ToolNames: []string{"a", "b"}, ServerName: "srv", ServerVersion: "1.0",
	}
	want := `{"status":"ok","tool_count":2,"resource_count":0,"prompt_count":1,` +
		`"tool_names":["a","b"],"server_name":"srv","server_version":"1.0"}`
	if got := marshal(t, r); got != want {
		t.Errorf("domain.MCPProbeResult JSON drifted\n got: %s\nwant: %s", got, want)
	}
}

// The DTOs the HTTP surface returns are built from the same entities, so they
// are pinned too.
func TestGolden_SkillToDTO(t *testing.T) {
	at := time.Date(2026, 8, 23, 10, 30, 0, 0, time.UTC)
	dto := SkillToDTO(&domain.Skill{
		ID: "sk1", Name: "demo", Description: "d",
		Files:         []domain.SkillFile{{Path: "SKILL.md", Content: "x"}},
		PublishedSlug: "demo", PublishedPath: "/root/demo", PublishedAt: &at,
	})
	got := marshal(t, dto)
	if got == "" || got == "null" {
		t.Fatal("SkillToDTO produced nothing")
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(got), &probe); err != nil {
		t.Fatalf("SkillDTO does not round-trip: %v", err)
	}
	if _, leaked := probe["published_slug"]; leaked {
		t.Error("SkillDTO leaked published_slug")
	}
	for _, key := range []string{"id", "name", "files", "published_path", "published_at"} {
		if _, ok := probe[key]; !ok {
			t.Errorf("SkillDTO is missing %q: %s", key, got)
		}
	}
}
