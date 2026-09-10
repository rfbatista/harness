package unit

import (
	"testing"

	"operators-mcp/internal/adapter/out/persistence/memory"
	"operators-mcp/internal/domain"
)

func TestStore_CreateListGetUpdateAssignPath(t *testing.T) {
	ps := memory.NewProjectStore()
	p, err := ps.Create("myproject", "/some/root")
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	s := memory.NewStore()

	rules1 := []domain.Prompt{{ID: "rule-1", Name: "No UI"}}
	agents1 := []domain.Agent{{ID: "agent-1", Name: "Agent 1"}}
	z, err := s.Create(p.ID, "backend", "cmd/.*", "Server code", rules1, agents1)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if z.ID == "" {
		t.Error("expected id")
	}
	if z.ProjectID != p.ID {
		t.Errorf("ProjectID: got %q", z.ProjectID)
	}
	if z.Name != "backend" {
		t.Errorf("name: got %q", z.Name)
	}
	if len(z.AssignedAgents) != 1 || z.AssignedAgents[0].ID != "agent-1" {
		t.Errorf("AssignedAgents: got %v", z.AssignedAgents)
	}

	list := s.ListByProject(p.ID)
	if len(list) != 1 {
		t.Fatalf("ListByProject: got %d zones", len(list))
	}
	if list[0].Name != "backend" {
		t.Errorf("List[0].Name: got %q", list[0].Name)
	}

	got := s.Get(z.ID)
	if got == nil {
		t.Fatal("Get: nil")
	}
	if got.Name != "backend" {
		t.Errorf("Get.Name: got %q", got.Name)
	}

	agents2 := []domain.Agent{{ID: "agent-2", Name: "Agent 2"}}
	updated, err := s.Update(z.ID, "backend-updated", "internal/.*", "Internal pkgs", nil, agents2)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "backend-updated" {
		t.Errorf("Update name: got %q", updated.Name)
	}
	if len(updated.AssignedAgents) != 1 || updated.AssignedAgents[0].ID != "agent-2" {
		t.Errorf("AssignedAgents after Update: got %v", updated.AssignedAgents)
	}

	assigned, err := s.AssignPath(z.ID, "internal/blueprint")
	if err != nil {
		t.Fatalf("AssignPath: %v", err)
	}
	if len(assigned.ExplicitPaths) != 1 || assigned.ExplicitPaths[0] != "internal/blueprint" {
		t.Errorf("ExplicitPaths: got %v", assigned.ExplicitPaths)
	}
}

func TestStore_CreateEmptyName_Error(t *testing.T) {
	ps := memory.NewProjectStore()
	p, _ := ps.Create("p", "/root")
	s := memory.NewStore()
	_, err := s.Create(p.ID, "", "x", "", nil, nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if se, ok := err.(*domain.StructuredError); !ok || se.Code != "INVALID_NAME" {
		t.Errorf("expected INVALID_NAME, got %v", err)
	}
}

func TestStore_GetNotFound_Nil(t *testing.T) {
	s := memory.NewStore()
	if s.Get("nonexistent") != nil {
		t.Error("Get nonexistent should return nil")
	}
}

func TestStore_UpdateNotFound_Error(t *testing.T) {
	s := memory.NewStore()
	_, err := s.Update("nonexistent", "x", "", "", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if se, ok := err.(*domain.StructuredError); !ok || se.Code != "ZONE_NOT_FOUND" {
		t.Errorf("expected ZONE_NOT_FOUND, got %v", err)
	}
}
