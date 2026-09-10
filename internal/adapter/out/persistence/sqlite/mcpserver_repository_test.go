package sqlite

import (
	"testing"

	"github.com/rfbatista/harnesskit/errs"
	"github.com/rfbatista/harnesskit/mcpserver"

	"operators-mcp/internal/domain"
)

func newMCPRepo(t *testing.T) *MCPServerRepository {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return NewMCPServerRepository(db)
}

func stdioInput(name string) domain.MCPServerInput {
	return domain.MCPServerInput{
		Name: name, Description: "d", Transport: "stdio", Command: "npx",
		Args: []string{"-y", name}, Env: map[string]string{"K": "V"},
		Headers: map[string]string{"H": "1"},
	}
}

func TestMCPServerRepo_CreateGetList(t *testing.T) {
	r := newMCPRepo(t)
	if got := r.Get("nope"); got != nil {
		t.Fatalf("Get(unknown) = %+v, want nil", got)
	}
	if got := r.List(); got == nil || len(got) != 0 {
		t.Fatalf("List() on an empty repo = %v, want an empty non-nil slice", got)
	}

	created, err := r.Create(stdioInput("alpha"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create() returned an empty id")
	}

	got := r.Get(created.ID)
	if got == nil {
		t.Fatal("Get() after Create returned nil")
	}
	if got.Name != "alpha" || got.Command != "npx" {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	if len(got.Args) != 2 || got.Env["K"] != "V" || got.Headers["H"] != "1" {
		t.Fatalf("round-trip lost collections: %+v", got)
	}
}

// The update writes a column map, so a wrong key is a SQL error at runtime
// rather than a compile error. This is what catches that.
func TestMCPServerRepo_UpdateWritesEveryColumn(t *testing.T) {
	r := newMCPRepo(t)
	created, err := r.Create(stdioInput("alpha"))
	if err != nil {
		t.Fatal(err)
	}

	updated, err := r.Update(created.ID, domain.MCPServerInput{
		Name: "renamed", Description: "changed", Transport: "streamable-http",
		Command: "", Args: []string{"one"}, URL: "https://example.test",
		Env: map[string]string{"A": "B"}, Headers: map[string]string{"C": "D"},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	for _, tc := range []struct{ field, got, want string }{
		{"Name", updated.Name, "renamed"},
		{"Description", updated.Description, "changed"},
		{"Transport", updated.Transport, "streamable-http"},
		{"URL", updated.URL, "https://example.test"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.field, tc.got, tc.want)
		}
	}
	if len(updated.Args) != 1 || updated.Args[0] != "one" {
		t.Errorf("Args = %v, want [one]", updated.Args)
	}
	if updated.Env["A"] != "B" || updated.Headers["C"] != "D" {
		t.Errorf("Env/Headers = %v/%v", updated.Env, updated.Headers)
	}

	// And it must survive a re-read, not just live in the returned value.
	reread := r.Get(created.ID)
	if reread.Name != "renamed" || reread.URL != "https://example.test" {
		t.Fatalf("update did not persist: %+v", reread)
	}
}

// An ordinary edit must not discard the cached probe results.
func TestMCPServerRepo_UpdatePreservesProbeCache(t *testing.T) {
	r := newMCPRepo(t)
	created, err := r.Create(stdioInput("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateProbeResult(created.ID, domain.MCPProbeResult{
		Status: mcpserver.ProbeStatusOK, ToolCount: 7, ResourceCount: 1, PromptCount: 2,
	}); err != nil {
		t.Fatalf("UpdateProbeResult() error = %v", err)
	}

	updated, err := r.Update(created.ID, stdioInput("renamed"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.ToolCount != 7 || updated.LastProbeStatus != mcpserver.ProbeStatusOK {
		t.Fatalf("probe cache lost on update: %+v", updated)
	}
	if updated.LastProbeAt == nil {
		t.Fatal("LastProbeAt lost on update")
	}
}

func TestMCPServerRepo_UnknownIDErrors(t *testing.T) {
	r := newMCPRepo(t)
	if _, err := r.Update("nope", stdioInput("x")); !errs.HasCode(err, mcpserver.CodeNotFound) {
		t.Errorf("Update(unknown) error = %v, want %s", err, mcpserver.CodeNotFound)
	}
	if _, err := r.UpdateProbeResult("nope", domain.MCPProbeResult{}); !errs.HasCode(err, mcpserver.CodeNotFound) {
		t.Errorf("UpdateProbeResult(unknown) error = %v, want %s", err, mcpserver.CodeNotFound)
	}
	if err := r.Delete("nope"); !errs.HasCode(err, mcpserver.CodeNotFound) {
		t.Errorf("Delete(unknown) error = %v, want %s", err, mcpserver.CodeNotFound)
	}
}

func TestMCPServerRepo_Delete(t *testing.T) {
	r := newMCPRepo(t)
	created, err := r.Create(stdioInput("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if r.Get(created.ID) != nil {
		t.Fatal("Get() after Delete returned a server")
	}
}
