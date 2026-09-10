package orchestration

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestResolveAttachedMCPServers_AttachNoneWhenEmpty(t *testing.T) {
	agent := &domain.Agent{ID: "a1", MCPServerIDs: nil}
	all := []*domain.MCPServer{
		{ID: "m1", Name: "github"},
		{ID: "m2", Name: "linear"},
	}
	got := resolveAttachedMCPServers(agent, all)
	if len(got) != 0 {
		t.Fatalf("want no MCPs when agent has empty ids, got %d", len(got))
	}
}

func TestResolveAttachedMCPServers_FiltersByID(t *testing.T) {
	agent := &domain.Agent{ID: "a1", MCPServerIDs: []string{"m2"}}
	all := []*domain.MCPServer{
		{ID: "m1", Name: "github"},
		{ID: "m2", Name: "linear"},
	}
	got := resolveAttachedMCPServers(agent, all)
	if len(got) != 1 || got[0].ID != "m2" {
		t.Fatalf("want only m2, got %+v", got)
	}
}

func TestResolveAttachedMCPServers_NilAgent(t *testing.T) {
	got := resolveAttachedMCPServers(nil, []*domain.MCPServer{{ID: "m1"}})
	if len(got) != 0 {
		t.Fatalf("want nil for nil agent, got %d", len(got))
	}
}
